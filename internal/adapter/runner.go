package adapter

import (
	"bytes"
	"context"
	"os/exec"
)

// Runner abstracts execution of a native CLI so adapters can be unit-tested
// without the real claude/codex binaries present.
type Runner interface {
	// Run executes name with args and returns stdout, stderr, and any error.
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
	// Look reports the resolved path of name, or an error if not found.
	Look(name string) (string, error)
}

// ExecRunner is the production Runner backed by os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

func (ExecRunner) Look(name string) (string, error) { return exec.LookPath(name) }

// Call records a single Run invocation for assertions in tests.
type Call struct {
	Name string
	Args []string
}

// RecordingRunner is a fake Runner for tests. It records each call and returns
// canned output keyed by the joined argv, falling back to Default*.
type RecordingRunner struct {
	Calls []Call

	// LookPaths maps a binary name to the path Look should return. A missing
	// entry makes Look return ErrNotFound.
	LookPaths map[string]string

	// Responses maps an argv prefix match (space-joined) to canned output.
	Stdout     map[string]string
	Stderr     map[string]string
	Errs       map[string]error
	DefaultErr error
}

// ErrNotFound is returned by RecordingRunner.Look for unregistered binaries.
var ErrNotFound = exec.ErrNotFound

func (r *RecordingRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	r.Calls = append(r.Calls, Call{Name: name, Args: append([]string(nil), args...)})
	key := name
	for _, a := range args {
		key += " " + a
	}
	var out, errOut string
	var err error
	if r.Stdout != nil {
		out = r.Stdout[key]
	}
	if r.Stderr != nil {
		errOut = r.Stderr[key]
	}
	if r.Errs != nil {
		if e, ok := r.Errs[key]; ok {
			err = e
		} else {
			err = r.DefaultErr
		}
	} else {
		err = r.DefaultErr
	}
	return []byte(out), []byte(errOut), err
}

func (r *RecordingRunner) Look(name string) (string, error) {
	if r.LookPaths != nil {
		if p, ok := r.LookPaths[name]; ok {
			return p, nil
		}
	}
	return "", ErrNotFound
}
