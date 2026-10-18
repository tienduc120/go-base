-- name: InsertNote :exec
INSERT INTO notes (id, name, created_at) VALUES ($1, $2, $3);
