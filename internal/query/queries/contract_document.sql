-- name: InsertContractDocument :exec
INSERT INTO contract_document (id, athlete_id, contract_id, filename, content_type, raw_text, file_data, label, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListContractDocumentsByAthlete :many
SELECT id, athlete_id, contract_id, filename, content_type, raw_text, label, status, created_at
FROM contract_document
WHERE athlete_id = $1
ORDER BY created_at DESC;

-- name: GetContractDocument :one
SELECT id, athlete_id, contract_id, filename, content_type, raw_text, label, status, created_at
FROM contract_document
WHERE id = $1 AND athlete_id = $2;

-- name: UpdateContractDocumentText :exec
UPDATE contract_document
SET raw_text = $3, status = 'analyzed'
WHERE id = $1 AND athlete_id = $2;

-- name: DeleteContractDocument :exec
DELETE FROM contract_document
WHERE id = $1 AND athlete_id = $2;
