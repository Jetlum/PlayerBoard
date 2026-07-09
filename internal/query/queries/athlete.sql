-- name: GetAthlete :one
SELECT id, handle, display_name, created_at
FROM athlete
WHERE id = $1;
