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

// Clone shallow-clones repoURL at ref into dir. A non-empty ref is passed via
// --branch, which resolves branch and tag names; an empty ref clones the
// repository's default branch.
func (ExecGit) Clone(ctx context.Context, repoURL, ref, dir string) error {
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, repoURL, dir)
	return runGit(ctx, "", args...)
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
