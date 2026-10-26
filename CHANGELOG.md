# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-10-26

### Added

- Topology diff: export a vhost topology snapshot (queues, exchanges,
  bindings, policies with their arguments) to deterministic JSON, and
  diff two brokers, two exported snapshots, or a mix — with a
  color-coded report in the UI and a plain-text rendering via
  `POST /api/topology/diff` (`{"format": "text"}`)

## [0.1.1] - 2026-10-26

### Fixed

- Docker image is now published for `linux/amd64` and `linux/arm64` (arm64/v8);
  build stages cross-compile natively from the builder platform instead of
  running under QEMU emulation

## [0.1.0] - 2026-10-25

### Added

- Multi-connection management (env or JSON connections file) with vhost switcher
- Topology browser: queues, exchanges, bindings with live stats over SSE
- Queue peek with content search caveats documented, JSON payload viewer,
  full AMQP properties and the `x-death` dead-letter history panel
- Backlog age estimation from message timestamps or `x-death`, including
  quorum queues where the broker does not report it
- Dead-letter replay: bulk operation with substring/header filters, dry-run
  mode, automatic target detection from `x-death`, optional `x-death` stripping,
  publisher-confirm-then-ack delivery, live progress and cancellation;
  replayed messages are marked with `x-replayed-by` and `x-replay-time`
- Republish of a peeked message to its original exchange/routing key
- Publish form with headers, AMQP properties and publisher confirms
- Queue purge with typed confirmation
- Read-only mode (`RMQ_CLUE_READ_ONLY=true`) that disables all mutating endpoints
- Dark and light themes
- Single static binary with the embedded UI; distroless Docker image
- CI: golangci-lint v2, unit and broker-backed integration tests, frontend
  typecheck/build, Docker build, govulncheck; release pipeline with
  cross-compiled binaries and ghcr.io images on `v*` tags