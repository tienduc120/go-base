-- name: GetNote :one
-- agent:allow no-such-rule: a typo that would otherwise look like an accepted finding
SELECT id FROM notes WHERE id = $1;
