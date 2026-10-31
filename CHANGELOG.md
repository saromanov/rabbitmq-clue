# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Message journey: peek responses now carry a `journey` object for
  messages with `x-death` headers — the chronological hop list (queue,
  reason, count, time, source exchange and routing keys), the origin
  queue, total deaths and retries, detected retry ladders (repeated
  deaths on one queue, the TTL+DLX exponential backoff topology), and
  the current queue with the time since the last death. Rendered as a
  timeline in the peek viewer.
- `internal/mgmt` now tolerates the RabbitMQ 4.x management API quirk
  of serialising empty maps as empty JSON arrays (`"properties": []`),
  which previously failed peeking plain messages, and normalises the
  4.x `x-death` shape (unix-second times, flat `x-first-death-*` keys).

## [0.3.0] - 2026-10-26

### Added

- Protobuf schema core: in-memory `.proto` compilation
  (`internal/schema`, protocompile + dynamicpb, no protoc or generated
  code), JSON skeletons with field descriptions, payload encoding to
  binary protobuf or JSON (optional `google.protobuf.Any` wrapping),
  and payload decoding with automatic `Any` unwrapping by `type_url`
- Schema store: saved schemas as directories under
  `RMQ_CLUE_SCHEMAS_DIR` (default `data/schemas`) with exchange/queue
  bindings
- Schema API: compile, CRUD (`/api/schemas`), publish via schema
  (`POST /api/publish/proto`), and peek decoding (`POST /api/queues/peek`
  with `schema_id` returns messages with decoded JSON)
- Web UI: schema picker with import (paste `.proto` text or upload
  files) in the publish form, JSON skeleton loading with a field panel,
  encoding and wrap-Any options, and peek decoding with a schema
  selector, decoded payload view and per-message decode status
- Integration test: publish → peek → decode round-trip against a real
  broker (binary proto and Any-wrapped payloads)

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