# PlayerBoard

A working Go backend + two live dashboards, built for a Backend Golang interview at Score
Technologies GmbH, demonstrating a player-side companion to ScoreTech's ScoreBoard / ScoreAlerts.

**Idea in one line:** ScoreBoard gives clubs real-time visibility into contract obligations;
players have none. PlayerBoard mirrors that same data, owned by the player — and now ships as
**two consoles**: **ClubBoard** (the club recording what happened) and **PlayerBoard** (the
specific athlete finding out about it, live).

## Files
1. **01_PRODUCT_AND_ARCHITECTURE.md** — the as-built architecture: every module, the data model,
   the exact request/event flows, the security model (what's real vs. deferred), the full API
   surface. *Read this first — it describes the actual running system, not just the concept.*
2. **02_LLM_BUILD_PROMPT.md** — the original prompt used to scaffold the backend, now annotated
   with a status banner showing what shipped vs. what was simplified.
3. **03_INTERVIEW_TALKING_POINTS.md** — decision-and-tradeoff cheat sheet, reconciled with what's
   actually implemented (corrected where the original concept overclaimed).
4. **README-run.md** — every command: first run, and how to rebuild/restart after a code change.

## The demo
Open the **ClubBoard** (`/club.html`) and the **PlayerBoard** (`/index.html`) side by side. Click
**"Record appearance"** for a player on ClubBoard: a signed webhook flows through an idempotent Go
ingest pipeline, advances that player's performance clause, and **that specific player's own
PlayerBoard updates over WebSocket in real time** — while ClubBoard's broadcast feed shows the
same event. One more click crosses the bonus threshold and both consoles show the payout fire,
simultaneously, scoped correctly to the one player it belongs to.

## Quick start
```bash
docker compose up -d postgres nats
./scripts/dev-restart.sh        # builds + starts api (:8080) and worker
```
Then open `http://localhost:8080/` (PlayerBoard) and `http://localhost:8080/club.html` (ClubBoard).
Full details, env vars, and the "after you change code" workflow are in **README-run.md**
