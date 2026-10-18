package debt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tienduc120/go-base/tools/internal/debt"
)

func today() time.Time { return time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC) }

func item(id, kind, evidence string) debt.Item {
	return debt.Item{
		ID:       id,
		Kind:     kind,
		Where:    "a.go:1",
		Subject:  "s",
		Evidence: evidence,
		Status:   debt.StatusUnconfirmed,
		Score:    3,
	}
}

func statusOf(t *testing.T, items []debt.Item, id string) string {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it.Status
		}
	}
	t.Fatalf("no item %s in %+v", id, items)
	return ""
}

func TestApplyDerivesEachItemsStatusFromTheRegister(t *testing.T) {
	t.Parallel()

	reg := debt.Register{Entries: []debt.Entry{
		{ID: "a", Status: debt.StatusConfirmed},
		{ID: "b", Status: debt.StatusFalsePositive, Reason: "it is a string, not a directive"},
		{ID: "c", Status: debt.StatusAccepted, Reason: "waiting on the vendor fix", Expires: "2026-12-31"},
		{ID: "d", Status: debt.StatusAccepted, Reason: "waiting on the vendor fix", Expires: "2026-10-01"},
	}}
	items := []debt.Item{
		item("a", debt.KindComplexity, ""), item("b", debt.KindSuppression, ""), item("c", debt.KindSuppression, ""),
		item("d", debt.KindSuppression, ""), item("e", debt.KindUntrackedTodo, ""),
	}

	merged, resolved := debt.Apply(items, reg, today())

	want := map[string]string{
		"a": debt.StatusConfirmed, "b": debt.StatusFalsePositive, "c": debt.StatusAccepted,
		"d": debt.StatusExpired, "e": debt.StatusUnconfirmed,
	}
	for id, status := range want {
		if got := statusOf(t, merged, id); got != status {
			t.Errorf("%s: status = %s, want %s", id, got, status)
		}
	}
	if len(resolved) != 0 {
		t.Errorf("every entry still has its item: %+v", resolved)
	}
}

func TestApplyReportsEntriesWhoseDebtIsGone(t *testing.T) {
	t.Parallel()

	reg := debt.Register{Entries: []debt.Entry{{ID: "fixed", Status: debt.StatusConfirmed}}}
	_, resolved := debt.Apply([]debt.Item{item("other", debt.KindComplexity, "")}, reg, today())

	if len(resolved) != 1 || resolved[0].ID != "fixed" {
		t.Errorf("resolved = %+v, want the entry whose debt was paid", resolved)
	}
}

func TestConfirmRecordsEvidenceFromTheFreshScan(t *testing.T) {
	t.Parallel()

	var reg debt.Register
	items := []debt.Item{item("x", debt.KindComplexity, "cyclomatic 21, 90 lines")}

	if err := reg.Confirm(items, debt.Decision{ID: "x", Status: debt.StatusConfirmed}, today()); err != nil {
		t.Fatal(err)
	}
	e := reg.Entries[0]
	if e.Evidence != "cyclomatic 21, 90 lines" || e.Decided != "2026-10-18" || e.Status != debt.StatusConfirmed {
		t.Errorf("entry = %+v: the proof is what the scan found, not what the caller says", e)
	}
}

func TestConfirmRefusesWhatTheScanNoLongerFinds(t *testing.T) {
	t.Parallel()

	var reg debt.Register
	err := reg.Confirm(
		[]debt.Item{item("x", debt.KindComplexity, "")},
		debt.Decision{ID: "ghost", Status: debt.StatusConfirmed},
		today(),
	)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("err = %v: a claim about debt that does not exist is not evidence", err)
	}
	if len(reg.Entries) != 0 {
		t.Error("nothing may be recorded for an unknown item")
	}
}

func TestConfirmEnforcesTheRulesForEachDecision(t *testing.T) {
	t.Parallel()

	items := []debt.Item{item("x", debt.KindSuppression, "e")}
	tests := []struct {
		name string
		d    debt.Decision
		want string
	}{
		{"unknown status", debt.Decision{ID: "x", Status: "later"}, "status"},
		{
			"accepted needs a reason",
			debt.Decision{ID: "x", Status: debt.StatusAccepted, Expires: "2026-12-01"},
			"reason",
		},
		{
			"a one word reason is not one",
			debt.Decision{ID: "x", Status: debt.StatusAccepted, Reason: "ok", Expires: "2026-12-01"},
			"reason",
		},
		{
			"accepted needs an expiry",
			debt.Decision{ID: "x", Status: debt.StatusAccepted, Reason: "waiting on the vendor"},
			"expires",
		},
		{
			"expiry must be a date",
			debt.Decision{ID: "x", Status: debt.StatusAccepted, Reason: "waiting on the vendor", Expires: "soon"},
			"expires",
		},
		{
			"expiry must be in the future",
			debt.Decision{ID: "x", Status: debt.StatusAccepted, Reason: "waiting on the vendor", Expires: "2026-10-18"},
			"expires",
		},
		{
			"debt cannot be accepted forever",
			debt.Decision{ID: "x", Status: debt.StatusAccepted, Reason: "waiting on the vendor", Expires: "2027-12-31"},
			"180",
		},
		{"false positive needs a reason", debt.Decision{ID: "x", Status: debt.StatusFalsePositive}, "reason"},
		{
			"confirmed takes no expiry",
			debt.Decision{ID: "x", Status: debt.StatusConfirmed, Expires: "2026-12-01"},
			"expires",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var reg debt.Register
			err := reg.Confirm(items, tc.d, today())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestConfirmAcceptsAWellFormedAcceptanceAndReplacesAnOlderDecision(t *testing.T) {
	t.Parallel()

	var reg debt.Register
	items := []debt.Item{item("x", debt.KindSuppression, "e")}
	if err := reg.Confirm(items, debt.Decision{ID: "x", Status: debt.StatusConfirmed}, today()); err != nil {
		t.Fatal(err)
	}
	accepted := debt.Decision{
		ID:      "x",
		Status:  debt.StatusAccepted,
		Reason:  "waiting on the vendor fix",
		Expires: "2027-03-01",
	}
	if err := reg.Confirm(items, accepted, today()); err != nil {
		t.Fatal(err)
	}

	if len(reg.Entries) != 1 || reg.Entries[0].Status != debt.StatusAccepted || reg.Entries[0].Expires != "2027-03-01" {
		t.Errorf("a new decision about an item replaces the old one: %+v", reg.Entries)
	}
}

func TestBaselineAcceptsEverythingCurrentWithAnExpiry(t *testing.T) {
	t.Parallel()

	var reg debt.Register
	items := []debt.Item{item("a", debt.KindSuppression, ""), item("b", debt.KindComplexity, "")}
	reg.Entries = append(reg.Entries, debt.Entry{ID: "a", Status: debt.StatusConfirmed})

	n, err := reg.Baseline(items, "present when debt tracking was adopted", today())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("baselined %d, want only the untriaged one: a was already decided", n)
	}
	var b debt.Entry
	for _, e := range reg.Entries {
		if e.ID == "b" {
			b = e
		}
	}
	if b.Status != debt.StatusAccepted || b.Expires != "2027-04-16" {
		t.Errorf("b = %+v, want accepted until 180 days out (2027-04-16)", b)
	}
	if _, err = reg.Baseline(items, "no", today()); err == nil {
		t.Error("a baseline needs a real reason")
	}
}

func TestCheckFailsOnNewDebtAndExpiredAcceptance(t *testing.T) {
	t.Parallel()

	merged := []debt.Item{
		{ID: "ok", Status: debt.StatusAccepted},
		{ID: "fp", Status: debt.StatusFalsePositive},
		{ID: "c", Status: debt.StatusConfirmed},
		{
			ID:       "new",
			Kind:     debt.KindSuppression,
			Where:    "a.go:9",
			Evidence: "//nolint:gosec // x",
			Status:   debt.StatusUnconfirmed,
		},
		{ID: "old", Kind: debt.KindComplexity, Where: "b.go:3", Status: debt.StatusExpired, Expires: "2026-10-01"},
	}
	problems := debt.Check(merged)

	if len(problems) != 2 {
		t.Fatalf("problems = %+v, want the new item and the expired one", problems)
	}
	joined := problems[0] + "\n" + problems[1]
	for _, want := range []string{"a.go:9", "b.go:3", "2026-10-01"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems lack %q:\n%s", want, joined)
		}
	}
}

func TestCheckPassesWhenEverythingIsTriaged(t *testing.T) {
	t.Parallel()

	merged := []debt.Item{{ID: "a", Status: debt.StatusConfirmed}, {ID: "b", Status: debt.StatusAccepted}}
	if problems := debt.Check(merged); len(problems) != 0 {
		t.Errorf("problems = %v", problems)
	}
}

func TestPruneDropsOnlyThePaidOffEntries(t *testing.T) {
	t.Parallel()

	reg := debt.Register{Entries: []debt.Entry{{ID: "keep"}, {ID: "paid"}}}
	reg.Prune([]debt.Entry{{ID: "paid"}})

	if len(reg.Entries) != 1 || reg.Entries[0].ID != "keep" {
		t.Errorf("entries = %+v", reg.Entries)
	}
}

func TestRegisterRoundTripsSortedAndDeterministic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "docs", "debt", "register.json")
	reg := debt.Register{
		Entries: []debt.Entry{{ID: "b", Status: debt.StatusConfirmed}, {ID: "a", Status: debt.StatusConfirmed}},
	}
	if err := reg.Save(path); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)

	loaded, err := debt.LoadRegister(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 2 || loaded.Entries[0].ID != "a" {
		t.Errorf("entries must come back sorted by id: %+v", loaded.Entries)
	}
	if err = loaded.Save(path); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(path); string(first) != string(second) {
		t.Error("saving twice must give identical bytes, so diffs show only real decisions")
	}
	if !strings.HasSuffix(string(first), "\n") {
		t.Error("files end with a newline")
	}
}

func TestLoadRegisterOfAMissingFileIsEmptyNotAnError(t *testing.T) {
	t.Parallel()

	reg, err := debt.LoadRegister(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(reg.Entries) != 0 {
		t.Errorf("reg = %+v, err = %v: no register means nothing decided yet", reg, err)
	}
}

func TestLoadRegisterRejectsAnUnreadableFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "register.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := debt.LoadRegister(path); err == nil {
		t.Error("a corrupt register must not be read as an empty one: that would forget every decision")
	}
}
