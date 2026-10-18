package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tienduc120/go-base/services/example/internal/application"
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
	for id, name := range map[string]string{"id-1": "first", "id-2": "second"} {
		if err := repo.Save(t.Context(), registered(t, id, name)); err != nil {
			t.Fatal(err)
		}
	}

	for id, name := range map[string]string{"id-1": "first", "id-2": "second"} {
		got, ok := repo.Find(domain.NoteID(id))
		if !ok || got.Name() != name {
			t.Errorf("Find(%s) = %+v, %v, want the note saved as %q", id, got, ok, name)
		}
	}
	if _, found := repo.Find("missing"); found {
		t.Error("Find invented a note that was never saved")
	}
}

func TestSaveRefusesATakenID(t *testing.T) {
	t.Parallel()

	repo := memory.NewNoteRepository()
	if err := repo.Save(t.Context(), registered(t, "id-1", "first")); err != nil {
		t.Fatal(err)
	}

	err := repo.Save(t.Context(), registered(t, "id-1", "second"))
	if !errors.Is(err, application.ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
	if got, _ := repo.Find("id-1"); got.Name() != "first" {
		t.Errorf("name = %q: a refused save must leave the stored value alone", got.Name())
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
