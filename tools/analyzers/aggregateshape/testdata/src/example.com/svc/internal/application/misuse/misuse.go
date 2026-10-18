package misuse

import "example.com/svc/internal/domain/good"

func Rebuild() good.Invoice {
	return good.RestoreInvoice("inv-1", 10) // want `RestoreInvoice is for repositories only`
}

func Create() (good.Invoice, error) {
	return good.NewInvoice("inv-1", 10)
}
