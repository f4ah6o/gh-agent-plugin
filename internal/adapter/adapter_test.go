package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

func argvOf(c Call) string {
	return c.Name + " " + strings.Join(c.Args, " ")
}

func TestClaudeInstall_BuildsScopedArgv(t *testing.T) {
	r := &RecordingRunner{LookPaths: map[string]string{"claude": "/usr/bin/claude"}}
	c := NewClaude(r)
	err := c.InstallPlugin(context.Background(), InstallRequest{Plugin: "formatter@company", Scope: ScopeUser})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(r.Calls))
	}
	got := argvOf(r.Calls[0])
	want := "claude plugin install formatter@company --scope user"
	if got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestClaudeRemove_KeepDataAndPrune(t *testing.T) {
	r := &RecordingRunner{}
	c := NewClaude(r)
	if err := c.RemovePlugin(context.Background(), RemoveRequest{Plugin: "formatter@company", KeepData: true, Prune: true}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got := argvOf(r.Calls[0])
	want := "claude plugin uninstall formatter@company --keep-data --prune"
	if got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestClaudeUnsupportedScope(t *testing.T) {
	// Claude supports project, so use an invalid scope value to trigger rejection.
	c := NewClaude(&RecordingRunner{})
	err := c.InstallPlugin(context.Background(), InstallRequest{Plugin: "x", Scope: Scope("global")})
	assertCode(t, err, exit.UnsupportedCapability)
}

func TestCodexRejectsScope(t *testing.T) {
	c := NewCodex(&RecordingRunner{})
	err := c.InstallPlugin(context.Background(), InstallRequest{Plugin: "x", Scope: ScopeProject})
	assertCode(t, err, exit.UnsupportedCapability)
	if !strings.Contains(err.Error(), `does not support scope "project"`) {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestCodexEnableDisableUnsupported(t *testing.T) {
	c := NewCodex(&RecordingRunner{})
	assertCode(t, c.EnablePlugin(context.Background(), EnableRequest{Plugin: "x"}), exit.UnsupportedCapability)
	assertCode(t, c.DisablePlugin(context.Background(), DisableRequest{Plugin: "x"}), exit.UnsupportedCapability)
}

func TestCodexListParsesJSON(t *testing.T) {
	r := &RecordingRunner{
		LookPaths: map[string]string{"codex": "/usr/bin/codex"},
		Stdout: map[string]string{
			"codex plugin list --json": `[{"id":"formatter@company","name":"formatter","marketplace":"company","version":"1.2.0","status":"installed"}]`,
		},
	}
	c := NewCodex(r)
	plugins, err := c.ListPlugins(context.Background(), ListRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if p.ID != "formatter@company" || p.Version != "1.2.0" || p.Agent != "codex" {
		t.Fatalf("unexpected plugin: %+v", p)
	}
}

func TestClaudeListUnsupported(t *testing.T) {
	c := NewClaude(&RecordingRunner{LookPaths: map[string]string{"claude": "/usr/bin/claude"}})
	_, err := c.ListPlugins(context.Background(), ListRequest{})
	assertCode(t, err, exit.UnsupportedCapability)
	_, err = c.ListMarketplaces(context.Background())
	assertCode(t, err, exit.UnsupportedCapability)
}

func TestCodexListMarketplacesUnsupported(t *testing.T) {
	c := NewCodex(&RecordingRunner{LookPaths: map[string]string{"codex": "/usr/bin/codex"}})
	_, err := c.ListMarketplaces(context.Background())
	assertCode(t, err, exit.UnsupportedCapability)
}

func TestDetectAbsentBinary(t *testing.T) {
	c := NewClaude(&RecordingRunner{}) // no LookPaths -> not found
	d, err := c.Detect(context.Background())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if d.Installed {
		t.Fatal("expected not installed")
	}
}

func assertCode(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %d, got nil", code)
	}
	if got := exit.CodeOf(err); got != code {
		t.Fatalf("exit code = %d, want %d (err: %v)", got, code, err)
	}
}
