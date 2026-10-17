// Package domain holds the billing aggregates and the rules that keep them valid.
package domain

import (
	"errors"
	"time"
)

var (
	ErrEmptyID       = errors.New("invoice id must not be empty")
	ErrInvalidAmount = errors.New("invoice amount must be positive")
)

type InvoiceID string

func NewInvoiceID(raw string) (InvoiceID, error) {
	if raw == "" {
		return "", ErrEmptyID
	}
	return InvoiceID(raw), nil
}

type Invoice struct {
	id          InvoiceID
	amountCents int64
	issuedAt    time.Time
}

// IssueInvoice takes the time as a value: the domain never reads the clock.
func IssueInvoice(id InvoiceID, amountCents int64, issuedAt time.Time) (Invoice, error) {
	if amountCents <= 0 {
		return Invoice{}, ErrInvalidAmount
	}
	return Invoice{id: id, amountCents: amountCents, issuedAt: issuedAt}, nil
}

func (i Invoice) ID() InvoiceID { return i.id }

func (i Invoice) AmountCents() int64 { return i.amountCents }

func (i Invoice) IssuedAt() time.Time { return i.issuedAt }
