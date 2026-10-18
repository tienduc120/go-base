package postgres

import (
	"log/slog"
	"net/http"
)

// BudgetMiddleware gives every request a query budget and logs a warning when the request breaks it. It names the
// route pattern, never the raw path, which carries ids.
func BudgetMiddleware(limits Limits, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, budget := WithBudget(r.Context(), limits)
			next.ServeHTTP(w, r.WithContext(ctx))

			if err := budget.Err(); err != nil {
				logger.WarnContext(ctx, "query budget exceeded", "route", r.Pattern, "error", err)
			}
		})
	}
}
