-- name: ListRoster :many
-- Club-side view across every player under contract — unlike the /me/* queries this is
-- intentionally not scoped to one athlete_id.
SELECT a.id AS athlete_id, a.display_name,
       m.id AS milestone_id, m.metric, m.progress, m.target, m.tranche, m.state, m.amount, m.currency
FROM athlete a
JOIN milestone m ON m.athlete_id = a.id
ORDER BY a.display_name;

-- name: IncrementAppearanceCounter :one
-- Single atomic statement: Postgres serializes concurrent callers, so two rapid clicks for the
-- same athlete always get distinct, correctly-ordered values (fixes a real race — see migration
-- 0004 for the incident this replaced: reading milestone.progress then adding 1 in Go).
INSERT INTO appearance_counter (athlete_id, metric, value)
VALUES ($1, $2, 1)
ON CONFLICT (athlete_id, metric) DO UPDATE SET value = appearance_counter.value + 1
RETURNING value;
