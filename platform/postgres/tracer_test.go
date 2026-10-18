package postgres_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tienduc120/go-base/platform/postgres"
)

// recorder keeps the log records a handler is given.
type recorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}
func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recorder) WithGroup(string) slog.Handler      { return r }

func (r *recorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, rec := range r.records {
		b.WriteString(rec.Level.String() + " " + rec.Message)
		rec.Attrs(func(a slog.Attr) bool {
			b.WriteString(" " + a.Key + "=" + a.Value.String())
			return true
		})
		b.WriteString("\n")
	}
	return b.String()
}

// steppingClock returns each of its times in turn.
type steppingClock struct {
	mu    sync.Mutex
	times []time.Time
}

func (c *steppingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.times[0]
	if len(c.times) > 1 {
		c.times = c.times[1:]
	}
	return now
}

func TestTheTracerCountsEveryQueryAgainstTheContextsBudget(t *testing.T) {
	t.Parallel()

	tracer := postgres.NewTracer(postgres.TracerOptions{})
	ctx, budget := postgres.WithBudget(t.Context(), postgres.Limits{Total: 10, Repeat: 2})

	for range 3 {
		qctx := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT id FROM notes WHERE id = $1"})
		tracer.TraceQueryEnd(qctx, nil, pgx.TraceQueryEndData{})
	}

	if budget.Count() != 3 || budget.Err() == nil {
		t.Errorf(
			"count = %d, err = %v: three runs of one statement exceed a repeat limit of two",
			budget.Count(),
			budget.Err(),
		)
	}
}

func TestTheTracerCountsEachQueryOfABatch(t *testing.T) {
	t.Parallel()

	tracer := postgres.NewTracer(postgres.TracerOptions{})
	ctx, budget := postgres.WithBudget(t.Context(), postgres.DefaultLimits())

	ctx = tracer.TraceBatchStart(ctx, nil, pgx.TraceBatchStartData{})
	for range 4 {
		tracer.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{SQL: "INSERT INTO notes (id) VALUES ($1)"})
	}
	tracer.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})

	if budget.Count() != 4 {
		t.Errorf("count = %d, want 4: a batch hides N queries in one round trip, not from the budget", budget.Count())
	}
}

func TestTheTracerNeedsNoBudget(t *testing.T) {
	t.Parallel()

	tracer := postgres.NewTracer(postgres.TracerOptions{})
	ctx := tracer.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
}

func TestASlowQueryIsLoggedWithItsTextAndNeverItsArguments(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 18, 9, 0, 0, 0, time.UTC)
	log := &recorder{}
	tracer := postgres.NewTracer(postgres.TracerOptions{
		Logger: slog.New(log),
		Slow:   200 * time.Millisecond,
		Clock:  &steppingClock{times: []time.Time{start, start.Add(750 * time.Millisecond)}},
	})

	ctx := tracer.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{
		SQL: "SELECT id FROM people WHERE email = $1", Args: []any{"ada@example.com"},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	out := log.text()
	if !strings.Contains(out, "slow query") || !strings.Contains(out, "SELECT id FROM people WHERE email = $1") ||
		!strings.Contains(out, "750ms") {
		t.Errorf("log = %q", out)
	}
	if strings.Contains(out, "ada@example.com") {
		t.Errorf("arguments carry personal data and must never be logged: %q", out)
	}
}

func TestAFastQueryIsNotLogged(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 18, 9, 0, 0, 0, time.UTC)
	log := &recorder{}
	tracer := postgres.NewTracer(postgres.TracerOptions{
		Logger: slog.New(log),
		Slow:   200 * time.Millisecond,
		Clock:  &steppingClock{times: []time.Time{start, start.Add(5 * time.Millisecond)}},
	})

	ctx := tracer.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	if out := log.text(); out != "" {
		t.Errorf("log = %q", out)
	}
}

func TestAFailedQueryIsLoggedEvenWhenFast(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 18, 9, 0, 0, 0, time.UTC)
	log := &recorder{}
	tracer := postgres.NewTracer(postgres.TracerOptions{
		Logger: slog.New(log),
		Slow:   time.Second,
		Clock:  &steppingClock{times: []time.Time{start, start.Add(time.Millisecond)}},
	})

	ctx := tracer.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: context.DeadlineExceeded})

	if out := log.text(); !strings.Contains(out, "query failed") || !strings.Contains(out, "deadline") {
		t.Errorf("a statement the server cancelled is the signal a timeout works; log = %q", out)
	}
}
