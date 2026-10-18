// Package domain is a violation fixture.
package domain

type InvoiceID string

// Invoice is a well-formed aggregate root.
//
//agent:aggregate
type Invoice struct {
	id InvoiceID
}

func NewInvoice(id InvoiceID) Invoice { return Invoice{id: id} }

func RestoreInvoice(id InvoiceID) Invoice { return Invoice{id: id} }
