# Establish production readiness for release and CLI contracts

Status: rejected
Model: unknown
Created: 2026-07-13
Updated: 2026-07-13
Branch: chore/20260713-production-readiness-gaps

## 概要

Resolve the release, documentation, and CLI contract gaps identified during the production-readiness review so the extension can be distributed and used predictably in automation.

## 背景

The repository currently builds and passes the existing local checks (`go vet ./...`, `go test ./...`, and `go build ./...`). The repository is using the CalVer tag `2026.7.0`, while the release workflow currently filters tag pushes with `v*`.

The project documents stable JSON output and command-line flags as part of its user-facing interface. Several documented or accepted options do not currently have matching behavior.

## 問題

- `.github/workflows/release.yml` only triggers for tags beginning with `v`, so the current CalVer tag format does not publish the precompiled extension assets.
- `README.md` says that `install --ref` is rejected, although the current implementation supports GitHub ref pinning through the cache path.
- `README.md` describes `--all` as an agent fan-out option, while the implementation reserves bare `--all` for command-specific behavior and uses `--agent all` for agent fan-out.
- `--jq`, `--template`, and `--json-fields` are accepted and then ignored with warnings. Automation can therefore appear to succeed while receiving unfiltered or differently shaped output.
- Several commands consume only the first positional argument(s) and silently ignore additional positional arguments, allowing malformed invocations to run against an unintended target.
- Existing tests use injected runners and do not verify the actual release trigger or the complete command-line contract.

## 目標

Make the documented release and command-line behavior explicit, internally consistent, and covered by automated checks before declaring the extension production ready.

## 対象外

- Implementing Phase 2 commands such as `search` or `publish`.
- Replacing the native Claude Code or Codex plugin managers.
- Expanding the static security scanner into a general plugin sandbox or malware detector.
- Adding a new external issue tracker or migrating this local issue to GitHub.

## 提案する方針

1. Choose one supported release-tag convention, align the workflow trigger and README examples with it, and add a CI-level validation that the chosen tag format invokes the release job.
2. Update the README to match the implemented `--ref` and agent-selection semantics, including the distinction between `--agent all` and command-specific `--all`.
3. Treat reserved output flags consistently: either implement their documented behavior or reject them with a non-zero invalid-arguments error until implemented. Do not silently ignore them.
4. Centralize positional-argument validation so every command rejects unexpected extra arguments with exit code 2 while preserving valid interspersed-flag invocations.
5. Add focused command tests for the rejected/accepted flag cases, extra arguments, stable output behavior, and release-tag configuration. Keep the existing `go vet`, `go test`, and `go build` checks as required gates.

## 受け入れ条件

- [ ] The release tag convention is consistent across the repository, and pushing a tag in that convention matches the release workflow trigger.
- [ ] The README accurately documents GitHub `--ref` install behavior and the separate semantics of `--agent all` and bare `--all`.
- [ ] `--jq`, `--template`, and `--json-fields` are either fully implemented and tested or rejected explicitly with exit code 2; none are silently ignored.
- [ ] Commands reject extra positional arguments with exit code 2 and retain support for flags interspersed with valid positional arguments.
- [ ] Automated tests cover the release trigger convention and the command-line contract changes.
- [ ] `go vet ./...`, `go test ./...`, and `go build ./...` pass in CI on the supported operating systems.

## テスト計画

- Run `go vet ./...`, `go test ./...`, and `go build ./...` locally and in the existing multi-OS CI matrix.
- Add command-level tests for reserved output flags, extra positional arguments, valid interspersed flags, and the documented `--ref` and agent-selection behavior.
- Validate the release workflow configuration against the selected tag convention and perform a dry-run or test-tag verification that the release job is selected.
- Manually compare the command examples and limitations in `README.md` with the final parser and dispatch behavior.

## リスク

- Changing the tag convention may affect existing release automation or consumers that refer to the current tag format; preserve an explicit migration path or compatibility trigger if needed.
- Rejecting previously accepted-but-ignored output flags may break scripts that relied on the warning-only behavior, but retaining silent no-op behavior is less predictable for production automation.
- Tightening positional validation may expose callers that currently pass redundant arguments; the error should identify the unexpected arguments and return the documented usage code.

## 変更履歴

`CHANGES.md` impact: yes

項目案：

- Align release tag handling, CLI option validation, and documentation with production behavior.

## 注記

- 2026-07-13: Review evidence: the current worktree is clean at `cd3ac9d` / tag `2026.7.0`; `go vet ./...`, `go test ./...`, and `go build ./...` pass, but the release workflow trigger is `v*`.
- 2026-07-13: Split into four independently implementable production-readiness issues.

Related:
- issues/polished/20260713-calver-release-trigger.md
- issues/polished/20260713-align-cli-documentation.md
- issues/polished/20260713-reject-reserved-output-flags.md
- issues/polished/20260713-reject-extra-arguments.md
