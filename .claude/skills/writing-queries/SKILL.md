---
name: writing-queries
description: Use when adding or changing a SQL query or a migration in a service. Writes queries the query gate accepts the first time and fixes a finding at its cause instead of accepting it.
---

# Write SQL that survives scale

Queries run through sqlc. They live in `services/<name>/internal/infrastructure/postgres/queries/*.sql`, the schema in `migrations/NNNN_name.sql` beside them. `agentctl db check` reads both, and the pre-commit hook runs it.

## Writing a query

1. **Name it** `Create`, `Get`, `List`, `Update`, `Delete`, `Count` or `Exists` plus the subject, with the sqlc command: `-- name: ListNotesSince :many`.
2. **Select the columns you use.** Never `*`, never `RETURNING *`.
3. **Lists paginate by key**, never by `OFFSET`: `WHERE (created_at, id) > ($1, $2) ORDER BY created_at, id LIMIT $3`. A list has both `ORDER BY` and `LIMIT`.
4. **Writes name their rows**: `UPDATE` and `DELETE` always have a `WHERE`.
5. **Every filter and every `ORDER BY` has an index** that serves it. Add it in a new migration in the same change.
6. **No more than three joins.** More means the query wants a read model or two queries.
7. **One query per use case step, never one per row.** A query inside a loop is an N+1; fetch the set with `= ANY($1)` or a join.

## Changing the schema

Migrations are forward-only. Never edit one that has been committed: add `0002_...sql`. Expand first (add the column nullable, backfill, then enforce), contract in a later change, so old and new code can run together.

## When `db check` fails

Fix the query. Read the finding: it names the file, the line, the rule, and the fix.

- `seq-scan`: the filter or ordering has no index, or the index does not lead with the filtered column. Add or reshape the index in a migration.
- `list-needs-limit`, `no-offset`, `list-needs-order`: change the pagination, not the rule.
- `plan-failed`: Postgres rejected the query when planning; the message says why.

Only when the finding is true and acceptable (a lookup table that will never pass a few dozen rows) accept it in the query file with the reason, then tell the user, because an allow is debt:

```sql
-- name: ListKinds :many
-- agent:allow list-needs-limit: the kinds table is a fixed enum of under ten rows
SELECT id, label FROM kinds ORDER BY id;
```

An allow with no reason is itself a finding. Never edit `tools/internal/dbcheck` to make a query pass.

## Run it

`go run ./tools/agentctl db check` (needs Docker for plans; `--no-plan` skips them). The hook runs it for you at commit; run it by hand only to read a failure.
