package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tienduc120/go-base/services/example/internal/application"
	"github.com/tienduc120/go-base/services/example/internal/domain"
)

type fakeRepository struct {
	saved []domain.Note
	err   error
}

func (r *fakeRepository) Save(_ context.Context, agg domain.Note) error {
	if r.err != nil {
		return r.err
	}
	r.saved = append(r.saved, agg)
	return nil
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

func TestCreateNoteSavesWithTheClocksTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 18, 9, 0, 0, 0, time.UTC)
	repo := &fakeRepository{}
	usecase := application.NewCreateNote(repo, fixedClock(now))

	res, err := usecase.Execute(t.Context(), application.CreateNoteCommand{ID: "id-1", Name: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "id-1" || len(repo.saved) != 1 || !repo.saved[0].CreatedAt().Equal(now) {
		t.Errorf("result %+v, saved %+v: want one note created at %v", res, repo.saved, now)
	}
}

func TestCreateNoteRejectsAnInvalidCommandWithoutSaving(t *testing.T) {
	t.Parallel()

	for name, cmd := range map[string]application.CreateNoteCommand{
		"empty id":   {ID: "", Name: "first"},
		"blank name": {ID: "id-1", Name: "  "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeRepository{}
			_, err := application.NewCreateNote(repo, fixedClock{}).Execute(t.Context(), cmd)
			if !errors.Is(err, application.ErrInvalidCommand) {
				t.Errorf("err = %v, want ErrInvalidCommand", err)
			}
			if len(repo.saved) != 0 {
				t.Errorf("an invalid command must not reach the repository, saved %+v", repo.saved)
			}
		})
	}
}

func TestCreateNoteReportsAStorageFailureAsItsOwnError(t *testing.T) {
	t.Parallel()

	boom := errors.New("storage down")
	usecase := application.NewCreateNote(&fakeRepository{err: boom}, fixedClock{})

	_, err := usecase.Execute(t.Context(), application.CreateNoteCommand{ID: "id-1", Name: "first"})
	if !errors.Is(err, boom) || errors.Is(err, application.ErrInvalidCommand) {
		t.Errorf("err = %v, want the storage error and not ErrInvalidCommand: the caller did nothing wrong", err)
	}
}

func TestCreateNoteKeepsAnAlreadyExistsErrorRecognisable(t *testing.T) {
	t.Parallel()

	usecase := application.NewCreateNote(&fakeRepository{err: application.ErrAlreadyExists}, fixedClock{})

	_, err := usecase.Execute(t.Context(), application.CreateNoteCommand{ID: "id-1", Name: "first"})
	if !errors.Is(err, application.ErrAlreadyExists) || errors.Is(err, application.ErrInvalidCommand) {
		t.Errorf("err = %v, want ErrAlreadyExists, which is a conflict and not a malformed command", err)
	}
}
