// Package memory is an in-process adapter for the example ports, used by tests and local runs.
package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/tienduc120/go-base/services/example/internal/domain"
)

type NoteRepository struct {
	mu    sync.Mutex
	items map[domain.NoteID]domain.Note
}

func NewNoteRepository() *NoteRepository {
	return &NoteRepository{items: make(map[domain.NoteID]domain.Note)}
}

func (r *NoteRepository) Save(ctx context.Context, agg domain.Note) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("save note: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[agg.ID()] = agg
	return nil
}

// Find is for tests and local runs; the application port declares only what a use case needs.
func (r *NoteRepository) Find(id domain.NoteID) (domain.Note, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	agg, ok := r.items[id]
	return agg, ok
}
