# GitHub Agent Plugin

This repository contains a Codex plugin that helps agents work from GitHub issues and pull requests.

## What it provides

- A plugin manifest at `.codex-plugin/plugin.json` for Codex plugin discovery.
- A `github-agent` skill with a repeatable workflow for issue-driven implementation.
- Starter prompts for common GitHub workflows, including implementing an issue, reviewing a pull request, and summarizing issues.

## Validation

The plugin manifest is JSON and can be checked with:

```bash
python3 -m json.tool .codex-plugin/plugin.json
```
