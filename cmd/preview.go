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
	var noCache bool
	fs := newFlagSet("preview", env)
	cf.register(fs)
	fs.BoolVar(&security, "security", false, "reserved: deeper security scan (PluginSpector, phase 2)")
	fs.BoolVar(&noCache, "no-cache", false, "discard any cached checkout and re-clone before preview")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()
	cf.warnReservedFlags(env)
	_ = security // preview always reports static findings; --security is reserved.

	spec, err := source.Parse(pos, cf.ref, cf.fromLocal)
	if err != nil {
		return err
	}

	repoRoot, srcMeta, err := resolveRepoRoot(env, spec, noCache)
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
// discovery. Local sources are used as-is; a GitHub source is cloned into the
// regenerable cache (issue #4, Phase 2) and the resolved revision is recorded in
// the returned metadata. When noCache is true the cached checkout is discarded
// and a fresh clone is performed. A marketplace selector has no local files to
// inspect, so previewing it remains unsupported.
func resolveRepoRoot(env *Env, spec source.Spec, noCache bool) (string, map[string]string, error) {
	switch spec.Kind {
	case source.KindLocal:
		return spec.Path, map[string]string{"type": "local", "path": spec.Path}, nil
	case source.KindGitHub:
		if env.Cache == nil {
			return "", nil, exit.Errorf(exit.GeneralError, "source cache is unavailable; cannot resolve a GitHub source")
		}
		if noCache {
			if err := env.Cache.InvalidateCheckout(spec.Repository, spec.Ref); err != nil {
				return "", nil, err
			}
		}
		dir, revision, err := env.Cache.Checkout(env.Ctx, spec.Repository, spec.Ref)
		if err != nil {
			return "", nil, err
		}
		meta := map[string]string{
			"type":       "github",
			"repository": spec.Repository,
			"path":       dir,
			"revision":   revision,
		}
		if spec.Ref != "" {
			meta["ref"] = spec.Ref
		}
		return dir, meta, nil
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
	fmt.Fprintf(w, "Source:           %s\n", sourceLine(src))
	if p := src["path"]; p != "" && src["type"] == "github" {
		fmt.Fprintf(w, "Cache path:       %s\n", p)
	}
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

// sourceLine renders the human-readable source description from preview
// metadata, including the resolved revision for a GitHub source.
func sourceLine(src map[string]string) string {
	switch src["type"] {
	case "github":
		s := "github " + src["repository"]
		if ref := src["ref"]; ref != "" {
			s += "@" + ref
		}
		if rev := src["revision"]; rev != "" {
			s += " (" + shortRev(rev) + ")"
		}
		return s
	case "local":
		return "local " + src["path"]
	default:
		return src["type"] + " " + src["path"]
	}
}

// shortRev abbreviates a commit SHA for display, leaving non-SHA values intact.
func shortRev(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
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
