package good

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Invoice stands for a domain value: a concrete type, so calling its methods inside a transaction is fine.
type Invoice struct{}

func (Invoice) Total() int { return 1 }

type InvoiceRepository interface {
	Save(ctx context.Context, invoice Invoice) error
}

type Outbox interface {
	Add(ctx context.Context, event string) error
}

type Clock interface {
	Now() time.Time
}

type Transactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type Mailer interface {
	Send(ctx context.Context, to string) error
}

type Issue struct {
	tx     Transactor
	repo   InvoiceRepository
	outbox Outbox
	clock  Clock
	mailer Mailer
}

func (u *Issue) Execute(ctx context.Context) error {
	err := u.tx.InTransaction(ctx, func(ctx context.Context) error {
		invoice := Invoice{}
		_ = invoice.Total()
		if ctx.Err() != nil {
			return fmt.Errorf("stopped: %w", ctx.Err())
		}
		_ = u.clock.Now()
		if err := u.repo.Save(ctx, invoice); err != nil {
			return err
		}
		return u.outbox.Add(ctx, "invoice issued")
	})
	if err != nil {
		return errors.Join(err)
	}
	// An effect after the transaction has committed is the right place for it.
	return u.mailer.Send(ctx, "customer@example.com")
}
