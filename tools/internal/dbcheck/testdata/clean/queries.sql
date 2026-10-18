-- name: CreateNote :exec
INSERT INTO notes (id, name, created_at) VALUES ($1, $2, $3);

-- name: GetNote :one
SELECT id, name, created_at FROM notes WHERE id = $1;

-- name: ListNotes :many
SELECT id, name, created_at FROM notes
WHERE (created_at, id) > ($1, $2)
ORDER BY created_at, id
LIMIT $3;

-- name: UpdateNote :exec
UPDATE notes SET name = $2 WHERE id = $1;

-- name: DeleteNote :exec
DELETE FROM notes WHERE id = $1;

-- name: CountNotesSince :one
SELECT count(*) FROM notes WHERE created_at > $1;

-- name: ExistsNote :one
SELECT EXISTS (SELECT 1 FROM notes WHERE id = $1);
