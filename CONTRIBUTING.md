# Contributing to rabbitmq-clue

Thank you for considering a contribution. This document describes how to set
up a development environment and the rules this repository follows.

## Development setup

Requirements:

- Go 1.25+
- Node.js 24+ (for the embedded web UI)
- Docker (for a local RabbitMQ and integration tests)

Quick start:

```bash
make web-install     # npm ci for the SPA
make web-build       # build the UI into internal/web/dist
make build           # build the single binary
make run             # run against RMQ_CLUE_* env vars
```

A local broker for manual testing:

```bash
docker run --rm -p 5672:5672 -p 15672:15672 rabbitmq:4-management
```

## Testing and linting

```bash
make test            # unit tests with -race
make test-integration # integration tests (needs RMQ_CLUE_TEST_AMQP_URL)
make lint            # golangci-lint v2
make tidy            # verify go.mod/go.sum are tidy
```

CI runs all of the above plus govulncheck. Pull requests are expected to be
green on every job.

## Rules

- **One feature per PR.** Small, focused pull requests keep review fast and
  history clean (every PR is squash-merged).
- **Tests are required** for behavior changes: unit tests for logic,
  integration tests (guarded by `RMQ_CLUE_TEST_AMQP_URL`) for anything that
  talks to a broker.
- **Everything is in English**: code, comments, commit messages, PRs, issues.
- **Commit messages** follow Conventional Commits: `feat:`, `fix:`,
  `docs:`, `chore:`, `ci:`, `refactor:`, `test:`.
- **No secrets in code or logs.** Broker credentials live in environment
  variables or the connections file only.

## Pull requests

Use the pull request template. CI must pass before review. Maintainers
squash-merge; the squash commit message should follow Conventional Commits.

## Reporting issues

Bugs: use the bug report template and include your RabbitMQ version and how
you run rabbitmq-clue (binary/Docker/compose). Feature ideas: use the feature
request template and describe the operational pain, not just the solution.