-- name: CreateNote :exec
INSERT INTO notes (id, name, created_at) VALUES ($1, $2, $3);

-- name: GetNote :one
SELECT id, name, created_at FROM notes WHERE id = $1;
