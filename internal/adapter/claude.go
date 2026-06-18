package adapter

import (
	"context"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

// claudeBin is the native Claude Code CLI binary name.
const claudeBin = "claude"

// Claude is the adapter for Claude Code's native plugin manager. It delegates to
// `claude plugin ...` and supports user/project/local scopes plus enable/disable.
type Claude struct {
	Runner Runner
}

// NewClaude builds a Claude adapter using the given runner (defaults to exec).
func NewClaude(r Runner) *Claude {
	if r == nil {
		r = ExecRunner{}
	}
	return &Claude{Runner: r}
}

func (*Claude) ID() string { return "claude-code" }

func (c *Claude) Detect(ctx context.Context) (Detection, error) {
	path, err := c.Runner.Look(claudeBin)
	if err != nil {
		return Detection{Installed: false}, nil
	}
	d := Detection{Installed: true, Path: path}
	if out, _, err := c.Runner.Run(ctx, claudeBin, "--version"); err == nil {
		d.Version = strings.TrimSpace(string(out))
	}
	return d, nil
}

func (*Claude) Capabilities(ctx context.Context) (Capabilities, error) {
	return Capabilities{
		Scopes:            []Scope{ScopeUser, ScopeProject, ScopeLocal},
		EnableDisable:     true,
		MarketplaceUpdate: true,
		DependencyPrune:   true,
		JSONOutput:        false,
		LocalMarketplace:  true,
		GitMarketplace:    true,
	}, nil
}

// requireScope rejects an unsupported scope with exit code 4.
func (c *Claude) requireScope(ctx context.Context, s Scope) error {
	if s == "" {
		return nil
	}
	caps, _ := c.Capabilities(ctx)
	if !caps.SupportsScope(s) {
		return exit.Errorf(exit.UnsupportedCapability, "agent %s does not support scope %q", c.ID(), s)
	}
	return nil
}

func scopeArgs(s Scope) []string {
	if s == "" {
		return nil
	}
	return []string{"--scope", string(s)}
}

func (c *Claude) ListMarketplaces(ctx context.Context) ([]Marketplace, error) {
	// Claude Code has no machine-readable marketplace listing, so rather than
	// returning a misleading empty-but-successful result we surface the
	// limitation explicitly. Structured parsing is tracked for Phase 2.
	return nil, exit.Errorf(exit.UnsupportedCapability,
		"agent %s cannot enumerate marketplaces (no machine-readable output; Phase 2)", c.ID())
}

func (c *Claude) AddMarketplace(ctx context.Context, req AddMarketplaceRequest) error {
	args := []string{"plugin", "marketplace", "add", req.Source}
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, claudeBin, args...); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Claude) UpdateMarketplace(ctx context.Context, req UpdateMarketplaceRequest) error {
	args := []string{"plugin", "marketplace", "update"}
	if req.Name != "" {
		args = append(args, req.Name)
	}
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, claudeBin, args...); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Claude) RemoveMarketplace(ctx context.Context, req RemoveMarketplaceRequest) error {
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, claudeBin, "plugin", "marketplace", "remove", req.Name); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Claude) ListPlugins(ctx context.Context, req ListRequest) ([]Plugin, error) {
	if err := c.requireScope(ctx, req.Scope); err != nil {
		return nil, err
	}
	// Claude Code's `plugin list` has no machine-readable form in Phase 1.
	// Returning an explicit unsupported error keeps a core command honest
	// instead of silently reporting an empty plugin set. (Phase 2 hardening.)
	return nil, exit.Errorf(exit.UnsupportedCapability,
		"agent %s cannot enumerate plugins (no machine-readable output; Phase 2)", c.ID())
}

func (c *Claude) InstallPlugin(ctx context.Context, req InstallRequest) error {
	if err := c.requireScope(ctx, req.Scope); err != nil {
		return err
	}
	args := append([]string{"plugin", "install", req.Plugin}, scopeArgs(req.Scope)...)
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, claudeBin, args...); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Claude) RemovePlugin(ctx context.Context, req RemoveRequest) error {
	if err := c.requireScope(ctx, req.Scope); err != nil {
		return err
	}
	args := append([]string{"plugin", "uninstall", req.Plugin}, scopeArgs(req.Scope)...)
	if req.KeepData {
		args = append(args, "--keep-data")
	}
	if req.Prune {
		args = append(args, "--prune")
	}
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, claudeBin, args...); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Claude) EnablePlugin(ctx context.Context, req EnableRequest) error {
	if err := c.requireScope(ctx, req.Scope); err != nil {
		return err
	}
	args := append([]string{"plugin", "enable", req.Plugin}, scopeArgs(req.Scope)...)
	if _, _, err := c.Runner.Run(ctx, claudeBin, args...); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

func (c *Claude) DisablePlugin(ctx context.Context, req DisableRequest) error {
	if err := c.requireScope(ctx, req.Scope); err != nil {
		return err
	}
	args := append([]string{"plugin", "disable", req.Plugin}, scopeArgs(req.Scope)...)
	if _, _, err := c.Runner.Run(ctx, claudeBin, args...); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
}

// nativeErr wraps a native CLI failure with exit code 8.
func nativeErr(agent string, err error) error {
	return exit.Errorf(exit.NativeCLIFailure, "agent %s native CLI failed: %v", agent, err)
}
