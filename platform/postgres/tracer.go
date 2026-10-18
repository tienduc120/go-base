package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tienduc120/go-base/platform/clock"
)

const defaultSlow = 200 * time.Millisecond

// Clock is the time the tracer measures with; a test hands it fixed instants.
type Clock interface{ Now() time.Time }

// TracerOptions configure NewTracer. The zero value logs nowhere and treats 200ms as slow.
type TracerOptions struct {
	Logger *slog.Logger
	// Slow is how long a statement may take before it is logged; non-positive means the default, never "off".
	Slow  time.Duration
	Clock Clock
}

// Tracer is the pgx tracer that feeds the query budget and logs slow or failed statements. It logs the statement
// text and never its arguments, which carry personal data.
type Tracer struct {
	logger *slog.Logger
	slow   time.Duration
	clock  Clock
}

// NewTracer builds a Tracer.
func NewTracer(opts TracerOptions) *Tracer {
	t := &Tracer{logger: opts.Logger, slow: opts.Slow, clock: opts.Clock}
	if t.logger == nil {
		t.logger = slog.New(slog.DiscardHandler)
	}
	if t.slow <= 0 {
		t.slow = defaultSlow
	}
	if t.clock == nil {
		t.clock = clock.System{}
	}
	return t
}

type startKey struct{}

type started struct {
	at  time.Time
	sql string
}

// TraceQueryStart counts the statement against the context's budget and remembers when it began.
func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if b := BudgetFrom(ctx); b != nil {
		b.Record(data.SQL)
	}
	return context.WithValue(ctx, startKey{}, started{at: t.clock.Now(), sql: data.SQL})
}

// TraceQueryEnd logs a failed statement, or one that took longer than the slow threshold.
func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	s, ok := ctx.Value(startKey{}).(started)
	if !ok {
		return
	}
	took := t.clock.Now().Sub(s.at)

	switch {
	case data.Err != nil:
		t.logger.WarnContext(ctx, "query failed", "sql", clip(normalize(s.sql)), "duration", took, "error", data.Err)
	case took >= t.slow:
		t.logger.WarnContext(ctx, "slow query", "sql", clip(normalize(s.sql)), "duration", took)
	}
}

// TraceBatchStart does nothing; each queued statement is counted as it is traced.
func (t *Tracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return ctx
}

// TraceBatchQuery counts one statement of a batch. A batch is one round trip, not one statement, and a loop that
// queues a query per row into a batch is still an N+1.
func (t *Tracer) TraceBatchQuery(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	if b := BudgetFrom(ctx); b != nil {
		b.Record(data.SQL)
	}
}

// TraceBatchEnd does nothing.
func (t *Tracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}
