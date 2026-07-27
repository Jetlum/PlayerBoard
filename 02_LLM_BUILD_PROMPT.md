# LLM Build Prompt — PlayerBoard Go Backend

> **Status: this prompt was executed.** Stages 1–4 below shipped essentially as specified. Stage
> 5 (sponsorship conflict guard) was **not** built. The build then went *beyond* this prompt with
> a feature the user asked for afterward: a second console (**ClubBoard**, `web/club.html` +
> `internal/club`) representing the club/ScoreBoard side, so a demo viewer can trigger an
> appearance for a specific player and watch it land on that player's PlayerBoard in real time,
> while the club sees the same event on a broadcast feed. See `01_PRODUCT_AND_ARCHITECTURE.md`
> for the full as-built picture and exactly what was simplified for the demo (no Postgres RLS, no
> ClubBoard auth, no `testcontainers-go` — a hand-written integration test + `TEST_DATABASE_URL`
> was used instead). The original prompt is kept below unedited, as the historical spec.

Paste the block below into a capable coding LLM (Claude, GPT, etc.). It's written so the model
produces a runnable, idiomatic Go backend for the MVP (build steps 1–4 from the architecture
doc). Feed it in stages if the model truncates — the "Deliverables per stage" list is ordered.

---

## PROMPT

You are an expert Backend Golang Engineer. Build the backend for **PlayerBoard**, a player-side
companion to a football contract-management system ("ScoreBoard"). PlayerBoard is a per-player
tenant that keeps an eventually-consistent projection of the club's contract data and shows the
player their contracts, live performance-bonus progress, and sponsorship conflicts.

Follow this architecture exactly; do not invent extra services.

### Non-negotiable constraints
- Go 1.22+, standard project layout: `cmd/api`, `cmd/worker`, `internal/<module>`, `migrations`.
- Hexagonal: `internal/<module>/domain` imports no framework/DB/HTTP; adapters depend inward.
- HTTP router: `chi`. WebSocket: `gorilla/websocket`. DB: PostgreSQL via `sqlc` (typed SQL) with
  `pgx`. Bus: NATS JetStream. Config via env. Logging via `slog` (structured). Context everywhere.
- Multi-tenant safety is paramount: every `/me/*` route derives `athlete_id` from the JWT, never
  from path or body. Every DB query is scoped by `athlete_id`. Assume Postgres RLS is also on.

### Data model (Postgres) — generate migrations and sqlc queries
`athlete, consent, contract, clause, salary_schedule, payout_event, performance_stat,
milestone, personal_sponsorship, conflict, inbound_event, outbox, audit_log`.
- `clause.params` is `jsonb`, e.g. `{"metric":"appearances","target":20,"tranche":10,"max":-1}`
  (max -1 = unlimited repeats, like the source system's tranche logic).
- `inbound_event.source_event_id` is UNIQUE (webhook dedupe).
- `performance_stat.source_event_id` is UNIQUE.

### Stage 1 — platform + auth
- Config loader, `slog` setup, `pgxpool`, `golang-migrate` wiring.
- JWT middleware: validate access token, put `athlete_id` and `role` (`athlete`|`agent`) in
  request context. Provide a helper `AthleteID(ctx)`.
- Health/readiness endpoints.

### Stage 2 — contract module (reads)
- Repo + service + handlers for:
  `GET /api/v1/me/contracts`, `GET /api/v1/me/contracts/{id}/clauses`.
- Seed data resembling: player "Everton", contract Flamengo→(current Benfica), fixed 80k BRL,
  bonus, salary 95k BRL, and an appearances-based bonus clause (target 20, tranche 10).

### Stage 3 — ingest (signed webhooks, idempotent)
- `POST /api/v1/webhooks/scoreboard` — NO user JWT. Instead verify authenticity:
  - Headers: `X-Event-Id`, `X-Timestamp`, `X-Signature`.
  - Recompute signature = **HMAC-SHA256** over `timestamp + "." + rawBody` using a shared secret
    from config; constant-time compare (`hmac.Equal`). Also implement an RSA-SHA256 verifier
    behind an interface so the scheme can be swapped.
  - Reject if `|now - X-Timestamp| > 5m` (replay guard).
- Insert into `inbound_event` keyed by `source_event_id`; on duplicate return `200` no-op.
- Persist raw payload, publish to NATS, return `202 Accepted` immediately (no business logic on
  the request path).
- Implement the **transactional outbox**: writing an event and a state change happen in one DB
  transaction; a relay goroutine publishes unsent `outbox` rows.

### Stage 4 — milestone engine (worker) + realtime
- `cmd/worker` consumes performance events from NATS with per-`athlete_id` ordering.
- For each event: upsert `performance_stat`, recompute matching `milestone` rows
  (state machine: `quiet → warm → hot → fulfilled`; a tranche fulfills every `tranche`
  appearances up to `max`). On fulfillment, insert `payout_event(status='expected')` and emit a
  `MilestoneFulfilled` domain event via the outbox.
- Use a **bounded worker pool** (buffered channel + N goroutines), `context` for cancellation,
  `errgroup` for fan-out. Run `go test -race` clean.
- `GET /api/v1/me/milestones` returns progress. `WS /api/v1/me/stream` pushes
  `MilestoneAdvanced` / `MilestoneFulfilled` to the connected player.

### Stage 5 — sponsorship conflict guard
- `POST /api/v1/me/sponsorships` and `GET /api/v1/me/conflicts`.
- On sponsorship create/update, compare its `category` against the prohibited categories declared
  in the athlete's active exclusivity clauses (`clause.kind='exclusivity'`,
  `params.prohibited_categories`). Raise a `conflict` row (severity by exact vs. adjacent
  category) and push a WS notification. Run the comparison off the request path via a goroutine.

### Cross-cutting deliverables
- `docker-compose.yml` (api, worker, postgres, nats) so it runs with one command.
- Table-driven unit tests for tranche math and conflict rules; a `testcontainers-go` integration
  test that POSTs a signed webhook and asserts a milestone advances and an outbox event is
  written; a test proving a duplicate `X-Event-Id` is a no-op and a bad signature is rejected.
- `README.md` with run instructions and a `curl` that sends a correctly-signed sample webhook.
- Append-only `audit_log` writes on every contract read/write.

### Output format
Produce complete, compiling files (not snippets), grouped by path, in the stage order above.
After each stage, print a one-line summary of what to run to verify it. Prefer clarity and
idiomatic Go over cleverness. Add short comments only where the *why* isn't obvious.
