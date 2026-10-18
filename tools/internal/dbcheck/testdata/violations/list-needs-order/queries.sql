-- name: ListNotes :many
SELECT id FROM notes WHERE created_at > $1 LIMIT $2;
