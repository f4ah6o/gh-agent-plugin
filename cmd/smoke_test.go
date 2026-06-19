//go:build !windows

// Package cmd smoke tests exercise the built binary against fake claude, codex,
// and git binaries on PATH, verifying real CLI surface rather than unit seams.
// They are skipped when go test -short is passed.
package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles the module root into a temporary binary and returns its
// path. The binary is automatically removed when the test ends.
func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "gh-agent-plugin")
	// The module root is one directory up from the cmd package.
	root := filepath.Join("..")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return binary
}

// makeFakeAgents creates minimal shell-script fakes for claude and codex in a
// temporary directory and returns that directory. Each fake script accepts the
// sub-commands used by the smoke tests and emits appropriate output.
func makeFakeAgents(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeScript := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}

	// claude: supports plugin list --json and marketplace list --json.
	writeScript("claude", `
case "$*" in
  "--version") echo "claude 1.0.0-fake" ;;
  "plugin list --json")
    echo '[{"name":"hello","marketplace":"acme","version":"1.0.0","scope":"user","enabled":true}]' ;;
  "plugin marketplace list --json")
    echo '[{"name":"acme","type":"github","url":"owner/repo"}]' ;;
  "plugin marketplace add "*)
    exit 0 ;;
  "plugin install "*)
    exit 0 ;;
  *) exit 0 ;;
esac
`)

	// codex: supports plugin list --json.
	writeScript("codex", `
case "$*" in
  "--version") echo "codex 0.9.0-fake" ;;
  "plugin list --json")
    echo '[{"id":"widget@store","name":"widget","marketplace":"store","version":"2.0.0","status":"installed"}]' ;;
  "plugin add "*)
    exit 0 ;;
  "plugin marketplace add "*)
    exit 0 ;;
  *) exit 0 ;;
esac
`)

	return dir
}

// smokeEnv prepares PATH for a smoke test and returns the built binary path.
func smokeEnv(t *testing.T) (binary string, env []string) {
	t.Helper()
	if testing.Short() {
		t.Skip("smoke tests skipped in -short mode")
	}
	binary = buildBinary(t)
	fakeDir := makeFakeAgents(t)
	env = append(os.Environ(), "PATH="+fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return binary, env
}

// runSmoke runs the binary with the given args and smoke PATH, returning stdout
// and stderr as strings and the exit code.
func runSmoke(t *testing.T, binary string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	}
	return out.String(), errBuf.String(), code
}

func TestSmoke_List_JSON(t *testing.T) {
	binary, env := smokeEnv(t)
	stdout, stderr, code := runSmoke(t, binary, env, "list", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}
	var got struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, stdout)
	}
	if len(got.Plugins) == 0 {
		t.Fatalf("expected at least one plugin in JSON output\nstdout: %s", stdout)
	}
}

func TestSmoke_List_Table(t *testing.T) {
	binary, env := smokeEnv(t)
	stdout, stderr, code := runSmoke(t, binary, env, "list")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "hello") && !strings.Contains(stdout, "widget") {
		t.Fatalf("expected plugin names in table output\nstdout: %s", stdout)
	}
}

func TestSmoke_Install_MarketplaceSelector(t *testing.T) {
	binary, env := smokeEnv(t)
	// A marketplace selector (PLUGIN@MARKETPLACE) must install without a
	// registration step and exit 0.
	_, stderr, code := runSmoke(t, binary, env, "install", "hello@acme", "--agent", "claude-code")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}
}

func TestSmoke_Install_GitHubSource_CodexFallback(t *testing.T) {
	binary, env := smokeEnv(t)
	// Codex can't enumerate marketplaces, so it falls back to bare name.
	// The install should still succeed.
	_, stderr, code := runSmoke(t, binary, env, "install", "owner/repo", "widget", "--agent", "codex")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}
}

func TestSmoke_Install_ReservedFlags_Warn(t *testing.T) {
	binary, env := smokeEnv(t)
	_, stderr, _ := runSmoke(t, binary, env, "install", "hello@acme", "--agent", "claude-code", "--jq", ".results")
	if !strings.Contains(stderr, "--jq") {
		t.Errorf("expected --jq warning in stderr:\n%s", stderr)
	}
}

func TestSmoke_Preview_Local(t *testing.T) {
	binary, env := smokeEnv(t)
	// The sample repo has blocking findings; exit 5 is expected.
	_, stderr, code := runSmoke(t, binary, env, "preview", "../testdata/sample-repo", "example", "--from-local")
	if code != 5 {
		t.Fatalf("exit = %d, want 5 (validation failed)\nstderr: %s", code, stderr)
	}
}

func TestSmoke_Doctor(t *testing.T) {
	binary, env := smokeEnv(t)
	stdout, stderr, code := runSmoke(t, binary, env, "doctor")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "claude") && !strings.Contains(stdout, "codex") {
		t.Fatalf("doctor output missing agent info\nstdout: %s", stdout)
	}
}

func TestSmoke_UnknownCommand(t *testing.T) {
	binary, env := smokeEnv(t)
	_, _, code := runSmoke(t, binary, env, "frobnicate")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (invalid arguments)", code)
	}
}
