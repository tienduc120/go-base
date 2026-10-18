package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/tienduc120/go-base/services/example/internal/domain"
	"github.com/tienduc120/go-base/services/example/internal/infrastructure/memory"
)

func registered(t *testing.T, id, name string) domain.Note {
	t.Helper()
	agg, err := domain.RegisterNote(domain.NoteID(id), name, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return agg
}

func TestSaveThenFind(t *testing.T) {
	t.Parallel()

	repo := memory.NewNoteRepository()
	if err := repo.Save(t.Context(), registered(t, "id-1", "first")); err != nil {
		t.Fatal(err)
	}

	got, ok := repo.Find("id-1")
	if !ok || got.Name() != "first" {
		t.Errorf("Find = %+v, %v, want the saved note", got, ok)
	}
	if _, found := repo.Find("missing"); found {
		t.Error("Find invented a note that was never saved")
	}
}

func TestSaveReplacesTheSameID(t *testing.T) {
	t.Parallel()

	repo := memory.NewNoteRepository()
	for _, name := range []string{"first", "second"} {
		if err := repo.Save(t.Context(), registered(t, "id-1", name)); err != nil {
			t.Fatal(err)
		}
	}

	if got, _ := repo.Find("id-1"); got.Name() != "second" {
		t.Errorf("name = %q, want the latest save to win", got.Name())
	}
}

func TestSaveHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	repo := memory.NewNoteRepository()
	if err := repo.Save(ctx, registered(t, "id-1", "first")); err == nil {
		t.Error("a cancelled context must stop the save")
	}
	if _, ok := repo.Find("id-1"); ok {
		t.Error("a save that failed must not leave anything behind")
	}
}
