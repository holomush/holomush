# Pull Request Guide

This guide covers the pull request workflow for HoloMUSH, including creation,
review, and merge processes.

## Overview

All code changes go through pull requests. This ensures:

- Code quality through automated and manual review
- Knowledge sharing across the team
- Traceable history of changes and decisions

## Before Creating a PR

### Prerequisites

| Requirement          | Description                                        |
| -------------------- | -------------------------------------------------- |
| Tests pass           | Run `task test` locally                            |
| Linting passes       | Run `task lint` to catch issues early              |
| Follow commit format | Use conventional commits (see [Commits](#commits)) |
| Keep PRs focused     | One logical change per PR                          |

### Self-Review Checklist

Before creating a PR, verify:

- [ ] Tests pass locally (`task test`)
- [ ] Linting passes (`task lint`)
- [ ] Code is formatted (`task fmt`)
- [ ] New code has appropriate test coverage (> 80%)
- [ ] Documentation updated if behavior changed
- [ ] No debug code or commented-out code left behind
- [ ] Code is as simple as possible

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

- `feat(plugin): add Lua script hot reload`
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

### What Reviewers Look For

| Area        | Focus                                   |
| ----------- | --------------------------------------- |
| Correctness | Does the code do what it's supposed to? |
| Testing     | Are there adequate tests?               |
| Clarity     | Is the code easy to understand?         |
| Patterns    | Does it follow project conventions?     |
| Security    | Are there any security concerns?        |
| Performance | Any obvious performance issues?         |

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

| Requirement           | Description                            |
| --------------------- | -------------------------------------- |
| Be atomic             | One logical change per commit          |
| Have clear message    | Describe what and why, not how         |
| Never include secrets | No credentials, tokens, or keys        |
| Reference issues      | Use `Closes #123` or `Related to #456` |

## Merging

### Merge Requirements

Before merging, ensure:

| Requirement     | Description                          |
| --------------- | ------------------------------------ |
| CI passing      | All automated checks green           |
| Review approval | At least one approving review        |
| Up to date      | Rebased on latest main               |
| Squash merge    | All merges to main are squash merges |

### Why Squash Merge?

All merges to main use squash merge to maintain a clean, linear history:

- Each PR becomes one atomic commit on main
- Easier to bisect, revert, and understand history
- WIP commits and fixups don't clutter the main branch
- Commit message can be refined at merge time

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
6. Get review and address findings
7. Get approval and merge
```

## Further Reading

- [Coding Standards](coding-standards.md) - Style and conventions
- [Architecture](architecture.md) - System design overview
- [Contributing Guide](index.md) - Getting started
