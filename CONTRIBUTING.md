# Contributing to Lemuria

Thank you for your interest in contributing to Lemuria! This document provides guidelines and instructions for contributing.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<your-username>/lemuria.git`
3. Create a feature branch: `git checkout -b feature/my-feature`
4. Make your changes
5. Submit a pull request

## Development Requirements

- Go 1.25+ (required by argo-cd/v3 dependency)
- Docker (for e2e tests)
- k3d (for e2e cluster tests)

## Coding Standards

### Code Style

- Follow standard Go conventions and idioms
- Use `gofmt` for formatting (enforced by CI)
- Use `golangci-lint` for linting
- Keep cyclomatic complexity under 15 per function

### YAML Handling

- Use `sigs.k8s.io/yaml` for YAML-to-struct (handles json tags)
- Use `gopkg.in/yaml.v3` only for multi-document splitting

### Logging

- Use `log/slog` with structured key-value pairs
- Example: `slog.Info("processing event", "repo", repo, "pr", prNum)`

### Error Handling

- Wrap errors with context: `fmt.Errorf("context: %w", err)`
- Use interfaces for testability (e.g., `lock.Manager`, `commands.VCSClient`)

## Testing

All code changes must include appropriate tests.

### Running Tests

```bash
# Unit tests
make test

# Linting
make lint

# Formatting
make fmt

# End-to-end tests (requires Docker + k3d)
make test-e2e

# Single test
go test -v -run "TestName" ./internal/commands/...
```

### Test Guidelines

- Use table-driven tests for multiple scenarios
- Test both success and error paths
- Mock external dependencies using interfaces
- Aim for at least 80% code coverage

## Submitting Changes

1. Ensure all tests pass: `make test`
2. Ensure linting passes: `make lint`
3. Ensure formatting is correct: `make fmt`
4. Write a clear commit message describing your change
5. Open a pull request with a description of what changed and why

## Bug Reports

- Use GitHub Issues to report bugs
- Include steps to reproduce the issue
- Include relevant logs or error messages
- Specify the version of Lemuria and ArgoCD being used

## Feature Requests

- Use GitHub Issues to request features
- Describe the use case and expected behavior
- Explain why the feature would be valuable

## License

By contributing, you agree that your contributions will be licensed under the same license as the project (see [LICENSE](LICENSE)).
