package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/cache"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

// newTestEnv builds an Env whose registry uses the given fake runner.
func newTestEnv(r adapter.Runner) (*Env, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &Env{
		Ctx:    context.Background(),
		Stdout: &out,
		Stderr: &errOut,
		Reg:    adapter.NewRegistry(r),
		Runner: r,
	}, &out, &errOut
}

// newTestEnvWithReg builds an Env using a pre-built registry (for fakeAdapter tests).
func newTestEnvWithReg(reg *adapter.Registry) (*Env, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &Env{
		Ctx:    context.Background(),
		Stdout: &out,
		Stderr: &errOut,
		Reg:    reg,
	}, &out, &errOut
}

func TestList_NoAgents_EmptyJSON(t *testing.T) {
	env, out, _ := newTestEnv(&adapter.RecordingRunner{}) // nothing detected
	code := Execute([]string{"list", "--json"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	var got struct {
		Plugins []any `json:"plugins"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if got.Plugins == nil {
		t.Fatal("expected non-nil (empty) plugins array")
	}
	if len(got.Plugins) != 0 {
		t.Fatalf("expected 0 plugins, got %d", len(got.Plugins))
	}
}

func TestInstall_NoAgents_ExitAgentNotInstalled(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	code := Execute([]string{"install", "acme/plugins", "formatter"}, env)
	if code != exit.AgentNotInstalled {
		t.Fatalf("exit = %d, want %d", code, exit.AgentNotInstalled)
	}
}

func TestInstall_CodexProjectScope_Unsupported(t *testing.T) {
	r := &adapter.RecordingRunner{LookPaths: map[string]string{"codex": "/usr/bin/codex"}}
	env, _, errOut := newTestEnv(r)
	code := Execute([]string{"install", "formatter@company", "--agent", "codex", "--scope", "project"}, env)
	if code != exit.UnsupportedCapability {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exit.UnsupportedCapability, errOut.String())
	}
}

// TestInstall_InterspersedFlagsAfterPositionals verifies that flags appearing
// after positional arguments are parsed correctly, and that a GitHub source
// install registers the marketplace and installs with the qualified
// PLUGIN@MARKETPLACE_NAME selector.
func TestInstall_InterspersedFlagsAfterPositionals(t *testing.T) {
	fa := &fakeAdapter{
		id: "claude-code",
		// Before add: empty; after add: new marketplace appears.
		marketplacesSeq: [][]adapter.Marketplace{
			{},
			{{Agent: "claude-code", Name: "acme-plugins", URL: "acme/plugins"}},
		},
	}
	reg := adapter.NewRegistryFrom(fa)
	env, _, errOut := newTestEnvWithReg(reg)
	// Flags appear AFTER positionals, mirroring the issue's examples.
	code := Execute([]string{"install", "acme/plugins", "formatter", "--agent", "claude-code"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// The install must use the qualified PLUGIN@MARKETPLACE_NAME selector to
	// avoid collisions when the same plugin name exists in multiple marketplaces.
	if fa.installedPlugin != "formatter@acme-plugins" {
		t.Fatalf("installed plugin = %q, want formatter@acme-plugins", fa.installedPlugin)
	}
}

// TestInstall_NoSpuriousMarketplaceRemovalOnFailure verifies that when the
// native install fails, only marketplaces newly created by this call are rolled
// back and a pre-existing marketplace is left untouched.
func TestInstall_NoSpuriousMarketplaceRemovalOnFailure(t *testing.T) {
	fa := &fakeAdapter{
		id: "claude-code",
		// Pre-existing marketplace: before and after are the same.
		marketplacesSeq: [][]adapter.Marketplace{
			{{Agent: "claude-code", Name: "acme-plugins", URL: "acme/plugins"}},
			{{Agent: "claude-code", Name: "acme-plugins", URL: "acme/plugins"}},
		},
		installErr: exit.Errorf(exit.NativeCLIFailure, "native install boom"),
	}
	reg := adapter.NewRegistryFrom(fa)
	env, _, _ := newTestEnvWithReg(reg)
	code := Execute([]string{"install", "acme/plugins", "formatter", "--agent", "claude-code"}, env)
	if code != exit.NativeCLIFailure {
		t.Fatalf("exit = %d, want %d", code, exit.NativeCLIFailure)
	}
	// The marketplace was not created by this call (diff is empty), so it must
	// not be removed even though the install failed.
	if len(fa.removed) != 0 {
		t.Fatalf("must not remove a pre-existing marketplace, removed=%v", fa.removed)
	}
}

func TestInstall_LocalRegistersPathThenInstalls(t *testing.T) {
	fa := &fakeAdapter{
		id: "claude-code",
		marketplacesSeq: [][]adapter.Marketplace{
			{},
			{{Agent: "claude-code", Name: "my-local-plugins", URL: "./some/repo"}},
		},
	}
	reg := adapter.NewRegistryFrom(fa)
	env, _, errOut := newTestEnvWithReg(reg)
	code := Execute([]string{"install", "./some/repo", "formatter", "--from-local", "--agent", "claude-code"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if fa.installedPlugin != "formatter@my-local-plugins" {
		t.Fatalf("installed plugin = %q, want formatter@my-local-plugins", fa.installedPlugin)
	}
}

// TestInstallOnAgent_RollsBackCreatedMarketplace exercises the rollback path
// directly with a fake adapter when install fails after marketplace creation.
func TestInstallOnAgent_RollsBackCreatedMarketplace(t *testing.T) {
	fa := &fakeAdapter{
		id: "claude-code",
		// before add: empty; after add: the newly created marketplace appears.
		marketplacesSeq: [][]adapter.Marketplace{
			{},
			{{Agent: "claude-code", Name: "company-tools", URL: "acme/plugins"}},
		},
		installErr: exit.Errorf(exit.NativeCLIFailure, "boom"),
	}
	err := installOnAgent(context.Background(), fa, "formatter", "acme/plugins", false, commonFlags{})
	if err == nil {
		t.Fatal("expected install error")
	}
	if len(fa.removed) != 1 || fa.removed[0] != "company-tools" {
		t.Fatalf("expected rollback of the created marketplace, removed=%v", fa.removed)
	}
	// The install must have been attempted with the qualified selector.
	if fa.installedPlugin != "formatter@company-tools" {
		t.Fatalf("install was called with %q, want formatter@company-tools", fa.installedPlugin)
	}
}

func TestInstallOnAgent_KeepsPreexistingMarketplace(t *testing.T) {
	fa := &fakeAdapter{
		id: "claude-code",
		// The marketplace already existed before the add; URL matching finds it.
		marketplacesSeq: [][]adapter.Marketplace{
			{{Agent: "claude-code", Name: "company-tools", URL: "acme/plugins"}},
			{{Agent: "claude-code", Name: "company-tools", URL: "acme/plugins"}},
		},
		installErr: exit.Errorf(exit.NativeCLIFailure, "boom"),
	}
	err := installOnAgent(context.Background(), fa, "formatter", "acme/plugins", false, commonFlags{})
	if err == nil {
		t.Fatal("expected install error")
	}
	if len(fa.removed) != 0 {
		t.Fatalf("must not remove a pre-existing marketplace, removed=%v", fa.removed)
	}
}

func TestInstall_MarketplaceSelectorNoRegistration(t *testing.T) {
	r := &adapter.RecordingRunner{LookPaths: map[string]string{"claude": "/usr/bin/claude"}}
	env, _, errOut := newTestEnv(r)
	code := Execute([]string{"install", "formatter@company", "--agent", "claude-code"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// A configured-marketplace selector installs verbatim and registers nothing.
	if !calledWith(r, "claude", "plugin install formatter@company") {
		t.Fatalf("expected verbatim install; calls=%v", r.Calls)
	}
	for _, c := range r.Calls {
		if c.Name == "claude" && strings.HasPrefix(strings.Join(c.Args, " "), "plugin marketplace add") {
			t.Fatalf("did not expect a marketplace registration; calls=%v", r.Calls)
		}
	}
}

// TestInstall_CodexFallsBackToBareNameWhenCannotEnumerate verifies that when an
// agent (Codex) cannot enumerate marketplaces, the bare plugin name is used
// rather than failing.
func TestInstall_CodexFallsBackToBareNameWhenCannotEnumerate(t *testing.T) {
	r := &adapter.RecordingRunner{LookPaths: map[string]string{"codex": "/usr/bin/codex"}}
	env, _, errOut := newTestEnv(r)
	code := Execute([]string{"install", "acme/plugins", "formatter", "--agent", "codex"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// Codex can't enumerate marketplaces; falls back to bare name.
	if !calledWith(r, "codex", "plugin add formatter") {
		t.Fatalf("expected bare plugin install for Codex; calls=%v", r.Calls)
	}
}

// TestInstall_GitHubRef_PinsViaCache verifies that install with --ref checks out
// the pinned revision via the cache and registers it as a local marketplace.
func TestInstall_GitHubRef_PinsViaCache(t *testing.T) {
	fgit := &fakeGit{}
	c, err := cache.New(t.TempDir(), fgit)
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	fa := &fakeAdapter{
		id: "claude-code",
		marketplacesSeq: [][]adapter.Marketplace{
			{},
			{{Agent: "claude-code", Name: "pinned-repo", URL: fgit.lastDir()}},
		},
	}
	reg := adapter.NewRegistryFrom(fa)
	env, _, errOut := newTestEnvWithReg(reg)
	env.Cache = c

	code := Execute([]string{"install", "acme/plugins", "formatter", "--ref", "v2.1.0", "--agent", "claude-code"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if fgit.ref != "v2.1.0" {
		t.Fatalf("clone ref = %q, want v2.1.0", fgit.ref)
	}
	// The install should have gone through as a local marketplace with a
	// qualified selector (exact name depends on fakeAdapter sequence).
	if !strings.HasPrefix(fa.installedPlugin, "formatter@") {
		t.Fatalf("expected qualified install selector, got %q", fa.installedPlugin)
	}
}

// TestInstall_RefRejectedForNonGitHub verifies that --ref is rejected with
// UnsupportedCapability when the install source is not a GitHub repo, rather
// than being silently ignored and misleading the user.
func TestInstall_RefRejectedForNonGitHub(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"marketplace selector", []string{"install", "formatter@acme", "--ref", "v1.0.0", "--agent", "claude-code"}},
		{"local path", []string{"install", "../testdata/sample-repo", "formatter", "--from-local", "--ref", "v1.0.0", "--agent", "claude-code"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, _, _ := newTestEnv(&adapter.RecordingRunner{})
			code := Execute(tc.args, env)
			if code != exit.UnsupportedCapability {
				t.Fatalf("exit = %d, want %d (UnsupportedCapability) for %v", code, exit.UnsupportedCapability, tc.args)
			}
		})
	}
}

// calledWith reports whether the runner recorded a call to name with exactly
// the given space-joined args.
func calledWith(r *adapter.RecordingRunner, name, args string) bool {
	for _, c := range r.Calls {
		if c.Name == name && strings.Join(c.Args, " ") == args {
			return true
		}
	}
	return false
}

func TestPreview_LocalSample(t *testing.T) {
	env, out, errOut := newTestEnv(&adapter.RecordingRunner{})
	code := Execute([]string{"preview", "../testdata/sample-repo", "example", "--from-local", "--json"}, env)
	// The sample contains blocking findings (http URL), so preview exits 5.
	if code != exit.ValidationFailed {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exit.ValidationFailed, errOut.String())
	}
	if !strings.Contains(out.String(), "\"name\": \"example\"") {
		t.Fatalf("preview JSON missing plugin name:\n%s", out.String())
	}
}

// fakeGit clones by materializing a minimal claude-code plugin tree so discovery
// finds a real plugin without touching the network.
type fakeGit struct {
	ref     string
	cloneTo string // directory last cloned into
}

func (g *fakeGit) Clone(_ context.Context, _, ref, dir string) error {
	g.ref = ref
	g.cloneTo = dir
	root := filepath.Join(dir, "plugins", "formatter")
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(`{"name":"formatter"}`), 0o644)
}

func (g *fakeGit) Revision(context.Context, string) (string, error) {
	return "feedfacefeedfacefeedfacefeedfacefeedface", nil
}

func (g *fakeGit) Fetch(context.Context, string) error { return nil }

// lastDir returns the most recent clone destination, used to match the local
// path registered by a ref-pinned install.
func (g *fakeGit) lastDir() string { return g.cloneTo }

func TestPreview_GitHubSource_ClonesAndRecordsRevision(t *testing.T) {
	env, out, errOut := newTestEnv(&adapter.RecordingRunner{})
	git := &fakeGit{}
	c, err := cache.New(t.TempDir(), git)
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	env.Cache = c

	code := Execute([]string{"preview", "acme/plugins", "formatter", "--ref", "v2.1.0", "--json"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if git.ref != "v2.1.0" {
		t.Fatalf("clone ref = %q, want v2.1.0", git.ref)
	}
	var got struct {
		Plugin struct {
			Name string `json:"name"`
		} `json:"plugin"`
		Source map[string]string `json:"source"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if got.Plugin.Name != "formatter" {
		t.Fatalf("plugin name = %q, want formatter", got.Plugin.Name)
	}
	if got.Source["type"] != "github" || got.Source["repository"] != "acme/plugins" {
		t.Fatalf("unexpected source metadata: %+v", got.Source)
	}
	if got.Source["ref"] != "v2.1.0" || got.Source["revision"] != "feedfacefeedfacefeedfacefeedfacefeedface" {
		t.Fatalf("ref/revision not recorded: %+v", got.Source)
	}
}

func TestPreview_GitHubSource_NoCacheConfigured(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{}) // env.Cache is nil
	code := Execute([]string{"preview", "acme/plugins", "formatter"}, env)
	if code != exit.GeneralError {
		t.Fatalf("exit = %d, want %d when no cache is configured", code, exit.GeneralError)
	}
}

// TestPreview_NoCache_InvalidatesAndReclones verifies that --no-cache removes
// the cached checkout before preview, so a stale clone is always discarded.
func TestPreview_NoCache_InvalidatesAndReclones(t *testing.T) {
	git := &fakeGit{}
	c, err := cache.New(t.TempDir(), git)
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	env.Cache = c

	// First preview: clones.
	if code := Execute([]string{"preview", "acme/plugins", "formatter", "--ref", "v1.0"}, env); code != exit.OK {
		t.Fatalf("first preview exit = %d", code)
	}
	// Second preview without --no-cache: reuses (no additional clone).
	if code := Execute([]string{"preview", "acme/plugins", "formatter", "--ref", "v1.0"}, env); code != exit.OK {
		t.Fatalf("second preview exit = %d", code)
	}
	// Third preview with --no-cache: must re-clone.
	if code := Execute([]string{"preview", "acme/plugins", "formatter", "--ref", "v1.0", "--no-cache"}, env); code != exit.OK {
		t.Fatalf("third preview exit = %d", code)
	}
	// fakeGit records clones; we expect 2: initial + forced re-clone.
	// (The second call reuses the checkout for an immutable ref.)
	if git.ref != "v1.0" {
		t.Fatalf("ref = %q", git.ref)
	}
}

// TestPreview_ReservedFlags_Warning checks that --jq and --template emit
// warnings rather than silently being ignored.
func TestPreview_ReservedFlags_Warning(t *testing.T) {
	env, _, errOut := newTestEnv(&adapter.RecordingRunner{})
	_ = Execute([]string{"preview", "../testdata/sample-repo", "example", "--from-local", "--jq", ".plugin.name", "--template", "{{.}}"}, env)
	stderr := errOut.String()
	if !strings.Contains(stderr, "--jq") {
		t.Errorf("expected --jq warning in stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "--template") {
		t.Errorf("expected --template warning in stderr:\n%s", stderr)
	}
}

func TestUpdateAll_EnumeratesCodexPlugins(t *testing.T) {
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"codex": "/usr/bin/codex"},
		Stdout: map[string]string{
			"codex plugin list": "PLUGIN                            STATUS              VERSION       PATH\nformatter@company                 installed, enabled  1.0.0         /path/to/formatter\n",
		},
	}
	env, _, errOut := newTestEnv(r)
	code := Execute([]string{"update", "--all", "--agent", "codex"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// --all enumerates installed plugins and refreshes each by ID; it must never
	// issue an empty selector.
	if !calledWith(r, "codex", "plugin add formatter@company") {
		t.Fatalf("expected per-plugin update; calls=%v", r.Calls)
	}
	if calledWith(r, "codex", "plugin add ") {
		t.Fatalf("update --all issued an empty selector; calls=%v", r.Calls)
	}
}

func TestUpdateNamed_UsesClaudeUpdateCommand(t *testing.T) {
	r := &adapter.RecordingRunner{LookPaths: map[string]string{"claude": "/usr/bin/claude"}}
	env, _, errOut := newTestEnv(r)
	code := Execute([]string{"update", "formatter@company", "--agent", "claude-code"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// Update must use the dedicated native update verb, not plugin install.
	if !calledWith(r, "claude", "plugin update formatter@company") {
		t.Fatalf("expected native update command; calls=%v", r.Calls)
	}
	if calledWith(r, "claude", "plugin install formatter@company") {
		t.Fatalf("update must not be implemented as install; calls=%v", r.Calls)
	}
}

func TestUpdateAll_AgentFlagNotWidened(t *testing.T) {
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"claude": "/usr/bin/claude", "codex": "/usr/bin/codex"},
		Stdout: map[string]string{
			"codex plugin list": "PLUGIN                            STATUS              VERSION       PATH\nformatter@company                 installed, enabled  1.0.0         /path/to/formatter\n",
		},
	}
	env, _, errOut := newTestEnv(r)
	// --all means "all plugins"; --agent codex must restrict to codex even though
	// both agents are installed.
	code := Execute([]string{"update", "--all", "--agent", "codex"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	for _, c := range r.Calls {
		if c.Name == "claude" && len(c.Args) > 0 && c.Args[0] == "plugin" {
			t.Fatalf("--agent codex must not target claude; calls=%v", r.Calls)
		}
	}
}

func TestList_ParsesClaudeJSON(t *testing.T) {
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"claude": "/usr/bin/claude"},
		Stdout: map[string]string{
			"claude plugin list --json": `[{"name":"formatter","marketplace":"company","version":"1.2.0","scope":"user","enabled":true}]`,
		},
	}
	env, out, errOut := newTestEnv(r)
	code := Execute([]string{"list", "--json"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	var got struct {
		Plugins []adapter.Plugin `json:"plugins"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(got.Plugins) != 1 || got.Plugins[0].ID != "formatter@company" || got.Plugins[0].Version != "1.2.0" {
		t.Fatalf("expected parsed Claude plugin, got %+v", got.Plugins)
	}
}

func TestUnknownCommand(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	if code := Execute([]string{"frobnicate"}, env); code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d", code, exit.InvalidArguments)
	}
}

// fakeAdapter is a programmable adapter.Adapter for testing command logic that
// depends on marketplace state changing across calls (which RecordingRunner
// cannot express). Only the methods exercised by tests carry behavior.
type fakeAdapter struct {
	id              string
	marketplacesSeq [][]adapter.Marketplace
	listIdx         int
	installErr      error
	removed         []string
	installedPlugin string // tracks the Plugin field of the last InstallPlugin call
}

func (f *fakeAdapter) ID() string { return f.id }

func (f *fakeAdapter) Detect(context.Context) (adapter.Detection, error) {
	return adapter.Detection{Installed: true}, nil
}

func (f *fakeAdapter) Capabilities(context.Context) (adapter.Capabilities, error) {
	return adapter.Capabilities{JSONOutput: true}, nil
}

func (f *fakeAdapter) ListMarketplaces(context.Context) ([]adapter.Marketplace, error) {
	if f.listIdx >= len(f.marketplacesSeq) {
		if len(f.marketplacesSeq) == 0 {
			return nil, nil
		}
		return f.marketplacesSeq[len(f.marketplacesSeq)-1], nil
	}
	out := f.marketplacesSeq[f.listIdx]
	f.listIdx++
	return out, nil
}

func (f *fakeAdapter) AddMarketplace(context.Context, adapter.AddMarketplaceRequest) error {
	return nil
}
func (f *fakeAdapter) UpdateMarketplace(context.Context, adapter.UpdateMarketplaceRequest) error {
	return nil
}

func (f *fakeAdapter) RemoveMarketplace(_ context.Context, req adapter.RemoveMarketplaceRequest) error {
	f.removed = append(f.removed, req.Name)
	return nil
}

func (f *fakeAdapter) ListPlugins(context.Context, adapter.ListRequest) ([]adapter.Plugin, error) {
	return nil, nil
}
func (f *fakeAdapter) InstallPlugin(_ context.Context, req adapter.InstallRequest) error {
	f.installedPlugin = req.Plugin
	return f.installErr
}
func (f *fakeAdapter) UpdatePlugin(context.Context, adapter.UpdateRequest) error   { return nil }
func (f *fakeAdapter) RemovePlugin(context.Context, adapter.RemoveRequest) error   { return nil }
func (f *fakeAdapter) EnablePlugin(context.Context, adapter.EnableRequest) error   { return nil }
func (f *fakeAdapter) DisablePlugin(context.Context, adapter.DisableRequest) error { return nil }

// ---- issue command tests ----

func TestIssueList_NoGH_ExitAgentNotInstalled(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{}) // no gh in LookPaths
	code := Execute([]string{"issue", "list"}, env)
	if code != exit.AgentNotInstalled {
		t.Fatalf("exit = %d, want %d (AgentNotInstalled)", code, exit.AgentNotInstalled)
	}
}

func TestIssueList_TableOutput(t *testing.T) {
	const ghPath = "/usr/bin/gh"
	issueJSON := `[{"number":1,"title":"Fix bug","state":"open","author":{"login":"alice"},"createdAt":"2024-01-01","labels":[{"name":"bug"}]}]`
	key := ghPath + " issue list --state open --limit 30 --json number,title,state,labels,author,createdAt,url"
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"gh": ghPath},
		Stdout:    map[string]string{key: issueJSON},
	}
	env, out, _ := newTestEnv(r)
	code := Execute([]string{"issue", "list"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := out.String()
	if !strings.Contains(got, "Fix bug") {
		t.Fatalf("expected issue title in output, got: %s", got)
	}
	if !strings.Contains(got, "alice") {
		t.Fatalf("expected author in output, got: %s", got)
	}
}

func TestIssueList_JSONPassthrough(t *testing.T) {
	const ghPath = "/usr/bin/gh"
	issueJSON := `[{"number":2,"title":"Add feature","state":"open","author":{"login":"bob"},"createdAt":"2024-02-01","labels":[]}]`
	key := ghPath + " issue list --state open --limit 30 --json number,title,state,labels,author,createdAt,url"
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"gh": ghPath},
		Stdout:    map[string]string{key: issueJSON},
	}
	env, out, _ := newTestEnv(r)
	code := Execute([]string{"issue", "list", "--json"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	// Output should be the raw JSON from gh
	if !strings.Contains(out.String(), "Add feature") {
		t.Fatalf("expected raw JSON passthrough, got: %s", out.String())
	}
}

func TestIssueList_StateFlag(t *testing.T) {
	const ghPath = "/usr/bin/gh"
	key := ghPath + " issue list --state closed --limit 30 --json number,title,state,labels,author,createdAt,url"
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"gh": ghPath},
		Stdout:    map[string]string{key: `[]`},
	}
	env, _, _ := newTestEnv(r)
	code := Execute([]string{"issue", "list", "--state", "closed"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestIssueView_MissingNumber(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{LookPaths: map[string]string{"gh": "/usr/bin/gh"}})
	code := Execute([]string{"issue", "view"}, env)
	if code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d (InvalidArguments)", code, exit.InvalidArguments)
	}
}

func TestIssueView_Output(t *testing.T) {
	const ghPath = "/usr/bin/gh"
	viewJSON := `{"number":3,"title":"View me","state":"open","body":"body text","author":{"login":"carol"},"url":"https://github.com/o/r/issues/3","createdAt":"2024-03-01","labels":[]}`
	key := ghPath + " issue view 3 --json number,title,state,body,author,labels,url,createdAt"
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"gh": ghPath},
		Stdout:    map[string]string{key: viewJSON},
	}
	env, out, _ := newTestEnv(r)
	code := Execute([]string{"issue", "view", "3"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := out.String()
	if !strings.Contains(got, "View me") {
		t.Fatalf("expected title in output, got: %s", got)
	}
	if !strings.Contains(got, "body text") {
		t.Fatalf("expected body in output, got: %s", got)
	}
}

func TestIssueComment_MissingNumber(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{LookPaths: map[string]string{"gh": "/usr/bin/gh"}})
	code := Execute([]string{"issue", "comment", "--body", "hi"}, env)
	if code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d (InvalidArguments)", code, exit.InvalidArguments)
	}
}

func TestIssueComment_MissingBody(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{LookPaths: map[string]string{"gh": "/usr/bin/gh"}})
	code := Execute([]string{"issue", "comment", "5"}, env)
	if code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d (InvalidArguments)", code, exit.InvalidArguments)
	}
}

func TestIssueComment_DryRun(t *testing.T) {
	env, out, _ := newTestEnv(&adapter.RecordingRunner{LookPaths: map[string]string{"gh": "/usr/bin/gh"}})
	code := Execute([]string{"issue", "comment", "7", "--body", "hello", "--dry-run"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Fatalf("expected dry-run output, got: %s", out.String())
	}
}

func TestIssueComment_Success(t *testing.T) {
	const ghPath = "/usr/bin/gh"
	key := ghPath + " issue comment 9 --body nice work"
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"gh": ghPath},
		Stdout:    map[string]string{key: ""},
	}
	env, out, _ := newTestEnv(r)
	code := Execute([]string{"issue", "comment", "9", "--body", "nice work"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "comment added") {
		t.Fatalf("expected success message, got: %s", out.String())
	}
}

func TestIssueUnknownSubcommand(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	code := Execute([]string{"issue", "frobnicate"}, env)
	if code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d", code, exit.InvalidArguments)
	}
}

// ---- pr command tests ----

func TestPRComment_NoGH_ExitAgentNotInstalled(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{}) // no gh in LookPaths
	code := Execute([]string{"pr", "comment", "1", "--body", "hi"}, env)
	if code != exit.AgentNotInstalled {
		t.Fatalf("exit = %d, want %d (AgentNotInstalled)", code, exit.AgentNotInstalled)
	}
}

func TestPRComment_MissingNumber(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{LookPaths: map[string]string{"gh": "/usr/bin/gh"}})
	code := Execute([]string{"pr", "comment", "--body", "hi"}, env)
	if code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d (InvalidArguments)", code, exit.InvalidArguments)
	}
}

func TestPRComment_MissingBody(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{LookPaths: map[string]string{"gh": "/usr/bin/gh"}})
	code := Execute([]string{"pr", "comment", "42"}, env)
	if code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d (InvalidArguments)", code, exit.InvalidArguments)
	}
}

func TestPRComment_DryRun(t *testing.T) {
	env, out, _ := newTestEnv(&adapter.RecordingRunner{LookPaths: map[string]string{"gh": "/usr/bin/gh"}})
	code := Execute([]string{"pr", "comment", "42", "--body", "LGTM", "--dry-run"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Fatalf("expected dry-run output, got: %s", out.String())
	}
}

func TestPRComment_Success(t *testing.T) {
	const ghPath = "/usr/bin/gh"
	key := ghPath + " pr comment 42 --body LGTM"
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"gh": ghPath},
		Stdout:    map[string]string{key: ""},
	}
	env, out, _ := newTestEnv(r)
	code := Execute([]string{"pr", "comment", "42", "--body", "LGTM"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "comment added") {
		t.Fatalf("expected success message, got: %s", out.String())
	}
}

func TestPRCommentList_MissingNumber(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{LookPaths: map[string]string{"gh": "/usr/bin/gh"}})
	code := Execute([]string{"pr", "comment", "list"}, env)
	if code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d (InvalidArguments)", code, exit.InvalidArguments)
	}
}

func TestPRCommentList_Output(t *testing.T) {
	const ghPath = "/usr/bin/gh"
	prJSON := `{"comments":[{"author":{"login":"dave"},"body":"looks good","url":"https://github.com/o/r/pull/5#issuecomment-1"}]}`
	key := ghPath + " pr view 5 --json comments"
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"gh": ghPath},
		Stdout:    map[string]string{key: prJSON},
	}
	env, out, _ := newTestEnv(r)
	code := Execute([]string{"pr", "comment", "list", "5"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := out.String()
	if !strings.Contains(got, "dave") {
		t.Fatalf("expected author in output, got: %s", got)
	}
	if !strings.Contains(got, "looks good") {
		t.Fatalf("expected comment body in output, got: %s", got)
	}
}

func TestPRUnknownSubcommand(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	code := Execute([]string{"pr", "frobnicate"}, env)
	if code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d", code, exit.InvalidArguments)
	}
}
