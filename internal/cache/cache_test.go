package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeGit is a Git that "clones" by writing a sentinel tree into dir and a fixed
// revision, recording the ref it was asked for.
type fakeGit struct {
	clones   int
	fetches  int
	lastRef  string
	revision string
	files    map[string]string // relative path -> contents written on clone
	cloneErr error
	fetchErr error
}

func (g *fakeGit) Clone(_ context.Context, _ /*repoURL*/, ref, dir string) error {
	if g.cloneErr != nil {
		return g.cloneErr
	}
	g.clones++
	g.lastRef = ref
	// Mark the directory as a git checkout so a reuse is detected.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		return err
	}
	for rel, content := range g.files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (g *fakeGit) Revision(context.Context, string) (string, error) {
	if g.revision == "" {
		return "0123456789abcdef0123456789abcdef01234567", nil
	}
	return g.revision, nil
}

func (g *fakeGit) Fetch(_ context.Context, _ string) error {
	g.fetches++
	return g.fetchErr
}

func TestCheckout_ClonesAndResolvesRevision(t *testing.T) {
	g := &fakeGit{revision: "abc123def4567890\n", files: map[string]string{"README.md": "hi"}}
	c := &Cache{Root: t.TempDir(), Git: g}

	dir, rev, err := c.Checkout(context.Background(), "acme/plugins", "v1.0.0")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if rev != "abc123def4567890" {
		t.Fatalf("revision = %q, want trimmed SHA", rev)
	}
	if g.lastRef != "v1.0.0" {
		t.Fatalf("clone ref = %q, want v1.0.0", g.lastRef)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "README.md")); err != nil || string(b) != "hi" {
		t.Fatalf("checkout missing cloned files: %v", err)
	}
}

func TestCheckout_ReusesExistingCheckout(t *testing.T) {
	g := &fakeGit{}
	c := &Cache{Root: t.TempDir(), Git: g}
	ctx := context.Background()

	if _, _, err := c.Checkout(ctx, "acme/plugins", ""); err != nil {
		t.Fatalf("first Checkout: %v", err)
	}
	if _, _, err := c.Checkout(ctx, "acme/plugins", ""); err != nil {
		t.Fatalf("second Checkout: %v", err)
	}
	if g.clones != 1 {
		t.Fatalf("clones = %d, want 1 (second call must reuse)", g.clones)
	}
}

func TestCheckout_DistinctRefsDistinctDirs(t *testing.T) {
	g := &fakeGit{}
	c := &Cache{Root: t.TempDir(), Git: g}
	ctx := context.Background()

	d1, _, err := c.Checkout(ctx, "acme/plugins", "main")
	if err != nil {
		t.Fatalf("Checkout main: %v", err)
	}
	d2, _, err := c.Checkout(ctx, "acme/plugins", "release/1.x")
	if err != nil {
		t.Fatalf("Checkout release/1.x: %v", err)
	}
	if d1 == d2 {
		t.Fatalf("distinct refs share a directory: %s", d1)
	}
	// A ref with a slash must not create nested directories beyond the repo path.
	if filepath.Base(d2) == "1.x" {
		t.Fatalf("ref slash leaked into the path: %s", d2)
	}
}

func TestCheckout_InvalidRepo(t *testing.T) {
	c := &Cache{Root: t.TempDir(), Git: &fakeGit{}}
	if _, _, err := c.Checkout(context.Background(), "not-a-repo", ""); err == nil {
		t.Fatal("expected error for invalid repository")
	}
}

func TestCheckout_DefaultBranch_FetchErrorPropagates(t *testing.T) {
	fetchErr := context.DeadlineExceeded // any non-UnsupportedCapability error
	g := &fakeGit{fetchErr: fetchErr}
	c := &Cache{Root: t.TempDir(), Git: g}
	ctx := context.Background()

	if _, _, err := c.Checkout(ctx, "acme/plugins", ""); err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	// Second call triggers Fetch; the error must propagate, not be swallowed.
	_, _, err := c.Checkout(ctx, "acme/plugins", "")
	if err == nil {
		t.Fatal("expected Checkout to fail when Fetch fails, got nil")
	}
}

func TestCheckout_DefaultBranchFetchesOnReuse(t *testing.T) {
	g := &fakeGit{}
	c := &Cache{Root: t.TempDir(), Git: g}
	ctx := context.Background()

	if _, _, err := c.Checkout(ctx, "acme/plugins", ""); err != nil {
		t.Fatalf("first Checkout: %v", err)
	}
	if _, _, err := c.Checkout(ctx, "acme/plugins", ""); err != nil {
		t.Fatalf("second Checkout: %v", err)
	}
	// Clone happens once; subsequent default-branch checkout fetches.
	if g.clones != 1 {
		t.Fatalf("clones = %d, want 1", g.clones)
	}
	if g.fetches != 1 {
		t.Fatalf("fetches = %d, want 1 (second default-branch checkout must refresh)", g.fetches)
	}
}

func TestCheckout_ImmutableRefNoFetch(t *testing.T) {
	g := &fakeGit{}
	c := &Cache{Root: t.TempDir(), Git: g}
	ctx := context.Background()

	if _, _, err := c.Checkout(ctx, "acme/plugins", "v1.0.0"); err != nil {
		t.Fatalf("first Checkout: %v", err)
	}
	if _, _, err := c.Checkout(ctx, "acme/plugins", "v1.0.0"); err != nil {
		t.Fatalf("second Checkout: %v", err)
	}
	if g.fetches != 0 {
		t.Fatalf("fetches = %d, want 0 (immutable ref must not trigger fetch)", g.fetches)
	}
}

func TestInvalidateCheckout_ForcesReclone(t *testing.T) {
	g := &fakeGit{}
	c := &Cache{Root: t.TempDir(), Git: g}
	ctx := context.Background()

	if _, _, err := c.Checkout(ctx, "acme/plugins", "v1.0.0"); err != nil {
		t.Fatalf("initial Checkout: %v", err)
	}
	if err := c.InvalidateCheckout("acme/plugins", "v1.0.0"); err != nil {
		t.Fatalf("InvalidateCheckout: %v", err)
	}
	if _, _, err := c.Checkout(ctx, "acme/plugins", "v1.0.0"); err != nil {
		t.Fatalf("Checkout after invalidation: %v", err)
	}
	if g.clones != 2 {
		t.Fatalf("clones = %d, want 2 (invalidation must force re-clone)", g.clones)
	}
}
