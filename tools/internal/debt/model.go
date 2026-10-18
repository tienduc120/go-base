// Package debt finds technical debt that the gates permit, tracks whether a person or an agent has confirmed,
// accepted, or dismissed it, and refuses to let new debt appear untriaged.
//
// The lint gate already stops what it can name. Debt is what slips through: every suppression, every TODO nobody
// owns, functions drifting towards the complexity limit, and packages with thin tests.
package debt

import (
	"cmp"
	"slices"
)

// The kinds of debt.
const (
	KindSuppression   = "suppression"
	KindUntrackedTodo = "untracked-todo"
	KindComplexity    = "complexity"
	KindCoverage      = "coverage"
)

// The states of an item. Only the first is derived by the scan; the rest are recorded in the register.
const (
	StatusUnconfirmed   = "unconfirmed"
	StatusConfirmed     = "confirmed"
	StatusAccepted      = "accepted"
	StatusFalsePositive = "false-positive"
	// StatusExpired is an accepted item whose acceptance has run out, so it needs a decision again.
	StatusExpired = "expired"
)

const (
	defaultComplexityMin = 15
	defaultLinesMin      = 80
)

// Item is one piece of debt.
type Item struct {
	// ID is stable across edits that only move code, so a decision recorded about it keeps applying.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Where is a path with a line number, for a person to jump to; it is not part of the identity.
	Where   string `json:"where"`
	Subject string `json:"subject"`
	// Evidence is the one-line proof a claim about this item rests on.
	Evidence string  `json:"evidence"`
	Cost     string  `json:"cost"`
	Score    float64 `json:"score"`
	Status   string  `json:"status"`
	Reason   string  `json:"reason,omitempty"`
	Expires  string  `json:"expires,omitempty"`
}

type Options struct {
	// ComplexityMin is the cyclomatic complexity at which a function is flagged, below the lint limit.
	ComplexityMin int
	// LinesMin is the length at which a function is flagged, below the lint limit.
	LinesMin int
}

func (o Options) withDefaults() Options {
	if o.ComplexityMin <= 0 {
		o.ComplexityMin = defaultComplexityMin
	}
	if o.LinesMin <= 0 {
		o.LinesMin = defaultLinesMin
	}
	return o
}

// Rank orders items worst first, then by place so the order is stable.
func Rank(items []Item) {
	slices.SortStableFunc(items, func(a, b Item) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Where, b.Where); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}
