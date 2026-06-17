// Package exit defines the stable process exit codes used across the CLI and a
// CodedError type that carries an exit code alongside a message.
//
// Exit codes follow issue #1 exactly and are treated as a stable API.
package exit

import "fmt"

// Exit codes per issue #1. These are a stable contract; do not renumber.
const (
	OK                    = 0 // success
	GeneralError          = 1 // general error
	InvalidArguments      = 2 // invalid arguments
	AgentNotInstalled     = 3 // target agent not installed
	UnsupportedCapability = 4 // unsupported capability
	ValidationFailed      = 5 // validation failed
	NotFound              = 6 // plugin or marketplace not found
	PartialSuccess        = 7 // partial success
	NativeCLIFailure      = 8 // native CLI failure
)

// CodedError pairs an error message with a process exit code.
type CodedError struct {
	Code int
	Msg  string
}

func (e *CodedError) Error() string { return e.Msg }

// Errorf builds a CodedError with a formatted message.
func Errorf(code int, format string, args ...any) *CodedError {
	return &CodedError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// CodeOf returns the exit code associated with err. A nil error is OK; a
// CodedError returns its Code; any other error is a GeneralError.
func CodeOf(err error) int {
	if err == nil {
		return OK
	}
	if ce, ok := err.(*CodedError); ok {
		return ce.Code
	}
	return GeneralError
}
