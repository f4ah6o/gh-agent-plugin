# Implement plugin search command

Status: done
Model: unknown
Created: 2026-07-13
Updated: 2026-07-14
Branch: feat/20260713-implement-search

## 概要

Implement the read-only `gh agent-plugin search <query>` command to discover installable plugins and marketplaces in public GitHub repositories. The command must expose the repository and plugin metadata needed to continue with `install`, with stable table and JSON output.

## 背景

The current command table advertises `search`, but `cmd/stubs.go` returns an invalid-arguments error stating that the command is not implemented. The parent design defines `search <query> [flags]` as a Phase 2 command and explicitly excludes building a central registry. The repository already defines Claude Code and Codex plugin and marketplace manifest locations in its discovery model.

The behavior should follow the discovery-oriented UX of [`gh skill search`](https://cli.github.com/manual/gh_skill_search) while returning plugin-specific metadata. See the parent design in [issue #1](https://github.com/f4ah6o/gh-agent-plugin/issues/1).

## 問題

Users cannot discover a plugin before installing it through a known repository or marketplace selector. The current stub also accepts no useful query or output contract, so scripts and documentation cannot use `search` to find a valid `OWNER/REPO` and plugin selector.

## 目標

- Search public GitHub repositories for supported Claude Code and Codex plugin or marketplace manifests whose name or description matches the query.
- Support an optional owner filter, bounded pagination, and machine-readable JSON output.
- Report the plugin name, source repository, manifest path, supported agents, and description when available so the result can be used as input to `install` or `preview`.
- Keep the command read-only: it must not invoke a native agent plugin manager, execute plugin code, modify configured marketplaces, or write installation state.
- Document the command syntax, flags, result fields, and the public-GitHub search scope.

## 対象外

- Creating or operating a central plugin registry or marketplace service.
- Installing, previewing, publishing, or interactively selecting a result.
- Searching private repositories or adding a separate credential-management flow.
- Replacing the existing native Claude Code or Codex plugin-manager adapters.
- Implementing the `publish` command or unrelated output-filtering flags such as `--jq` and `--template`.

## 提案する方針

- Replace `runSearch` in `cmd/stubs.go` with a dedicated implementation, keeping `search <query>` at exactly one positional argument and allowing flags to be interspersed with the query.
- Add an injectable GitHub search client or runner boundary so command tests do not require network access. Use the GitHub search API or the existing GitHub CLI integration available to the extension, and map authentication, rate-limit, malformed-response, and transport failures to actionable non-zero errors.
- Search the repository and path conventions already used by discovery, including `.claude-plugin/plugin.json`, `.codex-plugin/plugin.json`, and the supported marketplace manifest locations. Parse only the metadata needed for search results; do not execute repository content.
- Deduplicate results by repository, manifest path, and plugin identity while preserving the service's relevance order. Define a stable result schema rooted at `results`, with fields for `name`, `repository`, `path`, `agents`, and `description`.
- Provide `--owner`, `--limit`, `--page`, and `--json` flags. Validate positive pagination values and reject unsupported output-filtering flags using the existing common-flag validation.
- Render a documented table for default output and stable JSON for `--json`; represent an empty search as a successful empty result and never as a not-found or installation operation.
- Update `README.md` and command help to remove the Phase 2 placeholder and show a search-to-install example.

## 受け入れ条件

- [x] `gh agent-plugin search <query>` performs a public GitHub search and no longer returns the Phase 2 not-implemented error.
- [x] A missing query or more than one positional query exits with code 2 before any network or native-agent operation.
- [x] `--owner`, `--limit`, and `--page` are parsed, validated, and included in the GitHub search request; invalid pagination values exit with code 2.
- [x] Results are limited to supported plugin or marketplace manifest entries, include the documented plugin name, repository, manifest path, supported agents, and description fields, and do not include duplicate identities.
- [x] Default output is a stable table and `--json` emits a stable `{"results": [...]}` schema for both non-empty and empty results.
- [x] Search never invokes a Claude Code or Codex plugin-manager operation, executes repository content, modifies marketplace configuration, or writes installation/cache state.
- [x] GitHub authentication, rate-limit, transport, and response-parsing failures return a non-zero actionable error without being reported as successful results.
- [x] README and command help document the query syntax, supported flags, result shape, public-repository scope, and the relationship to `install`/`preview`.

## テスト計画

- Add table-driven command tests with a fake GitHub client covering query validation, interspersed flags, owner filtering, pagination, limit validation, empty results, duplicate results, and API failures.
- Verify that successful table and JSON output have deterministic columns/fields and that all supported manifest layouts produce the expected agent metadata.
- Assert that search tests make no calls through the fake native-agent runner and do not create files or cache entries.
- Run `go test ./...`, `go vet ./...`, `go build ./...`, and `git diff --check`.
- Run the local issue validator and manually execute a public search with `--json` followed by an `install` or `preview` command using a returned repository/plugin pair.

## リスク

- GitHub code-search availability, authentication, rate limits, and indexing delay can make results incomplete or unavailable; errors must remain explicit and the documentation must not promise exhaustive discovery.
- Manifest formats may evolve independently for Claude Code and Codex. Keep parsing tolerant of unknown fields and cover each supported layout with fixtures.
- Public search results are untrusted repository metadata. Search must remain non-executing and `install`/`preview` must continue to perform their existing validation before use.
- Adding a network client increases test and platform surface area; keep the client isolated so the command remains unit-testable and failures can be reverted without changing native adapters.

## 変更履歴

`CHANGES.md` impact: no. This repository has no `CHANGES.md`; the user-visible
behavior is documented in `README.md` and command help.

## 注記

- Related parent issue: [f4ah6o/gh-agent-plugin#1](https://github.com/f4ah6o/gh-agent-plugin/issues/1)
- The completed implementation replaces the original Phase 2 stub in `cmd/stubs.go`.
- 2026-07-14: Implemented read-only GitHub manifest search with injectable API tests, stable table/JSON output, owner and pagination flags, public-repository filtering, and marketplace entry selectors.
- 2026-07-14: Verified with `go test ./...`, `go vet ./...`, `go build ./...`, `go test -race ./...`, `git diff --check`, a live `search terraform --limit 1 --json`, and a returned-result `preview` command.
- 2026-07-14: Implementation is complete and ready for completion verification.
- 2026-07-14: Verification is in progress on the implementation branch.
- 2026-07-14: Implementation and verification are complete; moving the issue to done.
