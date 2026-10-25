# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |
| < 0.1   | No        |

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: open the repository, go to
**Security → Report a vulnerability**, and describe the issue, its impact and
a reproduction if possible.

Please do not open public issues for security reports.

## Scope notes

rabbitmq-clue holds RabbitMQ credentials (from environment variables or the
connections file) and exposes a web UI that can mutate broker state. Areas of
particular interest:

- credential exposure in logs, API responses or error messages
- read-only mode bypass (`RMQ_CLUE_READ_ONLY=true`)
- default listen address (`127.0.0.1:8080` must not become `0.0.0.0` silently)

Replayed messages intentionally carry `x-replayed-by: rabbitmq-clue` headers;
reports about that header being forgeable are out of scope.