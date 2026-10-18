// Package domain holds the example aggregates and the rules that keep them valid.
package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrEmptyID   = errors.New("note id must not be empty")
	ErrEmptyName = errors.New("note name must not be empty")
)

type NoteID string

func NewNoteID(raw string) (NoteID, error) {
	if raw == "" {
		return "", ErrEmptyID
	}
	return NoteID(raw), nil
}

// Note is an aggregate root: the checker holds it to a typed id, NewNote, and RestoreNote.
//
//agent:aggregate
type Note struct {
	id        NoteID
	name      string
	createdAt time.Time
}

// NewNote takes the time as a value: the domain never reads the clock.
func NewNote(id NoteID, name string, createdAt time.Time) (Note, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Note{}, ErrEmptyName
	}
	return Note{id: id, name: name, createdAt: createdAt}, nil
}

// RestoreNote rebuilds a stored value for a repository. It was valid when it was saved, so nothing is checked
// again; new values come only from NewNote.
func RestoreNote(id NoteID, name string, createdAt time.Time) Note {
	return Note{id: id, name: name, createdAt: createdAt}
}

func (a Note) ID() NoteID { return a.id }

func (a Note) Name() string { return a.name }

func (a Note) CreatedAt() time.Time { return a.createdAt }
