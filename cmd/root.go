// Package cmd implements the `gh agent-plugin` command surface using only the
// standard library. Execute dispatches a subcommand; each subcommand lives in
// its own file and shares the helpers defined here.
package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/cache"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

// defaultTimeout is the maximum time any command is allowed to run before
// being cancelled. It guards against hung native CLIs and network operations.
const defaultTimeout = 5 * time.Minute

// Env carries the I/O streams, adapter registry, and source cache a command
// needs. It is injected so tests can swap streams, use a fake runner, and use a
// fake git for the GitHub-source cache.
type Env struct {
	Ctx    context.Context
	Stdout io.Writer
	Stderr io.Writer
	Reg    *adapter.Registry
	Cache  *cache.Cache
	Runner adapter.Runner
}

// commandFunc is the signature every subcommand implements.
type commandFunc func(args []string, env *Env) error

// aliases maps user-facing command aliases to canonical names (issue #1).
var aliases = map[string]string{
	"add":       "install",
	"uninstall": "remove",
	"rm":        "remove",
	"ls":        "list",
}

// commands is the canonical subcommand table.
var commands = map[string]commandFunc{
	"install":     runInstall,
	"list":        runList,
	"remove":      runRemove,
	"preview":     runPreview,
	"update":      runUpdate,
	"marketplace": runMarketplace,
	"doctor":      runDoctor,
	"search":      runSearch,
	"publish":     runPublish,
	"issue":       runIssue,
	"pr":          runPR,
}

// Main is the process entry point. It builds a production Env and returns the
// exit code. The context is left deadline-free here so that applyTimeout (called
// early in each command) can set either --timeout or the defaultTimeout without
// the flag being capped by a pre-existing parent deadline.
func Main(args []string) int {
	env := &Env{
		Ctx:    context.Background(),
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Reg:    adapter.NewRegistry(adapter.ExecRunner{}),
		Runner: adapter.ExecRunner{},
	}
	// A nil cache here defers the "cannot determine cache dir" error to the one
	// command (preview of a GitHub source) that actually needs it, so the rest of
	// the CLI keeps working even in an environment without a usable cache dir.
	if c, err := cache.New("", nil); err == nil {
		env.Cache = c
	}
	return Execute(args, env)
}

// Execute dispatches args[0] as a subcommand using env, returning an exit code.
func Execute(args []string, env *Env) int {
	if len(args) == 0 {
		printUsage(env.Stderr)
		return exit.InvalidArguments
	}
	name := args[0]
	if name == "-h" || name == "--help" || name == "help" {
		printUsage(env.Stdout)
		return exit.OK
	}
	if canonical, ok := aliases[name]; ok {
		name = canonical
	}
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(env.Stderr, "unknown command %q\n\n", args[0])
		printUsage(env.Stderr)
		return exit.InvalidArguments
	}
	if err := cmd(args[1:], env); err != nil {
		fmt.Fprintln(env.Stderr, "error:", err.Error())
		return exit.CodeOf(err)
	}
	return exit.OK
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `gh agent-plugin - manage Claude Code and Codex plugins and marketplaces

Usage:
  gh agent-plugin <command> [arguments] [flags]

Commands:
  install     Install a plugin from a GitHub repo, marketplace selector, or local path
  list        List installed plugins across detected agents
  remove      Remove an installed plugin
  preview     Statically inspect a plugin without executing it
  update      Update plugins
  marketplace Manage configured marketplaces (add/list/update/remove)
  doctor      Diagnose the environment and agent plugin support
  issue       Manage GitHub issues (list|view|comment)
  pr          Manage GitHub pull requests (comment)
  search      (phase 2) Search marketplaces
  publish     (phase 2) Publish a plugin

Aliases: add=install, rm/uninstall=remove, ls=list
`)
}

// stringList is a repeatable string flag (e.g. --agent claude-code --agent codex).
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// commonFlags holds the global flags shared by most commands (issue #1).
type commonFlags struct {
	agents     stringList
	all        bool
	scope      string
	ref        string
	fromLocal  bool
	dryRun     bool
	force      bool
	yes        bool
	jsonOut    bool
	jsonFields string
	jq         string
	template   string
	timeout    time.Duration
}

// register wires the common flags onto fs. Individual commands ignore the flags
// that do not apply to them.
func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.Var(&c.agents, "agent", "target agent (repeatable): claude-code|codex|all")
	fs.BoolVar(&c.all, "all", false, "apply to all detected agents")
	fs.StringVar(&c.scope, "scope", "", "scope: user|project|local")
	fs.StringVar(&c.ref, "ref", "", "git ref for a GitHub source")
	fs.BoolVar(&c.fromLocal, "from-local", false, "treat the source as a local path")
	fs.BoolVar(&c.dryRun, "dry-run", false, "show actions without executing them")
	fs.BoolVar(&c.force, "force", false, "force the operation")
	fs.BoolVar(&c.yes, "yes", false, "assume yes for confirmations")
	fs.BoolVar(&c.jsonOut, "json", false, "emit JSON output")
	fs.StringVar(&c.jsonFields, "json-fields", "", "comma-separated JSON fields to include")
	fs.StringVar(&c.jq, "jq", "", "jq filter applied to JSON output (reserved, not yet implemented)")
	fs.StringVar(&c.template, "template", "", "Go template applied to output (reserved, not yet implemented)")
	fs.DurationVar(&c.timeout, "timeout", 0, "override the default operation timeout (e.g. 30s, 2m)")
}

// applyTimeout derives a new context with the effective timeout, replacing
// env.Ctx, and returns a cancel function the caller must defer. It always
// applies a deadline: --timeout if given, otherwise defaultTimeout. Because
// Main leaves env.Ctx deadline-free, this is the only place a deadline is set,
// so --timeout can genuinely override (not merely shorten) the default.
func (c *commonFlags) applyTimeout(env *Env) func() {
	d := c.timeout
	if d <= 0 {
		d = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(env.Ctx, d)
	env.Ctx = ctx
	return cancel
}

// rejectReservedFlags rejects output-filtering flags that are parsed for future
// compatibility but are not implemented. FlagSet.Visit distinguishes an
// explicitly supplied empty value (for example, --jq=) from an omitted flag.
func (c *commonFlags) rejectReservedFlags(fs *flag.FlagSet) error {
	var supplied = map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		supplied[f.Name] = true
	})
	for _, name := range []string{"jq", "template", "json-fields"} {
		if supplied[name] {
			return exit.Errorf(exit.InvalidArguments,
				"unsupported flag --%s: output filtering is not implemented", name)
		}
	}
	return nil
}

// requirePositionals enforces a command's positional-argument arity after
// parseArgs has collected positionals while allowing flags to be interspersed.
func requirePositionals(command string, args []string, min, max int) error {
	if len(args) >= min && len(args) <= max {
		return nil
	}
	expected := fmt.Sprintf("between %d and %d", min, max)
	if min == max {
		expected = fmt.Sprintf("exactly %d", min)
	}
	msg := fmt.Sprintf("%s expects %s positional argument(s), got %d", command, expected, len(args))
	if len(args) > max {
		msg += "; unexpected: " + strings.Join(args[max:], " ")
	}
	return exit.Errorf(exit.InvalidArguments, "%s", msg)
}

// newFlagSet returns a FlagSet that writes errors to env.Stderr and does not
// call os.Exit on parse errors.
func newFlagSet(name string, env *Env) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs
}

// parseArgs parses args allowing flags to be interspersed with positional
// arguments (e.g. "install OWNER/REPO PLUGIN --agent codex"), which the standard
// library flag package does not support on its own. It returns the collected
// positional arguments in order.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
}

// wantsAll reports whether the user asked to target every detected agent. Agent
// fan-out is requested only via `--agent all`; the bare `--all` flag is
// command-specific (e.g. "all plugins" for update) and must not widen the agent
// set, so it is intentionally not consulted here.
func (c *commonFlags) wantsAll() bool {
	for _, a := range c.agents {
		if a == "all" {
			return true
		}
	}
	return false
}

// selectTargets resolves the agents a fan-out command should act on, defaulting
// to every detected agent when no --agent is given (like list), instead of
// requiring a selection. Explicit --agent values are honored via selectAdapters.
func (c *commonFlags) selectTargets(env *Env) ([]adapter.Adapter, error) {
	if len(c.agents) == 0 {
		installed := env.Reg.Installed(env.Ctx)
		if len(installed) == 0 {
			return nil, exit.Errorf(exit.AgentNotInstalled, "no supported agent CLI detected")
		}
		return installed, nil
	}
	return c.selectAdapters(env)
}

// selectAdapters resolves which adapters a command should act on, applying the
// agent-selection rules from issue #1.
func (c *commonFlags) selectAdapters(env *Env) ([]adapter.Adapter, error) {
	detected := env.Reg.DetectAll(env.Ctx)
	installed := map[string]adapter.Adapter{}
	var installedList []adapter.Adapter
	for _, d := range detected {
		if d.Detection.Installed {
			installed[d.Adapter.ID()] = d.Adapter
			installedList = append(installedList, d.Adapter)
		}
	}

	if c.wantsAll() {
		if len(installedList) == 0 {
			return nil, exit.Errorf(exit.AgentNotInstalled, "no supported agent CLI detected")
		}
		return installedList, nil
	}

	// Explicit --agent values (other than "all").
	var explicit []string
	for _, a := range c.agents {
		if a != "all" {
			explicit = append(explicit, a)
		}
	}
	if len(explicit) > 0 {
		var out []adapter.Adapter
		for _, id := range explicit {
			ad := env.Reg.Get(id)
			if ad == nil {
				return nil, exit.Errorf(exit.InvalidArguments, "unknown agent %q", id)
			}
			if _, ok := installed[id]; !ok {
				return nil, exit.Errorf(exit.AgentNotInstalled, "agent %s is not installed", id)
			}
			out = append(out, ad)
		}
		return out, nil
	}

	// No agent specified: unique installed agent is implied; otherwise require one.
	switch len(installedList) {
	case 0:
		return nil, exit.Errorf(exit.AgentNotInstalled, "no supported agent CLI detected")
	case 1:
		return installedList, nil
	default:
		return nil, exit.Errorf(exit.InvalidArguments, "multiple agents detected; specify --agent")
	}
}

// finalize maps a set of per-agent outcomes to a single error/exit code:
//   - all succeeded            -> nil
//   - some succeeded, some not -> partial success (7)
//   - all failed, shared code  -> that code (e.g. unsupported capability 4)
//   - all failed, mixed codes  -> native CLI failure (8)
func finalize(action string, results []agentResult, errs []error) error {
	if len(results) == 0 {
		// No targets acted on (e.g. nothing to do); treat as success rather than
		// synthesizing a misleading failure code.
		return nil
	}
	var ok, fail int
	for _, r := range results {
		if r.OK {
			ok++
		} else {
			fail++
		}
	}
	if fail == 0 {
		return nil
	}
	if ok > 0 {
		return exit.Errorf(exit.PartialSuccess, "%s partially succeeded; see per-agent results", action)
	}
	code := -1
	for _, e := range errs {
		if e == nil {
			continue
		}
		c := exit.CodeOf(e)
		if code == -1 {
			code = c
		} else if code != c {
			code = exit.NativeCLIFailure
			break
		}
	}
	if code == -1 {
		code = exit.NativeCLIFailure
	}
	return exit.Errorf(code, "%s failed for all targeted agents", action)
}

// sortedAgentIDs returns adapter IDs sorted for stable output.
func sortedAgentIDs(ads []adapter.Adapter) []string {
	ids := make([]string, 0, len(ads))
	for _, a := range ads {
		ids = append(ids, a.ID())
	}
	sort.Strings(ids)
	return ids
}
