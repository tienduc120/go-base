package bad

import (
	"context"
	"net/http"
)

type Mailer interface {
	Send(ctx context.Context, to string) error
}

type Transactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type Issue struct {
	tx     Transactor
	mailer Mailer
}

func (u *Issue) Execute(ctx context.Context, client *http.Client, req *http.Request) error {
	return u.tx.InTransaction(ctx, func(ctx context.Context) error {
		if _, err := http.Get("https://example.com"); err != nil { // want `net/http call inside a transaction`
			return err
		}
		if _, err := client.Do(req); err != nil { // want `net/http call inside a transaction`
			return err
		}
		if err := u.mailer.Send(ctx, "customer@example.com"); err != nil { // want `call on Mailer inside a transaction`
			return err
		}
		return u.tx.InTransaction(ctx, func(context.Context) error { return nil }) // want `nested transaction`
	})
}
