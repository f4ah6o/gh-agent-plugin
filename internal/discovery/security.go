package discovery

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Severity classifies a security finding.
type Severity string

const (
	SeverityWarn  Severity = "warning"
	SeverityError Severity = "error"
)

// Finding is a single static security observation about a plugin.
type Finding struct {
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	Detail   string   `json:"detail"`
	Path     string   `json:"path,omitempty"`
}

// shellHookExts are file extensions treated as executable/shell hook scripts.
var shellHookExts = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".py": true, ".rb": true,
	".pl": true, ".js": true, ".ts": true,
}

// credentialHints are substrings in an env var name that suggest a secret.
var credentialHints = []string{"TOKEN", "SECRET", "KEY", "PASSWORD", "CREDENTIAL", "API_KEY"}

// Scan runs the static security checks for a discovered plugin (issue #1,
// "Security checks"). It never executes plugin code.
func Scan(dp DiscoveredPlugin) []Finding {
	var findings []Finding
	root := dp.Root

	findings = append(findings, scanSymlinksAndScripts(root)...)
	findings = append(findings, scanMCP(root)...)
	findings = append(findings, scanHooks(root)...)
	findings = append(findings, scanSkillDrift(dp)...)

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Rule != findings[j].Rule {
			return findings[i].Rule < findings[j].Rule
		}
		return findings[i].Path < findings[j].Path
	})
	return findings
}

// scanSymlinksAndScripts walks the plugin root for symlinks escaping the root,
// path traversal, and executable script files.
func scanSymlinksAndScripts(root string) []Finding {
	var findings []Finding
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.Contains(rel, ".."+string(filepath.Separator)) || rel == ".." {
			findings = append(findings, Finding{SeverityError, "path-traversal", "path escapes plugin root", rel})
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if target, err := filepath.EvalSymlinks(path); err == nil {
				if !withinRoot(absRoot, target) {
					findings = append(findings, Finding{SeverityError, "symlink-escape", "symlink points outside plugin root: " + target, rel})
				}
			} else {
				findings = append(findings, Finding{SeverityWarn, "symlink-broken", "symlink could not be resolved", rel})
			}
			return nil
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if shellHookExts[ext] {
				findings = append(findings, Finding{SeverityWarn, "executable-script", "executable script present", rel})
			} else if info.Mode()&0111 != 0 && info.Mode().IsRegular() {
				findings = append(findings, Finding{SeverityWarn, "executable-bit", "file has executable bit set", rel})
			}
		}
		return nil
	})
	return findings
}

// scanMCP inspects .mcp.json for external-process servers, credential env vars,
// and http/unknown URLs.
func scanMCP(root string) []Finding {
	mcpPath := filepath.Join(root, ".mcp.json")
	if !fileExists(mcpPath) {
		return nil
	}
	var findings []Finding
	cfg := readMCP(mcpPath)
	for name, srv := range cfg.MCPServers {
		if srv.Command != "" {
			findings = append(findings, Finding{SeverityWarn, "mcp-external-process", "MCP server launches external process: " + srv.Command, name})
		}
		if u := srv.URL; u != "" {
			if strings.HasPrefix(strings.ToLower(u), "http://") {
				findings = append(findings, Finding{SeverityError, "insecure-url", "MCP server uses http:// URL: " + u, name})
			} else if !strings.HasPrefix(strings.ToLower(u), "https://") {
				findings = append(findings, Finding{SeverityWarn, "unknown-url", "MCP server uses non-https URL: " + u, name})
			}
		}
		for k := range srv.Env {
			if looksLikeCredential(k) {
				findings = append(findings, Finding{SeverityWarn, "credential-env", "MCP server requests credential env var: " + k, name})
			}
		}
	}
	return findings
}

// scanHooks flags hook files that contain shell command invocations.
func scanHooks(root string) []Finding {
	hooksDir := filepath.Join(root, "hooks")
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		return nil
	}
	var findings []Finding
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if shellHookExts[ext] {
			findings = append(findings, Finding{SeverityWarn, "shell-hook", "hook contains an executable script", filepath.Join("hooks", e.Name())})
		}
	}
	return findings
}

// scanSkillDrift detects duplicate skills and content differences between the
// Claude and Codex copies of a same-named skill.
func scanSkillDrift(dp DiscoveredPlugin) []Finding {
	seen := map[string]bool{}
	var findings []Finding
	for _, s := range dp.Skills {
		if seen[s] {
			findings = append(findings, Finding{SeverityError, "duplicate-skill", "skill defined more than once: " + s, "skills/" + s})
		}
		seen[s] = true
	}
	// Content drift across agent-specific skill copies, if both exist.
	claudeSkills := filepath.Join(dp.Root, "skills")
	codexSkills := filepath.Join(dp.Root, ".codex", "skills")
	if isDir(codexSkills) {
		for _, s := range dp.Skills {
			a := filepath.Join(claudeSkills, s, "SKILL.md")
			b := filepath.Join(codexSkills, s, "SKILL.md")
			if fileExists(a) && fileExists(b) && !sameFile(a, b) {
				findings = append(findings, Finding{SeverityWarn, "skill-content-drift", "skill differs between agents: " + s, "skills/" + s})
			}
		}
	}
	return findings
}

func looksLikeCredential(name string) bool {
	up := strings.ToUpper(name)
	for _, hint := range credentialHints {
		if strings.Contains(up, hint) {
			return true
		}
	}
	return false
}

func withinRoot(absRoot, target string) bool {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sameFile(a, b string) bool {
	da, err1 := os.ReadFile(a)
	db, err2 := os.ReadFile(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(da) == string(db)
}
