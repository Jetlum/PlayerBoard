# PlayerBoard — Latest Update

_Snapshot taken 2026-07-09. Backend + worker started and verified healthy._

## What this is
Event-driven Go backend behind two dashboards:
- **PlayerBoard** (`web/index.html`) — one athlete's contract/milestone view, live over WebSocket, scoped strictly to their own `athlete_id` via JWT.
- **ClubBoard** (`web/club.html`) — the club's roster view, a "Record appearance ▶" action per player, and an unscoped broadcast feed of every milestone event.

Both HTML dashboards and all SQL migrations are embedded into the single `api` binary via `go:embed` — there is no separate frontend server/build step.

## Architecture (one event, two audiences)
```
ClubBoard "Record appearance" ─▶ POST /api/v1/club/record-appearance
      ▶ signs + forwards exactly like a real ScoreAlerts webhook
      ▶ POST /api/v1/webhooks/scoreboard (HMAC/RSA signed)
      ▶ internal/ingest → outbox → NATS events.performance
      ▶ cmd/worker: internal/milestone.Engine (tranche state machine)
      ▶ outbox → NATS events.milestone
      ▶ cmd/api subscriber fans out twice:
            - hub.Push(athlete_id)   → only that athlete's PlayerBoard WS
            - clubHub.Broadcast()    → every ClubBoard viewer
```

- `cmd/api` — HTTP gateway: auth, contract reads, signed webhook ingest, club console routes, outbox relay, dual WebSocket fan-out, serves both dashboards.
- `cmd/worker` — milestone engine, bounded per-athlete-partitioned worker pool.
- `internal/` — platform (config/log/db/bus/httpx), auth, contract, ingest, milestone, club, realtime, query (sqlc), events.
- `migrations/` — golang-migrate SQL, auto-applied on `api` boot.

## Current run status
| Component | Status |
|---|---|
| Postgres (`playerboard-postgres-1`, port 5544) | ✅ running |
| NATS (`playerboard-nats-1`, ports 4222/8222) | ✅ running |
| `api` (port 8080) | ✅ running — `/healthz` → 200 |
| `worker` | ✅ running |
| PlayerBoard UI | http://localhost:8080/ |
| ClubBoard UI | http://localhost:8080/club.html |

Seeded roster (`GET /api/v1/club/athletes`):
- **Everton** — 18/20 appearances, tranche R$2.5M, state `hot` (2 clicks from firing)
- **Rafael Silva** — 13/15 appearances, tranche R$1.8M, state `hot` (2 clicks from firing)

## Endpoints
| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/healthz`, `/readyz` | — | liveness/readiness |
| POST | `/api/v1/webhooks/scoreboard` | HMAC/RSA sig | signed stat ingest |
| GET | `/api/v1/club/athletes` | — (deferred) | roster + milestones |
| POST | `/api/v1/club/record-appearance` | — (deferred) | `{athlete_id}` → signs + forwards webhook |
| WS | `/api/v1/club/stream` | — (deferred) | broadcast: every player's milestone events |
| GET | `/api/v1/me/contracts` | JWT | this athlete's contracts |
| GET | `/api/v1/me/contracts/{id}/clauses` | JWT | clauses for a contract |
| GET | `/api/v1/me/milestones` | JWT | this athlete's milestone progress |
| WS | `/api/v1/me/stream?token=…` | JWT | only this athlete's milestone events |
| POST | `/api/v1/dev/token`, `/api/v1/dev/simulate?value=N` | DEV_MODE | demo helpers |

## Deferred / known gaps (by design, documented in `01_PRODUCT_AND_ARCHITECTURE.md` §7/§10)
Sponsorship conflict guard, image-rights registry, payout ledger, RAG assistant, Postgres RLS,
ClubBoard auth, GDPR export/delete, `testcontainers-go` CI (a hand-written harness against
`TEST_DATABASE_URL` was used instead). No automated test suite is maintained for this demo project.

## Re-running after code changes
```bash
./scripts/dev-restart.sh
```
Rebuilds `api`+`worker` (picks up Go changes, embedded HTML, embedded migrations), restarts in
background. Logs: `tmp/logs/api.log`, `tmp/logs/worker.log`.
