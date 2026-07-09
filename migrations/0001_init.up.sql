-- PlayerBoard core schema (demo slice). All money fields are BIGINT minor units.
CREATE TABLE athlete (
    id           UUID PRIMARY KEY,
    handle       TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE contract (
    id           UUID PRIMARY KEY,
    athlete_id   UUID NOT NULL REFERENCES athlete(id),
    club_from    TEXT NOT NULL,
    club_to      TEXT NOT NULL,
    currency     TEXT NOT NULL,
    fixed_amount BIGINT NOT NULL,
    salary       BIGINT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_contract_athlete ON contract (athlete_id);

CREATE TABLE clause (
    id          UUID PRIMARY KEY,
    contract_id UUID NOT NULL REFERENCES contract (id),
    kind        TEXT NOT NULL,
    params      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_clause_contract ON clause (contract_id);

CREATE TABLE performance_stat (
    id              UUID PRIMARY KEY,
    athlete_id      UUID NOT NULL REFERENCES athlete (id),
    metric          TEXT NOT NULL,
    value           BIGINT NOT NULL,
    source_event_id TEXT NOT NULL UNIQUE,
    observed_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_stat_athlete_metric ON performance_stat (athlete_id, metric);

-- One milestone per performance clause. amount/currency copied from the clause so the
-- engine never has to reach into jsonb on the hot path.
CREATE TABLE milestone (
    id          UUID PRIMARY KEY,
    athlete_id  UUID NOT NULL REFERENCES athlete (id),
    clause_id   UUID NOT NULL UNIQUE REFERENCES clause (id),
    metric      TEXT NOT NULL,
    target      BIGINT NOT NULL,
    tranche     BIGINT NOT NULL,
    max_repeats BIGINT NOT NULL,          -- -1 = unlimited
    amount      BIGINT NOT NULL,
    currency    TEXT NOT NULL,
    progress    BIGINT NOT NULL DEFAULT 0,
    state       TEXT NOT NULL DEFAULT 'quiet',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_milestone_athlete ON milestone (athlete_id);

-- UNIQUE(milestone_id, boundary) makes payout creation idempotent per tranche boundary.
CREATE TABLE payout_event (
    id           UUID PRIMARY KEY,
    athlete_id   UUID NOT NULL REFERENCES athlete (id),
    milestone_id UUID NOT NULL REFERENCES milestone (id),
    boundary     BIGINT NOT NULL,
    amount       BIGINT NOT NULL,
    currency     TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'expected',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (milestone_id, boundary)
);

-- Webhook dedupe: source_event_id is the natural key from ScoreAlerts.
CREATE TABLE inbound_event (
    source_event_id TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    raw             JSONB NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Transactional outbox: state change + event emission committed together.
CREATE TABLE outbox (
    id           BIGSERIAL PRIMARY KEY,
    aggregate    TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    subject      TEXT NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);
CREATE INDEX idx_outbox_unpublished ON outbox (id) WHERE published_at IS NULL;

CREATE TABLE audit_log (
    id         BIGSERIAL PRIMARY KEY,
    athlete_id UUID,
    action     TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT '',
    at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
