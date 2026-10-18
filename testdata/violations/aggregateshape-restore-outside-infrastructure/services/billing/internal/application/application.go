// Package application is a violation fixture.
package application

import "fixture.test/aggregateshape-restore-outside-infrastructure/services/billing/internal/domain"

// Rebuild skips the checks NewInvoice makes: only a repository may do that.
func Rebuild() domain.Invoice { return domain.RestoreInvoice("inv-1") }
