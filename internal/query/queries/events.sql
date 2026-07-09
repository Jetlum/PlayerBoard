-- The transactional outbox already IS a durable, ordered event log (every MilestoneAdvanced /
-- MilestoneFulfilled ever emitted, with its payload and timestamp). Re-reading it on page load
-- is how both dashboards' live-feed panels survive a browser refresh instead of coming back
-- empty — the WebSocket only ever delivers events that happen *after* it connects.

-- name: ListRecentMilestoneEventsForAthlete :many
SELECT event_type, payload, created_at
FROM outbox
WHERE subject = 'events.milestone' AND payload->>'athlete_id' = $1::text
ORDER BY created_at DESC
LIMIT $2;

-- name: ListRecentMilestoneEvents :many
-- Unscoped: backs the ClubBoard feed, which shows every player's events.
SELECT event_type, payload, created_at
FROM outbox
WHERE subject = 'events.milestone'
ORDER BY created_at DESC
LIMIT $1;
