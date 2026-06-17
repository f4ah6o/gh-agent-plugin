package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

// doctorAgent is the per-agent diagnostic block.
type doctorAgent struct {
	Agent        string               `json:"agent"`
	Installed    bool                 `json:"installed"`
	Version      string               `json:"version,omitempty"`
	Path         string               `json:"path,omitempty"`
	Capabilities adapter.Capabilities `json:"capabilities"`
}

// doctorReport is the JSON shape emitted by `doctor --json`.
type doctorReport struct {
	GH        doctorBinary  `json:"gh"`
	CachePath string        `json:"cachePath"`
	CacheOK   bool          `json:"cacheReadable"`
	Agents    []doctorAgent `json:"agents"`
}

type doctorBinary struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
}

// runDoctor diagnoses the environment. It makes no changes by default; a future
// --fix flag will add repair (issue #1).
func runDoctor(args []string, env *Env) error {
	var cf commonFlags
	fs := newFlagSet("doctor", env)
	cf.register(fs)
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}

	rep := doctorReport{CachePath: cacheDir()}
	if _, err := os.Stat(rep.CachePath); err == nil {
		rep.CacheOK = true
	}
	if p, err := lookGH(); err == nil {
		rep.GH = doctorBinary{Installed: true, Path: p}
	}

	for _, da := range env.Reg.DetectAll(env.Ctx) {
		caps, _ := da.Adapter.Capabilities(env.Ctx)
		rep.Agents = append(rep.Agents, doctorAgent{
			Agent:        da.Adapter.ID(),
			Installed:    da.Detection.Installed,
			Version:      da.Detection.Version,
			Path:         da.Detection.Path,
			Capabilities: caps,
		})
	}

	if cf.jsonOut {
		return output.JSON(env.Stdout, rep)
	}

	w := env.Stdout
	fmt.Fprintf(w, "gh CLI:     %s\n", boolWord(rep.GH.Installed, rep.GH.Path))
	fmt.Fprintf(w, "cache:      %s (%s)\n", rep.CachePath, readableWord(rep.CacheOK))
	for _, a := range rep.Agents {
		fmt.Fprintf(w, "agent %s: %s\n", a.Agent, boolWord(a.Installed, a.Version))
		fmt.Fprintf(w, "  scopes=%v enable/disable=%v marketplace-update=%v json=%v\n",
			a.Capabilities.Scopes, a.Capabilities.EnableDisable, a.Capabilities.MarketplaceUpdate, a.Capabilities.JSONOutput)
	}
	return nil
}

// cacheDir returns the regenerable cache directory (issue #1, "Local state").
func cacheDir() string {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "gh-agent-plugin")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "gh-agent-plugin")
	}
	return filepath.Join(home, ".cache", "gh-agent-plugin")
}

func lookGH() (string, error) { return adapter.ExecRunner{}.Look("gh") }

func boolWord(installed bool, detail string) string {
	if !installed {
		return "not detected"
	}
	if detail != "" {
		return "detected (" + detail + ")"
	}
	return "detected"
}

func readableWord(ok bool) string {
	if ok {
		return "present"
	}
	return "absent, will be created on demand"
}
