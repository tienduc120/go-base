-- name: GetNote :one
SELECT id, name, created_at FROM notes WHERE id = $1;
