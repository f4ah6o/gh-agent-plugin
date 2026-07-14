# gh-agent-plugin

A [GitHub CLI](https://cli.github.com) extension that manages **Claude Code** and
**Codex** plugins and marketplaces from a single, consistent UX.

Where `gh skill` manages individual Agent Skills, `gh agent-plugin` manages whole
**plugins** — bundles of skills, MCP servers, hooks, agents, commands, and apps —
by delegating install/remove/enable to each agent's **native** plugin manager
(`claude plugin …`, `codex plugin …`). It never re-implements those managers or
replaces their settings as the source of truth.

> Status: **Phase 1 (MVP)**. See [issue #1](https://github.com/f4ah6o/gh-agent-plugin/issues/1)
> for the full design and the Phase 2 roadmap.

## Install

```bash
gh extension install f4ah6o/gh-agent-plugin
```

## Usage

```text
gh agent-plugin <command> [arguments] [flags]
```

| Command                         | Description                                              |
|---------------------------------|----------------------------------------------------------|
| `install <source> <plugin>`     | Install a plugin (GitHub repo, `PLUGIN@MARKETPLACE`, or local) |
| `list`                          | List installed plugins across detected agents            |
| `remove <plugin>`               | Remove an installed plugin                               |
| `preview <source> <plugin>`     | Statically inspect a plugin (no code execution)          |
| `update [plugin] [--all]`       | Update plugins                                           |
| `marketplace add/list/update/remove` | Manage configured marketplaces                     |
| `doctor`                        | Diagnose the environment and agent plugin support        |
| `search <query>`                | Search public GitHub plugin and marketplace manifests   |
| `publish`                       | Phase 2 (not yet implemented)                            |

Aliases: `add`=`install`, `rm`/`uninstall`=`remove`, `ls`=`list`.

### Sources and selectors

```bash
gh agent-plugin install OWNER/REPO PLUGIN --agent claude-code
gh agent-plugin install PLUGIN@MARKETPLACE --agent codex
gh agent-plugin install ./path/to/repo PLUGIN --from-local
gh agent-plugin preview ./path/to/repo PLUGIN --from-local --json
gh agent-plugin search "code review" --limit 10 --json
gh agent-plugin search terraform --owner acme
```

### Phase 1 limitations

- `--ref` (pinning a GitHub source to a revision) is honored by both `preview` and
  `install`. The requested branch, tag, or commit SHA is checked out into the
  source cache, and the resolved revision is recorded. `install` registers that
  pinned checkout as a local marketplace so the native agent installs the
  reviewed revision. Marketplace selectors and local paths reject `--ref`.
- `preview` of a `OWNER/REPO` source clones the repo into a regenerable cache
  under `~/.cache/gh-agent-plugin/` and discovers it there; `--from-local` is no
  longer required. `install` of a remote source is still delegated to the native
  CLI's own resolution.
- `list` parses native JSON for both Claude Code and Codex. `marketplace list`
  parses Claude Code's `--json`; Codex exposes no machine-readable marketplace
  listing yet, so that case is reported as an explicit note rather than a silent
  empty result.

### Search

`search` uses GitHub code search to find public repositories containing Claude
Code or Codex plugin and marketplace manifests. It does not execute repository
content, invoke a native agent, or modify marketplace configuration.

```bash
gh agent-plugin search terraform
gh agent-plugin search "code review" --owner acme --limit 10 --page 2
gh agent-plugin search formatter --json
```

Use `repository` and `name` from a direct `plugin` result with
`gh agent-plugin install OWNER/REPO PLUGIN`. A `marketplace-plugin` result also
contains `marketplace` and a `selector` such as `formatter@company`; add its
`repository` as a marketplace before installing that selector.

Search supports `--owner`, `--limit`, `--page`, and `--json`. JSON output has a
stable `results` array with `type`, `name`, `repository`, `path`, `agents`, and
`description`, plus marketplace fields when applicable. Results are ranked by
manifest relevance and may be incomplete when GitHub reports an incomplete
search.

### Agent selection and scope

- `--agent` is repeatable; `--agent all` targets every detected agent.
- Bare `--all` is command-specific. For example, `update --all` updates every
  installed plugin on the selected agents; it does not widen agent selection.
- A single detected agent is implied; with multiple, `--agent` is required.
- `--scope user|project|local` is delegated to agents that support it. Agents that
  do not (e.g. Codex has no scopes) return an explicit error rather than ignoring it.

### Output

Every read command supports a stable `--json` output whose field names are treated
as an API. Mutations report results **per agent**, so partial success across agents
is visible (exit code `7`).

## Exit codes

| Code | Meaning                  | Code | Meaning              |
|------|--------------------------|------|----------------------|
| 0    | success                  | 5    | validation failed    |
| 1    | general error            | 6    | not found            |
| 2    | invalid arguments        | 7    | partial success      |
| 3    | target agent not installed | 8  | native CLI failure   |
| 4    | unsupported capability   |      |                      |

## Development

```bash
go vet ./...
go test ./...
go build -o gh-agent-plugin .
./gh-agent-plugin preview ./testdata/sample-repo example --from-local
```

The codebase uses only the Go standard library. Agent differences are isolated in
`internal/adapter` behind a `Runner` interface, so adapters are unit-tested without
the real `claude`/`codex` binaries.

## Release

Pushing a CalVer tag in `YYYY.M.P` form publishes precompiled GitHub CLI extension
binaries:

```bash
git tag 2026.7.1
git push origin 2026.7.1
gh release view 2026.7.1 --repo f4ah6o/gh-agent-plugin
gh extension install f4ah6o/gh-agent-plugin
```
