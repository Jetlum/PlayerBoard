# PlayerBoard — Run Guide

Event-driven Go backend behind **two** dashboards: **ClubBoard** (records what happened) and
**PlayerBoard** (one athlete finding out about it, live). See `01_PRODUCT_AND_ARCHITECTURE.md` for
how it all fits together.

```
cmd/api      HTTP gateway: auth, contract reads, signed webhook ingest, club console routes,
             outbox relay, dual WebSocket fan-out (per-athlete + club broadcast), dashboards
cmd/worker   milestone engine — bounded, per-athlete-partitioned worker pool
internal/    platform (config/log/db/bus/httpx) · auth · contract · ingest · milestone ·
             club · realtime · query (sqlc) · events
migrations/  golang-migrate SQL — embedded into the api binary, applied automatically on boot
web/         PlayerBoard (index.html) + ClubBoard (club.html) — embedded into the api binary
```

## Prerequisites
- Go 1.22+  ·  Docker (Postgres + NATS)  ·  `openssl` (only for `scripts/send-webhook.sh`)
- `sqlc` only if you change SQL in `internal/query/queries/*.sql`:
  `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest && sqlc generate`

---

## First run

### 1. Start infrastructure
```bash
docker compose up -d postgres nats
```
> Host Postgres port is **5544** (5432 was already taken locally). NATS is on 4222/8222.

### 2. Configure env (host run — skip if using `docker compose up --build` instead, see below)
```bash
export DATABASE_URL="postgres://player:player@localhost:5544/playerboard?sslmode=disable"
export NATS_URL="nats://localhost:4222"
export JWT_SECRET="dev-jwt-secret-change-me"
export WEBHOOK_HMAC_SECRET="dev-webhook-secret-change-me"
export SIGNATURE_SCHEME="hmac"     # or "rsa" (set WEBHOOK_RSA_PUBLIC_KEY)
export DEV_MODE="true"             # enables /dev/token, /dev/simulate, and serves the dashboards
export PORT="8080"
export WORKER_POOL_SIZE="4"
```

### 3. Build and run
```bash
go build -o bin/api ./cmd/api && go build -o bin/worker ./cmd/worker
./bin/api      # applies migrations + seed on boot, listens on :8080
./bin/worker   # consumes performance events, runs the milestone engine
```
(Run each in its own terminal, or background them — see the restart script below, which does
exactly that.)

### 4. Open both dashboards
- **PlayerBoard** — http://localhost:8080/ — one athlete's contract, milestone, and live feed.
  Defaults to the seeded athlete "Everton"; open `?athlete_id=<id>` to view a different player
  (e.g. Rafael Silva: `66666666-6666-6666-6666-666666666666`).
- **ClubBoard** — http://localhost:8080/club.html — the roster (both seeded players), a
  **"Record appearance ▶"** button per player, and a broadcast feed of every milestone event.

Click "Record appearance" on ClubBoard for a player, then watch that same player's PlayerBoard tab
update live over WebSocket — and watch ClubBoard's own feed show the same event. Two clicks on a
player crosses their bonus threshold; both consoles show the payout fire at once.

---

## Re-running after you change code

**This is the one thing to know: `web/*.html` and `migrations/*.sql` are compiled into the `api`
binary via `go:embed`.** A browser refresh alone never picks up an HTML edit, and a new migration
file alone never runs — both need a rebuild. Go source changes obviously need one too.

### The one-liner
```bash
./scripts/dev-restart.sh
```
This stops any running `api`/`worker`, rebuilds both binaries (picking up Go changes, embedded
HTML, and embedded migrations), restarts them in the background, and prints the health check +
both dashboard URLs. Logs land in `tmp/logs/api.log` and `tmp/logs/worker.log`
(`tail -f tmp/logs/*.log` to watch them). Postgres/NATS are left running — only `api`/`worker`
restart. Override any env var by exporting it before calling the script (it defaults to the same
values as the first-run section above).

### The manual equivalent (what the script does)
```bash
pkill -f './bin/api'; pkill -f './bin/worker'      # stop the old processes
go build -o bin/api ./cmd/api && go build -o bin/worker ./cmd/worker
./bin/api &      # re-applies migrations on boot (new ones run, existing ones no-op)
./bin/worker &
```

### If you changed `internal/query/queries/*.sql`
Regenerate before rebuilding — the checked-in `internal/query/*.sql.go` files are generated code:
```bash
sqlc generate
./scripts/dev-restart.sh
```

### If you added a new migration file
Just add `migrations/000N_description.up.sql` (+ `.down.sql`) and run `./scripts/dev-restart.sh`.
`golang-migrate` tracks the applied version in Postgres, so only the new file runs.

### Common gotcha: "consumer is already bound to a subscription"
This means an old `worker` process is still running (holding the NATS durable consumer) when you
start a new one. Find and kill it first:
```bash
pgrep -af '/bin/worker'    # find the stale PID
kill <pid>
```
`scripts/dev-restart.sh` already does this via `pkill` before rebuilding — this only bites if you
started a `worker` manually outside the script.

---

## Drive it from the CLI (bypasses both UIs — pure backend proof)
```bash
# Mint a dev JWT for a specific athlete (defaults to Everton if omitted)
TOKEN=$(curl -s -X POST "localhost:8080/api/v1/dev/token?athlete_id=11111111-1111-1111-1111-111111111111" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

# Reads — athlete_id comes from the token, never the path
curl -s localhost:8080/api/v1/me/contracts  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
curl -s localhost:8080/api/v1/me/milestones -H "Authorization: Bearer $TOKEN" | python3 -m json.tool

# ClubBoard's own API — no auth (see 01_PRODUCT_AND_ARCHITECTURE.md §7 for why that's deferred)
curl -s localhost:8080/api/v1/club/athletes | python3 -m json.tool
curl -s -X POST localhost:8080/api/v1/club/record-appearance \
  -H "Content-Type: application/json" \
  -d '{"athlete_id":"11111111-1111-1111-1111-111111111111"}'

# Raw signed webhooks (hand-signs like a real ScoreAlerts caller would)
./scripts/send-webhook.sh 19 evt-appearances-19   # -> 202 Accepted
./scripts/send-webhook.sh 20 evt-appearances-20   # -> 202, crosses target=20, fires the bonus
./scripts/send-webhook.sh 20 evt-appearances-20   # -> 200 duplicate (idempotent no-op)
```

## Proving the ClubBoard → PlayerBoard scoping (what the demo is actually about)
Open two terminals with `wscat` or similar, or just trust the dashboards: recording an appearance
for athlete A must reach only athlete A's `/me/stream` and the club's `/club/stream` broadcast —
never athlete B's. To see it without a browser:
```bash
# Terminal 1: connect as Rafael specifically
TOKEN=$(curl -s -X POST "localhost:8080/api/v1/dev/token?athlete_id=66666666-6666-6666-6666-666666666666" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
websocat "ws://localhost:8080/api/v1/me/stream?token=$TOKEN"     # or any WS client

# Terminal 2: club records an appearance for Everton instead
curl -s -X POST localhost:8080/api/v1/club/record-appearance \
  -H "Content-Type: application/json" \
  -d '{"athlete_id":"11111111-1111-1111-1111-111111111111"}'
# -> Terminal 1 (Rafael's socket) prints nothing. Connect a /club/stream socket instead
#    and it prints Everton's MilestoneAdvanced/Fulfilled event.
```

## Resetting demo state
The seeded milestones start two appearances from their next payout (Everton 18/20, Rafael 13/15)
so a two-click demo always fires a bonus. After firing one, reset with:
```bash
docker exec playerboard-postgres-1 psql -U player -d playerboard -c "
  UPDATE milestone SET progress=18, state='hot' WHERE id='44444444-4444-4444-4444-444444444444';
  UPDATE milestone SET progress=13, state='hot' WHERE id='99999999-9999-9999-9999-999999999999';
  DELETE FROM payout_event;
  DELETE FROM performance_stat WHERE (athlete_id='11111111-1111-1111-1111-111111111111' AND value>18)
                                   OR (athlete_id='66666666-6666-6666-6666-666666666666' AND value>13);
"
```

## One-command containerized alternative
```bash
docker compose up --build     # postgres, nats, api, worker — api/worker use the docker network
```
Note: this rebuilds images from source, so it's also a valid "re-run after code changes" path —
just slower than `scripts/dev-restart.sh` for a tight edit loop.

## Endpoints
| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET  | `/healthz`, `/readyz` | — | liveness / readiness |
| POST | `/api/v1/webhooks/scoreboard` | HMAC/RSA signature | signed stat ingest (202/200/401) |
| GET  | `/api/v1/club/athletes` | — (deferred) | roster: every player + their milestone |
| POST | `/api/v1/club/record-appearance` | — (deferred) | `{athlete_id}` → signs + forwards a webhook |
| WS   | `/api/v1/club/stream` | — (deferred) | broadcast: every player's milestone events |
| GET  | `/api/v1/me/contracts` | JWT | this athlete's contracts |
| GET  | `/api/v1/me/contracts/{id}/clauses` | JWT | clauses for a contract |
| GET  | `/api/v1/me/milestones` | JWT | this athlete's milestone progress |
| WS   | `/api/v1/me/stream?token=…` | JWT | only this athlete's `MilestoneAdvanced`/`Fulfilled` |
| POST | `/api/v1/dev/token`, `/api/v1/dev/simulate?value=N` | DEV_MODE | demo helpers |
| GET  | `/`, `/club.html` | DEV_MODE | PlayerBoard / ClubBoard dashboards |

## Deferred (see `01_PRODUCT_AND_ARCHITECTURE.md` §7/§10 for the full list and why)
Sponsorship conflict guard, image-rights registry, payout ledger, RAG assistant, Postgres RLS,
ClubBoard auth, GDPR export/delete, `testcontainers-go` CI integration (a hand-written one against
`TEST_DATABASE_URL` was used instead). The NATS/outbox/hexagonal boundaries make each a drop-in.
