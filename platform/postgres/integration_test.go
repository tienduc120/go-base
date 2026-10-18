package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tienduc120/go-base/platform/postgres"
	"github.com/tienduc120/go-base/platform/postgres/postgrestest"
)

// connect opens a pool the way a service does, with cfg adjusted by edit.
func connect(t *testing.T, edit func(*postgres.Config)) *pgxpool.Pool {
	t.Helper()
	cfg := postgres.DefaultConfig(postgrestest.New(t, ""))
	edit(&cfg)
	pool, err := postgres.Connect(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func sqlState(err error) string {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code
	}
	return ""
}

func TestTheServerCancelsAStatementThatRunsPastTheTimeout(t *testing.T) {
	t.Parallel()

	pool := connect(t, func(c *postgres.Config) { c.StatementTimeout = 300 * time.Millisecond })

	started := time.Now()
	_, err := pool.Exec(t.Context(), "SELECT pg_sleep(10)")

	if sqlState(err) != "57014" {
		t.Fatalf("err = %v, want query_canceled (57014) from the server", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("took %v: the timeout is the server's, so it must end the statement near 300ms", took)
	}
}

func TestTheServerGivesUpWaitingForALock(t *testing.T) {
	t.Parallel()

	pool := connect(t, func(c *postgres.Config) { c.LockTimeout = 300 * time.Millisecond })

	holder, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Rollback(context.WithoutCancel(t.Context())) })
	if _, err = holder.Exec(t.Context(), "SELECT pg_advisory_xact_lock(42)"); err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(t.Context(), "SELECT pg_advisory_lock(42)")

	if sqlState(err) != "55P03" {
		t.Errorf(
			"err = %v, want lock_not_available (55P03): a waiter must not queue behind a stuck holder forever",
			err,
		)
	}
}

func TestEveryBoundReachesTheSession(t *testing.T) {
	t.Parallel()

	pool := connect(t, func(c *postgres.Config) {
		c.StatementTimeout = 7 * time.Second
		c.LockTimeout = 3 * time.Second
		c.IdleInTransactionTimeout = 11 * time.Second
		c.AppName = "budget-test"
	})

	for setting, want := range map[string]string{
		"statement_timeout":                   "7s",
		"lock_timeout":                        "3s",
		"idle_in_transaction_session_timeout": "11s",
		"application_name":                    "budget-test",
	} {
		var got string
		if err := pool.QueryRow(t.Context(), "SELECT current_setting($1)", setting).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", setting, got, want)
		}
	}
}

func TestTheBudgetCountsRealQueriesAndFindsTheLoop(t *testing.T) {
	t.Parallel()

	pool := connect(t, func(*postgres.Config) {})
	ctx, budget := postgres.WithBudget(t.Context(), postgres.Limits{Total: 50, Repeat: 3})

	for i := range 5 {
		var got int
		if err := pool.QueryRow(ctx, "SELECT $1::int", i).Scan(&got); err != nil {
			t.Fatal(err)
		}
	}

	if budget.Count() != 5 {
		t.Fatalf("count = %d, want 5: the pool must call the tracer for each query", budget.Count())
	}
	var broken *postgres.BudgetError
	if err := budget.Err(); !errors.As(err, &broken) || len(broken.Repeats) != 1 || broken.Repeats[0].Times != 5 {
		t.Errorf("err = %v, want the repeated SELECT named with its 5 runs", err)
	}
}

func TestTheBudgetCountsEachQueryOfARealBatch(t *testing.T) {
	t.Parallel()

	pool := connect(t, func(*postgres.Config) {})
	ctx, budget := postgres.WithBudget(t.Context(), postgres.DefaultLimits())

	batch := &pgx.Batch{}
	for i := range 4 {
		batch.Queue("SELECT $1::int", i)
	}
	results := pool.SendBatch(ctx, batch)
	for range 4 {
		if _, err := results.Exec(); err != nil {
			t.Fatal(err)
		}
	}
	if err := results.Close(); err != nil {
		t.Fatal(err)
	}

	if budget.Count() != 4 {
		t.Errorf("count = %d, want 4", budget.Count())
	}
}
