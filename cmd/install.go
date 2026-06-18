package cmd

import (
	"context"
	"fmt"
	"sort"

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
// it first registers that source as a marketplace (the native CLI derives the
// marketplace name from the source's manifest), then installs the bare plugin
// name, letting the native CLI resolve it across the configured marketplaces.
// A PLUGIN@MARKETPLACE selector is installed verbatim and registers nothing.
//
// Rollback (issue #1, "Failure and rollback") is driven by observing which
// marketplaces this call actually created: the marketplace set is snapshotted
// before and after AddMarketplace, and only the newly-created entries are
// removed if the install then fails. Because the names come from the native
// listing rather than a guess, a failed install can never delete a user's
// pre-existing marketplace, and when an agent cannot enumerate marketplaces
// nothing is rolled back.
func installOnAgent(ctx context.Context, ad adapter.Adapter, selector, regSource string, regLocal bool, cf commonFlags) error {
	var created []string
	if regSource != "" && !cf.dryRun {
		added, err := registerSourceMarketplace(ctx, ad, regSource, regLocal)
		if err != nil {
			return err
		}
		created = added
	}

	err := ad.InstallPlugin(ctx, adapter.InstallRequest{
		Plugin: selector,
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

// registerSourceMarketplace adds source as a marketplace and returns the names
// of marketplaces that this call newly created (for rollback). When the agent
// cannot enumerate marketplaces the created set is empty, so a later failure
// rolls back nothing.
func registerSourceMarketplace(ctx context.Context, ad adapter.Adapter, source string, local bool) ([]string, error) {
	before, beforeOK := marketplaceNameSet(ctx, ad)
	if err := ad.AddMarketplace(ctx, adapter.AddMarketplaceRequest{Source: source, Local: local}); err != nil {
		return nil, err
	}
	if !beforeOK {
		return nil, nil
	}
	after, afterOK := marketplaceNameSet(ctx, ad)
	if !afterOK {
		return nil, nil
	}
	var created []string
	for name := range after {
		if !before[name] {
			created = append(created, name)
		}
	}
	sort.Strings(created)
	return created, nil
}

// marketplaceNameSet returns the set of configured marketplace names for the
// agent. ok is false when the agent cannot enumerate marketplaces.
func marketplaceNameSet(ctx context.Context, ad adapter.Adapter) (set map[string]bool, ok bool) {
	markets, err := ad.ListMarketplaces(ctx)
	if err != nil {
		return nil, false
	}
	set = make(map[string]bool, len(markets))
	for _, m := range markets {
		set[m.Name] = true
	}
	return set, true
}

// installSelector renders the selector handed to the native CLI.
//   - PLUGIN@MARKETPLACE selector: used verbatim.
//   - GitHub or local source: the bare plugin name, resolved by the native CLI
//     against the marketplace registered from the source. The marketplace name
//     is never guessed from the repo/path.
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
