-- name: ListNotes :many
SELECT a.id FROM notes a
JOIN notes b ON b.id = a.id
JOIN notes c ON c.id = a.id
JOIN notes d ON d.id = a.id
JOIN notes e ON e.id = a.id
ORDER BY a.id
LIMIT $1;
