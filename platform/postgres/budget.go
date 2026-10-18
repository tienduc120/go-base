// Package postgres opens connection pools whose every bound is set, and counts the queries a request runs so a loop
// of queries is found by a test instead of by production.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

const (
	defaultTotal  = 20
	defaultRepeat = 3

	// shownSQL caps how much of a statement an error message carries.
	shownSQL = 120
)

// Limits bounds what one request may ask of the database.
type Limits struct {
	// Total is the most statements a request may run.
	Total int
	// Repeat is the most times one statement text may run: the same query again and again is an N+1.
	Repeat int
}

// DefaultLimits leave room for a normal use case and none for a query per row.
func DefaultLimits() Limits { return Limits{Total: defaultTotal, Repeat: defaultRepeat} }

// withDefaults replaces a non-positive limit with the default; a limit of zero must never mean unlimited.
func (l Limits) withDefaults() Limits {
	if l.Total <= 0 {
		l.Total = defaultTotal
	}
	if l.Repeat <= 0 {
		l.Repeat = defaultRepeat
	}
	return l
}

type budgetKey struct{}

// Budget counts the statements run under one context. It is safe for the goroutines of a request to share.
type Budget struct {
	limits Limits

	mu    sync.Mutex
	total int
	runs  map[string]int
	order []string
}

// WithBudget attaches a new budget to ctx. The pool's tracer records every statement run under the returned context.
func WithBudget(ctx context.Context, limits Limits) (context.Context, *Budget) {
	b := &Budget{limits: limits.withDefaults(), runs: map[string]int{}}
	return context.WithValue(ctx, budgetKey{}, b), b
}

// BudgetFrom returns the budget attached to ctx, or nil.
func BudgetFrom(ctx context.Context) *Budget {
	b, _ := ctx.Value(budgetKey{}).(*Budget)
	return b
}

// normalize makes two spellings of one statement equal: the same query on another line is still the same query.
func normalize(sql string) string { return strings.Join(strings.Fields(sql), " ") }

// Record counts one statement run.
func (b *Budget) Record(sql string) {
	key := normalize(sql)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.total++
	if _, seen := b.runs[key]; !seen {
		b.order = append(b.order, key)
	}
	b.runs[key]++
}

// Count is how many statements have run.
func (b *Budget) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

// Repeat is one statement that ran more often than allowed.
type Repeat struct {
	SQL   string
	Times int
}

// BudgetError says how a budget was broken.
type BudgetError struct {
	Total   int
	Limits  Limits
	Repeats []Repeat
}

func (e *BudgetError) Error() string {
	var parts []string
	if e.Total > e.Limits.Total {
		parts = append(parts, fmt.Sprintf("query budget exceeded: %d statements, limit %d", e.Total, e.Limits.Total))
	}
	for _, r := range e.Repeats {
		parts = append(parts, fmt.Sprintf("statement run %d times, limit %d: %s",
			r.Times, e.Limits.Repeat, clip(r.SQL)))
	}
	return strings.Join(parts, "; ")
}

func clip(sql string) string {
	if len(sql) <= shownSQL {
		return sql
	}
	return sql[:shownSQL] + "..."
}

// Err reports nil while the budget holds, and a *BudgetError once it is broken.
func (b *Budget) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	broken := &BudgetError{Total: b.total, Limits: b.limits}
	for _, sql := range b.order {
		if n := b.runs[sql]; n > b.limits.Repeat {
			broken.Repeats = append(broken.Repeats, Repeat{SQL: sql, Times: n})
		}
	}
	if b.total <= b.limits.Total && len(broken.Repeats) == 0 {
		return nil
	}
	return broken
}
