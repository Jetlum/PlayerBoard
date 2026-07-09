-- name: ListMilestonesByAthlete :many
SELECT id, athlete_id, clause_id, metric, target, tranche, max_repeats, amount, currency, progress, state, updated_at
FROM milestone
WHERE athlete_id = $1
ORDER BY updated_at DESC;

-- name: ListMilestonesByAthleteMetric :many
SELECT id, athlete_id, clause_id, metric, target, tranche, max_repeats, amount, currency, progress, state, updated_at
FROM milestone
WHERE athlete_id = $1 AND metric = $2
FOR UPDATE;

-- name: UpdateMilestoneProgress :exec
UPDATE milestone
SET progress = $2, state = $3, updated_at = now()
WHERE id = $1;
