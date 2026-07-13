# Reject unexpected positional arguments

Status: done
Model: unknown
Created: 2026-07-13
Updated: 2026-07-13
Branch: fix/20260713-reject-extra-arguments

## 概要

Make every command reject extra positional arguments instead of silently operating on only the first argument.

## 背景

`parseArgs` intentionally supports flags interspersed with positional arguments and returns all collected positionals. Several command handlers then consume only `rest[0]` or `pos[0]` without checking the remaining values. `source.Parse` similarly accepts the first source and plugin values while ignoring additional positionals.

## 問題

Malformed invocations such as `remove plugin-one plugin-two` or `issue view 12 13` can exit successfully while targeting only the first value. This is unsafe for scripts and inconsistent with the documented command syntax.

## 目標

Reject every unexpected positional argument with exit code 2 while preserving valid flags before, between, and after positional arguments.

## 対象外

- Changing the existing interspersed-flag parsing behavior.
- Changing source selector syntax or native-agent selector semantics.
- Reworking command help text beyond documenting the resulting strict arity.

## 提案する方針

- Add a shared positional-count validation helper in `cmd/root.go` that returns `exit.InvalidArguments` and includes the command name and unexpected arguments.
- Apply explicit arity checks to all command handlers: zero for list/doctor and list subcommands; one for remove, marketplace add/remove, issue/pr number commands, and named update; zero-or-one for update and marketplace update; and the source-specific exact arity for install/preview.
- Update `internal/source.Parse` so local and GitHub forms require exactly `PATH PLUGIN` / `OWNER/REPO PLUGIN`, while marketplace selectors require exactly one `PLUGIN@MARKETPLACE` argument.
- Perform arity validation before agent selection, cache access, or native command execution.

## 受け入れ条件

- [x] Given a command with more positional arguments than its declared arity, when executed, then it exits 2 and performs no side effect.
- [x] Given valid flags interspersed with the declared positional arguments, when executed, then the command retains its current behavior.
- [x] Given a GitHub or local source with an extra positional argument, when parsed, then `source.Parse` returns exit code 2.
- [x] Given a marketplace selector with an extra positional argument, when parsed, then `source.Parse` returns exit code 2.
- [x] All command handlers have tests for their zero-, one-, or two-positional-argument boundary.

## テスト計画

- Add table-driven parser tests for every command family and source form, including extra arguments before and after flags.
- Assert no fake adapter, Git, or GitHub runner calls occur after an arity error.
- Preserve and run `TestInstall_InterspersedFlagsAfterPositionals` as the regression test for valid interspersed flags.
- Run `go test ./...`, `go vet ./...`, and `go build ./...`.
- Run `git diff --check` and manually verify command usage messages identify the accepted arity.

## リスク

- Callers that accidentally pass redundant arguments will receive a new failure instead of silently targeting the first value.
- Tightening `source.Parse` affects all install and preview callers, so the existing valid selector tests must remain unchanged.

## 変更履歴

`CHANGES.md` impact: yes

項目案：

- Reject unexpected positional arguments with explicit invalid-arguments errors.

## 注記

- Related parent issue: issues/rejected/20260713-production-readiness-gaps.md
- 2026-07-13: Implementation started for strict positional-argument validation.
- 2026-07-13: Added shared command arity validation, strict source parsing, table-driven boundary tests, and preserved interspersed-flag coverage; all Go and Issue checks pass.
- 2026-07-13: Strict positional-argument validation and boundary tests are implemented and verified.
