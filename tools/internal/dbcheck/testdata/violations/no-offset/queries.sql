-- name: ListNotes :many
SELECT id FROM notes ORDER BY id LIMIT $1 OFFSET $2;
