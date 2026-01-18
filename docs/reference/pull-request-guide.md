# Pull Request Guide

This guide covers the pull request workflow for HoloMUSH, including creation, review, and merge processes.

## Overview

All code changes go through pull requests. This ensures:

- Code quality through automated and manual review
- Knowledge sharing across the team
- Traceable history of changes and decisions

## Before Creating a PR

### Prerequisites

| Requirement                   | Description                                        |
| ----------------------------- | -------------------------------------------------- |
| **MUST** have passing tests   | Run `task test` locally before creating PR         |
| **MUST** have passing lints   | Run `task lint` to catch issues early              |
| **MUST** follow commit format | Use conventional commits (see [Commits](#commits)) |
| **SHOULD** keep PRs focused   | One logical change per PR                          |

### Self-Review Checklist

Before creating a PR, verify:

- [ ] Tests pass locally (`task test`)
- [ ] Linting passes (`task lint`)
- [ ] Code is formatted (`task fmt`)
- [ ] New code has appropriate test coverage ( > 80% )
- [ ] Documentation updated if behavior changed
- [ ] Design patterns and best practices followed
- [ ] No obvious performance regressions
- [ ] No security vulnerabilities introduced
- [ ] No debug code or commented-out code left behind
- [ ] Aggressively hunt for needlessly complex code, be as simple as possible

## Creating a PR

### Branch Naming

Use descriptive branch names:

| Pattern           | Example                    | Use Case          |
| ----------------- | -------------------------- | ----------------- |
| `feature/<name>`  | `feature/add-dark-mode`    | New features      |
| `fix/<name>`      | `fix/login-redirect`       | Bug fixes         |
| `refactor/<name>` | `refactor/auth-middleware` | Code improvements |
| `docs/<name>`     | `docs/api-reference`       | Documentation     |

### PR Title Format

Follow conventional commit format for PR titles:

```text
<type>(<scope>): <description>
```

Examples:

- `feat(wasm): add plugin timeout configuration`
- `fix(telnet): handle connection reset gracefully`
- `docs(api): add authentication examples`

### PR Description Template

```markdown
## Summary

Brief description of what this PR does and why.

## Changes

- Bullet points of specific changes
- Include file paths for significant changes

## Testing

How was this tested? Include:

- Unit tests added/modified
- Manual testing performed
- Edge cases considered

## Related Issues

- Closes #123
- Related to #456
```

## Code Review Process

### Automated Review

All PRs automatically trigger:

1. **CI Pipeline** - Tests, linting, formatting checks
2. **Coverage Analysis** - Ensures coverage thresholds met

### Manual Review

Use the `pr-review-toolkit:review-pr` skill for comprehensive review:

```bash
# In Claude Code
/pr-review-toolkit:review-pr
```

This launches specialized review agents:

| Agent                   | Focus Area                                 |
| ----------------------- | ------------------------------------------ |
| `code-reviewer`         | General code quality, CLAUDE.md compliance |
| `comment-analyzer`      | Documentation accuracy and completeness    |
| `pr-test-analyzer`      | Test coverage and quality                  |
| `silent-failure-hunter` | Error handling and silent failures         |
| `type-design-analyzer`  | Type design and invariants                 |
| `code-simplifier`       | Code clarity and maintainability           |

### Review Requirements

| Requirement                     | Description                               |
| ------------------------------- | ----------------------------------------- |
| **MUST** address all findings   | Fix issues or document why not applicable |
| **MUST NOT** skip review        | Even for "simple" changes                 |
| **SHOULD** re-run after changes | Verify fixes don't introduce new issues   |

### Responding to Review Feedback

When addressing review comments:

1. **Fix the issue** - Make the requested change
2. **Explain if declining** - Provide reasoning if not making a change
3. **Ask for clarification** - If feedback is unclear, ask questions
4. **Don't argue** - If you disagree, discuss constructively

## Commits

### Conventional Commit Format

```text
<type>(<scope>): <description>

[optional body]

[optional footer]
Co-Authored-By: Claude <model>@anthropic.com
```

### Commit Types

| Type       | Description                      |
| ---------- | -------------------------------- |
| `feat`     | New feature                      |
| `fix`      | Bug fix                          |
| `docs`     | Documentation changes            |
| `style`    | Formatting (no code change)      |
| `refactor` | Code change without feature/fix  |
| `perf`     | Performance improvement          |
| `test`     | Adding or updating tests         |
| `build`    | Build system or dependencies     |
| `ci`       | CI configuration                 |
| `chore`    | Other changes (e.g., .gitignore) |

### Commit Best Practices

| Requirement                  | Description                            |
| ---------------------------- | -------------------------------------- |
| **MUST** be atomic           | One logical change per commit          |
| **MUST** have clear message  | Describe what and why, not how         |
| **MUST NOT** include secrets | No credentials, tokens, or keys        |
| **SHOULD** reference issues  | Use `Closes #123` or `Related to #456` |

## Merging

### Merge Requirements

Before merging, ensure:

| Requirement                   | Description                       |
| ----------------------------- | --------------------------------- |
| **MUST** have CI passing      | All automated checks green        |
| **MUST** have review approval | At least one approving review     |
| **MUST** be up to date        | Rebased on latest main            |
| **SHOULD** squash if messy    | Clean up WIP commits before merge |

### Merge Strategy

| Strategy     | When to Use                                 |
| ------------ | ------------------------------------------- |
| Squash merge | Multiple small commits that form one change |
| Merge commit | Well-organized commits worth preserving     |
| Rebase merge | Linear history preferred, clean commits     |

## Common Issues

### CI Failures

| Issue         | Solution                                   |
| ------------- | ------------------------------------------ |
| Test failures | Run `task test` locally, fix failing tests |
| Lint errors   | Run `task lint`, address each error        |
| Format issues | Run `task fmt`, commit formatted files     |
| Coverage drop | Add tests for new code paths               |

### Review Delays

If your PR isn't getting reviewed:

1. Ensure CI is passing
2. Check PR description is complete
3. Keep PR size reasonable (< 500 lines ideal)

## Quick Reference

### Commands

```bash
# Run before creating PR
task test           # Run tests
task lint           # Run linters
task fmt            # Format code

# Create PR
gh pr create --title "type(scope): description" --body "..."

# View PR status
gh pr view

# Request review
gh pr edit --add-reviewer username
```

### Workflow Summary

```text
1. Create feature branch
2. Make changes with atomic commits
3. Run tests and lints locally
4. Create PR with clear description
5. Address CI failures
6. Run /pr-review-toolkit:review-pr
7. Address review findings
8. Get approval and merge
```
