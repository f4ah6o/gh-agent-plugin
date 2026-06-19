package cmd

import (
	"context"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

// runUpdate updates plugins via each agent's native update command. With no
// plugin selector, --all updates every installed plugin; otherwise it updates
// the named PLUGIN@MARKETPLACE. Marketplace updates are handled by
// `marketplace update`.
//
// Here --all means "all plugins"; agent fan-out is controlled separately by
// --agent (and --agent all). With no --agent the command targets every detected
// agent, like list.
func runUpdate(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("update", env)
	cf.register(fs)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()
	cf.warnReservedFlags(env)
	if len(rest) == 0 && !cf.all {
		return exit.Errorf(exit.InvalidArguments, "specify a plugin selector or --all")
	}
	var selector string
	if len(rest) > 0 {
		selector = rest[0]
	}

	adapters, err := cf.selectTargets(env)
	if err != nil {
		return err
	}

	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		var err error
		if selector == "" {
			// "all plugins": enumerate the agent's installed plugins and update
			// each via the native update command.
			err = updateAllPlugins(env.Ctx, ad, cf)
		} else {
			err = ad.UpdatePlugin(env.Ctx, adapter.UpdateRequest{
				Plugin: selector,
				Scope:  adapter.Scope(cf.scope),
				DryRun: cf.dryRun,
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

// updateAllPlugins updates every plugin the agent reports as installed, using
// the native per-plugin update command. It relies on ListPlugins to enumerate
// them, so an agent that cannot enumerate plugins surfaces that adapter's error
// rather than guessing.
func updateAllPlugins(ctx context.Context, ad adapter.Adapter, cf commonFlags) error {
	plugins, err := ad.ListPlugins(ctx, adapter.ListRequest{Scope: adapter.Scope(cf.scope)})
	if err != nil {
		return err
	}
	for _, p := range plugins {
		if err := ad.UpdatePlugin(ctx, adapter.UpdateRequest{
			Plugin: p.ID,
			Scope:  adapter.Scope(cf.scope),
			DryRun: cf.dryRun,
		}); err != nil {
			return err
		}
	}
	return nil
}
