-- name: ListNotes :many
-- agent:allow list-needs-limit
SELECT id FROM notes ORDER BY id;
