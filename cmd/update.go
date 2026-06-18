package cmd

import (
	"context"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

// runUpdate updates plugins. With no plugin selector and --all it updates every
// plugin; otherwise it updates the named PLUGIN@MARKETPLACE. Marketplace updates
// are handled by `marketplace update`.
func runUpdate(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("update", env)
	cf.register(fs)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 && !cf.all {
		return exit.Errorf(exit.InvalidArguments, "specify a plugin selector or --all")
	}
	var selector string
	if len(rest) > 0 {
		selector = rest[0]
	}

	adapters, err := cf.selectAdapters(env)
	if err != nil {
		return err
	}

	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		var err error
		if selector == "" {
			// "all plugins": enumerate the agent's installed plugins and refresh
			// each. This keeps "all agents" (--agent all) distinct from "all
			// plugins" and never issues an empty selector to the native CLI.
			err = updateAllPlugins(env.Ctx, ad, cf)
		} else {
			// A specific plugin update is modeled as a forced reinstall of the
			// same selector via the native CLI.
			err = ad.InstallPlugin(env.Ctx, adapter.InstallRequest{
				Plugin: selector,
				Scope:  adapter.Scope(cf.scope),
				DryRun: cf.dryRun,
				Force:  true,
			})
		}
		res := agentResult{Agent: ad.ID(), Action: "update", OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		errs = append(errs, err)
		results = append(results, res)
	}

	if cf.jsonOut {
		if err := output.JSON(env.Stdout, map[string]any{"results": results}); err != nil {
			return err
		}
	} else {
		printResults(env, results)
	}
	return finalize("update", results, errs)
}

// updateAllPlugins refreshes every plugin the agent reports as installed. It
// relies on ListPlugins to enumerate them, so for an agent that cannot
// enumerate plugins (e.g. Claude Code in Phase 1) it surfaces that adapter's
// unsupported-capability error rather than guessing.
func updateAllPlugins(ctx context.Context, ad adapter.Adapter, cf commonFlags) error {
	plugins, err := ad.ListPlugins(ctx, adapter.ListRequest{Scope: adapter.Scope(cf.scope)})
	if err != nil {
		return err
	}
	for _, p := range plugins {
		if err := ad.InstallPlugin(ctx, adapter.InstallRequest{
			Plugin: p.ID,
			Scope:  adapter.Scope(cf.scope),
			DryRun: cf.dryRun,
			Force:  true,
		}); err != nil {
			return err
		}
	}
	return nil
}
