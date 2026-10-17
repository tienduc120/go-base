// Package lib is a violation fixture.
package lib

import (
	"context"
	"database/sql"
)

func List(ctx context.Context, db *sql.DB) (*sql.Rows, error) {
	return db.QueryContext(ctx, "SELECT * FROM invoices")
}
