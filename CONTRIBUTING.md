# Contributing to HoloMUSH

Thank you for your interest in contributing to HoloMUSH!

## Development Process

### Prerequisites

- Go 1.22+
- Task (task runner)
- PostgreSQL 16+

### Setup

```bash
# Clone the repository
git clone https://github.com/holomush/holomush.git
cd holomush

# Install development tools
task tools

# Install git hooks
task hooks:install

# Run tests
task test
```

### Workflow

We use [beads](https://github.com/steveyegge/beads) for task tracking:

1. Check `bd ready` for available tasks
2. Write failing tests first (TDD)
3. Implement until tests pass
4. Update documentation
5. Submit PR

### Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): description

Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert
```

Examples:

- `feat(events): add event replay on reconnection`
- `fix(telnet): handle connection timeout gracefully`
- `docs(readme): update quick start instructions`

### Code Style

- Go code follows `gofumpt` formatting
- Run `task lint` before committing
- Pre-commit hooks enforce style automatically

### Pull Requests

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run `task lint` and `task test`
5. Submit a PR with clear description

### Questions?

Open an issue for discussion before starting major work.

## Code of Conduct

Be respectful and constructive. We're all here to build something great.

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
