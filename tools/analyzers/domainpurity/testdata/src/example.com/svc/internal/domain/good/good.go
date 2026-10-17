package good

import "time"

type Invoice struct {
	id       string
	issuedAt time.Time
}

func (i Invoice) ID() string { return i.id }

func Issue(id string, at time.Time) Invoice { return Invoice{id: id, issuedAt: at} }

// Money is a plain data carrier.
//
//agent:allow-exported-fields a value object with no invariants beyond its types
type Money struct {
	Cents    int64
	Currency string
}

func Elapsed(from, to time.Time) time.Duration { return to.Sub(from) }
