package cmd

import "github.com/f4ah6o/gh-agent-plugin/internal/exit"

// runSearch is a Phase 2 feature; it reports an explicit not-implemented error
// rather than pretending to succeed (issue #1, MVP scope).
func runSearch(args []string, env *Env) error {
	return exit.Errorf(exit.InvalidArguments, "search is not implemented in Phase 1 (tracked for Phase 2)")
}

// runPublish is a Phase 2 feature.
func runPublish(args []string, env *Env) error {
	return exit.Errorf(exit.InvalidArguments, "publish is not implemented in Phase 1 (tracked for Phase 2)")
}
