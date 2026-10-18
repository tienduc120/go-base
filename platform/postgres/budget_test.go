package postgres_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"go.uber.org/goleak"

	"github.com/tienduc120/go-base/platform/postgres"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestABudgetWithinItsLimitsHasNoError(t *testing.T) {
	t.Parallel()

	_, budget := postgres.WithBudget(t.Context(), postgres.Limits{Total: 5, Repeat: 2})
	budget.Record("SELECT a FROM t WHERE id = $1")
	budget.Record("SELECT b FROM u WHERE id = $1")

	if err := budget.Err(); err != nil || budget.Count() != 2 {
		t.Errorf("err = %v, count = %d", err, budget.Count())
	}
}

func TestABudgetNamesTheTotalWhenItIsExceeded(t *testing.T) {
	t.Parallel()

	_, budget := postgres.WithBudget(t.Context(), postgres.Limits{Total: 2, Repeat: 10})
	for _, sql := range []string{"SELECT 1", "SELECT 2", "SELECT 3"} {
		budget.Record(sql)
	}

	err := budget.Err()
	var over *postgres.BudgetError
	if !errors.As(err, &over) || over.Total != 3 {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "3 statements") || !strings.Contains(err.Error(), "limit 2") {
		t.Errorf("the message should say how many ran and what the limit was: %v", err)
	}
}

func TestABudgetNamesTheStatementThatRepeats(t *testing.T) {
	t.Parallel()

	_, budget := postgres.WithBudget(t.Context(), postgres.Limits{Total: 100, Repeat: 3})
	for range 5 {
		budget.Record("SELECT  id\n  FROM notes   WHERE owner = $1")
	}
	budget.Record("SELECT 1")

	err := budget.Err()
	if err == nil {
		t.Fatal("five runs of one statement is an N+1")
	}
	for _, want := range []string{"5 times", "limit 3", "SELECT id FROM notes WHERE owner = $1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
}

func TestSpacingDoesNotHideARepeatedStatement(t *testing.T) {
	t.Parallel()

	_, budget := postgres.WithBudget(t.Context(), postgres.Limits{Total: 100, Repeat: 2})
	budget.Record("SELECT id FROM notes WHERE owner = $1")
	budget.Record("SELECT id\nFROM notes\nWHERE owner = $1")
	budget.Record("SELECT   id FROM notes WHERE owner = $1")

	if budget.Err() == nil {
		t.Error("the same statement written with other whitespace is still the same statement")
	}
}

func TestNonPositiveLimitsFallBackToTheDefaultsNotToNoLimit(t *testing.T) {
	t.Parallel()

	_, budget := postgres.WithBudget(t.Context(), postgres.Limits{})
	for i := range 21 {
		budget.Record("SELECT " + string(rune('a'+i%26)))
	}

	if budget.Err() == nil {
		t.Error("an unset limit must mean the default, since unlimited is how an N+1 reaches production")
	}
}

func TestBudgetFromFindsTheBudgetOnTheContextAndNothingElse(t *testing.T) {
	t.Parallel()

	if postgres.BudgetFrom(t.Context()) != nil {
		t.Error("a bare context has no budget")
	}
	ctx, budget := postgres.WithBudget(t.Context(), postgres.DefaultLimits())
	if postgres.BudgetFrom(ctx) != budget {
		t.Error("the budget should be recoverable from the context it was attached to")
	}
}

func TestABudgetIsSafeToShareAcrossGoroutines(t *testing.T) {
	t.Parallel()

	_, budget := postgres.WithBudget(t.Context(), postgres.Limits{Total: 1000, Repeat: 1000})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				budget.Record("SELECT 1")
			}
		})
	}
	wg.Wait()

	if budget.Count() != 400 {
		t.Errorf("count = %d, want 400: a request that fans out must still be counted whole", budget.Count())
	}
}
