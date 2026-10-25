# rabbitmq-clue

**Web UI for RabbitMQ operations the management UI can't do: dead-letter replay, safe message inspection, moving messages between queues, and payload forensics.**

One static binary with an embedded UI. No plugins, no database, no agents on the broker.

```bash
docker run --rm -p 8080:8080 \
  -e RMQ_CLUE_ADDR=0.0.0.0:8080 \
  -e RMQ_CLUE_MGMT_URL=http://host.docker.internal:15672 \
  -e RMQ_CLUE_AMQP_URL=amqp://guest:guest@host.docker.internal:5672 \
  -e RMQ_CLUE_USER=guest -e RMQ_CLUE_PASS=guest \
  ghcr.io/saromanov/rabbitmq-clue
```

Then open http://localhost:8080.

## Why

The stock management UI covers monitoring and basic config, but every team operating RabbitMQ eventually writes the same shell scripts:

- **Replaying a dead-letter queue** — RabbitMQ has no built-in replay; you wire up shovels or hand-roll consume/publish scripts.
- **Inspecting what's actually inside a queue** — the "Get Message" API is destructive-ish (requeueing shuffles messages), truncates payloads, and gives you no search.
- **Moving messages between queues** — shovel configs, CLI incantations.
- **Figuring out why a message died** — `x-death` headers are buried in base64 blobs.

rabbitmq-clue does these things in a browser, with confirmations, dry runs and an audit trail.

## Features

- **Dead-letter replay** — bulk replay from any queue with filters (payload substring, header match), dry-run mode, automatic target detection from `x-death`, optional `x-death` stripping, and live progress with cancel. Every message is published with publisher confirms *before* the source is acked, so a crash mid-replay never loses messages.
- **Message inspection** — batched peek with the requeue caveat shown honestly, pretty JSON payloads, full AMQP properties, and a dead-letter history panel (the `x-death` chain: which queue, which reason, how many times).
- **Backlog age estimation** — computes how old the head message of a queue is from its timestamp or `x-death` records — including for quorum queues, which the broker doesn't report this for.
- **Republish** — take a peeked message, edit it, send it back through its original exchange/routing key.
- **Publish** — a real publish form: headers, all AMQP properties, persistent/transient, publisher confirms.
- **Multi-connection** — configure several brokers (env or JSON file), switch connections and vhosts in the UI.
- **Read-only mode** — `RMQ_CLUE_READ_ONLY=true` disables every mutating endpoint; useful when the UI is pointed at production.

## Caveats (the honest section)

AMQP 0-9-1 has no browse-only mode (unlike JMS `QueueBrowser`). There is no way to read messages without affecting the queue. rabbitmq-clue is explicit about this instead of hiding it:

| Operation | What happens |
|---|---|
| Peek | Messages are fetched and requeued. The broker may reorder them. Payloads are truncated at 256 KiB. |
| Dry-run replay | Every message is moved to the tail of the source queue (get → re-publish → ack). Content is preserved, order is not. Counts are exact. |
| Filtered replay | Non-matching messages are re-published to the tail of the source queue and skipped. |
| Replay | Matching messages are published to the target with a confirm, then acked at the source. At-least-once: a crash between confirm and ack can duplicate a message. |

Replayed messages get `x-replayed-by: rabbitmq-clue` and `x-replay-time` headers so you can trace them.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `RMQ_CLUE_ADDR` | `127.0.0.1:8080` | Listen address |
| `RMQ_CLUE_MGMT_URL` | — | Management API URL (e.g. `http://127.0.0.1:15672`) |
| `RMQ_CLUE_AMQP_URL` | — | AMQP URL (e.g. `amqp://127.0.0.1:5672/%2F`) |
| `RMQ_CLUE_USER` / `RMQ_CLUE_PASS` | — | Broker credentials |
| `RMQ_CLUE_READ_ONLY` | `false` | Disable all mutating operations |
| `RMQ_CLUE_CONNECTIONS_FILE` | — | JSON file with multiple connections (see below) |

Multiple connections:

```json
[
  { "name": "prod", "mgmt_url": "https://rmq-prod:15672", "amqp_url": "amqps://rmq-prod:5671", "username": "svc", "password": "..." },
  { "name": "dev", "mgmt_url": "http://rmq-dev:15672", "amqp_url": "amqp://rmq-dev:5672", "username": "dev", "password": "..." }
]
```

Credentials are accepted only through environment or file — never command-line flags — and are never logged.

## Development

```bash
go build ./...
go test ./...                                            # unit tests
RMQ_CLUE_TEST_AMQP_URL=amqp://guest:guest@localhost:5672/%2F go test ./internal/replay/ -run Integration

cd web && npm ci && npm run dev                          # frontend with HMR, proxies /api to :8080
cd web && npm run build                                  # rebuild embedded UI into internal/web/dist
```

## Roadmap

- [ ] Move/drain workflows between arbitrary queues (parking-lot pattern)
- [ ] Topology diff between environments
- [ ] Payload decoders (gzip/deflate, protobuf descriptors)
- [ ] Audit log UI, RBAC
- [ ] Backpressure simulator

## License

MIT