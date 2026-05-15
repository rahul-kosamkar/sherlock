# Contributing to Sherlock

Thank you for your interest in contributing to Sherlock. This document explains
the process and guidelines for contributing.

## License

By contributing to Sherlock you agree that your contributions will be licensed
under the [Apache License 2.0](LICENSE).

## Developer Certificate of Origin (DCO)

All commits must be signed off to certify that you wrote or have the right to
submit the code under the project's license. This is done by adding a
`Signed-off-by` line to every commit message:

```
Signed-off-by: Your Name <your.email@example.com>
```

You can do this automatically with `git commit -s`. If you forget, you can amend
the most recent commit:

```
git commit --amend -s
```

Unsigned commits will be rejected by CI.

## Reporting Issues

- Search existing issues before opening a new one.
- Use a clear, descriptive title.
- Include steps to reproduce the problem, expected behaviour, and actual
  behaviour.
- Attach logs, screenshots, or configuration snippets when relevant.

## Pull Requests

1. Fork the repository and create a feature branch from `main`.
2. Keep PRs focused -- one logical change per PR.
3. Include tests for new functionality or bug fixes.
4. Make sure all checks pass locally before pushing (see Development Workflow
   below).
5. Write a clear PR description explaining *what* changed and *why*.
6. Reference the related issue (e.g. `Fixes #42`).

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/) style:

```
feat(slack): add thread summary command
fix(engine): handle nil pointer in correlation step
docs: update deployment guide
```

## Development Setup

### Prerequisites

- Go 1.23+
- Docker and Docker Compose
- (Optional) [golangci-lint](https://golangci-lint.run/) for linting

### Getting Started

```bash
git clone https://github.com/<your-fork>/sherlock.git
cd sherlock
cp .env.example .env   # adjust values as needed
make dev               # starts all dependencies via Docker Compose
```

### Makefile Targets

| Target       | Description                                           |
|--------------|-------------------------------------------------------|
| `make build` | Compile the binary to `bin/sherlock`                   |
| `make test`  | Run all tests with the race detector enabled          |
| `make lint`  | Run `go vet` and `golangci-lint`                      |
| `make dev`   | Start local development stack with Docker Compose     |
| `make fmt`   | Format Go source files                                |
| `make vet`   | Run `go vet ./...`                                    |
| `make clean` | Remove build artifacts and Go caches                  |

### Running Tests

```bash
make test
```

### Linting

```bash
make lint
```

## Code Review

All submissions require review before merging. Maintainers may request changes
or ask clarifying questions. Please respond in a timely manner so PRs do not go
stale.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).
Please read it before participating.
