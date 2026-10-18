// Package postgres is the Postgres adapter for the example ports. sqlc generates the queries in db from the files
// in queries and migrations; run `sqlc generate` here after changing either.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tienduc120/go-base/services/example/internal/application"
	"github.com/tienduc120/go-base/services/example/internal/domain"
	"github.com/tienduc120/go-base/services/example/internal/infrastructure/postgres/db"
)

// uniqueViolation is the SQLSTATE Postgres reports for a duplicate key.
const uniqueViolation = "23505"

type NoteRepository struct {
	queries *db.Queries
}

// NewNoteRepository takes a pool or a transaction; build the pool with platform/postgres.Connect.
func NewNoteRepository(conn db.DBTX) *NoteRepository {
	return &NoteRepository{queries: db.New(conn)}
}

func (r *NoteRepository) Save(ctx context.Context, agg domain.Note) error {
	err := r.queries.CreateNote(ctx, db.CreateNoteParams{
		ID:        string(agg.ID()),
		Name:      agg.Name(),
		CreatedAt: agg.CreatedAt(),
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation {
		return fmt.Errorf("save note %s: %w", agg.ID(), application.ErrAlreadyExists)
	}
	if err != nil {
		return fmt.Errorf("save note: %w", err)
	}
	return nil
}

// Find is for tests and local runs; the application port declares only what a use case needs.
func (r *NoteRepository) Find(
	ctx context.Context,
	id domain.NoteID,
) (domain.Note, bool, error) {
	row, err := r.queries.GetNote(ctx, string(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Note{}, false, nil
	}
	if err != nil {
		return domain.Note{}, false, fmt.Errorf("find note: %w", err)
	}
	return domain.RestoreNote(domain.NoteID(row.ID), row.Name, row.CreatedAt), true, nil
}
