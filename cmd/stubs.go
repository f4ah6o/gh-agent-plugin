package cmd

import "github.com/f4ah6o/gh-agent-plugin/internal/exit"

// runPublish is a Phase 2 feature.
func runPublish(args []string, env *Env) error {
	return exit.Errorf(exit.InvalidArguments, "publish is not implemented in Phase 1 (tracked for Phase 2)")
}
