package bad

import "context"

type Transactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type Adapter struct {
	tx Transactor
}

func (a *Adapter) Run(ctx context.Context) error {
	return a.tx.InTransaction(ctx, func(context.Context) error { return nil }) // want `transaction opened outside application`
}
