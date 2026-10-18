-- name: ListNotes :many
SELECT id FROM notes ORDER BY random() LIMIT $1;
