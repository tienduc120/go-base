package debt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// MaxAcceptDays is the longest debt can be accepted for. Accepted debt that never expires is just ignored debt.
	MaxAcceptDays = 180
	minReasonLen  = 10
	dateLayout    = "2006-01-02"
	registerMode  = 0o600
	dirMode       = 0o750
	version       = 1
	hoursPerDay   = 24
)

// Entry records a decision about one item, with the proof it was made on.
type Entry struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Evidence string `json:"evidence"`
	Expires  string `json:"expires,omitempty"`
	Decided  string `json:"decided"`
}

// Register is the committed memory of what has been decided about debt. Items it does not mention are new.
type Register struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Decision is what a person or an agent wants recorded about an item.
type Decision struct {
	ID      string
	Status  string
	Reason  string
	Expires string
}

// RegisterPath is where the committed register lives in a repository.
func RegisterPath(root string) string {
	return filepath.Join(root, "docs", "debt", "register.json")
}

// LoadRegister reads the register. A missing file is an empty register; a corrupt one is an error, because reading it
// as empty would forget every decision.
func LoadRegister(path string) (Register, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Register{Version: version}, nil
	}
	if err != nil {
		return Register{}, err
	}
	var reg Register
	if err = json.Unmarshal(data, &reg); err != nil {
		return Register{}, fmt.Errorf("%s: %w", path, err)
	}
	return reg, nil
}

// Save writes the register sorted by id, so two saves of the same decisions are byte-identical.
func (r *Register) Save(path string) error {
	r.Version = version
	slices.SortFunc(r.Entries, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })
	if r.Entries == nil {
		r.Entries = []Entry{}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), registerMode)
}

func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Apply gives each item the status its register entry says, and returns the entries whose debt no longer exists.
func Apply(items []Item, reg Register, today time.Time) ([]Item, []Entry) {
	byID := make(map[string]Entry, len(reg.Entries))
	for _, e := range reg.Entries {
		byID[e.ID] = e
	}

	merged := slices.Clone(items)
	found := map[string]bool{}
	for i := range merged {
		found[merged[i].ID] = true
		e, ok := byID[merged[i].ID]
		if !ok {
			merged[i].Status = StatusUnconfirmed
			continue
		}
		merged[i].Status, merged[i].Reason, merged[i].Expires = e.Status, e.Reason, e.Expires
		if e.Status == StatusAccepted && expired(e.Expires, today) {
			merged[i].Status = StatusExpired
		}
	}

	var resolved []Entry
	for _, e := range reg.Entries {
		if !found[e.ID] {
			resolved = append(resolved, e)
		}
	}
	return merged, resolved
}

func expired(expires string, today time.Time) bool {
	t, err := time.Parse(dateLayout, expires)
	return err != nil || day(today).After(t)
}

// Confirm records a decision about an item the fresh scan still finds, with the scan's evidence as proof.
func (r *Register) Confirm(items []Item, d Decision, today time.Time) error {
	idx := slices.IndexFunc(items, func(it Item) bool { return it.ID == d.ID })
	if idx < 0 {
		return fmt.Errorf("no debt item %q: scan again, it may already be fixed", d.ID)
	}
	if err := validate(d, today); err != nil {
		return err
	}
	r.put(Entry{
		ID: d.ID, Status: d.Status, Reason: strings.TrimSpace(d.Reason), Evidence: items[idx].Evidence,
		Expires: d.Expires, Decided: day(today).Format(dateLayout),
	})
	return nil
}

func validate(d Decision, today time.Time) error {
	switch d.Status {
	case StatusConfirmed:
		if d.Expires != "" {
			return errors.New("a confirmed item takes no expires date; only an accepted one does")
		}
		return nil
	case StatusAccepted, StatusFalsePositive:
		if len(strings.TrimSpace(d.Reason)) < minReasonLen {
			return fmt.Errorf(
				"%s needs a reason of at least %d characters that someone else could check",
				d.Status,
				minReasonLen,
			)
		}
		if d.Status == StatusFalsePositive {
			return nil
		}
		return validateExpiry(d.Expires, today)
	}
	return fmt.Errorf("status %q is not one of confirmed, accepted, false-positive", d.Status)
}

func validateExpiry(expires string, today time.Time) error {
	t, err := time.Parse(dateLayout, expires)
	if err != nil {
		return fmt.Errorf("accepted needs an expires date as YYYY-MM-DD, got %q", expires)
	}
	if !t.After(day(today)) {
		return fmt.Errorf("expires %s must be after today", expires)
	}
	if limit := day(today).AddDate(0, 0, MaxAcceptDays); t.After(limit) {
		return fmt.Errorf(
			"expires %s is more than %d days away; accepted debt must come back for review",
			expires,
			MaxAcceptDays,
		)
	}
	return nil
}

func (r *Register) put(e Entry) {
	if i := slices.IndexFunc(r.Entries, func(x Entry) bool { return x.ID == e.ID }); i >= 0 {
		r.Entries[i] = e
		return
	}
	r.Entries = append(r.Entries, e)
}

// Baseline accepts every item nobody has decided about yet, with the longest allowed expiry. It is how existing debt is
// adopted once, so that from then on only new debt fails the check.
func (r *Register) Baseline(items []Item, reason string, today time.Time) (int, error) {
	if len(strings.TrimSpace(reason)) < minReasonLen {
		return 0, fmt.Errorf("a baseline needs a reason of at least %d characters", minReasonLen)
	}
	merged, _ := Apply(items, *r, today)
	expires := day(today).AddDate(0, 0, MaxAcceptDays).Format(dateLayout)

	n := 0
	for _, it := range merged {
		if it.Status != StatusUnconfirmed {
			continue
		}
		r.put(Entry{
			ID: it.ID, Status: StatusAccepted, Reason: strings.TrimSpace(reason), Evidence: it.Evidence,
			Expires: expires, Decided: day(today).Format(dateLayout),
		})
		n++
	}
	return n, nil
}

// Prune drops the entries whose debt has been paid.
func (r *Register) Prune(resolved []Entry) {
	gone := map[string]bool{}
	for _, e := range resolved {
		gone[e.ID] = true
	}
	r.Entries = slices.DeleteFunc(r.Entries, func(e Entry) bool { return gone[e.ID] })
}

// Check lists what blocks a commit: new debt nobody has decided about, and acceptances that ran out.
func Check(merged []Item) []string {
	var problems []string
	for _, it := range merged {
		switch it.Status {
		case StatusUnconfirmed:
			problems = append(problems, fmt.Sprintf(
				"new debt is untriaged: %s %s (%s). Fix it, or record a decision: debt confirm %s",
				it.Where, it.Evidence, it.Kind, it.ID))
		case StatusExpired:
			problems = append(problems, fmt.Sprintf(
				"acceptance expired on %s: %s %s (%s). Fix it, or accept it again with a new expiry: debt confirm %s",
				it.Expires, it.Where, it.Evidence, it.Kind, it.ID))
		}
	}
	slices.Sort(problems)
	return problems
}
