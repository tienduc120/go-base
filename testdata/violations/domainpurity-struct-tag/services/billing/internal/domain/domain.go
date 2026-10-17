// Package domain is a violation fixture.
package domain

type Invoice struct {
	id string `json:"id"`
}

func (i Invoice) ID() string { return i.id }
