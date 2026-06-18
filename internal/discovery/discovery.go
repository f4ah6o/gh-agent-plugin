// Package discovery inspects a repository checkout and reports the plugins it
// contains, supporting Claude Code, Codex, and dual-target layouts (issue #1,
// "Repository layout discovery"). It performs no code execution.
package discovery

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

// Manifest relative paths within a plugin root.
const (
	claudeManifest = ".claude-plugin/plugin.json"
	codexManifest  = ".codex-plugin/plugin.json"
)

// Marketplace manifest relative paths within a repository root.
const (
	ClaudeMarketplace = ".claude-plugin/marketplace.json"
	CodexMarketplace  = ".agents/plugins/marketplace.json"
)

// DiscoveredPlugin describes a plugin found in a repository (issue #1).
type DiscoveredPlugin struct {
	Name         string            `json:"name"`
	Root         string            `json:"root"`
	Agents       []string          `json:"agents"`
	Manifests    map[string]string `json:"manifests"`
	Skills       []string          `json:"skills"`
	MCPServers   []string          `json:"mcpServers"`
	Hooks        []string          `json:"hooks"`
	Commands     []string          `json:"commands"`
	AgentsConfig []string          `json:"agentsConfig"`
	Apps         []string          `json:"apps"`
}

// pluginRoot resolves the directory that holds a named plugin. It prefers
// <repoRoot>/plugins/<name> and falls back to repoRoot itself.
func pluginRoot(repoRoot, name string) string {
	candidate := filepath.Join(repoRoot, "plugins", name)
	if isDir(candidate) {
		return candidate
	}
	return repoRoot
}

// DiscoverPlugin inspects a single named plugin within repoRoot.
func DiscoverPlugin(repoRoot, name string) (DiscoveredPlugin, error) {
	root := pluginRoot(repoRoot, name)
	if !isDir(root) {
		return DiscoveredPlugin{}, exit.Errorf(exit.NotFound, "plugin %q not found under %s", name, repoRoot)
	}

	dp := DiscoveredPlugin{
		Name:      name,
		Root:      root,
		Manifests: map[string]string{},
	}

	if p := filepath.Join(root, claudeManifest); fileExists(p) {
		dp.Manifests["claude-code"] = p
		dp.Agents = append(dp.Agents, "claude-code")
	}
	if p := filepath.Join(root, codexManifest); fileExists(p) {
		dp.Manifests["codex"] = p
		dp.Agents = append(dp.Agents, "codex")
	}
	if len(dp.Agents) == 0 {
		return DiscoveredPlugin{}, exit.Errorf(exit.ValidationFailed, "no plugin manifest found in %s", root)
	}

	dp.Skills = listSkills(root)
	dp.Commands = listDirEntries(filepath.Join(root, "commands"))
	dp.AgentsConfig = listDirEntries(filepath.Join(root, "agents"))
	dp.Hooks = listDirEntries(filepath.Join(root, "hooks"))
	if fileExists(filepath.Join(root, ".mcp.json")) {
		dp.MCPServers = mcpServerNames(filepath.Join(root, ".mcp.json"))
	}
	if fileExists(filepath.Join(root, ".app.json")) {
		dp.Apps = []string{".app.json"}
	}
	return dp, nil
}

// listSkills returns the skill directory names that contain a SKILL.md file.
func listSkills(root string) []string {
	skillsDir := filepath.Join(root, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && fileExists(filepath.Join(skillsDir, e.Name(), "SKILL.md")) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// listDirEntries returns the names of files/dirs directly under dir.
func listDirEntries(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
