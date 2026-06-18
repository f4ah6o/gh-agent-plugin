package cache

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckoutDir_NoRefCollision guards the cache key against refs that sanitize
// to the same display string (the PR-review collision case).
func TestCheckoutDir_NoRefCollision(t *testing.T) {
	c := &Cache{Root: "/cache"}
	a := c.checkoutDir("acme/plugins", "release/1.x")
	b := c.checkoutDir("acme/plugins", "release-1.x")
	if a == b {
		t.Fatalf("refs collided to the same dir: %s", a)
	}
	// The same ref must map to a stable directory.
	if got := c.checkoutDir("acme/plugins", "release/1.x"); got != a {
		t.Fatalf("checkoutDir is not stable: %s != %s", got, a)
	}
}

// TestExecGit_Clone exercises the real git binary against a local source repo so
// the branch/tag/SHA fetch paths are validated without network access.
func TestExecGit_Clone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	src := t.TempDir()

	mustGit := func(dir string, args ...string) string {
		t.Helper()
		// Disable commit/tag signing: some environments force it on globally, which
		// would fail this isolated fixture repo.
		full := append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)
		cmd := exec.Command("git", full...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	mustGit(src, "init", "-q", "-b", "main")
	// Allow fetching an arbitrary SHA from this local repo over the file transport.
	mustGit(src, "config", "uploadpack.allowAnySHA1InWant", "true")
	mustGit(src, "config", "uploadpack.allowReachableSHA1InWant", "true")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(src, "add", "README.md")
	mustGit(src, "commit", "-q", "-m", "init")
	mustGit(src, "tag", "v1.0.0")
	sha := mustGit(src, "rev-parse", "HEAD")

	for _, ref := range []string{"", "main", "v1.0.0", sha} {
		t.Run("ref="+refLabel(ref), func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "dest")
			if err := (ExecGit{}).Clone(ctx, src, ref, dest); err != nil {
				t.Fatalf("Clone(ref=%q): %v", ref, err)
			}
			if b, err := os.ReadFile(filepath.Join(dest, "README.md")); err != nil || string(b) != "hi" {
				t.Fatalf("cloned tree missing README: %v", err)
			}
			rev, err := (ExecGit{}).Revision(ctx, dest)
			if err != nil {
				t.Fatalf("Revision: %v", err)
			}
			if strings.TrimSpace(rev) != sha {
				t.Fatalf("revision = %q, want %q", strings.TrimSpace(rev), sha)
			}
		})
	}
}

func refLabel(ref string) string {
	if ref == "" {
		return "default"
	}
	return ref
}
