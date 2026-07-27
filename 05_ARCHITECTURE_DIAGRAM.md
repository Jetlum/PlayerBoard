# PlayerBoard — Architecture Diagram

## System overview

```mermaid
flowchart TB
    subgraph clients["Browsers (dev-mode static, same origin)"]
        CB["ClubBoard<br/>club.html"]
        PB["PlayerBoard<br/>index.html<br/>(JWT: athlete_id)"]
    end

    subgraph api["cmd/api — HTTP gateway :8080"]
        CLUB["club handler<br/>POST /club/appearance<br/>(signs webhook server-side)"]
        ING["ingest handler<br/>POST /webhooks/scoreboard<br/>1 MiB cap · ts window · HMAC/RSA verify<br/>verify BEFORE parse"]
        RELAY["outbox relay<br/>poll 250ms, batch 100<br/>publish-then-mark"]
        SUB["milestone subscriber<br/>(durable: api-ws)"]
        HUB["Hub<br/>athlete_id → WS conns"]
        CHUB["ClubHub<br/>broadcast WS"]
        ME["/me REST<br/>contracts · milestones · documents<br/>(JWT middleware)"]
    end

    subgraph pg["PostgreSQL 16 (source of truth)"]
        INB[("inbound_event<br/>PK source_event_id = dedupe")]
        OUT[("outbox<br/>partial idx WHERE published_at IS NULL")]
        DOM[("performance_stat · milestone<br/>payout_event UNIQUE(milestone_id, boundary)<br/>contract · clause · contract_document")]
    end

    subgraph nats["NATS JetStream — stream EVENTS"]
        PERF[/"events.performance"/]
        MILE[/"events.milestone"/]
    end

    subgraph worker["cmd/worker"]
        PART["partition router<br/>fnv32a(athlete_id) % N<br/>MaxAckPending 256"]
        ENG["milestone engine — one tx:<br/>stat insert · milestones FOR UPDATE<br/>domain.Advance · payout · outbox<br/>ack / nak"]
    end

    CB -->|"record appearance"| CLUB
    CLUB -->|"signed webhook (self-call)"| ING
    EXT["External sender<br/>(ScoreAlerts-style)"] -->|"HMAC-SHA256(ts.body)"| ING

    ING -->|"one tx: dedupe + outbox"| INB
    ING --> OUT
    RELAY -->|drain| OUT
    RELAY --> PERF
    PERF --> PART
    PART --> ENG
    ENG --> DOM
    ENG -->|"MilestoneChanged → outbox"| OUT
    RELAY --> MILE
    MILE --> SUB
    SUB -->|"Push(athlete_id)"| HUB
    SUB -->|Broadcast| CHUB
    HUB -->|"WS /me/stream"| PB
    CHUB -->|"WS /club/stream"| CB
    PB --> ME
    ME --> DOM
```

## One event, end to end

```mermaid
sequenceDiagram
    participant C as ClubBoard
    participant A as API
    participant P as Postgres
    participant N as JetStream
    participant W as Worker
    participant B as PlayerBoard (WS)

    C->>A: POST /club/appearance
    A->>A: sign ts.body (HMAC-SHA256), self-POST /webhooks/scoreboard
    A->>A: check timestamp (±5 min), verify signature
    A->>P: TX: INSERT inbound_event (dedupe) + INSERT outbox
    A-->>C: 202 Accepted (or 200 duplicate)
    Note over A,P: request path ends — no business logic yet

    loop every 250 ms
        A->>P: SELECT unpublished outbox
        A->>N: publish events.performance
        A->>P: mark published
    end

    N->>W: deliver (partition by athlete_id)
    W->>P: TX: stat insert · milestones FOR UPDATE · Advance() · payout · outbox
    W->>N: ack (nak on error → redelivery)

    loop relay again
        A->>N: publish events.milestone
    end
    N->>A: deliver (durable api-ws)
    A->>B: Hub.Push → that athlete only
    A->>C: ClubHub.Broadcast → all club viewers
```

## Guarantees at a glance

| Concern | Mechanism | Where |
|---|---|---|
| No dual-write loss | transactional outbox | `ingest/handler.go`, `outbox` table |
| Webhook dedupe | PK `source_event_id` | `inbound_event` |
| No double payout | `UNIQUE(milestone_id, boundary)` | `payout_event` |
| Per-athlete ordering | fnv partition (perf) + `FOR UPDATE` (correctness) | `cmd/worker`, engine |
| Replay protection | ±5 min window, ts bound into signature | `ingest/verify.go` |
| WS privacy | athlete_id from JWT claim, never from client | `realtime/hub.go` |
| Delivery | at-least-once everywhere, idempotent consumers | outbox + JetStream ack/nak |
