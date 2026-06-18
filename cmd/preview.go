package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/discovery"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
	"github.com/f4ah6o/gh-agent-plugin/internal/source"
)

// previewResult is the JSON shape emitted by `preview --json`.
type previewResult struct {
	Plugin   discovery.DiscoveredPlugin `json:"plugin"`
	Source   map[string]string          `json:"source"`
	Findings []discovery.Finding        `json:"securityFindings"`
}

// runPreview statically inspects a plugin and prints its components and security
// findings. It never executes plugin code.
func runPreview(args []string, env *Env) error {
	var cf commonFlags
	var security bool
	fs := newFlagSet("preview", env)
	cf.register(fs)
	fs.BoolVar(&security, "security", false, "reserved: deeper security scan (PluginSpector, phase 2)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	_ = security // preview always reports static findings; --security is reserved.

	spec, err := source.Parse(pos, cf.ref, cf.fromLocal)
	if err != nil {
		return err
	}

	repoRoot, srcMeta, err := resolveRepoRoot(spec)
	if err != nil {
		return err
	}

	dp, err := discovery.DiscoverPlugin(repoRoot, spec.Plugin)
	if err != nil {
		return err
	}

	findings := discovery.Scan(dp)

	// Emit output in the requested format first, so consumers always see the
	// findings, then map blocking findings to a non-zero exit regardless of
	// output format.
	if cf.jsonOut {
		if err := output.JSON(env.Stdout, previewResult{Plugin: dp, Source: srcMeta, Findings: findings}); err != nil {
			return err
		}
	} else {
		printPreview(env, dp, srcMeta, findings)
	}

	if n := countBlocking(findings); n > 0 {
		return exit.Errorf(exit.ValidationFailed, "preview found %d blocking security issue(s)", n)
	}
	return nil
}

// resolveRepoRoot resolves a source spec to a local repository root for static
// discovery. Local sources are used as-is; GitHub/marketplace sources require
// network resolution which is deferred to the install path, so preview of those
// is reported as unsupported in this phase unless a local path is given.
func resolveRepoRoot(spec source.Spec) (string, map[string]string, error) {
	switch spec.Kind {
	case source.KindLocal:
		return spec.Path, map[string]string{"type": "local", "path": spec.Path}, nil
	case source.KindGitHub:
		// Phase 1 previews operate on a local checkout. A full implementation
		// would clone spec.Repository@spec.Ref into the cache first.
		return "", nil, exit.Errorf(exit.GeneralError,
			"previewing a GitHub source requires a local checkout in this phase; use --from-local with a cloned path")
	default:
		// PLUGIN@MARKETPLACE is valid syntax but previewing a configured
		// marketplace source is not supported yet (it has no local files to
		// inspect), so this is an unsupported capability rather than a usage
		// error.
		return "", nil, exit.Errorf(exit.UnsupportedCapability, "preview supports local or GitHub sources, not a marketplace selector")
	}
}

func printPreview(env *Env, dp discovery.DiscoveredPlugin, src map[string]string, findings []discovery.Finding) {
	w := env.Stdout
	fmt.Fprintf(w, "Plugin:           %s\n", dp.Name)
	fmt.Fprintf(w, "Root:             %s\n", dp.Root)
	fmt.Fprintf(w, "Supported agents: %s\n", join(dp.Agents))
	fmt.Fprintf(w, "Source:           %s %s\n", src["type"], src["path"])
	fmt.Fprintf(w, "Manifests:        %s\n", joinMap(dp.Manifests))
	fmt.Fprintf(w, "Skills:           %s\n", join(dp.Skills))
	fmt.Fprintf(w, "Commands:         %s\n", join(dp.Commands))
	fmt.Fprintf(w, "Agents config:    %s\n", join(dp.AgentsConfig))
	fmt.Fprintf(w, "Hooks:            %s\n", join(dp.Hooks))
	fmt.Fprintf(w, "MCP servers:      %s\n", join(dp.MCPServers))
	fmt.Fprintf(w, "Apps:             %s\n", join(dp.Apps))
	if len(findings) == 0 {
		fmt.Fprintln(w, "Security:         no findings")
		return
	}
	fmt.Fprintln(w, "Security findings:")
	for _, f := range findings {
		fmt.Fprintf(w, "  [%s] %s: %s (%s)\n", f.Severity, f.Rule, f.Detail, f.Path)
	}
}

func countBlocking(findings []discovery.Finding) int {
	n := 0
	for _, f := range findings {
		if f.Severity == discovery.SeverityBlocking {
			n++
		}
	}
	return n
}

func join(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

func joinMap(m map[string]string) string {
	if len(m) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
