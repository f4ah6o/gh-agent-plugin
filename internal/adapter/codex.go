package adapter

import (
	"context"
	"regexp"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

// codexBin is the native Codex CLI binary name.
const codexBin = "codex"

// Codex is the adapter for Codex's native plugin manager. It delegates to
// `codex plugin ...`. Codex has no scope vocabulary and no enable/disable, so
// those operations are rejected as unsupported capabilities. Plugin IDs use the
// PLUGIN@MARKETPLACE form.
type Codex struct {
	Runner Runner
}

// NewCodex builds a Codex adapter using the given runner (defaults to exec).
func NewCodex(r Runner) *Codex {
	if r == nil {
		r = ExecRunner{}
	}
	return &Codex{Runner: r}
}

func (*Codex) ID() string { return "codex" }

func (c *Codex) Detect(ctx context.Context) (Detection, error) {
	path, err := c.Runner.Look(codexBin)
	if err != nil {
		return Detection{Installed: false}, nil
	}
	d := Detection{Installed: true, Path: path}
	if out, _, err := c.Runner.Run(ctx, codexBin, "--version"); err == nil {
		d.Version = strings.TrimSpace(string(out))
	}
	return d, nil
}

func (*Codex) Capabilities(ctx context.Context) (Capabilities, error) {
	return Capabilities{
		Scopes:            nil, // Codex has no scope vocabulary.
		EnableDisable:     false,
		MarketplaceUpdate: true,
		DependencyPrune:   false,
		JSONOutput:        false,
		LocalMarketplace:  true,
		GitMarketplace:    true,
	}, nil
}

// rejectScope rejects any non-empty scope, since Codex supports none.
func (c *Codex) rejectScope(s Scope) error {
	if s == "" {
		return nil
	}
	return exit.Errorf(exit.UnsupportedCapability, "agent %s does not support scope %q", c.ID(), s)
}

func (c *Codex) ListMarketplaces(ctx context.Context) ([]Marketplace, error) {
	// Codex exposes no machine-readable marketplace listing, so report the
	// limitation explicitly rather than returning an empty-but-successful list
	// (which callers could mistake for "no marketplaces configured"). Phase 2.
	return nil, exit.Errorf(exit.UnsupportedCapability,
		"agent %s cannot enumerate marketplaces (no machine-readable output; Phase 2)", c.ID())
}

func (c *Codex) AddMarketplace(ctx context.Context, req AddMarketplaceRequest) error {
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, codexBin, "plugin", "marketplace", "add", req.Source); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Codex) UpdateMarketplace(ctx context.Context, req UpdateMarketplaceRequest) error {
	// Codex spells marketplace update as "upgrade".
	args := []string{"plugin", "marketplace", "upgrade"}
	if req.Name != "" {
		args = append(args, req.Name)
	}
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, codexBin, args...); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Codex) RemoveMarketplace(ctx context.Context, req RemoveMarketplaceRequest) error {
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, codexBin, "plugin", "marketplace", "remove", req.Name); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

// codexTableRowRe matches a data row from `codex plugin list` table output.
// Columns: PLUGIN  STATUS  VERSION  PATH
// STATUS may contain a comma (e.g. "installed, enabled"), so we match it as
// two words separated by optional ", ".
var codexTableRowRe = regexp.MustCompile(
	`^(\S+)\s{2,}(not installed|installed(?:,\s*\w+)?)\s{2,}(\S*)\s{2,}(.+?)\s*$`,
)

func (c *Codex) ListPlugins(ctx context.Context, req ListRequest) ([]Plugin, error) {
	if err := c.rejectScope(req.Scope); err != nil {
		return nil, err
	}
	out, _, err := c.Runner.Run(ctx, codexBin, "plugin", "list")
	if err != nil {
		return nil, nativeErr(c.ID(), err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	var plugins []Plugin
	for _, line := range strings.Split(trimmed, "\n") {
		if line == "" || strings.HasPrefix(line, "Marketplace ") ||
			strings.HasPrefix(line, "PLUGIN") || strings.HasPrefix(line, "/") {
			continue
		}
		m := codexTableRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := m[1]
		status := strings.TrimSpace(m[2])
		version := m[3]

		name := id
		var marketplace string
		if at := strings.LastIndex(id, "@"); at > 0 {
			name = id[:at]
			marketplace = id[at+1:]
		}
		plugins = append(plugins, Plugin{
			Agent:       c.ID(),
			ID:          id,
			Name:        name,
			Marketplace: marketplace,
			Status:      status,
			Enabled:     strings.Contains(status, "enabled"),
			Version:     version,
			Source:      Source{Type: "marketplace"},
		})
	}
	if len(plugins) == 0 {
		return nil, exit.Errorf(exit.NativeCLIFailure, "agent %s returned output but no plugins could be parsed", c.ID())
	}
	return plugins, nil
}

func (c *Codex) InstallPlugin(ctx context.Context, req InstallRequest) error {
	if err := c.rejectScope(req.Scope); err != nil {
		return err
	}
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, codexBin, "plugin", "add", req.Plugin); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

// UpdatePlugin refreshes a plugin. Codex has no dedicated update verb; its
// `plugin add` re-fetches the plugin from its marketplace, which is Codex's
// update path.
func (c *Codex) UpdatePlugin(ctx context.Context, req UpdateRequest) error {
	if err := c.rejectScope(req.Scope); err != nil {
		return err
	}
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, codexBin, "plugin", "add", req.Plugin); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Codex) RemovePlugin(ctx context.Context, req RemoveRequest) error {
	if err := c.rejectScope(req.Scope); err != nil {
		return err
	}
	if req.KeepData || req.Prune {
		return exit.Errorf(exit.UnsupportedCapability, "agent %s does not support --keep-data/--prune", c.ID())
	}
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, codexBin, "plugin", "remove", req.Plugin); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Codex) EnablePlugin(ctx context.Context, req EnableRequest) error {
	return exit.Errorf(exit.UnsupportedCapability, "agent %s does not support enable/disable", c.ID())
}

func (c *Codex) DisablePlugin(ctx context.Context, req DisableRequest) error {
	return exit.Errorf(exit.UnsupportedCapability, "agent %s does not support enable/disable", c.ID())
}
