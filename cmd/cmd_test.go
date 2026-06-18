package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
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

func TestInstall_InterspersedFlagsAfterPositionals(t *testing.T) {
	r := &adapter.RecordingRunner{LookPaths: map[string]string{"claude": "/usr/bin/claude"}}
	env, out, errOut := newTestEnv(r)
	// Flags appear AFTER positionals, mirroring the issue's examples.
	code := Execute([]string{"install", "acme/plugins", "formatter", "--agent", "claude-code"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// A GitHub source registers the repo as a marketplace, then installs the
	// bare plugin name (the marketplace name is NOT guessed from the repo).
	if !calledWith(r, "claude", "plugin marketplace add acme/plugins") {
		t.Fatalf("expected marketplace registration; calls=%v", r.Calls)
	}
	if !calledWith(r, "claude", "plugin install formatter") {
		t.Fatalf("expected bare native install of formatter; calls=%v", r.Calls)
	}
	_ = out
}

func TestInstall_NoSpuriousMarketplaceRemovalOnFailure(t *testing.T) {
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"claude": "/usr/bin/claude"},
		// Make the bare native install fail.
		Errs: map[string]error{"claude plugin install formatter": errors.New("boom")},
	}
	env, _, _ := newTestEnv(r)
	code := Execute([]string{"install", "acme/plugins", "formatter", "--agent", "claude-code"}, env)
	if code != exit.NativeCLIFailure {
		t.Fatalf("exit = %d, want %d", code, exit.NativeCLIFailure)
	}
	// The marketplace list does not change across the add (the fake returns the
	// same empty list), so no marketplace is treated as newly created and none
	// is removed on failure.
	for _, c := range r.Calls {
		if c.Name == "claude" && strings.HasPrefix(strings.Join(c.Args, " "), "plugin marketplace remove") {
			t.Fatalf("must not remove a marketplace it did not create; calls=%v", r.Calls)
		}
	}
}

func TestInstall_RejectsRef(t *testing.T) {
	r := &adapter.RecordingRunner{LookPaths: map[string]string{"claude": "/usr/bin/claude"}}
	env, _, _ := newTestEnv(r)
	code := Execute([]string{"install", "acme/plugins", "formatter", "--agent", "claude-code", "--ref", "v1.2.0"}, env)
	if code != exit.UnsupportedCapability {
		t.Fatalf("exit = %d, want %d (--ref should be rejected)", code, exit.UnsupportedCapability)
	}
	// Nothing should have been installed.
	if calledWith(r, "claude", "plugin install formatter") {
		t.Fatalf("install should not run when --ref is rejected; calls=%v", r.Calls)
	}
}

func TestInstall_LocalRegistersPathThenInstalls(t *testing.T) {
	r := &adapter.RecordingRunner{LookPaths: map[string]string{"claude": "/usr/bin/claude"}}
	env, _, errOut := newTestEnv(r)
	code := Execute([]string{"install", "./some/repo", "formatter", "--from-local", "--agent", "claude-code"}, env)
	if code != exit.OK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	// The local path is registered as a marketplace (not discarded) and the
	// bare plugin name is installed.
	if !calledWith(r, "claude", "plugin marketplace add ./some/repo") {
		t.Fatalf("expected local path to be registered; calls=%v", r.Calls)
	}
	if !calledWith(r, "claude", "plugin install formatter") {
		t.Fatalf("expected bare native install; calls=%v", r.Calls)
	}
}

// TestInstallOnAgent_RollsBackCreatedMarketplace exercises the rollback path
// directly with a fake adapter, since RecordingRunner cannot model the
// marketplace set changing across an add.
func TestInstallOnAgent_RollsBackCreatedMarketplace(t *testing.T) {
	fa := &fakeAdapter{
		id: "claude-code",
		// before add: empty; after add: the newly created marketplace appears.
		marketplacesSeq: [][]adapter.Marketplace{
			{},
			{{Agent: "claude-code", Name: "company-tools"}},
		},
		installErr: errors.New("boom"),
	}
	err := installOnAgent(context.Background(), fa, "formatter", "acme/plugins", false, commonFlags{})
	if err == nil {
		t.Fatal("expected install error")
	}
	if len(fa.removed) != 1 || fa.removed[0] != "company-tools" {
		t.Fatalf("expected rollback of the created marketplace, removed=%v", fa.removed)
	}
}

func TestInstallOnAgent_KeepsPreexistingMarketplace(t *testing.T) {
	fa := &fakeAdapter{
		id: "claude-code",
		// The marketplace already existed before the add, so it must not be
		// removed when the install fails.
		marketplacesSeq: [][]adapter.Marketplace{
			{{Agent: "claude-code", Name: "company-tools"}},
			{{Agent: "claude-code", Name: "company-tools"}},
		},
		installErr: errors.New("boom"),
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

func TestUpdateAll_EnumeratesCodexPlugins(t *testing.T) {
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"codex": "/usr/bin/codex"},
		Stdout: map[string]string{
			"codex plugin list --json": `[{"id":"formatter@company","name":"formatter","marketplace":"company","version":"1.0.0","status":"installed"}]`,
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
			"codex plugin list --json": `[{"id":"formatter@company","name":"formatter","version":"1.0.0"}]`,
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
func (f *fakeAdapter) InstallPlugin(context.Context, adapter.InstallRequest) error {
	return f.installErr
}
func (f *fakeAdapter) UpdatePlugin(context.Context, adapter.UpdateRequest) error   { return nil }
func (f *fakeAdapter) RemovePlugin(context.Context, adapter.RemoveRequest) error   { return nil }
func (f *fakeAdapter) EnablePlugin(context.Context, adapter.EnableRequest) error   { return nil }
func (f *fakeAdapter) DisablePlugin(context.Context, adapter.DisableRequest) error { return nil }
