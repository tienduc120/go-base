package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tienduc120/go-base/services/example/internal/domain"
)

func TestNewNoteIDRejectsAnEmptyValue(t *testing.T) {
	t.Parallel()

	if _, err := domain.NewNoteID(""); !errors.Is(err, domain.ErrEmptyID) {
		t.Errorf("err = %v, want ErrEmptyID", err)
	}
}

func TestNewNoteRejectsABlankName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", "   ", "\t"} {
		_, err := domain.NewNote("id-1", name, time.Time{})
		if !errors.Is(err, domain.ErrEmptyName) {
			t.Errorf("name %q: err = %v, want ErrEmptyName", name, err)
		}
	}
}

func TestNewNoteKeepsWhatItWasGiven(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC)
	got, err := domain.NewNote("id-1", "  first  ", createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "id-1" || got.Name() != "first" || !got.CreatedAt().Equal(createdAt) {
		t.Errorf("note does not hold the values it was registered with: %+v", got)
	}
}

func TestRestoreNoteKeepsStoredValuesAsTheyAre(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC)
	got := domain.RestoreNote("id-1", "  stored as is  ", createdAt)

	if got.ID() != "id-1" || got.Name() != "  stored as is  " || !got.CreatedAt().Equal(createdAt) {
		t.Errorf("a stored value must come back exactly as it went in: %+v", got)
	}
}
