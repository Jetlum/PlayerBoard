-- name: InsertInboundEvent :one
-- Returns the row only on first insert; ON CONFLICT DO NOTHING => no row (pgx.ErrNoRows) on duplicate.
INSERT INTO inbound_event (source_event_id, kind, raw)
VALUES ($1, $2, $3)
ON CONFLICT (source_event_id) DO NOTHING
RETURNING source_event_id;

-- name: InsertPerformanceStat :exec
INSERT INTO performance_stat (id, athlete_id, metric, value, source_event_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (source_event_id) DO NOTHING;

-- name: InsertPayoutEvent :exec
INSERT INTO payout_event (id, athlete_id, milestone_id, boundary, amount, currency, status)
VALUES ($1, $2, $3, $4, $5, $6, 'expected')
ON CONFLICT (milestone_id, boundary) DO NOTHING;

-- name: InsertOutbox :one
INSERT INTO outbox (aggregate, event_type, subject, payload)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: ListUnpublishedOutbox :many
SELECT id, subject, payload
FROM outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT $1;

-- name: MarkOutboxPublished :exec
UPDATE outbox SET published_at = now() WHERE id = $1;

-- name: InsertAudit :exec
INSERT INTO audit_log (athlete_id, action, detail)
VALUES ($1, $2, $3);
