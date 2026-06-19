package cmd

import (
	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

// runRemove removes a plugin from each selected agent. The plugin selector must
// identify the marketplace unambiguously (PLUGIN@MARKETPLACE) when required.
func runRemove(args []string, env *Env) error {
	var cf commonFlags
	var keepData, prune bool
	fs := newFlagSet("remove", env)
	cf.register(fs)
	fs.BoolVar(&keepData, "keep-data", false, "keep plugin data (Claude Code only)")
	fs.BoolVar(&prune, "prune", false, "prune dependencies (Claude Code only)")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()
	cf.warnReservedFlags(env)
	if len(rest) == 0 {
		return exit.Errorf(exit.InvalidArguments, "a plugin selector is required (PLUGIN@MARKETPLACE)")
	}
	selector := rest[0]

	adapters, err := cf.selectAdapters(env)
	if err != nil {
		return err
	}

	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		err := ad.RemovePlugin(env.Ctx, adapter.RemoveRequest{
			Plugin:   selector,
			Scope:    adapter.Scope(cf.scope),
			KeepData: keepData,
			Prune:    prune,
			DryRun:   cf.dryRun,
			Yes:      cf.yes,
		})
		res := agentResult{Agent: ad.ID(), Action: "remove", OK: err == nil}
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
	return finalize("remove", results, errs)
}
