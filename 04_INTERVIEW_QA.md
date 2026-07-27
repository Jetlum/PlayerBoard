# 25 Interview Questions & Answers — PlayerBoard

Grounded in the actual code. File references point at the real implementation.

## Architecture & Design

**1. Walk me through the architecture end to end.**
Two Go binaries sharing one module: `cmd/api` (HTTP gateway) and `cmd/worker` (event processor). A signed webhook hits `POST /api/v1/webhooks/scoreboard`, the API verifies signature + timestamp, dedupes it, and writes the event to a transactional outbox — all in one Postgres transaction, no business logic on the request path. A relay goroutine drains the outbox to NATS JetStream (`events.performance`). The worker consumes it, runs the milestone engine in a single transaction (stat upsert, milestone progress, payout creation, outbox emission of `events.milestone`), and the API subscribes to that subject to fan events out over WebSocket — scoped to the one athlete on their board, broadcast to the club console. Postgres is the source of truth; NATS is transport.

**2. Why a transactional outbox instead of publishing to NATS directly from the HTTP handler?**
Dual-write problem. If I write to Postgres and then publish to NATS as two separate operations, either can fail after the other succeeded — DB row with no event, or event with no DB row. The outbox makes the state change and the intent-to-publish one atomic commit (`internal/ingest/handler.go`), and the relay (`internal/ingest/outbox.go`) retries publishing until it succeeds. Consequence: at-least-once delivery, so every consumer must be idempotent — which they are.

**3. What delivery guarantees does the system give, and how do you handle duplicates?**
At-least-once everywhere, exactly-once *effect* via idempotency at each layer:
- Ingest: `inbound_event.source_event_id` is the PRIMARY KEY — duplicate webhook is a no-op returning `{"status":"duplicate"}`.
- Worker: `performance_stat.source_event_id` is UNIQUE, and the engine skips milestones whose progress wouldn't change.
- Payouts: `UNIQUE (milestone_id, boundary)` means a redelivered event can never create a second payout for the same tranche boundary.
Exactly-once delivery is a myth in distributed systems; exactly-once *processing* via idempotent consumers is the practical answer.

**4. The relay does publish-then-mark. What happens if it crashes between the two?**
The row is still unpublished, so next tick it publishes again — a duplicate on the bus. That's fine by design: consumers are idempotent (Q3). The alternative, mark-then-publish, would risk *losing* events, which is worse. I also break out of the drain loop on first failure to preserve outbox ordering rather than skipping ahead (`outbox.go:53`).

**5. How do you guarantee ordering of events for a single athlete?**
Two layers. In the worker, a partitioned pool (`cmd/worker/main.go`): FNV-1a hash of `athlete_id` mod pool size pins all of one athlete's events to the same goroutine/channel, so they process serially while different athletes run in parallel. In the database, `ListMilestonesByAthleteMetric` uses `SELECT ... FOR UPDATE`, so even if two worker *instances* processed the same athlete concurrently, row locks serialize them. The partition is a performance optimization; the row lock is the correctness guarantee.

**6. Why NATS JetStream and not Kafka or RabbitMQ?**
Fit for scale. JetStream gives me the three things I need — durable streams, ack/nak redelivery, durable consumers — in a single ~15 MB binary that starts in milliseconds in docker-compose. Kafka buys partition-level ordering and replayable log semantics at massive scale, but the operational cost (brokers, controllers, partition management) isn't justified for this workload. I do ordering myself with the partitioned pool. The bus is wrapped in a thin package (`internal/platform/bus`) so swapping brokers touches one file.

**7. Why poll the outbox every 250 ms instead of Postgres LISTEN/NOTIFY or CDC (Debezium)?**
Polling a partial index (`idx_outbox_unpublished ... WHERE published_at IS NULL`) is nearly free — the query touches only unpublished rows. LISTEN/NOTIFY adds a second delivery mechanism with its own failure modes (dropped notifications on reconnect) and you still need the poll as a fallback. CDC is the "right" answer at large scale but is a whole deployment (connector, schema registry). 250 ms latency is invisible next to human-facing WebSocket updates. Upgrade path exists if throughput demands it.

**8. Why two separate binaries instead of one process, or full microservices?**
The API and worker have different scaling profiles: API scales with connected clients and HTTP traffic, worker scales with event throughput. Separate binaries let them deploy and scale independently while sharing one module, one schema, one set of internal packages — none of the coordination cost of true microservices (separate repos, versioned APIs between services, distributed tracing across boundaries). It's a modular monolith split along the one boundary that already exists naturally: the message bus.

**9. How does the realtime layer work, and how do you stop one player seeing another's data?**
`internal/realtime/hub.go` keeps a map of `athlete_id → set of WebSocket clients`. The athlete ID comes from the JWT claim — never from a query parameter — so a client cannot subscribe to someone else's stream. When a milestone event arrives on the bus, the API pushes it only to that athlete's connections and broadcasts to the club hub. Each client has a buffered send channel; if the buffer fills, the message is dropped for that client rather than blocking the whole fan-out — slow-consumer protection.

**10. What happens when a WebSocket client is slow or dead?**
Non-blocking send with `select`/`default`: full buffer means the message is dropped for that client (`hub.go:56`). This protects the hub from one stalled TCP connection back-pressuring everyone. Missed events aren't lost data — the milestone state lives in Postgres and the client refetches on reconnect. WebSocket is a notification channel, not the source of truth.

## Go Specifics

**11. Explain the worker's concurrency model.**
`errgroup.WithContext` runs N partition goroutines, each owning one buffered channel of 64 deliveries. The NATS consumer callback hashes athlete_id and routes into the matching channel. `MaxAckPending(256)` on the JetStream consumer bounds total in-flight messages — backpressure so a slow DB can't cause unbounded memory growth. On failure the engine returns an error and the delivery is NAK'd for redelivery; on shutdown, context cancellation drains all partitions and `g.Wait()` collects them. No mutexes in the hot path — ownership via channels.

**12. How does graceful shutdown work in both binaries?**
`signal.NotifyContext` for SIGINT/SIGTERM. The API runs `srv.Shutdown` with a 10-second timeout in a goroutine watching `ctx.Done()` — in-flight requests finish, listener closes, `http.ErrServerClosed` is treated as clean exit. The worker cancels its errgroup context; each partition goroutine exits its loop, undelivered messages get NAK'd and redelivered later. Deferred `pool.Close()` and `bus.Close()` (which calls NATS `Drain`) run last. Nothing is lost because everything is redeliverable.

**13. Why sqlc instead of an ORM like GORM?**
I write real SQL in `internal/query/queries/*.sql`; sqlc generates type-safe Go at build time. I get compile-time checking of every query against the schema, zero runtime reflection, and no surprise queries — what's in the file is what hits the database. ORMs hide the N+1s and make `FOR UPDATE`-style locking awkward. sqlc is essentially free abstraction: the generated code is what I'd hand-write.

**14. How do you handle errors from the bus consumer — what's the retry story?**
Engine returns error → delivery NAK'd → JetStream redelivers. Unparseable payloads return `nil` deliberately (`engine.go:33`) — a poison message that can never parse would otherwise redeliver forever. Transient failures (DB down) NAK and retry. Deliberate gap: no dead-letter queue and no max-delivery cap yet; production would add `MaxDeliver` plus a DLQ subject and an alert.

**15. Where do you use interfaces, and why so few?**
One meaningful interface: `ingest.Verifier` (`verify.go`) — two real implementations exist (HMAC, RSA) selected by config at boot. That's the Go rule: interfaces are discovered where variance actually exists, not declared speculatively. Handlers take `*pgxpool.Pool` concretely; wrapping the DB in a repository interface with one implementation would be ceremony without benefit. Accept interfaces, return structs — but only when there's a second implementation or a test seam that needs it.

## Data & Postgres

**16. Why is money stored as BIGINT and not NUMERIC or float?**
Floats are disqualified outright — binary floating point cannot represent 0.10 and accumulates error in payouts. BIGINT minor units (cents) gives exact integer arithmetic, cheap comparison and indexing, and no rounding-mode ambiguity. NUMERIC would also be correct but is slower and pushes rounding decisions into every query. Currency is carried alongside as text; formatting is the frontend's job.

**17. I see `amount` and `currency` duplicated from clause params into the milestone table. Isn't that denormalization?**
Yes, deliberately. The clause's JSONB `params` is the flexible authoring format; the milestone row is the hot-path execution format. The engine processes events without ever parsing JSONB — plain typed columns, indexable, compile-time checked by sqlc. Copy-on-create denormalization, same idea as a materialized read model in CQRS. Tradeoff: editing a clause after milestone creation requires a sync step — acceptable because contracts are effectively immutable once signed.

**18. How do migrations run, and what's the tradeoff of migrating on startup?**
`golang-migrate` with migrations embedded via `embed.FS` (`internal/platform/db/db.go`) — the binary is self-contained, no migration files to ship separately. The API runs `Migrate()` before serving; it's idempotent (no-op when current). Fine for a single-writer demo. In production with N replicas rolling out simultaneously you'd get harmless-but-noisy lock contention, so I'd move migration to a dedicated job (init container / CI step) and have the app only *verify* schema version. Down migrations exist for every up.

**19. A duplicate webhook and a legitimate re-send with the same event ID — how does the DB distinguish them?**
It doesn't have to: `source_event_id` is the contract. The sender (ScoreAlerts-style source) owns event identity; same ID means same event, and the `inbound_event` PK makes redelivery a no-op. A genuinely new observation must carry a new ID. This pushes idempotency responsibility to the boundary where the natural key exists — the alternative (content hashing) breaks on legitimately repeated identical events, like two appearances with the same payload.

**20. The contract-document upload stores file bytes in Postgres. Defend that.**
For this scale, BYTEA in the same row is the simplest thing that works: one datastore, transactional consistency between metadata and content, ownership enforced by the same `athlete_id` predicate as every other query (`doc_handler.go` — every query is scoped `WHERE id = $1 AND athlete_id = $2`, so IDOR is impossible by construction). Ceiling: 10 MiB upload cap, and Postgres bloat beyond a few GB of documents. Upgrade path is object storage (S3/GCS) with a signed-URL flow and only metadata in Postgres — a swap contained in the DocHandler.

## Security

**21. Walk me through webhook security.**
Defense in layers, all before the payload is parsed (`ingest/verify.go`, `handler.go`):
1. Body capped at 1 MiB via `io.LimitReader` — no memory exhaustion.
2. Timestamp check: ±5 min window rejects replayed captures.
3. Signature: HMAC-SHA256 over `"<timestamp>.<rawBody>"` — binding the timestamp into the signed input means an attacker can't take a valid old signature and refresh the timestamp. Compare with `hmac.Equal` (constant-time, no timing oracle).
4. Only then JSON parse + field validation.
Verify-before-parse matters: hostile bytes never reach the deserializer. The scheme is swappable to RSA via config for senders who won't share a symmetric secret.

**22. How does user auth work, and what's deliberately deferred?**
JWT (HMAC-signed, `golang-jwt/v5`) carrying `athlete_id` and role. Everything under `/api/v1/me` goes through `auth.Middleware`; handlers read the athlete ID from the token claims via context — never from the URL — so every query is self-scoped and horizontal privilege escalation is structurally impossible. Deferred, and documented as such: the club console has no auth (it's the demo's "trusted operator" side), tokens are symmetric-secret rather than asymmetric with rotation, and there's no refresh flow. I'd add club-staff JWTs with a role claim and per-club scoping first.

## Infrastructure & Deployment

**23. Explain the Docker setup and how you'd deploy this to production.**
Multi-stage Dockerfile: `golang:1.25-alpine` build stage with `go mod download` cached as its own layer, `CGO_ENABLED=0` static binaries, then two slim alpine targets (`api`, `worker`) selected via `docker-compose` `build.target`. Compose orchestrates Postgres 16 and NATS with healthchecks; app services gate on `service_healthy`.
Production on Kubernetes: one Deployment per binary; API gets `livenessProbe: /healthz` and `readinessProbe: /readyz` (readyz pings the DB, so a broken DB pulls pods out of the load balancer rather than restarting them — that distinction is why there are two endpoints); HPA on CPU for the API, on JetStream consumer lag for the worker; secrets from a secret manager instead of env literals; managed Postgres; migration as a pre-deploy job. CI: GitHub Actions — vet, build, test, docker build/push, deploy.

**24. Where does this fall over at 100× load, and what would you change first?**
In order of appearance:
1. **Single outbox relay** — one API instance drains the outbox; multiple instances would double-publish (harmless but wasteful). Fix: `FOR UPDATE SKIP LOCKED` in the drain query so N relays cooperate, or a leader lease.
2. **WebSocket hub is per-instance** — already solved structurally: fan-out goes through NATS, so every API instance pushes to its own connected clients; just needs sticky-less horizontal scaling, which it has.
3. **Worker throughput** — scale pool size, then instances; DB row locks keep per-athlete correctness (Q5). Then Postgres itself: read replicas for the dashboard queries, partition `performance_stat` by time.
4. **`MaxConns: 10` pool** is a knob deliberately left visible.
The honest answer: Postgres is the eventual bottleneck, and everything before it is cheap horizontal scaling.

**25. What did you deliberately *not* build, and why?**
- **Metrics/tracing** — structured `slog` with request IDs only. First production add: Prometheus counters on ingest/engine/outbox lag and OpenTelemetry traces across the bus hop.
- **Dead-letter queue** — NAK-retry only (Q14).
- **Club-side auth** (Q22).
- **Kafka-grade broker, CDC, service mesh** — scale doesn't justify them; boundaries exist so they can be added without rewrites.
- **Extensive test pyramid** — targeted tests where logic is subtle: signature verification table tests (`verify_test.go`) and a dedupe integration test against real Postgres (`dedupe_integration_test.go`), because idempotency and crypto are the two things that fail silently.
Every cut is documented in the architecture doc. The skill being demonstrated isn't building everything — it's knowing the upgrade path for each simplification and the trigger that justifies it.
