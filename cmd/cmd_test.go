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
	// plugin via the repo-derived PLUGIN@MARKETPLACE selector.
	if !calledWith(r, "claude", "plugin marketplace add acme/plugins") {
		t.Fatalf("expected marketplace registration; calls=%v", r.Calls)
	}
	if !calledWith(r, "claude", "plugin install formatter@plugins") {
		t.Fatalf("expected native install of formatter@plugins; calls=%v", r.Calls)
	}
	_ = out
}

func TestInstall_GitHubRollbackOnFailure(t *testing.T) {
	r := &adapter.RecordingRunner{
		LookPaths: map[string]string{"claude": "/usr/bin/claude"},
		// Make the native install fail so rollback must kick in.
		Errs: map[string]error{"claude plugin install formatter@plugins": errors.New("boom")},
	}
	env, _, _ := newTestEnv(r)
	code := Execute([]string{"install", "acme/plugins", "formatter", "--agent", "claude-code"}, env)
	if code != exit.NativeCLIFailure {
		t.Fatalf("exit = %d, want %d", code, exit.NativeCLIFailure)
	}
	// The marketplace registered for this install must be rolled back.
	if !calledWith(r, "claude", "plugin marketplace remove plugins") {
		t.Fatalf("expected marketplace rollback; calls=%v", r.Calls)
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

func TestUnknownCommand(t *testing.T) {
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	if code := Execute([]string{"frobnicate"}, env); code != exit.InvalidArguments {
		t.Fatalf("exit = %d, want %d", code, exit.InvalidArguments)
	}
}
