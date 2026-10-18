-- name: ListNotesByName :many
SELECT id FROM notes WHERE name = $1 LIMIT $2;

-- name: ListNotesByNameOrdered :many
SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2;
