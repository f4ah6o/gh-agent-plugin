package cmd

import (
	"context"
	"fmt"
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

	spec, err := source.Parse(pos, cf.ref, cf.fromLocal)
	if err != nil {
		return err
	}
	// Pinning a GitHub source to a ref requires resolving and registering that
	// revision, which depends on the Phase 2 clone/cache path. Rather than
	// accept --ref and silently install the default branch, reject it.
	if spec.Ref != "" {
		return exit.Errorf(exit.UnsupportedCapability,
			"--ref pinning is not yet supported (Phase 2, issue #4); omit --ref to install the default revision")
	}

	adapters, err := cf.selectAdapters(env)
	if err != nil {
		return err
	}

	selector := installSelector(spec)
	regName, regSource := installRegistration(spec)
	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		if err := installOnAgent(env.Ctx, ad, spec, selector, regName, regSource, cf); err != nil {
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

// installOnAgent performs the native install for one agent. For a GitHub source
// it first registers the repository as a marketplace; if (and only if) we can
// confirm that marketplace did not already exist and the install then fails, the
// registration is rolled back so a failed install leaves no orphaned, unused
// marketplace (issue #1, "Failure and rollback").
//
// The rollback is deliberately conservative: it never removes a marketplace
// whose prior existence we cannot confirm, so a failed install can never delete
// a user's pre-existing marketplace. Confident creation-detection depends on
// native marketplace listing, which lands in Phase 2; until then rollback only
// triggers for adapters that can enumerate marketplaces.
func installOnAgent(ctx context.Context, ad adapter.Adapter, spec source.Spec, selector, regName, regSource string, cf commonFlags) error {
	rollback := false
	if regName != "" && !cf.dryRun {
		existed, known := marketplaceConfigured(ctx, ad, regName)
		if err := ad.AddMarketplace(ctx, adapter.AddMarketplaceRequest{Name: regName, Source: regSource}); err != nil {
			return err
		}
		rollback = known && !existed
	}

	err := ad.InstallPlugin(ctx, adapter.InstallRequest{
		Plugin: selector,
		Scope:  adapter.Scope(cf.scope),
		Ref:    spec.Ref,
		DryRun: cf.dryRun,
		Force:  cf.force,
		Yes:    cf.yes,
	})
	if err != nil && rollback {
		_ = ad.RemoveMarketplace(ctx, adapter.RemoveMarketplaceRequest{Name: regName})
	}
	return err
}

// marketplaceConfigured reports whether a marketplace named name is already
// configured for the agent. The second return value is false when the adapter
// cannot enumerate marketplaces, in which case the existence is unknown and
// callers must not assume the marketplace is absent.
func marketplaceConfigured(ctx context.Context, ad adapter.Adapter, name string) (exists, known bool) {
	markets, err := ad.ListMarketplaces(ctx)
	if err != nil {
		return false, false
	}
	for _, m := range markets {
		if m.Name == name {
			return true, true
		}
	}
	return false, true
}

// installSelector renders the selector handed to the native CLI for a source.
//   - PLUGIN@MARKETPLACE selector: used verbatim.
//   - GitHub OWNER/REPO: PLUGIN@<repo-derived marketplace>.
//   - local: the bare plugin name.
func installSelector(spec source.Spec) string {
	switch spec.Kind {
	case source.KindMarketplace:
		return spec.Plugin + "@" + spec.Marketplace
	case source.KindGitHub:
		return spec.Plugin + "@" + deriveMarketplace(spec.Repository)
	default:
		return spec.Plugin
	}
}

// installRegistration returns the marketplace (name, source) that must be
// registered for this install, or ("","") when none is needed. Only GitHub
// sources introduce a new marketplace; a PLUGIN@MARKETPLACE selector assumes the
// marketplace is already configured.
func installRegistration(spec source.Spec) (name, repoSource string) {
	if spec.Kind == source.KindGitHub {
		return deriveMarketplace(spec.Repository), spec.Repository
	}
	return "", ""
}

// deriveMarketplace derives a marketplace name from an OWNER/REPO slug (the repo
// segment).
func deriveMarketplace(repository string) string {
	if i := strings.LastIndex(repository, "/"); i >= 0 {
		return repository[i+1:]
	}
	return repository
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
