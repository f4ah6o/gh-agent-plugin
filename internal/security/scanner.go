// Package security performs deterministic, offline inspection of agent plugin
// bundles. It never executes plugin content and never follows symlinks.
package security

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const ScannerName = "plugin-spector-go"

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityBlocking Severity = "blocking"
)

type RiskSeverity string

const (
	RiskLow      RiskSeverity = "LOW"
	RiskMedium   RiskSeverity = "MEDIUM"
	RiskHigh     RiskSeverity = "HIGH"
	RiskCritical RiskSeverity = "CRITICAL"
)

type Category string
type RuleID string

const (
	CategoryPromptInjection     Category = "prompt_injection"
	CategoryDataExfiltration    Category = "data_exfiltration"
	CategoryPrivilegeEscalation Category = "privilege_escalation"
	CategorySupplyChain         Category = "supply_chain"
	CategoryMCP                 Category = "mcp"
	CategoryHooks               Category = "hooks"
	CategorySecrets             Category = "secrets"
	CategoryStructure           Category = "structure"
	CategoryBinary              Category = "binary"
)

// Finding keeps the original preview JSON fields and adds richer scanner data.
type Finding struct {
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	Detail   string   `json:"detail"`
	Path     string   `json:"path,omitempty"`
	ID       RuleID   `json:"id"`
	Category Category `json:"category"`
	Message  string   `json:"message"`
	Blocking bool     `json:"blocking"`
	risk     RiskSeverity
}

type Report struct {
	Scanner        string       `json:"scanner"`
	RiskScore      int          `json:"riskScore"`
	RiskSeverity   RiskSeverity `json:"riskSeverity"`
	Recommendation string       `json:"recommendation"`
	Partial        bool         `json:"partial"`
	ScannedFiles   int          `json:"scannedFiles"`
	SkippedFiles   []string     `json:"skippedFiles"`
	BlockReasons   []RuleID     `json:"blockReasons"`
	Findings       []Finding    `json:"-"`
	deep           bool
}

func (r Report) ShouldBlock() bool { return len(r.BlockReasons) > 0 || (r.deep && r.RiskScore > 50) }

type Options struct {
	Deep          bool
	MaxFiles      int
	MaxDepth      int
	MaxFileBytes  int64
	MaxTotalBytes int64
}

const (
	defaultMaxFiles      = 2000
	defaultMaxDepth      = 20
	defaultMaxFileBytes  = int64(1_000_000)
	defaultMaxTotalBytes = int64(200 * 1024 * 1024)
)

var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true,
	"dist": true, "target": true, "__pycache__": true, ".cache": true,
	".pytest_cache": true, ".tox": true,
}

var scriptExts = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".py": true, ".rb": true,
	".pl": true, ".js": true, ".ts": true,
}

var suspiciousBinaryExts = map[string]bool{
	".exe": true, ".dll": true, ".dylib": true, ".so": true, ".bin": true,
}

type patternRule struct {
	id       RuleID
	legacy   string
	category Category
	risk     RiskSeverity
	message  string
	re       *regexp.Regexp
}

var deepRules = []patternRule{
	{"P1", "prompt-instruction-override", CategoryPromptInjection, RiskHigh, "Potential instruction override pattern detected", regexp.MustCompile(`(?i)(ignore|disregard|forget)\s+(all\s+)?(previous|prior|above|system)\s+(instructions?|prompts?)`)},
	{"P2", "prompt-hidden-instruction", CategoryPromptInjection, RiskHigh, "Potential hidden or concealed instruction detected", regexp.MustCompile(`(?i)(do not (tell|reveal|mention)|hidden instruction|system prompt)`)},
	{"E1", "external-transmission", CategoryDataExfiltration, RiskMedium, "Outbound transfer command detected", regexp.MustCompile(`(?i)\b(curl|wget)\b|https?://[^\s)"']+`)},
	{"E2", "environment-harvesting", CategoryDataExfiltration, RiskHigh, "Environment or credential harvesting detected", regexp.MustCompile(`(?i)(\bprintenv\b|\benv\s*\||os\.environ|process\.env|\$\{?(TOKEN|SECRET|PASSWORD|API_KEY))`)},
	{"PE1", "privilege-escalation", CategoryPrivilegeEscalation, RiskHigh, "Privilege escalation command detected", regexp.MustCompile(`(?i)\bsudo\b|\bchmod\s+[0-7]*[67][0-7]*\b|\bchown\b`)},
	{"PE2", "persistence-mutation", CategoryPrivilegeEscalation, RiskHigh, "Shell profile or service persistence mutation detected", regexp.MustCompile(`(?i)(\.bashrc|\.zshrc|LaunchAgents|systemd/system|crontab\b)`)},
	{"SC1", "remote-script-execution", CategorySupplyChain, RiskHigh, "Remote content is piped to a shell", regexp.MustCompile(`(?i)(curl|wget)[^\n|]{0,300}\|\s*(sh|bash|zsh)\b`)},
	{"SC2", "untrusted-package-install", CategorySupplyChain, RiskMedium, "Package installation command detected", regexp.MustCompile(`(?i)\b(npm|pnpm|yarn|pip|pip3|gem|cargo)\s+(install|add)\b`)},
	{"SEC001", "credential-reference", CategorySecrets, RiskMedium, "Credential or secret reference detected", regexp.MustCompile(`(?i)(API[_-]?KEY|ACCESS[_-]?TOKEN|PASSWORD|PRIVATE[_-]?KEY|CREDENTIALS?)`)},
}

func defaults(o Options) Options {
	if o.MaxFiles <= 0 {
		o.MaxFiles = defaultMaxFiles
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = defaultMaxDepth
	}
	if o.MaxFileBytes <= 0 {
		o.MaxFileBytes = defaultMaxFileBytes
	}
	if o.MaxTotalBytes <= 0 {
		o.MaxTotalBytes = defaultMaxTotalBytes
	}
	return o
}

func Scan(root string, options Options) (Report, error) {
	o := defaults(options)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Report{}, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return Report{}, err
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("security scan root is not a directory: %s", root)
	}

	r := Report{Scanner: ScannerName, Findings: []Finding{}, SkippedFiles: []string{}, BlockReasons: []RuleID{}, deep: o.Deep}
	var total int64
	hasScript := false
	stop := false
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if stop {
			return fs.SkipAll
		}
		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			r.Partial = true
			return nil
		}
		rel = filepath.ToSlash(rel)
		if walkErr != nil {
			r.Partial = true
			r.SkippedFiles = append(r.SkippedFiles, rel+": "+walkErr.Error())
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != absRoot && skippedDirs[entry.Name()] {
				r.SkippedFiles = append(r.SkippedFiles, rel+"/")
				return filepath.SkipDir
			}
			if rel != "." && len(strings.Split(rel, "/")) > o.MaxDepth {
				r.Partial = true
				r.SkippedFiles = append(r.SkippedFiles, rel+"/: depth limit")
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, evalErr := filepath.EvalSymlinks(path)
			if evalErr != nil {
				r.add(newFinding("symlink-broken", "symlink could not be resolved", rel, "CP003", CategoryStructure, RiskMedium, false))
				return nil
			}
			if !withinRoot(absRoot, target) {
				r.add(newFinding("symlink-escape", "symlink points outside plugin root: "+target, rel, "CP003", CategoryStructure, RiskHigh, true))
			}
			return nil
		}
		if r.ScannedFiles >= o.MaxFiles {
			r.Partial = true
			r.SkippedFiles = append(r.SkippedFiles, rel+": file limit")
			stop = true
			return fs.SkipAll
		}
		fi, statErr := entry.Info()
		if statErr != nil {
			r.Partial = true
			r.SkippedFiles = append(r.SkippedFiles, rel+": stat failed")
			return nil
		}
		if fi.Size() > o.MaxFileBytes || total+fi.Size() > o.MaxTotalBytes {
			r.Partial = true
			r.SkippedFiles = append(r.SkippedFiles, rel+": size limit")
			if total+fi.Size() > o.MaxTotalBytes {
				stop = true
				return fs.SkipAll
			}
			return nil
		}
		r.ScannedFiles++
		total += fi.Size()
		ext := strings.ToLower(filepath.Ext(path))
		if scriptExts[ext] {
			hasScript = true
			r.add(newFinding("executable-script", "executable script present", rel, "HK001", CategoryHooks, RiskMedium, false))
			if strings.HasPrefix(rel, "hooks/") {
				r.add(newFinding("shell-hook", "hook contains an executable script", rel, "HK002", CategoryHooks, RiskMedium, false))
			}
		} else if fi.Mode()&0o111 != 0 && fi.Mode().IsRegular() {
			r.add(newFinding("executable-bit", "file has executable bit set", rel, "BIN001", CategoryBinary, RiskLow, false))
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			r.Partial = true
			r.SkippedFiles = append(r.SkippedFiles, rel+": read failed")
			return nil
		}
		if isBinary(data) {
			if suspiciousBinaryExts[ext] || fi.Mode()&0o111 != 0 {
				r.add(newFinding("suspicious-binary", "binary or native executable present", rel, "BIN002", CategoryBinary, RiskHigh, false))
			}
			return nil
		}
		if filepath.Base(path) == ".mcp.json" {
			r.scanMCP(data, rel)
		}
		if o.Deep {
			r.scanContent(string(data), rel)
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	r.scanDeclaredPaths(absRoot)
	r.scanSkillDrift(absRoot)
	r.finish(hasScript)
	return r, nil
}

func (r *Report) scanDeclaredPaths(root string) {
	for _, rel := range []string{".claude-plugin/plugin.json", ".codex-plugin/plugin.json"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		var value any
		if json.Unmarshal(data, &value) != nil {
			continue
		}
		var inspect func(any)
		inspect = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				for _, child := range x {
					inspect(child)
				}
			case []any:
				for _, child := range x {
					inspect(child)
				}
			case string:
				if !strings.Contains(filepath.ToSlash(x), "../") && x != ".." {
					return
				}
				resolved := filepath.Clean(filepath.Join(root, filepath.FromSlash(x)))
				if !withinRoot(root, resolved) {
					r.add(newFinding("path-traversal", "declared component path escapes plugin root: "+x, rel, "CP002", CategoryStructure, RiskHigh, true))
				}
			}
		}
		inspect(value)
	}
}

func (r *Report) scanSkillDrift(root string) {
	claude := filepath.Join(root, "skills")
	codex := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(claude)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		a, errA := os.ReadFile(filepath.Join(claude, entry.Name(), "SKILL.md"))
		b, errB := os.ReadFile(filepath.Join(codex, entry.Name(), "SKILL.md"))
		if errA == nil && errB == nil && !bytes.Equal(a, b) {
			r.add(newFinding("skill-content-drift", "skill differs between agents: "+entry.Name(), "skills/"+entry.Name(), "SC6", CategorySupplyChain, RiskMedium, false))
		}
	}
}

func newFinding(rule, detail, path string, id RuleID, category Category, risk RiskSeverity, blocking bool) Finding {
	return Finding{Severity: legacySeverity(risk, blocking), Rule: rule, Detail: detail, Path: path, ID: id, Category: category, Message: detail, Blocking: blocking, risk: risk}
}

func legacySeverity(r RiskSeverity, blocking bool) Severity {
	if blocking {
		return SeverityBlocking
	}
	if r == RiskCritical || r == RiskHigh || r == RiskMedium {
		return SeverityWarning
	}
	return SeverityInfo
}

func (r *Report) add(f Finding) { r.Findings = append(r.Findings, f) }

func (r *Report) scanContent(content, path string) {
	for _, rule := range deepRules {
		if rule.re.FindStringIndex(content) != nil {
			r.add(newFinding(rule.legacy, rule.message, path, rule.id, rule.category, rule.risk, false))
		}
	}
}

type mcpConfig struct {
	MCPServers map[string]struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		URL     string            `json:"url"`
		Env     map[string]string `json:"env"`
	} `json:"mcpServers"`
}

func (r *Report) scanMCP(data []byte, path string) {
	var cfg mcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		r.add(newFinding("mcp-invalid-config", "MCP configuration is not valid JSON", path, "MCP004", CategoryMCP, RiskMedium, false))
		return
	}
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		srv := cfg.MCPServers[name]
		loc := path + "#" + name
		if srv.Command != "" {
			r.add(newFinding("mcp-external-process", "MCP server launches external process: "+srv.Command, loc, "MCP002", CategoryMCP, RiskMedium, false))
		}
		if srv.URL != "" {
			u, err := url.Parse(srv.URL)
			switch {
			case err != nil || u.Scheme == "":
				r.add(newFinding("unknown-url", "MCP server uses an invalid URL: "+srv.URL, loc, "MCP004", CategoryMCP, RiskMedium, false))
			case strings.EqualFold(u.Scheme, "http"):
				r.add(newFinding("insecure-url", "MCP server uses http:// URL: "+srv.URL, loc, "MCP001", CategoryMCP, RiskHigh, true))
			case strings.EqualFold(u.Scheme, "https"):
				r.add(newFinding("external-url", "MCP server references external URL: "+srv.URL, loc, "MCP003", CategoryMCP, RiskLow, false))
			default:
				r.add(newFinding("unknown-url", "MCP server uses non-https URL: "+srv.URL, loc, "MCP004", CategoryMCP, RiskMedium, false))
			}
		}
		keys := make([]string, 0, len(srv.Env))
		for key := range srv.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if credentialName(key) {
				r.add(newFinding("credential-env", "MCP server requests credential env var: "+key, loc, "SEC002", CategorySecrets, RiskMedium, false))
			}
		}
	}
}

func (r *Report) finish(hasScript bool) {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		if riskWeight(r.Findings[i].risk) != riskWeight(r.Findings[j].risk) {
			return riskWeight(r.Findings[i].risk) > riskWeight(r.Findings[j].risk)
		}
		if r.Findings[i].ID != r.Findings[j].ID {
			return r.Findings[i].ID < r.Findings[j].ID
		}
		return r.Findings[i].Path < r.Findings[j].Path
	})
	sort.Strings(r.SkippedFiles)
	score := 0
	reasons := map[RuleID]bool{}
	for _, f := range r.Findings {
		score += riskWeight(f.risk)
		if f.Blocking {
			reasons[f.ID] = true
		}
	}
	if hasScript {
		score = int(float64(score) * 1.3)
	}
	if score > 100 {
		score = 100
	}
	r.RiskScore = score
	switch {
	case score >= 81:
		r.RiskSeverity = RiskCritical
	case score >= 51:
		r.RiskSeverity = RiskHigh
	case score >= 21:
		r.RiskSeverity = RiskMedium
	default:
		r.RiskSeverity = RiskLow
	}
	if r.deep && score > 50 {
		for _, f := range r.Findings {
			if f.risk == RiskCritical || f.risk == RiskHigh {
				reasons[f.ID] = true
			}
		}
	}
	for id := range reasons {
		r.BlockReasons = append(r.BlockReasons, id)
	}
	sort.Slice(r.BlockReasons, func(i, j int) bool { return r.BlockReasons[i] < r.BlockReasons[j] })
	if r.ShouldBlock() {
		r.Recommendation = "DO_NOT_INSTALL"
	} else if score >= 21 || r.Partial {
		r.Recommendation = "CAUTION"
	} else {
		r.Recommendation = "SAFE"
	}
}

func riskWeight(s RiskSeverity) int {
	switch s {
	case RiskCritical:
		return 50
	case RiskHigh:
		return 25
	case RiskMedium:
		return 10
	default:
		return 5
	}
}

func isBinary(data []byte) bool {
	sample := data
	if len(sample) > 8000 {
		sample = sample[:8000]
	}
	return bytes.IndexByte(sample, 0) >= 0
}

func withinRoot(root, target string) bool {
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func credentialName(name string) bool {
	up := strings.ToUpper(name)
	for _, hint := range []string{"TOKEN", "SECRET", "KEY", "PASSWORD", "CREDENTIAL", "API_KEY"} {
		if strings.Contains(up, hint) {
			return true
		}
	}
	return false
}
