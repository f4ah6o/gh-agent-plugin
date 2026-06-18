package discovery

import (
	"path/filepath"
	"runtime"
	"testing"
)

// sampleRepo returns the path to the committed sample dual-target repository.
func sampleRepo(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "sample-repo")
}

func TestDiscoverPlugin_DualTarget(t *testing.T) {
	dp, err := DiscoverPlugin(sampleRepo(t), "example")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := len(dp.Agents); got != 2 {
		t.Fatalf("expected 2 agents, got %d (%v)", got, dp.Agents)
	}
	if _, ok := dp.Manifests["claude-code"]; !ok {
		t.Error("missing claude-code manifest")
	}
	if _, ok := dp.Manifests["codex"]; !ok {
		t.Error("missing codex manifest")
	}
	if want := []string{"greeter"}; !equal(dp.Skills, want) {
		t.Errorf("skills = %v, want %v", dp.Skills, want)
	}
	if !equal(dp.Commands, []string{"hello.md"}) {
		t.Errorf("commands = %v", dp.Commands)
	}
	if !equal(dp.AgentsConfig, []string{"reviewer.md"}) {
		t.Errorf("agentsConfig = %v", dp.AgentsConfig)
	}
	if !equal(dp.MCPServers, []string{"local-tool", "remote-tool"}) {
		t.Errorf("mcpServers = %v", dp.MCPServers)
	}
	if !equal(dp.Apps, []string{".app.json"}) {
		t.Errorf("apps = %v", dp.Apps)
	}
}

func TestDiscoverPlugin_NotFound(t *testing.T) {
	if _, err := DiscoverPlugin(sampleRepo(t), "does-not-exist"); err == nil {
		t.Fatal("expected error for missing plugin")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
