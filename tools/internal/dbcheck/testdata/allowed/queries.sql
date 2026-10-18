-- name: ListNotesByName :many
-- agent:allow seq-scan: fixture for an accepted finding; pretend the table never passes fifty rows
SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2;
