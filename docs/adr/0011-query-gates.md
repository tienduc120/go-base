# 0011 - Query gates

- **Status**: accepted
- **Date**: 2026-10-18

## Context

ADR-0006 left two problems to the database layer: an N+1 is invisible to pprof, and a query that is fast on a developer's empty database can scan a whole table in production. Agents write queries that pass every test and fail only at scale, and nothing so far reads SQL.

`sqlc vet` was the obvious tool, and a spike against sqlc 1.31.1 showed why it is not enough alone. A rule receives the compiled query text, in which `SELECT *` is already expanded to a column list, so the most common rule cannot be written. Its `EXPLAIN` data exposes the root plan node, so a sequential scan under a `Limit` or a join passes. And its rules live in `sqlc.yaml`, which a service can edit.

## Decision

1. **A service's SQL lives in `services/<name>/internal/infrastructure/postgres/`**: `migrations/NNNN_name.sql` (forward-only plain SQL, applied in name order) and `queries/*.sql` (sqlc query files). sqlc generates the Go from them.
2. **`agentctl db check [--no-plan] [service...]` judges every service's queries.** It has sqlc compile them against the migrations, then applies these rules to the source as written and to the compiled text: no `SELECT *` or `RETURNING *`; `UPDATE` and `DELETE` need `WHERE`; a `:many` query needs `LIMIT` and an `ORDER BY` to give the limit meaning; no `OFFSET` (paginate by key); no `ORDER BY random()`; at most three joins; names start with Create, Get, List, Update, Delete, Count or Exists.
3. **The rules live in `tools/internal/dbcheck`, not in `sqlc.yaml`.** A service cannot loosen them by editing its own configuration, and they can read both the source and the compiled text.
4. **Plans are read on a throwaway Postgres.** Unless `--no-plan`, `db check` starts a container with no published port that removes itself after ten minutes even if `agentctl` is killed, applies each service's migrations to a database of its own, and runs `EXPLAIN (GENERIC_PLAN, FORMAT JSON)` on every query with sequential scans disabled for the session. Disabled, a sequential scan appears only when no index can serve the query, which an empty table would otherwise hide. The whole plan tree is walked, and an index read end to end only to filter by an unindexed column counts as a scan too, because it touches every row. A query Postgres cannot plan is a finding, not a pass.
5. **A finding is accepted in the query file with a reason**: `-- agent:allow <rule>: <why>`. The output keeps listing accepted findings. An allow with no reason, or naming a rule that does not exist, is itself a finding. The debt loop counts every allow as a suppression, so it needs triage like a `//nolint` (ADR-0008).
6. **Generated code must match its queries.** For a service with a `sqlc.yaml`, `db check` runs `sqlc diff` and reports `generated-stale` when the committed code differs from what sqlc makes of the current queries and migrations. This finding is about a service, so it cannot be accepted with an allow.
7. **Each rule has a fixture, proven against the real tools.** `tools/internal/dbcheck/testdata` holds one violating query file per rule, a compliant twin, and an accepted case. A test runs real `sqlc` and a real Postgres container over all of them. When either is missing the test skips with a loud message, unless `AGENTCTL_REQUIRE_DB` is set, which CI sets so an absent tool fails the build.

## Consequences

- Positive: the failure modes that only show at scale fail at commit, with the line, the rule, and the fix. Plans are checked on the Postgres major version production runs, via `--image`.
- Negative: the plan checks need Docker and add seconds to a run. A plan on an empty table proves the access path, not the cost; whether a query is fast at a million rows is for the load scenarios.
- Negative: sqlc writes its own version into the generated code, so everyone must use the version `agentctl doctor` pins; a newer sqlc makes `generated-stale` fire until the code is regenerated and committed.
- Neutral: migrations are forward-only. A mistake is fixed by a new migration, which suits expand-and-contract deploys.

## Alternatives considered

- **`sqlc vet` rules in `sqlc.yaml`**: standard, but blind to `SELECT *` and to anything below the root plan node, and editable per service.
- **`testcontainers-go`**: a large dependency for starting one container, and it would pull Docker client libraries into the tools module. The Docker CLI through the bounded runner is enough.
- **Running `EXPLAIN` through a Go driver**: needs a published port and a driver in the tools module; `docker exec psql` needs neither.
- **A migration linter such as squawk**: worthwhile for locks and unsafe schema changes, and a separate gate. It can join this one later.
