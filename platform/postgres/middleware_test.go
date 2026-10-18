package postgres_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/platform/postgres"
)

func serve(t *testing.T, limits postgres.Limits, log *recorder, queries int) {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle("GET /notes/{id}", postgres.BudgetMiddleware(limits, slog.New(log))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			budget := postgres.BudgetFrom(r.Context())
			if budget == nil {
				t.Error("the middleware must attach a budget to the request context")
				return
			}
			for range queries {
				budget.Record("SELECT id FROM notes WHERE id = $1")
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/notes/42", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the middleware must not change the response: %d", rec.Code)
	}
}

func TestBudgetMiddlewareWarnsWhenARequestRunsTooManyQueries(t *testing.T) {
	t.Parallel()

	log := &recorder{}
	serve(t, postgres.Limits{Total: 20, Repeat: 3}, log, 6)

	out := log.text()
	if !strings.Contains(out, "query budget exceeded") || !strings.Contains(out, "6 times") {
		t.Errorf("log = %q", out)
	}
	if !strings.Contains(out, "GET /notes/{id}") {
		t.Errorf("name the route, not the raw path: %q", out)
	}
	if strings.Contains(out, "/notes/42") {
		t.Errorf("a raw path carries ids, which are personal data and unbounded cardinality: %q", out)
	}
}

func TestBudgetMiddlewareIsSilentWithinTheBudget(t *testing.T) {
	t.Parallel()

	log := &recorder{}
	serve(t, postgres.DefaultLimits(), log, 2)

	if out := log.text(); out != "" {
		t.Errorf("log = %q", out)
	}
}
