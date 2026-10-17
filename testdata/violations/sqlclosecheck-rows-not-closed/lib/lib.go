// Package lib is a violation fixture.
package lib

import (
	"context"
	"database/sql"
)

func Names(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT name FROM users")
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	return nil
}
