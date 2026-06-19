package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
	"github.com/f4ah6o/gh-agent-plugin/internal/source"
)

// agentResult records the outcome of a per-agent operation for partial-success
// reporting (issue #1).
type agentResult struct {
	Agent  string `json:"agent"`
	OK     bool   `json:"ok"`
	Action string `json:"action"`
	Error  string `json:"error,omitempty"`
}

// runInstall installs a plugin into each selected agent, delegating to the
// native CLI. Results are reported per agent; a partial success exits 7.
func runInstall(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("install", env)
	cf.register(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()
	cf.warnReservedFlags(env)

	spec, err := source.Parse(pos, cf.ref, cf.fromLocal)
	if err != nil {
		return err
	}

	// Ref pinning: for a GitHub source with --ref, resolve the pinned revision
	// via the cache and register that local checkout as a local marketplace.
	// This ensures the installed plugin comes from the exact ref the user
	// reviewed, not whatever the native CLI fetches at install time.
	if spec.Ref != "" && spec.Kind == source.KindGitHub {
		if env.Cache == nil {
			return exit.Errorf(exit.GeneralError,
				"source cache is unavailable; cannot pin --ref %s (cache directory could not be initialised)", spec.Ref)
		}
		localDir, _, err := env.Cache.Checkout(env.Ctx, spec.Repository, spec.Ref)
		if err != nil {
			return err
		}
		// Replace the GitHub source with a local path pointing to the pinned
		// checkout, so the rest of the install flow registers it as a local
		// marketplace and installs from that exact revision.
		pinned := source.Spec{
			Kind:   source.KindLocal,
			Plugin: spec.Plugin,
			Path:   localDir,
		}
		spec = pinned
	}

	adapters, err := cf.selectAdapters(env)
	if err != nil {
		return err
	}

	selector := installSelector(spec)
	regSource, regLocal := installRegistration(spec)
	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		if err := installOnAgent(env.Ctx, ad, selector, regSource, regLocal, cf); err != nil {
			results = append(results, agentResult{Agent: ad.ID(), Action: "install", OK: false, Error: err.Error()})
			errs = append(errs, err)
			continue
		}
		results = append(results, agentResult{Agent: ad.ID(), Action: "install", OK: true})
		errs = append(errs, nil)
	}

	if cf.jsonOut {
		if err := output.JSON(env.Stdout, map[string]any{"results": results}); err != nil {
			return err
		}
	} else {
		printResults(env, results)
	}
	return finalize("install", results, errs)
}

// installOnAgent installs one plugin on one agent. For a GitHub or local source
// it first registers that source as a marketplace, derives the marketplace name
// from the before/after listing diff, then installs PLUGIN@MARKETPLACE_NAME to
// avoid ambiguity when multiple marketplaces share a plugin name.
//
// For agents that cannot enumerate marketplaces (e.g. Codex), the bare plugin
// name is used as a fallback. For agents that support enumeration but the name
// cannot be determined, the install fails with an explicit error rather than
// risking resolution from the wrong marketplace.
//
// Rollback (issue #1) removes only the marketplaces this call newly created.
func installOnAgent(ctx context.Context, ad adapter.Adapter, selector, regSource string, regLocal bool, cf commonFlags) error {
	installPlugin := selector // pre-qualified for marketplace selectors; bare for GitHub/local
	var created []string
	if regSource != "" && !cf.dryRun {
		name, added, canEnum, err := registerSourceMarketplace(ctx, ad, regSource, regLocal)
		if err != nil {
			return err
		}
		created = added
		if name != "" {
			installPlugin = selector + "@" + name
		} else if canEnum {
			// Agent supports enumeration but the marketplace name is still unknown
			// (e.g. AddMarketplace didn't produce a new entry). Proceeding with a
			// bare name risks installing from the wrong marketplace.
			for _, n := range created {
				_ = ad.RemoveMarketplace(ctx, adapter.RemoveMarketplaceRequest{Name: n})
			}
			return exit.Errorf(exit.NativeCLIFailure,
				"could not determine the marketplace name for source %q after registration; "+
					"re-run using a PLUGIN@MARKETPLACE selector instead", regSource)
		}
		// !canEnum: agent cannot enumerate (e.g. Codex); proceed with bare name.
	}

	err := ad.InstallPlugin(ctx, adapter.InstallRequest{
		Plugin: installPlugin,
		Scope:  adapter.Scope(cf.scope),
		DryRun: cf.dryRun,
		Force:  cf.force,
		Yes:    cf.yes,
	})
	if err != nil {
		for _, name := range created {
			_ = ad.RemoveMarketplace(ctx, adapter.RemoveMarketplaceRequest{Name: name})
		}
	}
	return err
}

// registerSourceMarketplace adds source as a marketplace and returns:
//   - name: the marketplace name to use for a qualified PLUGIN@NAME selector,
//     empty when it cannot be determined;
//   - created: names of marketplaces newly created by this call (for rollback);
//   - canEnum: true when the agent supports marketplace enumeration; false for
//     agents (e.g. Codex) that reject listing with UnsupportedCapability.
//
// When the agent can enumerate, name is derived by diffing the marketplace set
// before and after AddMarketplace. If exactly one new entry appears, its name
// is returned. When the diff is empty (marketplace was already registered), URL
// matching against the after listing is attempted so re-installs still qualify.
func registerSourceMarketplace(ctx context.Context, ad adapter.Adapter, src string, local bool) (name string, created []string, canEnum bool, err error) {
	before, beforeCanEnum, err := snapshotMarketplaces(ctx, ad)
	if err != nil {
		return "", nil, true, err
	}
	if addErr := ad.AddMarketplace(ctx, adapter.AddMarketplaceRequest{Source: src, Local: local}); addErr != nil {
		return "", nil, beforeCanEnum, addErr
	}
	if !beforeCanEnum {
		return "", nil, false, nil
	}

	after, afterCanEnum, err := snapshotMarketplaces(ctx, ad)
	if err != nil {
		return "", nil, true, err
	}
	if !afterCanEnum {
		// Should not happen (before succeeded), but propagate as a hard failure
		// rather than falling back to bare-name resolution.
		return "", nil, true, exit.Errorf(exit.NativeCLIFailure,
			"marketplace listing succeeded before AddMarketplace but failed after; cannot determine marketplace name")
	}

	beforeSet := toMarketplaceNameSet(before)
	for _, m := range after {
		if !beforeSet[m.Name] {
			created = append(created, m.Name)
		}
	}
	sort.Strings(created)

	if len(created) == 1 {
		return created[0], created, true, nil
	}

	// 0 or multiple newly created: try URL/source matching against the after
	// listing so a pre-existing or multi-added marketplace can still be found.
	return marketplaceMatchSource(after, src), created, true, nil
}

// snapshotMarketplaces lists the agent's configured marketplaces.
// canEnum is false only when the agent explicitly does not support enumeration
// (UnsupportedCapability). Any other error — network failure, parse error,
// context cancellation — is returned so the caller can abort rather than
// silently falling back to bare-name resolution and reintroducing the
// marketplace-collision risk.
func snapshotMarketplaces(ctx context.Context, ad adapter.Adapter) (markets []adapter.Marketplace, canEnum bool, err error) {
	m, e := ad.ListMarketplaces(ctx)
	if e != nil {
		if exit.CodeOf(e) == exit.UnsupportedCapability {
			return nil, false, nil
		}
		return nil, true, e
	}
	return m, true, nil
}

// toMarketplaceNameSet builds a set of marketplace names for O(1) lookup.
func toMarketplaceNameSet(markets []adapter.Marketplace) map[string]bool {
	s := make(map[string]bool, len(markets))
	for _, m := range markets {
		s[m.Name] = true
	}
	return s
}

// marketplaceMatchSource finds the first marketplace in the list whose URL
// matches src (an OWNER/REPO slug or a local path). It tolerates full GitHub
// HTTPS URLs as well as bare owner/repo slugs returned by different agent CLIs.
func marketplaceMatchSource(markets []adapter.Marketplace, src string) string {
	for _, m := range markets {
		u := m.URL
		if u == src ||
			strings.HasSuffix(u, "/"+src) ||
			strings.HasSuffix(u, "/"+src+".git") {
			return m.Name
		}
	}
	return ""
}

// installSelector renders the selector handed to the native CLI.
//   - PLUGIN@MARKETPLACE selector: used verbatim.
//   - GitHub or local source: the bare plugin name; installOnAgent qualifies
//     it with the discovered marketplace name before passing to native CLI.
func installSelector(spec source.Spec) string {
	if spec.Kind == source.KindMarketplace {
		return spec.Plugin + "@" + spec.Marketplace
	}
	return spec.Plugin
}

// installRegistration returns the marketplace source to register for this
// install (and whether it is a local path), or ("", false) when none is needed.
// GitHub and local sources are registered; a PLUGIN@MARKETPLACE selector assumes
// the marketplace already exists.
func installRegistration(spec source.Spec) (regSource string, local bool) {
	switch spec.Kind {
	case source.KindGitHub:
		return spec.Repository, false
	case source.KindLocal:
		return spec.Path, true
	default:
		return "", false
	}
}

func printResults(env *Env, results []agentResult) {
	for _, r := range results {
		status := "ok"
		if !r.OK {
			status = "failed: " + r.Error
		}
		fmt.Fprintf(env.Stdout, "%-12s %s %s\n", r.Agent, r.Action, status)
	}
}
