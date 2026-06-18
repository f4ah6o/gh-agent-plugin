// Package cache manages regenerable local checkouts of GitHub plugin sources
// under the user cache directory (e.g. ~/.cache/gh-agent-plugin/). Cloning a
// OWNER/REPO@ref makes the repository's files available for static discovery and
// validation without requiring the caller to pre-clone, which is what the
// Phase 1 `preview` of a GitHub source required (issue #4, Phase 2 GitHub-source
// path).
//
// Everything under the cache root is safe to delete at any time; it is rebuilt
// on demand. The native agent settings remain the source of truth (issue #1).
package cache

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

// Git abstracts the git operations the cache needs so the clone/resolve path can
// be unit-tested without the real binary or network access. It mirrors the
// Runner pattern used by the adapters.
type Git interface {
	// Clone fetches repoURL at ref into dir, which the cache guarantees is a
	// fresh, empty directory. An empty ref selects the repository default branch.
	Clone(ctx context.Context, repoURL, ref, dir string) error
	// Revision resolves the checked-out commit SHA of the repository in dir.
	Revision(ctx context.Context, dir string) (string, error)
}

// Cache resolves GitHub sources into local checkouts rooted at Root.
type Cache struct {
	Root string
	Git  Git
}

// New builds a Cache. A nil git uses the production ExecGit; an empty root uses
// <user-cache-dir>/gh-agent-plugin.
func New(root string, git Git) (*Cache, error) {
	if git == nil {
		git = ExecGit{}
	}
	if root == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, exit.Errorf(exit.GeneralError, "cannot determine user cache directory: %v", err)
		}
		root = filepath.Join(base, "gh-agent-plugin")
	}
	return &Cache{Root: root, Git: git}, nil
}

// Checkout ensures OWNER/REPO@ref is present locally and returns the checkout
// directory and the resolved commit revision. An empty ref selects the default
// branch. A checkout already present in the cache is reused (the cache is
// regenerable, so a stale reuse is corrected by deleting the cache root).
func (c *Cache) Checkout(ctx context.Context, repo, ref string) (dir string, revision string, err error) {
	if !validRepo(repo) {
		return "", "", exit.Errorf(exit.InvalidArguments, "invalid repository %q, expected OWNER/REPO", repo)
	}
	dir = c.checkoutDir(repo, ref)
	if !isGitCheckout(dir) {
		// Clone into a temporary sibling and rename into place, so an interrupted
		// clone never leaves a half-populated directory that isGitCheckout would
		// later mistake for a complete checkout.
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", "", exit.Errorf(exit.GeneralError, "cannot create cache directory: %v", err)
		}
		tmp, err := os.MkdirTemp(filepath.Dir(dir), ".clone-*")
		if err != nil {
			return "", "", exit.Errorf(exit.GeneralError, "cannot create temporary clone directory: %v", err)
		}
		if err := c.Git.Clone(ctx, repoURL(repo), ref, tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return "", "", exit.Errorf(exit.NativeCLIFailure, "git clone %s failed: %v", repo, err)
		}
		_ = os.RemoveAll(dir)
		if err := os.Rename(tmp, dir); err != nil {
			_ = os.RemoveAll(tmp)
			return "", "", exit.Errorf(exit.GeneralError, "cannot finalize checkout: %v", err)
		}
	}
	rev, err := c.Git.Revision(ctx, dir)
	if err != nil {
		return "", "", exit.Errorf(exit.NativeCLIFailure, "cannot resolve revision for %s: %v", repo, err)
	}
	return dir, strings.TrimSpace(rev), nil
}

// checkoutDir is the deterministic on-disk location for a repo@ref checkout.
func (c *Cache) checkoutDir(repo, ref string) string {
	parts := strings.SplitN(repo, "/", 2)
	refKey := ref
	if refKey == "" {
		refKey = "HEAD"
	}
	return filepath.Join(c.Root, "sources", parts[0], parts[1], sanitizeRef(refKey))
}

// sanitizeRef makes a ref safe to use as a single path element (refs may contain
// slashes, e.g. "release/1.x").
func sanitizeRef(ref string) string {
	return strings.NewReplacer("/", "-", string(filepath.Separator), "-").Replace(ref)
}

func repoURL(repo string) string {
	return "https://github.com/" + repo + ".git"
}

func isGitCheckout(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

func validRepo(s string) bool {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}
