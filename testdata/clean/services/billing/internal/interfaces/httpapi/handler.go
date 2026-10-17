// Package httpapi exposes the billing use cases over HTTP.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"fixture.test/clean/services/billing/internal/application"
)

// maxBodyBytes caps a request body so one client cannot hold memory.
const maxBodyBytes = 1 << 20

type Issuer interface {
	Execute(ctx context.Context, cmd application.IssueInvoiceCommand) (application.IssueInvoiceResult, error)
}

type issueRequest struct {
	ID          string `json:"id"`
	AmountCents int64  `json:"amount_cents"`
}

type issueResponse struct {
	ID string `json:"id"`
}

func NewHandler(issuer Issuer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /invoices", func(w http.ResponseWriter, r *http.Request) {
		var req issueRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
			http.Error(w, "malformed body", http.StatusBadRequest)
			return
		}

		cmd := application.IssueInvoiceCommand{ID: req.ID, AmountCents: req.AmountCents}
		res, err := issuer.Execute(r.Context(), cmd)
		switch {
		case errors.Is(err, application.ErrInvalidCommand):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case err != nil:
			http.Error(w, "internal error", http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(issueResponse{ID: res.ID})
		}
	})
	return mux
}
