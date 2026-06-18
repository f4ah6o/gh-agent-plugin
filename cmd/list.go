package cmd

import (
	"fmt"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

// runList lists installed plugins across the selected agents. With no agents
// detected it prints an empty, stable result and exits 0.
func runList(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("list", env)
	cf.register(fs)
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}

	// list defaults to all detected agents rather than requiring a selection.
	var adapters []adapter.Adapter
	if len(cf.agents) == 0 {
		adapters = env.Reg.Installed(env.Ctx)
	} else {
		sel, err := cf.selectAdapters(env)
		if err != nil {
			return err
		}
		adapters = sel
	}

	var plugins []adapter.Plugin
	for _, ad := range adapters {
		ps, err := ad.ListPlugins(env.Ctx, adapter.ListRequest{Scope: adapter.Scope(cf.scope)})
		if err != nil {
			// An agent that cannot enumerate plugins (Phase 1 Claude Code) is
			// noted on stderr so the user knows why it is absent, without
			// failing the whole command for the agents that can list.
			if exit.CodeOf(err) == exit.UnsupportedCapability {
				fmt.Fprintf(env.Stderr, "note: %s\n", err.Error())
				continue
			}
			return err
		}
		plugins = append(plugins, ps...)
	}
	if plugins == nil {
		plugins = []adapter.Plugin{}
	}

	if cf.jsonOut {
		return output.JSON(env.Stdout, map[string]any{"plugins": plugins})
	}

	header := []string{"AGENT", "PLUGIN", "STATUS", "VERSION", "SCOPE", "SOURCE"}
	rows := make([][]string, 0, len(plugins))
	for _, p := range plugins {
		status := p.Status
		if p.Enabled && status == "installed" {
			status = "installed, enabled"
		}
		rows = append(rows, []string{p.Agent, p.ID, status, p.Version, string(p.Scope), sourceLabel(p.Source)})
	}
	return output.Table(env.Stdout, header, rows)
}

// sourceLabel renders a Source for the table SOURCE column.
func sourceLabel(s adapter.Source) string {
	switch s.Type {
	case "github":
		if s.Repository != "" {
			return "github.com/" + s.Repository
		}
		return "github"
	default:
		return s.Type
	}
}
