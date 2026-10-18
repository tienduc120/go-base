// Package application is a violation fixture.
package application

import (
	"context"
	"net/http"
)

type Transactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type UseCase struct {
	tx Transactor
}

func (u *UseCase) Execute(ctx context.Context, client *http.Client, req *http.Request) error {
	return u.tx.InTransaction(ctx, func(context.Context) error {
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		return resp.Body.Close()
	})
}
