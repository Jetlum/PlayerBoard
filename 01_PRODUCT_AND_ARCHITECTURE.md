# PlayerBoard — As-Built Product & Backend Architecture

*A player-side companion to ScoreTech's ScoreBoard / ScoreAlerts.*

> **Status: implemented.** This document describes the system as it actually exists in this
> repository — not just the original concept. For the historical prompt used to bootstrap the
> build, see `02_LLM_BUILD_PROMPT.md` (now annotated with what shipped vs. what was simplified).
> For exact commands to run/rebuild it, see `README-run.md`.

---

## 1. The problem

ScoreBoard already digitizes and monitors football contracts **for clubs and agencies**: fixed
fees, bonuses, salary schedules, and performance-triggered clauses (the "Appearances ≥ 20 →
bonus" logic). The club is the account holder. The player is *data inside someone else's system*
— they never see the alert the club gets the moment a bonus clause fires.

**PlayerBoard is the mirror image of ScoreBoard, owned by the player.** Same underlying contract
data model, but framed around the athlete's own view of it, updated the instant the club's system
records something that affects them.

## 2. The two boards — this is the core of the demo

The system is deliberately split into **two separate consoles that talk to each other only
through the backend**, because that split *is* the product argument ("the club always knew, the
player never did"):

```
┌─────────────────────────────┐                              ┌─────────────────────────────┐
│         ClubBoard            │                              │        PlayerBoard          │
│   web/club.html               │                              │   web/index.html             │
│                               │                              │                              │
│  "the club's own system"      │                              │  "one specific athlete"      │
│  • roster of every player     │                              │  • that athlete's contract    │
│    under contract             │                              │  • that athlete's milestone   │
│  • "Record appearance ▶"      │                              │  • live WS feed — theirs only │
│    per player                 │                              │                              │
│  • broadcast live feed        │                              │  scoped by JWT → athlete_id,  │
│    (every player, unscoped)   │                              │  never spoofable from the URL │
└──────────────┬────────────────┘                              └───────────────▲──────────────┘
               │ POST /api/v1/club/record-appearance                            │
               │  { athlete_id }                                                 │ WS push,
               ▼                                                                 │ scoped to
     internal/club.Handler                                                      │ this athlete_id
               │  looks up current progress for that athlete,                    │
               │  signs + forwards exactly like a real                          │
               │  ScoreAlerts webhook would                                     │
               ▼                                                                 │
     POST /api/v1/webhooks/scoreboard  (HMAC/RSA signed, same code path)         │
               │                                                                 │
               ▼                                                                 │
     internal/ingest → outbox → NATS `events.performance`                       │
               │                                                                 │
               ▼                                                                 │
     cmd/worker: internal/milestone.Engine (tranche state machine)              │
               │                                                                 │
               ▼                                                                 │
     outbox → NATS `events.milestone`                                           │
               │                                                                 │
               ▼                                                                 │
     cmd/api subscriber ──────────────┬──────────────────────────────────────────┘
                                       │
                                       └──▶ realtime.ClubHub.Broadcast()  (every ClubBoard viewer)
```

**One event, two audiences, one code path.** The milestone engine doesn't know or care whether
the event that fired came from `/dev/simulate` (a raw curl demo) or `/club/record-appearance`
(the ClubBoard UI) — both are just signed webhooks. Every `MilestoneChanged` event that comes off
the bus is fanned out **twice** in `cmd/api`:

```go
if aid, err := uuid.Parse(evt.AthleteID); err == nil {
    hub.Push(aid, data)       // only the connected client(s) for THIS athlete_id
}
clubHub.Broadcast(data)      // every connected ClubBoard viewer, unscoped
```

This is provable, not just claimed — see `README-run.md` §7 for the exact commands that show a
club action for player A never reaching player B's WebSocket, while the club console sees both.

## 3. Repository layout (actual)

```
cmd/
  api/main.go        HTTP gateway: routing, auth middleware, webhook route, club routes,
                      /me/* routes, WS upgrade for both boards, outbox relay goroutine,
                      dev-only token minter + simulate endpoint, static dashboard serving
  worker/main.go      bounded worker pool consuming NATS, running the milestone engine

internal/
  platform/
    config/           env-var loader (Config struct, sane dev defaults)
    log/              slog JSON logger setup
    db/                pgxpool + golang-migrate wiring (migrations are compiled into the binary)
    bus/               NATS JetStream connect/publish/subscribe/consume wrapper
    httpx/             tiny JSON response helpers
  auth/               JWT middleware + Mint(); AthleteID(ctx)/Role(ctx) accessors
  contract/           domain types + repo (sqlc) + service (adds audit log) + handler (/me/contracts)
  ingest/             Verifier interface (HMAC + RSA impls), webhook handler, outbox relay,
                      ForwardSigned() — the one function that "signs and sends a webhook",
                      shared by the dev simulate endpoint AND the ClubBoard action
  milestone/
    domain/           tranche.go — the pure state machine (quiet→warm→hot→fulfilled), zero I/O
    engine.go          worker-side: applies one event to an athlete's milestones, in one DB tx
    handler.go          GET /api/v1/me/milestones
  club/               roster read + "record appearance" action — the ClubBoard's server side
  realtime/
    hub.go / ws.go     per-athlete-scoped WebSocket hub for PlayerBoard (/me/stream)
    club_hub.go         unscoped broadcast hub for ClubBoard (/club/stream)
  events/             shared event payload types (PerformanceObserved, MilestoneChanged)
  query/              sqlc-generated Go (DO NOT EDIT) + queries/*.sql (hand-written, source of truth)

migrations/           golang-migrate SQL, embedded via go:embed, applied automatically on api boot
  0001_init            full schema
  0002_seed             seed athlete #1 "Everton" (Flamengo→Benfica)
  0003_seed_second_athlete  seed athlete #2 "Rafael Silva" (Palmeiras→Corinthians) — added
                        specifically to make cross-athlete WS scoping demonstrable

web/                  embedded via go:embed, served by cmd/api at DEV_MODE=true
  index.html           PlayerBoard — one athlete's read-only view + live feed
  club.html            ClubBoard  — roster + record-appearance + broadcast feed

scripts/
  send-webhook.sh       hand-signs and posts a raw webhook (bypasses both UIs, pure backend demo)
  dev-restart.sh         rebuild + restart api & worker after a code change (see README-run.md)
```

Two details that matter operationally (see `README-run.md`): **both `migrations/*.sql` and
`web/*.html` are compiled into the `api` binary via `go:embed`.** Editing either requires
rebuilding the binary — a browser refresh or a new migration file alone does nothing until you
run `go build` again.

## 4. Data model (PostgreSQL, actual tables)

```
athlete(id, handle, display_name, created_at)
contract(id, athlete_id, club_from, club_to, currency, fixed_amount, salary, status, created_at)
clause(id, contract_id, kind, params jsonb, created_at)
    -- params example: {"metric":"appearances","target":20,"tranche":10,"max":-1,
    --                   "amount":2500000,"currency":"BRL"}
performance_stat(id, athlete_id, metric, value, source_event_id UNIQUE, observed_at)
milestone(id, athlete_id, clause_id UNIQUE, metric, target, tranche, max_repeats,
          amount, currency, progress, state, updated_at)
    -- state machine: quiet -> warm -> hot -> fulfilled  (internal/milestone/domain/tranche.go)
payout_event(id, athlete_id, milestone_id, boundary, amount, currency, status, created_at,
             UNIQUE(milestone_id, boundary))   -- idempotent per tranche boundary
inbound_event(source_event_id PRIMARY KEY, kind, raw jsonb, received_at)   -- webhook dedupe
outbox(id, aggregate, event_type, subject, payload jsonb, created_at, published_at)
audit_log(id, athlete_id, action, detail, at)
```

All monetary columns store **minor units** (cents); the frontend divides by 100 for display.

### Seed data (migrations 0002 + 0003)

| Athlete | `athlete_id` | Contract | Clause | Starts at |
|---|---|---|---|---|
| Everton | `1111…1111` | Flamengo → Benfica, 80,000 BRL fixed / 95,000 BRL salary | appearances: target 20, tranche 10, 25,000 BRL/tranche | 18/20 (`hot`) |
| Rafael Silva | `6666…6666` | Palmeiras → Corinthians, 60,000 BRL fixed / 72,000 BRL salary | appearances: target 15, tranche 5, 18,000 BRL/tranche | 13/15 (`hot`) |

Both start two appearances from their next payout on purpose — one click on ClubBoard advances,
a second fires the bonus, for either player, independently.

## 5. The three request/event flows

### 5.1 Read flow — `GET /api/v1/me/*`

```
browser → Authorization: Bearer <JWT> → auth.Middleware validates + extracts athlete_id
        → contract.Handler / milestone.ReadHandler
        → repo query, always `WHERE athlete_id = $1` from the token, never from the URL/body
```
`athlete_id` **cannot be spoofed** — it is derived from the verified JWT claims
(`auth.AthleteID(ctx)`), not from any request path or payload.

### 5.2 Signed webhook ingest — `POST /api/v1/webhooks/scoreboard`

This is the security-critical path and the one endpoint that represents "the outside world"
(ScoreBoard/ScoreAlerts, or in this demo, the ClubBoard/dev-simulate acting as a stand-in for it).
No user JWT — authenticity comes entirely from the signature.

```
1. Read raw body (capped at 1 MiB).
2. Replay guard: reject if |now - X-Timestamp| > 5 minutes.
3. Verify X-Signature BEFORE parsing JSON:
     HMAC-SHA256 over "timestamp.rawBody", constant-time compare (hmac.Equal)
     — or RSA-SHA256 against a configured public key (Verifier interface, swappable).
4. Parse the now-trusted JSON payload.
5. One DB transaction:
     a. INSERT INTO inbound_event (source_event_id UNIQUE) — ON CONFLICT DO NOTHING.
        If it already existed (pgx.ErrNoRows): roll back, return 200 "duplicate" (idempotent no-op).
     b. INSERT INTO outbox (event_type=PerformanceObserved, subject=events.performance)
     c. COMMIT — both writes land together or neither does (transactional outbox).
6. Return 202 Accepted immediately. No business logic runs on the request path.
```

A separate goroutine (`ingest.Relay`, started in `cmd/api`) polls `outbox` every 250ms, publishes
unpublished rows to NATS, then marks them published — at-least-once delivery, decoupled from the
request/response cycle.

### 5.3 Milestone engine — `cmd/worker`

```
NATS events.performance → partitioned by fnv32(athlete_id) % pool_size → per-partition goroutine
        → milestone.Engine.Handle(), one DB transaction per event:
            1. INSERT performance_stat (ON CONFLICT source_event_id DO NOTHING — idempotent)
            2. SELECT … FOR UPDATE the athlete's milestones for this metric (row lock — 
               serializes concurrent events for the SAME athlete; different athletes proceed 
               in parallel on different partitions)
            3. domain.Advance(oldProgress, newValue, target, tranche, maxRepeats) — pure function,
               returns new progress, new state, and every tranche boundary newly crossed
            4. UPDATE milestone (progress, state)
            5. for each crossed boundary: INSERT payout_event (UNIQUE(milestone_id,boundary) — 
               idempotent even if the event is redelivered)
            6. INSERT outbox (MilestoneAdvanced, or MilestoneFulfilled if any boundary crossed;
               subject=events.milestone)
            7. COMMIT
        → ack the NATS message only after a successful commit; NAK (redelivery) on any error
```

`domain.Advance` is the one truly interview-worthy piece of business logic: it is a **pure
function with zero I/O**, fully exercised by table-driven tests covering multi-boundary jumps
(e.g. 18 → 45 appearances in one event correctly fires *three* payouts), `max_repeats` capping,
and idempotent replay of the same value.

### 5.4 Realtime fan-out — dual hubs, one subscription

`cmd/api` subscribes once to NATS `events.milestone` and does two things with every message:

```go
if aid, err := uuid.Parse(evt.AthleteID); err == nil {
    hub.Push(aid, data)      // realtime.Hub — keyed by athlete_id, only that athlete's
                              // connected /me/stream socket(s) receive it
}
clubHub.Broadcast(data)      // realtime.ClubHub — every /club/stream socket receives everything
```

`Hub` (player-scoped) and `ClubHub` (broadcast) are two small, separate types in
`internal/realtime` — deliberately not unified into one "topic" abstraction, because their
authorization models are different in principle (player: exactly one tenant; club: an entire
roster) even though today neither the exact JWT-vs-no-auth split is production-hardened (see §7).

### 5.5 ClubBoard's "Record appearance" action

`internal/club.Handler.recordAppearance`:
```
1. Look up the athlete's current milestone progress (ListMilestonesByAthlete, filter metric).
2. next = progress + 1.
3. ingest.ForwardSigned(ctx, selfURL, webhookSecret, "club-appearance", athleteID, "appearances", next)
     — builds the JSON body, computes the HMAC signature, POSTs to the real
       /api/v1/webhooks/scoreboard endpoint on the same process.
4. Relay the ingest response straight back to the ClubBoard UI.
```
This is intentionally **not** a shortcut that mutates the database directly — it goes through the
exact same signed-webhook code path a real ScoreAlerts call would, so the demo proves the
integration, not just the UI.

## 6. Concurrency model

- **Worker pool** (`cmd/worker`): N buffered-channel partitions (`WORKER_POOL_SIZE`, default 4),
  each with its own goroutine. Incoming NATS deliveries are routed to a partition by
  `fnv32(athlete_id) % N` — one athlete's events always land on the same goroutine (ordering),
  different athletes spread across goroutines (parallelism).
- `errgroup.WithContext` fans the N worker goroutines out and cancels them together on
  SIGINT/SIGTERM or on the first unrecoverable error.
- `context.Context` carries cancellation through every DB call and NATS operation.
- The outbox relay (`cmd/api`) and the NATS→hub subscriber are independent goroutines from the
  HTTP server; a slow or crashed webhook handler never blocks them, and vice versa.
- WebSocket writes are serialized per-connection by a dedicated `writePump` goroutine reading off
  a bounded channel (32-deep); a slow browser client gets messages dropped, not the hub blocked.

## 7. Security — what's real vs. what's deferred

**Implemented:**
- Inbound webhook authenticity: HMAC-SHA256 (default) or RSA-SHA256 (`Verifier` interface,
  config-selected), verified **before** JSON parsing, constant-time compare, 5-minute replay
  window.
- Idempotent ingest: `inbound_event.source_event_id UNIQUE` — duplicate delivery is a `200` no-op,
  proven by an integration test.
- User auth: JWT (HS256), `athlete_id`/`role` in claims, `auth.AthleteID(ctx)` is the *only* way a
  handler learns which athlete it's serving — never trusted from the URL or body.
- Append-only `audit_log` row written on every contract/clause read.

**Deferred (explicitly, not accidentally):**
- **ClubBoard has no auth at all** (`/api/v1/club/*` is open). It represents "the club's own
  backend calling in," which in production would carry its own service credential or an
  agent-scoped JWT — building that model was out of scope for a demo whose point is the pipeline,
  not club-side authz. Documented here so it isn't mistaken for an oversight.
- **No Postgres Row-Level Security.** Tenant isolation today is `WHERE athlete_id = $1` in every
  query, which is correct but single-layered; RLS would be the defense-in-depth second layer.
- **No column/field encryption at rest**, no KMS/pgcrypto — amounts and clause text are plaintext
  in Postgres.
- **No consent model / GDPR export-delete** — there's no `consent` table gating what a club may
  sync, and no subject-access-request endpoints.
- **No rate limiting or Redis** — nothing stops a client from hammering `/dev/token` or
  `/club/record-appearance`.

## 8. API surface (actual)

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/healthz`, `/readyz` | — | liveness / readiness (readyz pings the DB) |
| POST | `/api/v1/webhooks/scoreboard` | HMAC/RSA signature | signed ingest, 202/200/401 |
| GET | `/api/v1/club/athletes` | — (deferred) | roster across all players + their milestone |
| POST | `/api/v1/club/record-appearance` | — (deferred) | `{athlete_id}` → signs + forwards a webhook |
| WS | `/api/v1/club/stream` | — (deferred) | broadcast: every `MilestoneAdvanced`/`Fulfilled` |
| GET | `/api/v1/me/contracts` | JWT | this athlete's contracts |
| GET | `/api/v1/me/contracts/{id}/clauses` | JWT | clauses for one contract, still athlete-scoped |
| GET | `/api/v1/me/milestones` | JWT | this athlete's milestone progress |
| WS | `/api/v1/me/stream?token=…` | JWT | only this athlete's `MilestoneAdvanced`/`Fulfilled` |
| POST | `/api/v1/dev/token`, `/api/v1/dev/simulate` | DEV_MODE only | dev convenience, see README-run.md |

## 9. Tech stack (actual)

| Concern | Choice |
|---|---|
| Language | Go 1.25 (module declares 1.22 minimum) |
| HTTP router | `chi` v5 |
| WebSocket | `gorilla/websocket` |
| DB driver / codegen | `pgx/v5` + `sqlc` (hand-written SQL in `internal/query/queries/*.sql`) |
| Migrations | `golang-migrate`, embedded via `go:embed`, applied on api boot |
| Message bus | NATS JetStream (durable, per-partition ordering) |
| Auth | `golang-jwt/v5`, HS256 |
| Logging | `log/slog`, JSON handler |
| Concurrency | `golang.org/x/sync/errgroup`, buffered channels, `context` |
| Frontend | two self-contained HTML+JS pages, no build step, embedded via `go:embed` |
| Containerization | multi-stage `Dockerfile` (api/worker targets) + `docker-compose.yml` |

## 10. What's deferred (product features, not backend plumbing)

Sponsorship conflict guard, image-rights registry, payout ledger / proof-of-income export, the
RAG "ask my contract" assistant, a reconciler against ScoreBoard's `source_version` cursor. All
of these were in the original product concept (see the file history / `03_INTERVIEW_TALKING_POINTS.md`)
but are out of scope for this backend-and-two-dashboards demo. The module boundaries
(`internal/<name>`, event bus, outbox) are shaped so any of them could be added as a new
`internal/` package without touching the ones that exist.
