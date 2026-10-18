# 0012 - Postgres runtime guards

- **Status**: accepted
- **Date**: 2026-10-18

## Context

The query gates (ADR-0011) judge SQL before it runs. Two things they cannot see are left: how a service behaves when the database is slow or stuck, and a loop that runs a good query once per row. The first is the "no stuck process" goal at the database; the second is the N+1 that ADR-0006 said pprof cannot show.

## Decision

1. **The root module takes `github.com/jackc/pgx/v5` (MIT) for Postgres.** ADR-0001 chose pgx with sqlc; this adds the dependency.
2. **`platform/postgres.Connect` is the only way a service opens a pool, and it refuses a zero timeout.** Every connection is created with `statement_timeout`, `lock_timeout`, and `idle_in_transaction_session_timeout` set as session parameters, so the server cancels a runaway statement, a lock wait, or a transaction left open, whatever the client does. The pool also bounds its size, connection lifetime, idle time, and connect time. Defaults: 5s statements, 2s lock waits, 10s idle in a transaction.
3. **A query budget makes an N+1 a test failure.** `postgres.WithBudget(ctx, limits)` attaches a counter to a context; the pool's tracer records every statement run under it, batch members included. `Budget.Err()` reports a total over the limit, or one statement text run too many times, which is the signature of a query in a loop. Defaults: 20 statements, and 3 runs of any one. A use-case test asserts `budget.Err()` is nil, and `postgres.BudgetMiddleware` logs a warning for any HTTP request that exceeds its budget.
4. **The tracer logs slow queries without their arguments.** A statement slower than the configured threshold (200ms) is logged with its text and duration. Arguments are never logged, since they carry personal data.
5. **Tests that need a real database use `platform/postgres/postgrestest` and `TEST_DATABASE_URL`.** Each test gets its own database with the service's migrations applied. When the variable is unset the test skips loudly, unless `AGENTCTL_REQUIRE_DB` is set, as in ADR-0011. `agentctl db test` starts a throwaway Postgres, sets the variable, and runs the tests.

## Consequences

- Positive: a stuck or slow database cannot hold a request or a connection indefinitely, and the most common ORM-era performance bug fails a test with the offending statement named.
- Negative: session parameters are sent in the startup message, which some connection poolers in transaction mode reject; if a service ever sits behind one, the parameters move to a per-transaction `SET LOCAL`. The budget counts statements and cannot tell a legitimate loop from a bad one; the defaults leave room and a test can raise its own limits with a reason.
- Neutral: the budget is per context, so work started on a context that outlives the request is not counted.

## Alternatives considered

- **An ORM with eager loading**: hides the N+1 instead of failing it, and conflicts with sqlc and the layering rules.
- **`otelpgx` tracing**: the right tool for production traces, and it can sit beside this tracer later. It does not fail a test.
- **Client-side deadlines only**: a context deadline cancels the client's wait, but a server left running a statement for a client that gave up still holds locks; the session parameters stop it at the source.
- **`testcontainers-go` for the test database**: rejected in ADR-0011 for the same reasons.
