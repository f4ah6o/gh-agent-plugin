package cmd

import (
	"fmt"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
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

	adapters, err := cf.selectAdapters(env)
	if err != nil {
		return err
	}

	selector := installSelector(spec)
	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		// Register the marketplace first when one is named and not yet present.
		if spec.Marketplace != "" {
			_ = ad.AddMarketplace(env.Ctx, adapter.AddMarketplaceRequest{Name: spec.Marketplace, Source: spec.Repository, DryRun: cf.dryRun})
		}
		err := ad.InstallPlugin(env.Ctx, adapter.InstallRequest{
			Plugin:      selector,
			Marketplace: spec.Marketplace,
			Scope:       adapter.Scope(cf.scope),
			Ref:         spec.Ref,
			DryRun:      cf.dryRun,
			Force:       cf.force,
			Yes:         cf.yes,
		})
		res := agentResult{Agent: ad.ID(), Action: "install", OK: err == nil}
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
	return finalize("install", results, errs)
}

// installSelector renders the selector handed to the native CLI for a source.
func installSelector(spec source.Spec) string {
	if spec.Marketplace != "" {
		return spec.Plugin + "@" + spec.Marketplace
	}
	return spec.Plugin
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
