package cache

import (
	"bytes"
	"context"
	"os/exec"
)

// gitBin is the native git binary name.
const gitBin = "git"

// ExecGit is the production Git backed by os/exec. It performs shallow clones to
// keep the regenerable cache small and never executes repository code.
type ExecGit struct{}

// Clone shallow-clones repoURL at ref into dir. An empty ref clones the
// repository's default branch via `git clone`. A non-empty ref is fetched
// explicitly (`git init` + `git fetch --depth 1 origin <ref>` +
// `git checkout --detach FETCH_HEAD`) so that branch names, tag names, and raw
// commit SHAs all work — `git clone --branch` cannot check out a commit SHA,
// which is the primary pinning use case (issue #4 / PR review).
func (ExecGit) Clone(ctx context.Context, repoURL, ref, dir string) error {
	if ref == "" {
		return runGit(ctx, "", "clone", "--depth", "1", repoURL, dir)
	}
	if err := runGit(ctx, "", "init", "-q", dir); err != nil {
		return err
	}
	if err := runGit(ctx, dir, "remote", "add", "origin", repoURL); err != nil {
		return err
	}
	if err := runGit(ctx, dir, "fetch", "--depth", "1", "origin", ref); err != nil {
		return err
	}
	return runGit(ctx, dir, "checkout", "--detach", "-q", "FETCH_HEAD")
}

// Revision returns the commit SHA currently checked out in dir.
func (ExecGit) Revision(ctx context.Context, dir string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, gitBin, "-C", dir, "rev-parse", "HEAD")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, gitBin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run()
}
