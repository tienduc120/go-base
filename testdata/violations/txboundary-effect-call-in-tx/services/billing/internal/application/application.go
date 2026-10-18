// Package application is a violation fixture.
package application

import "context"

type Transactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type Mailer interface {
	Send(ctx context.Context, to string) error
}

type UseCase struct {
	tx     Transactor
	mailer Mailer
}

func (u *UseCase) Execute(ctx context.Context) error {
	return u.tx.InTransaction(ctx, func(ctx context.Context) error {
		return u.mailer.Send(ctx, "customer@example.com")
	})
}
