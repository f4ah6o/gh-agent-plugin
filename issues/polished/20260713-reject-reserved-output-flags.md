# Reject unsupported output filtering flags

Status: polished
Model: unknown
Created: 2026-07-13
Updated: 2026-07-13
Branch: fix/20260713-reject-reserved-output-flags

## 概要

Prevent unsupported output-filtering flags from silently being ignored by returning an explicit invalid-arguments error.

## 背景

`cmd/root.go` registers `--jq`, `--template`, and `--json-fields` for common command parsing. The current `warnReservedFlags` helper only prints warnings and the command continues, so a script can exit successfully while receiving unfiltered output. Existing tests assert the warning-only behavior.

## 問題

The accepted command-line surface implies that the three flags affect output, but none of them changes output. This makes automation results ambiguous and violates the CLI's stable-output contract.

## 目標

Reject each unsupported flag explicitly with exit code 2 before any agent, cache, GitHub CLI, or native agent mutation is performed.

## 対象外

- Implementing jq evaluation, Go template rendering, or JSON field projection.
- Changing the existing `--json` output schemas.
- Adding external runtime dependencies.

## 提案する方針

- Replace `warnReservedFlags` with a validation helper returning `exit.CodedError` using `exit.InvalidArguments`.
- Report the first supplied unsupported flag deterministically in the order `--jq`, `--template`, `--json-fields`.
- Invoke the validation in every command that currently registers common flags, immediately after argument parsing and before timeout-dependent work or target resolution.
- Update unit and smoke tests so each flag returns code 2 and no native runner call occurs; remove warning-only expectations.

## 受け入れ条件

- [ ] Given any supported command with `--jq`, when executed, then it exits 2 with an unsupported-flag error and performs no operation.
- [ ] Given any supported command with `--template`, when executed, then it exits 2 with an unsupported-flag error and performs no operation.
- [ ] Given any supported command with `--json-fields`, when executed, then it exits 2 with an unsupported-flag error and performs no operation.
- [ ] Supplying multiple reserved flags reports the deterministic first flag and still exits 2.
- [ ] Existing commands without reserved flags preserve their output and exit behavior.

## テスト計画

- Add table-driven command tests covering all three flags on a read command and a mutation command.
- Assert the fake adapter/runner has no calls when validation fails.
- Update `TestPreview_ReservedFlags_Warning` and `TestSmoke_Install_ReservedFlags_Warn` to assert exit code 2 and the new error text.
- Run `go test ./...`, `go vet ./...`, and `git diff --check`.

## リスク

- Existing scripts that relied on the warning-only no-op behavior will fail explicitly and must remove the flags or wait for a separate implementation.

## 変更履歴

`CHANGES.md` impact: yes

項目案：

- Reject unsupported output filtering flags instead of silently ignoring them.

## 注記

- Related parent issue: issues/rejected/20260713-production-readiness-gaps.md
