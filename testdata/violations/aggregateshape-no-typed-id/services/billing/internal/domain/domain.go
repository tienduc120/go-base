// Package domain is a violation fixture.
package domain

// Invoice is an aggregate root whose id is a bare string.
//
//agent:aggregate
type Invoice struct {
	id string
}

func NewInvoice(id string) Invoice { return Invoice{id: id} }

func RestoreInvoice(id string) Invoice { return Invoice{id: id} }
