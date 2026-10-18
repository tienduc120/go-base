package postgres_test

import (
	"errors"
	"testing"
	"time"

	platform "github.com/tienduc120/go-base/platform/postgres"
	"github.com/tienduc120/go-base/platform/postgres/postgrestest"
	"github.com/tienduc120/go-base/services/example/internal/application"
	"github.com/tienduc120/go-base/services/example/internal/domain"
	"github.com/tienduc120/go-base/services/example/internal/infrastructure/postgres"
)

// These tests run against a real Postgres and skip, loudly, without one: `agentctl db test` provides it.

func newRepository(t *testing.T) *postgres.NoteRepository {
	t.Helper()
	return postgres.NewNoteRepository(postgrestest.Pool(t, "migrations"))
}

func registered(t *testing.T, id, name string) domain.Note {
	t.Helper()
	createdAt := time.Date(2026, time.October, 18, 9, 0, 0, 0, time.UTC)
	agg, err := domain.NewNote(domain.NoteID(id), name, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	return agg
}

func TestSaveThenFindReturnsWhatWasSaved(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	saved := registered(t, "id-1", "first")
	other := registered(t, "id-2", "second")
	for _, agg := range []domain.Note{saved, other} {
		if err := repo.Save(t.Context(), agg); err != nil {
			t.Fatal(err)
		}
	}

	got, found, err := repo.Find(t.Context(), "id-1")
	if err != nil || !found {
		t.Fatalf("found = %v, err = %v", found, err)
	}
	if got.Name() != "first" || !got.CreatedAt().Equal(saved.CreatedAt()) {
		t.Errorf("got %+v, want what was saved %+v: another row must not bleed into it", got, saved)
	}
}

func TestSaveRefusesATakenID(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	if err := repo.Save(t.Context(), registered(t, "id-1", "first")); err != nil {
		t.Fatal(err)
	}

	err := repo.Save(t.Context(), registered(t, "id-1", "second"))
	if !errors.Is(err, application.ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists: the caller must not need to know it was a unique key", err)
	}
}

func TestFindOfAnUnknownIDIsNotAnError(t *testing.T) {
	t.Parallel()

	_, found, err := newRepository(t).Find(t.Context(), "missing")
	if found || err != nil {
		t.Errorf("found = %v, err = %v, want not found and no error", found, err)
	}
}

func TestEachOperationRunsOneStatement(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx, budget := platform.WithBudget(t.Context(), platform.Limits{Total: 1, Repeat: 1})
	if err := repo.Save(ctx, registered(t, "id-1", "first")); err != nil {
		t.Fatal(err)
	}
	if err := budget.Err(); err != nil {
		t.Errorf("a save is one statement: %v", err)
	}

	ctx, budget = platform.WithBudget(t.Context(), platform.Limits{Total: 1, Repeat: 1})
	if _, _, err := repo.Find(ctx, "id-1"); err != nil {
		t.Fatal(err)
	}
	if err := budget.Err(); err != nil {
		t.Errorf("a find is one statement: %v", err)
	}
}
