package ok

import "example.com/svc/internal/domain/good"

// A repository may rebuild a stored value: that is what Restore is for.
func Load() good.Invoice {
	return good.RestoreInvoice("inv-1", 10)
}
