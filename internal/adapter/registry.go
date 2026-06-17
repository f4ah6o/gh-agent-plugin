package adapter

import "context"

// Registry holds the known adapters keyed by their stable ID.
type Registry struct {
	adapters []Adapter
}

// NewRegistry builds a registry with the Claude Code and Codex adapters, all
// sharing the given runner. A nil runner uses the production ExecRunner.
func NewRegistry(r Runner) *Registry {
	if r == nil {
		r = ExecRunner{}
	}
	return &Registry{adapters: []Adapter{NewClaude(r), NewCodex(r)}}
}

// All returns every registered adapter.
func (reg *Registry) All() []Adapter { return reg.adapters }

// Get returns the adapter with the given ID, or nil if none matches.
func (reg *Registry) Get(id string) Adapter {
	for _, a := range reg.adapters {
		if a.ID() == id {
			return a
		}
	}
	return nil
}

// DetectedAgent pairs an adapter with its detection result.
type DetectedAgent struct {
	Adapter   Adapter
	Detection Detection
}

// DetectAll detects every registered adapter.
func (reg *Registry) DetectAll(ctx context.Context) []DetectedAgent {
	out := make([]DetectedAgent, 0, len(reg.adapters))
	for _, a := range reg.adapters {
		d, _ := a.Detect(ctx)
		out = append(out, DetectedAgent{Adapter: a, Detection: d})
	}
	return out
}

// Installed returns only the adapters whose native CLI was detected.
func (reg *Registry) Installed(ctx context.Context) []Adapter {
	var out []Adapter
	for _, da := range reg.DetectAll(ctx) {
		if da.Detection.Installed {
			out = append(out, da.Adapter)
		}
	}
	return out
}
