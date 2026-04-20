# Contributing to Ancora

Thank you for your interest in contributing to Ancora!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone git@github.com:YOUR_USERNAME/ancora.git`
3. Create a branch: `git checkout -b feature/your-feature`
4. Make your changes
5. Run checks: `make verify`
6. Commit: `git commit -m "feat: your feature"`
7. Push: `git push origin feature/your-feature`
8. Open a Pull Request

## Development Setup

**Requirements:**
- Go 1.25+
- Make (optional, for convenience)
- `lefthook` for repository-managed git hooks

Install helper tools:

```bash
make hooks-install
```

`make lint` bootstraps the pinned `golangci-lint` version automatically on first run.

`make verify` uses `gotestsum` when available for cleaner output and falls back to `go test -v` otherwise.

**Build:**
```bash
go build -o ancora ./cmd/ancora
```

**Run checks before opening a PR:**
```bash
make verify
```

**Run tests:**
```bash
go test ./...
```

**Install git hooks:**
```bash
make hooks-install
```

This installs:
- `pre-commit`: formatting and lint checks
- `pre-push`: test and build checks

**Run locally:**
```bash
./ancora --help
```

## Commit Message Format

We use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation changes
- `test:` Test changes
- `refactor:` Code refactoring
- `perf:` Performance improvements
- `chore:` Build/tooling changes

## Pull Request Guidelines

- Keep PRs focused (one feature/fix per PR)
- Update tests for code changes
- Update documentation for user-facing changes
- Ensure `make verify` passes
- Keep commits clean and well-described

## Code Style

- Follow standard Go conventions
- Run `make fmt` before committing if `make fmt-check` reports changes
- Use meaningful variable names
- Add comments for non-obvious logic

## Questions?

Open an issue or start a discussion!
