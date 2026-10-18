// Package application orchestrates the example use cases and declares the ports their adapters implement.
package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tienduc120/go-base/services/example/internal/domain"
)

var (
	ErrInvalidCommand = errors.New("invalid command")
	// ErrAlreadyExists is what a repository returns when the ID is taken, so callers need not know the storage.
	ErrAlreadyExists = errors.New("already exists")
)

// NoteRepository is a port: the application declares it and infrastructure implements it.
type NoteRepository interface {
	Save(ctx context.Context, agg domain.Note) error
}

type Clock interface {
	Now() time.Time
}

type CreateNoteCommand struct {
	ID   string
	Name string
}

type CreateNoteResult struct {
	ID string
}

type CreateNote struct {
	repo  NoteRepository
	clock Clock
}

func NewCreateNote(repo NoteRepository, clock Clock) *CreateNote {
	return &CreateNote{repo: repo, clock: clock}
}

func (u *CreateNote) Execute(
	ctx context.Context,
	cmd CreateNoteCommand,
) (CreateNoteResult, error) {
	id, err := domain.NewNoteID(cmd.ID)
	if err != nil {
		return CreateNoteResult{}, fmt.Errorf("%w: %w", ErrInvalidCommand, err)
	}

	agg, err := domain.RegisterNote(id, cmd.Name, u.clock.Now())
	if err != nil {
		return CreateNoteResult{}, fmt.Errorf("%w: %w", ErrInvalidCommand, err)
	}

	if err = u.repo.Save(ctx, agg); err != nil {
		return CreateNoteResult{}, fmt.Errorf("save note: %w", err)
	}
	return CreateNoteResult{ID: string(id)}, nil
}
