// Package application orchestrates billing use cases and declares the ports their adapters implement.
package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"fixture.test/clean/services/billing/internal/domain"
)

var ErrInvalidCommand = errors.New("invalid command")

type InvoiceRepository interface {
	Save(ctx context.Context, invoice domain.Invoice) error
}

type Clock interface {
	Now() time.Time
}

type IssueInvoiceCommand struct {
	ID          string
	AmountCents int64
}

type IssueInvoiceResult struct {
	ID string
}

type IssueInvoice struct {
	repo  InvoiceRepository
	clock Clock
}

func NewIssueInvoice(repo InvoiceRepository, clock Clock) *IssueInvoice {
	return &IssueInvoice{repo: repo, clock: clock}
}

func (u *IssueInvoice) Execute(ctx context.Context, cmd IssueInvoiceCommand) (IssueInvoiceResult, error) {
	id, err := domain.NewInvoiceID(cmd.ID)
	if err != nil {
		return IssueInvoiceResult{}, fmt.Errorf("%w: %w", ErrInvalidCommand, err)
	}

	invoice, err := domain.NewInvoice(id, cmd.AmountCents, u.clock.Now())
	if err != nil {
		return IssueInvoiceResult{}, fmt.Errorf("%w: %w", ErrInvalidCommand, err)
	}

	if err = u.repo.Save(ctx, invoice); err != nil {
		return IssueInvoiceResult{}, fmt.Errorf("save invoice: %w", err)
	}
	return IssueInvoiceResult{ID: string(id)}, nil
}
