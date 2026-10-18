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

type Note struct {
	id        NoteID
	name      string
	createdAt time.Time
}

// RegisterNote takes the time as a value: the domain never reads the clock.
func RegisterNote(id NoteID, name string, createdAt time.Time) (Note, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Note{}, ErrEmptyName
	}
	return Note{id: id, name: name, createdAt: createdAt}, nil
}

func (a Note) ID() NoteID { return a.id }

func (a Note) Name() string { return a.name }

func (a Note) CreatedAt() time.Time { return a.createdAt }
