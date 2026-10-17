package domain_test

import (
	"errors"
	"testing"
	"time"

	"fixture.test/clean/services/billing/internal/domain"
)

func TestIssueInvoiceRejectsNonPositiveAmounts(t *testing.T) {
	t.Parallel()

	for _, amount := range []int64{0, -1} {
		_, err := domain.IssueInvoice("inv-1", amount, time.Time{})
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Errorf("amount %d: err = %v, want ErrInvalidAmount", amount, err)
		}
	}
}

func TestIssueInvoiceKeepsWhatItWasGiven(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.October, 17, 0, 0, 0, 0, time.UTC)
	invoice, err := domain.IssueInvoice("inv-1", 500, issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.ID() != "inv-1" || invoice.AmountCents() != 500 || !invoice.IssuedAt().Equal(issuedAt) {
		t.Errorf("invoice does not hold the values it was issued with: %+v", invoice)
	}
}
