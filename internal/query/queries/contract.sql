-- name: ListContractsByAthlete :many
SELECT id, athlete_id, club_from, club_to, currency, fixed_amount, salary, status, created_at
FROM contract
WHERE athlete_id = $1
ORDER BY created_at;

-- name: ListClausesForAthleteContract :many
SELECT c.id, c.contract_id, c.kind, c.params, c.created_at
FROM clause c
JOIN contract ct ON ct.id = c.contract_id
WHERE c.contract_id = $1
  AND ct.athlete_id = $2
ORDER BY c.created_at;
