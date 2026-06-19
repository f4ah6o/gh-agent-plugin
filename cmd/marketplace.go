package cmd

import (
	"fmt"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

// runMarketplace dispatches the marketplace sub-subcommands.
func runMarketplace(args []string, env *Env) error {
	if len(args) == 0 {
		return exit.Errorf(exit.InvalidArguments, "marketplace requires a subcommand: add|list|update|remove")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "add":
		return marketplaceAdd(rest, env)
	case "list", "ls":
		return marketplaceList(rest, env)
	case "update", "upgrade":
		return marketplaceUpdate(rest, env)
	case "remove", "rm":
		return marketplaceRemove(rest, env)
	default:
		return exit.Errorf(exit.InvalidArguments, "unknown marketplace subcommand %q", sub)
	}
}

func marketplaceAdd(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("marketplace add", env)
	cf.register(fs)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()
	cf.warnReservedFlags(env)
	if len(rest) < 1 {
		return exit.Errorf(exit.InvalidArguments, "usage: marketplace add SOURCE (OWNER/REPO, URL, or local path)")
	}
	src := rest[0]
	adapters, err := cf.selectAdapters(env)
	if err != nil {
		return err
	}
	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		err := ad.AddMarketplace(env.Ctx, adapter.AddMarketplaceRequest{Source: src, Local: cf.fromLocal, DryRun: cf.dryRun})
		res := agentResult{Agent: ad.ID(), Action: "marketplace add", OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		errs = append(errs, err)
		results = append(results, res)
	}
	return reportMutations(env, cf.jsonOut, "marketplace add", results, errs)
}

func marketplaceList(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("marketplace list", env)
	cf.register(fs)
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()
	cf.warnReservedFlags(env)
	adapters := env.Reg.Installed(env.Ctx)
	if len(cf.agents) > 0 {
		sel, err := cf.selectAdapters(env)
		if err != nil {
			return err
		}
		adapters = sel
	}
	var markets []adapter.Marketplace
	for _, ad := range adapters {
		ms, err := ad.ListMarketplaces(env.Ctx)
		if err != nil {
			if exit.CodeOf(err) == exit.UnsupportedCapability {
				fmt.Fprintf(env.Stderr, "note: %s\n", err.Error())
				continue
			}
			return err
		}
		markets = append(markets, ms...)
	}
	if markets == nil {
		markets = []adapter.Marketplace{}
	}
	if cf.jsonOut {
		return output.JSON(env.Stdout, map[string]any{"marketplaces": markets})
	}
	header := []string{"AGENT", "MARKETPLACE", "TYPE", "URL"}
	rows := make([][]string, 0, len(markets))
	for _, m := range markets {
		rows = append(rows, []string{m.Agent, m.Name, m.Type, m.URL})
	}
	return output.Table(env.Stdout, header, rows)
}

func marketplaceUpdate(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("marketplace update", env)
	cf.register(fs)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()
	cf.warnReservedFlags(env)
	var name string
	if len(rest) > 0 {
		name = rest[0]
	}
	adapters, err := cf.selectAdapters(env)
	if err != nil {
		return err
	}
	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		err := ad.UpdateMarketplace(env.Ctx, adapter.UpdateMarketplaceRequest{Name: name, DryRun: cf.dryRun})
		res := agentResult{Agent: ad.ID(), Action: "marketplace update", OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		errs = append(errs, err)
		results = append(results, res)
	}
	return reportMutations(env, cf.jsonOut, "marketplace update", results, errs)
}

func marketplaceRemove(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("marketplace remove", env)
	cf.register(fs)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()
	cf.warnReservedFlags(env)
	if len(rest) == 0 {
		return exit.Errorf(exit.InvalidArguments, "usage: marketplace remove NAME")
	}
	adapters, err := cf.selectAdapters(env)
	if err != nil {
		return err
	}
	results := make([]agentResult, 0, len(adapters))
	errs := make([]error, 0, len(adapters))
	for _, ad := range adapters {
		err := ad.RemoveMarketplace(env.Ctx, adapter.RemoveMarketplaceRequest{Name: rest[0], DryRun: cf.dryRun})
		res := agentResult{Agent: ad.ID(), Action: "marketplace remove", OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		errs = append(errs, err)
		results = append(results, res)
	}
	return reportMutations(env, cf.jsonOut, "marketplace remove", results, errs)
}

// reportMutations prints per-agent results and maps them to an exit code.
func reportMutations(env *Env, jsonOut bool, action string, results []agentResult, errs []error) error {
	if jsonOut {
		if err := output.JSON(env.Stdout, map[string]any{"results": results}); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			status := "ok"
			if !r.OK {
				status = "failed: " + r.Error
			}
			fmt.Fprintf(env.Stdout, "%-12s %s %s\n", r.Agent, r.Action, status)
		}
	}
	return finalize(action, results, errs)
}
