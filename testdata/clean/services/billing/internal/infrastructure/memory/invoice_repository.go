// Package memory is an in-process adapter for the billing ports, used by tests and local runs.
package memory

import (
	"context"
	"fmt"
	"sync"

	"fixture.test/clean/services/billing/internal/domain"
)

type InvoiceRepository struct {
	mu       sync.Mutex
	invoices map[domain.InvoiceID]domain.Invoice
}

func NewInvoiceRepository() *InvoiceRepository {
	return &InvoiceRepository{invoices: make(map[domain.InvoiceID]domain.Invoice)}
}

func (r *InvoiceRepository) Save(ctx context.Context, invoice domain.Invoice) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("save invoice: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.invoices[invoice.ID()] = invoice
	return nil
}
