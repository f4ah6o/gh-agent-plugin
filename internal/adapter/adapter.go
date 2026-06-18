// Package adapter defines the agent-neutral interface that the CLI uses to talk
// to each native plugin manager (Claude Code, Codex), plus the request/response
// value types. Concrete adapters delegate to the agent's native CLI; agent
// differences are surfaced explicitly through Capabilities rather than silently
// absorbed (issue #1, "Native adapter model").
package adapter

import "context"

// Scope is the shared plugin scope vocabulary. Agents that do not support a
// requested scope must return an unsupported-capability error rather than
// ignoring it.
type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeLocal   Scope = "local"
)

// Detection reports whether an agent's native CLI is present.
type Detection struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
}

// Capabilities describes what an agent's native plugin manager can do. Unset
// capabilities mean the operation must be rejected, not approximated.
type Capabilities struct {
	Scopes            []Scope `json:"scopes"`
	EnableDisable     bool    `json:"enableDisable"`
	MarketplaceUpdate bool    `json:"marketplaceUpdate"`
	DependencyPrune   bool    `json:"dependencyPrune"`
	JSONOutput        bool    `json:"jsonOutput"`
	LocalMarketplace  bool    `json:"localMarketplace"`
	GitMarketplace    bool    `json:"gitMarketplace"`
}

// SupportsScope reports whether s is offered by these capabilities.
func (c Capabilities) SupportsScope(s Scope) bool {
	for _, have := range c.Scopes {
		if have == s {
			return true
		}
	}
	return false
}

// Source records where an installed plugin came from.
type Source struct {
	Type       string `json:"type"` // github | local | marketplace
	Repository string `json:"repository,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Revision   string `json:"revision,omitempty"`
}

// Plugin is a single installed plugin as reported by an agent.
type Plugin struct {
	Agent       string `json:"agent"`
	ID          string `json:"id"` // PLUGIN@MARKETPLACE
	Name        string `json:"name"`
	Marketplace string `json:"marketplace,omitempty"`
	Status      string `json:"status"` // installed | ...
	Enabled     bool   `json:"enabled"`
	Version     string `json:"version,omitempty"`
	Scope       Scope  `json:"scope,omitempty"`
	Source      Source `json:"source"`
}

// Marketplace is a configured marketplace entry for an agent.
type Marketplace struct {
	Agent string `json:"agent"`
	Name  string `json:"name"`
	Type  string `json:"type,omitempty"` // github | local | git
	URL   string `json:"url,omitempty"`
}

// Request value types. Each carries the fields a native CLI invocation needs.

type ListRequest struct {
	Scope Scope
}

type InstallRequest struct {
	Plugin string // plugin name or PLUGIN@MARKETPLACE selector
	Scope  Scope
	Ref    string
	DryRun bool
	Force  bool
	Yes    bool
}

type RemoveRequest struct {
	Plugin   string
	Scope    Scope
	KeepData bool // Claude-specific
	Prune    bool // Claude-specific
	DryRun   bool
	Yes      bool
}

type EnableRequest struct {
	Plugin string
	Scope  Scope
}

type DisableRequest struct {
	Plugin string
	Scope  Scope
}

// UpdateRequest updates a single installed plugin to its latest version.
type UpdateRequest struct {
	Plugin string
	Scope  Scope
	DryRun bool
}

type AddMarketplaceRequest struct {
	Source string // OWNER/REPO, URL, or local path; the marketplace name is
	// declared by the source's manifest, so it is not supplied here.
	Local  bool
	DryRun bool
}

type UpdateMarketplaceRequest struct {
	Name   string // empty means all
	DryRun bool
}

type RemoveMarketplaceRequest struct {
	Name   string
	DryRun bool
}

// Adapter is the agent-neutral plugin manager interface (issue #1).
type Adapter interface {
	ID() string
	Detect(ctx context.Context) (Detection, error)
	Capabilities(ctx context.Context) (Capabilities, error)

	ListMarketplaces(ctx context.Context) ([]Marketplace, error)
	AddMarketplace(ctx context.Context, req AddMarketplaceRequest) error
	UpdateMarketplace(ctx context.Context, req UpdateMarketplaceRequest) error
	RemoveMarketplace(ctx context.Context, req RemoveMarketplaceRequest) error

	ListPlugins(ctx context.Context, req ListRequest) ([]Plugin, error)
	InstallPlugin(ctx context.Context, req InstallRequest) error
	UpdatePlugin(ctx context.Context, req UpdateRequest) error
	RemovePlugin(ctx context.Context, req RemoveRequest) error
	EnablePlugin(ctx context.Context, req EnableRequest) error
	DisablePlugin(ctx context.Context, req DisableRequest) error
}
