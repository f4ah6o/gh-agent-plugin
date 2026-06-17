package cmd

import (
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
		// Update is modeled as a reinstall of the same selector via the native
		// CLI; the adapter keeps the agent-specific behavior internal.
		err := ad.InstallPlugin(env.Ctx, adapter.InstallRequest{
			Plugin: selector,
			Scope:  adapter.Scope(cf.scope),
			DryRun: cf.dryRun,
			Force:  true,
		})
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
