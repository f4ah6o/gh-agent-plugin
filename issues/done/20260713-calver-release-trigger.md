# Align release workflow with CalVer tags

Status: done
Model: unknown
Created: 2026-07-13
Updated: 2026-07-13
Branch: chore/20260713-calver-release-trigger

## 概要

Make the GitHub release workflow publish the precompiled extension for the repository's existing CalVer tag format.

## 背景

The repository currently has the tag `2026.7.0`. `.github/workflows/release.yml` currently triggers only for tags matching `v*`, while `README.md` documents a `v0.1.0` example. A tag push using the current CalVer format therefore does not select the release workflow.

## 問題

A maintainer can push the repository's current tag style and receive no release job or precompiled GitHub CLI extension assets.

## 目標

Use `YYYY.M.P` CalVer tags, such as `2026.7.1`, as the only documented release convention and ensure the release workflow selects those tags before running `cli/gh-extension-precompile@v2`.

## 対象外

- Changing the binary build matrix or the `cli/gh-extension-precompile` action.
- Publishing or recreating the already-existing `2026.7.0` release as part of this change.
- Supporting a second `v`-prefixed SemVer convention.

## 提案する方針

- Update `.github/workflows/release.yml` so tags beginning with `20` are selected and add an early shell validation step requiring `^20[0-9]{2}\.[0-9]+\.[0-9]+$` before precompilation.
- Update the Release section of `README.md` to use `2026.7.1` and describe the `YYYY.M.P` convention.
- Keep `permissions: contents: write` and the existing Go version source unchanged.

## 受け入れ条件

- [x] Given a push of tag `2026.7.1`, when GitHub Actions evaluates the workflow, then the release job is selected and the CalVer validation passes.
- [x] Given a tag such as `2026.7`, when the release job is selected by the broad `20*` trigger, then the validation step fails before precompilation.
- [x] Given a tag such as `v2026.7.1`, when GitHub Actions evaluates workflow triggers, then the release workflow is not selected.
- [x] The workflow still uses `cli/gh-extension-precompile@v2` with `go_version_file: go.mod` and `contents: write` permission.
- [x] README release commands consistently use the `YYYY.M.P` format.

## テスト計画

- Run `actionlint .github/workflows/release.yml` when `actionlint` is available; otherwise validate the YAML and shell step by GitHub Actions workflow checks.
- Review the workflow trigger and validation regex against `2026.7.1`, `v2026.7.1`, and `2026.7`.
- On the next release candidate, push a disposable CalVer test tag and confirm the release job and generated assets in GitHub Actions.
- Run `git diff --check` after editing the workflow and README.

## リスク

- A stricter tag convention may require maintainers to rename future tags that currently use a `v` prefix.
- The broad `20*` trigger may start a job for malformed tags, but the validation step prevents precompilation for them.

## 変更履歴

`CHANGES.md` impact: yes

項目案：

- Align release tags with the documented CalVer convention.

## 注記

- Related parent issue: issues/rejected/20260713-production-readiness-gaps.md
- 2026-07-13: Implementation started on the CalVer release workflow and README release contract.
- 2026-07-13: Updated `.github/workflows/release.yml` and `README.md`; local regex and Go checks pass. `actionlint` is unavailable and real GitHub Actions tag verification remains pending.
- 2026-07-13: Clarified acceptance behavior for malformed CalVer tags versus unsupported `v`-prefixed tags after diff review.
- 2026-07-13: Pushed tag `2026.7.1`; GitHub Actions release run `29224204810` completed successfully.
- 2026-07-13: CalVer tag 2026.7.1 successfully selected and completed release workflow run 29224204810.
