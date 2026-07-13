# Align CLI documentation with implemented behavior

Status: done
Model: unknown
Created: 2026-07-13
Updated: 2026-07-13
Branch: docs/20260713-align-cli-documentation

## 概要

Update the user-facing README so source pinning and agent selection describe the behavior implemented by the current CLI.

## 背景

`cmd/install.go` supports GitHub `--ref` by checking out the requested ref through the source cache and registering the pinned checkout as a local marketplace. Agent fan-out is requested with `--agent all`; bare `--all` is command-specific and is used by `update --all` to mean all installed plugins.

## 問題

The README currently states that `install --ref` is rejected and says that bare `--all` is an agent fan-out alias. Both statements are inconsistent with the implementation and can cause users to select the wrong operation.

## 目標

Make the README examples and limitations match the current command behavior without changing the CLI implementation.

## 対象外

- Adding or changing CLI flags.
- Implementing output filtering for `--jq`, `--template`, or `--json-fields`.
- Changing release tag documentation, which is owned by the CalVer release issue.

## 提案する方針

- In the Phase 1 limitations section, state that `preview` and `install` honor GitHub `--ref`, that the resolved checkout is pinned to the requested ref, and that marketplace selectors and local paths reject `--ref`.
- In Agent selection and scope, document `--agent all` as the agent fan-out form and describe bare `--all` only as a command-specific flag such as `update --all`.
- Keep all existing valid command examples and the native-agent delegation model unchanged.

## 受け入れ条件

- [x] README no longer claims that `install --ref` is rejected.
- [x] README states that `--ref` is supported for GitHub sources and rejected for marketplace selectors and local paths.
- [x] README distinguishes `--agent all` from command-specific bare `--all` behavior.
- [x] No README example contradicts the source syntax or agent-selection implementation.

## テスト計画

- Compare the edited README against `cmd/install.go`, `cmd/root.go`, and the existing `TestInstall_GitHubRef_PinsViaCache` and `TestUpdateAll_AgentFlagNotWidened` tests.
- Run `rg -n 'still rejects|or --all' README.md` and confirm no stale statements remain.
- Run `go test ./...` and `git diff --check`.

## リスク

- Documentation changes expose the existing implementation as the public contract; any later behavior change must update this documentation and its tests together.

## 変更履歴

`CHANGES.md` impact: yes

項目案：

- Correct documentation for GitHub ref pinning and agent selection.

## 注記

- Related parent issue: issues/rejected/20260713-production-readiness-gaps.md
- 2026-07-13: Implementation started for README source-pinning and agent-selection documentation.
- 2026-07-13: Updated README source-pinning and agent-selection documentation; stale-text check, `go test ./...`, and Issue validation pass.
- 2026-07-13: README documentation updated and verified against the implemented CLI behavior.
