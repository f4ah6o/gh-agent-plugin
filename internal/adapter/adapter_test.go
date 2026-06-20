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

func TestCodexListParsesTable(t *testing.T) {
	table := "Marketplace `company`\n" +
		"/path/to/marketplace.json\n" +
		"\n" +
		"PLUGIN                            STATUS              VERSION       PATH\n" +
		"formatter@company                 installed, enabled  1.2.0         /path/to/formatter\n" +
		"other@company                     not installed                     /path/to/other\n"
	r := &RecordingRunner{
		LookPaths: map[string]string{"codex": "/usr/bin/codex"},
		Stdout: map[string]string{
			"codex plugin list": table,
		},
	}
	c := NewCodex(r)
	plugins, err := c.ListPlugins(context.Background(), ListRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}
	p := plugins[0]
	if p.ID != "formatter@company" || p.Name != "formatter" || p.Marketplace != "company" || p.Version != "1.2.0" || p.Agent != "codex" || !p.Enabled {
		t.Fatalf("unexpected plugin[0]: %+v", p)
	}
	p2 := plugins[1]
	if p2.ID != "other@company" || p2.Status != "not installed" || p2.Enabled {
		t.Fatalf("unexpected plugin[1]: %+v", p2)
	}
}

func TestClaudeListParsesJSON(t *testing.T) {
	r := &RecordingRunner{
		LookPaths: map[string]string{"claude": "/usr/bin/claude"},
		Stdout: map[string]string{
			"claude plugin list --json":             `[{"id":"formatter@company","version":"1.2.0","scope":"user","enabled":true}]`,
			"claude plugin marketplace list --json": `[{"name":"company","type":"github","url":"github.com/acme/plugins"}]`,
		},
	}
	c := NewClaude(r)

	plugins, err := c.ListPlugins(context.Background(), ListRequest{})
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(plugins) != 1 || plugins[0].ID != "formatter@company" || plugins[0].Agent != "claude-code" || plugins[0].Version != "1.2.0" {
		t.Fatalf("unexpected plugin: %+v", plugins)
	}

	markets, err := c.ListMarketplaces(context.Background())
	if err != nil {
		t.Fatalf("ListMarketplaces: %v", err)
	}
	if len(markets) != 1 || markets[0].Name != "company" {
		t.Fatalf("unexpected marketplaces: %+v", markets)
	}
}

func TestClaudeListEmptyJSON(t *testing.T) {
	// Empty native output must parse as an empty slice, not an error.
	c := NewClaude(&RecordingRunner{
		LookPaths: map[string]string{"claude": "/usr/bin/claude"},
		Stdout:    map[string]string{"claude plugin list --json": "[]"},
	})
	plugins, err := c.ListPlugins(context.Background(), ListRequest{})
	if err != nil || len(plugins) != 0 {
		t.Fatalf("expected empty plugins, got %v, err=%v", plugins, err)
	}
}

func TestClaudeUpdateUsesUpdateVerb(t *testing.T) {
	r := &RecordingRunner{LookPaths: map[string]string{"claude": "/usr/bin/claude"}}
	c := NewClaude(r)
	if err := c.UpdatePlugin(context.Background(), UpdateRequest{Plugin: "formatter@company"}); err != nil {
		t.Fatalf("UpdatePlugin: %v", err)
	}
	if got := argvOf(r.Calls[0]); got != "claude plugin update formatter@company" {
		t.Fatalf("argv = %q", got)
	}
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
