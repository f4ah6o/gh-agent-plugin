package adapter

import (
	"context"
	"encoding/json"
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
		JSONOutput:        true,
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

// claudeMarketplaceJSON is a tolerant view of an entry from
// `claude plugin marketplace list --json`. Unknown fields are ignored.
type claudeMarketplaceJSON struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	URL    string `json:"url"`
	Type   string `json:"type"`
}

func (c *Claude) ListMarketplaces(ctx context.Context) ([]Marketplace, error) {
	out, _, err := c.Runner.Run(ctx, claudeBin, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return nil, nativeErr(c.ID(), err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var raw []claudeMarketplaceJSON
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, exit.Errorf(exit.NativeCLIFailure, "agent %s returned unparseable marketplace JSON: %v", c.ID(), err)
	}
	markets := make([]Marketplace, 0, len(raw))
	for _, m := range raw {
		url := m.URL
		if url == "" {
			url = m.Source
		}
		markets = append(markets, Marketplace{Agent: c.ID(), Name: m.Name, Type: m.Type, URL: url})
	}
	return markets, nil
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

// claudePluginJSON is a tolerant view of an entry from
// `claude plugin list --json`. Unknown fields are ignored.
type claudePluginJSON struct {
	RawID       string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Marketplace string `json:"marketplace"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
	Enabled     *bool  `json:"enabled"`
	Source      struct {
		Type       string `json:"type"`
		Repository string `json:"repository"`
		Repo       string `json:"repo"`
		Ref        string `json:"ref"`
		Revision   string `json:"revision"`
	} `json:"source"`
}

func (c *Claude) ListPlugins(ctx context.Context, req ListRequest) ([]Plugin, error) {
	if err := c.requireScope(ctx, req.Scope); err != nil {
		return nil, err
	}
	// `claude plugin list` has no --scope filter, so scope (already validated
	// above) is not forwarded to the native argv.
	out, _, err := c.Runner.Run(ctx, claudeBin, "plugin", "list", "--json")
	if err != nil {
		return nil, nativeErr(c.ID(), err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}
	var raw []claudePluginJSON
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, exit.Errorf(exit.NativeCLIFailure, "agent %s returned unparseable plugin JSON: %v", c.ID(), err)
	}
	plugins := make([]Plugin, 0, len(raw))
	for _, p := range raw {
		if p.Name == "" && p.RawID != "" {
			if at := strings.IndexByte(p.RawID, '@'); at >= 0 {
				p.Name = p.RawID[:at]
				if p.Marketplace == "" {
					p.Marketplace = p.RawID[at+1:]
				}
			} else {
				p.Name = p.RawID
			}
		}
		id := p.Name
		if p.Marketplace != "" {
			id = p.Name + "@" + p.Marketplace
		}
		status := p.Status
		if status == "" {
			status = "installed"
		}
		enabled := true
		if p.Enabled != nil {
			enabled = *p.Enabled
		}
		repo := p.Source.Repository
		if repo == "" {
			repo = p.Source.Repo
		}
		plugins = append(plugins, Plugin{
			Agent:       c.ID(),
			ID:          id,
			Name:        p.Name,
			Marketplace: p.Marketplace,
			Status:      status,
			Enabled:     enabled,
			Version:     p.Version,
			Scope:       Scope(p.Scope),
			Source:      Source{Type: p.Source.Type, Repository: repo, Ref: p.Source.Ref, Revision: p.Source.Revision},
		})
	}
	return plugins, nil
}

func (c *Claude) UpdatePlugin(ctx context.Context, req UpdateRequest) error {
	if err := c.requireScope(ctx, req.Scope); err != nil {
		return err
	}
	args := append([]string{"plugin", "update", req.Plugin}, scopeArgs(req.Scope)...)
	if req.DryRun {
		return nil
	}
	if _, _, err := c.Runner.Run(ctx, claudeBin, args...); err != nil {
		return nativeErr(c.ID(), err)
	}
	return nil
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
