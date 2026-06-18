// Command gh-agent-plugin is a GitHub CLI extension that manages Claude Code and
// Codex plugins and marketplaces through a single UX, delegating install/remove
// to each agent's native plugin manager (issue #1).
package main

import (
	"os"

	"github.com/f4ah6o/gh-agent-plugin/cmd"
)

func main() {
	os.Exit(cmd.Main(os.Args[1:]))
}
