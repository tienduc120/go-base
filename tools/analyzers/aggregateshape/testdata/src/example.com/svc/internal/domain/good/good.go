package good

import "errors"

type InvoiceID string

// Invoice is an aggregate root.
//
//agent:aggregate
type Invoice struct {
	id     InvoiceID
	amount int64
}

func NewInvoice(id InvoiceID, amount int64) (Invoice, error) {
	if amount <= 0 {
		return Invoice{}, errors.New("amount must be positive")
	}
	return Invoice{id: id, amount: amount}, nil
}

func RestoreInvoice(id InvoiceID, amount int64) Invoice { return Invoice{id: id, amount: amount} }

func (i Invoice) ID() InvoiceID { return i.id }

// Money is a value object: it carries no marker, so none of the aggregate rules apply to it.
type Money struct{ cents int64 }

// Pointer constructors are fine too.
//
//agent:aggregate
type Account struct {
	id AccountID
}

type AccountID int64

func NewAccount(id AccountID) *Account { return &Account{id: id} }

func RestoreAccount(id AccountID) *Account { return &Account{id: id} }
