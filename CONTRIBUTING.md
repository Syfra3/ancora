# Contributing to Ancora

Thank you for your interest in contributing to Ancora!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone git@github.com:YOUR_USERNAME/ancora.git`
3. Create a branch: `git checkout -b feature/your-feature`
4. Make your changes
5. Run tests: `go test ./...`
6. Commit: `git commit -m "feat: your feature"`
7. Push: `git push origin feature/your-feature`
8. Open a Pull Request

## Development Setup

**Requirements:**
- Go 1.25+
- Make (optional, for convenience)

**Build:**
```bash
go build -o ancora ./cmd/ancora
```

**Run tests:**
```bash
go test ./...
```

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
- Ensure all tests pass
- Keep commits clean and well-described

## Code Style

- Follow standard Go conventions
- Run `go fmt` before committing
- Use meaningful variable names
- Add comments for non-obvious logic

## Questions?

Open an issue or start a discussion!
