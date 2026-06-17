---
name: github-agent
description: Use when working from GitHub issues or pull requests in a repository, especially when the user references an issue or PR number such as "#123".
---

# GitHub Agent

Follow this workflow when a task is driven by GitHub issue or pull request context.

## 1. Establish context

- Inspect the current branch, working tree, and configured remotes before editing.
- If a user references an issue or pull request number, retrieve the title and body with the most direct available tool:
  - Prefer a local GitHub CLI (`gh issue view <number>` or `gh pr view <number>`) when it is installed and authenticated.
  - Otherwise use repository metadata from the current checkout or any context supplied by the user.
- If the issue content cannot be retrieved, make the smallest useful repository change that matches the repository purpose and clearly report the limitation.

## 2. Implement safely

- Keep changes scoped to the issue or PR request.
- Preserve existing project conventions and any repository-specific instructions.
- Do not overwrite unrelated user changes.
- Prefer adding documentation or tests alongside code when they clarify the intended behavior.

## 3. Validate

- Run the most relevant fast checks available in the repository.
- If the repository has no runnable checks yet, validate machine-readable files directly, such as parsing JSON manifests.
- Record any environment limitation separately from implementation failures.

## 4. Prepare pull request notes

Include the following in the final pull request summary:

- What changed.
- How it was validated.
- Any issue or environment context that affected the implementation.
