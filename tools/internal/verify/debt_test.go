package verify_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/debt"
	"github.com/tienduc120/go-base/tools/internal/verify"
)

func runDebtStep(t *testing.T, files map[string]string, register string) verify.Result {
	t.Helper()
	root := workspace(t, files)
	if register != "" {
		path := debt.RegisterPath(root)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(register), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := env(t, root, &fakeProc{})
	return verify.Execute(t.Context(), e, []verify.Step{verify.DebtStep(e)})[0]
}

const untracked = "package main\n\n// TODO: handle the empty case\nfunc a() {}\n"

func idOf(t *testing.T, files map[string]string) string {
	t.Helper()
	items, err := debt.Scan(workspace(t, files), debt.Options{})
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
	return items[0].ID
}

func TestDebtStepPassesWhenThereIsNoDebt(t *testing.T) {
	t.Parallel()

	got := runDebtStep(t, map[string]string{"tools/main.go": "package main\n\nfunc a() {}\n"}, "")
	if !got.Pass {
		t.Errorf("no debt, no failure: %+v", got)
	}
}

func TestDebtStepFailsOnNewUntriagedDebt(t *testing.T) {
	t.Parallel()

	got := runDebtStep(t, map[string]string{"tools/main.go": untracked}, "")

	if got.Pass || got.Name != "debt ratchet" {
		t.Fatalf("new debt nobody decided about must fail: %+v", got)
	}
	if !strings.Contains(got.Detail, "tools/main.go:3") || !strings.Contains(got.Detail, "debt confirm") {
		t.Errorf("the failure must say where and how to resolve it: %q", got.Detail)
	}
}

func TestDebtStepPassesOnceTheDebtIsAcceptedAndFailsWhenTheAcceptanceExpires(t *testing.T) {
	t.Parallel()

	files := map[string]string{"tools/main.go": untracked}
	id := idOf(t, files)
	entry := func(expires string) string {
		return `{"version":1,"entries":[{"id":"` + id + `","status":"accepted","reason":"adopted with debt tracking","evidence":"e","expires":"` + expires + `","decided":"2026-10-18"}]}`
	}

	if got := runDebtStep(t, files, entry("2999-01-01")); !got.Pass {
		t.Errorf("accepted debt passes: %+v", got)
	}
	expired := runDebtStep(t, files, entry("2000-01-01"))
	if expired.Pass || !strings.Contains(expired.Detail, "expired") {
		t.Errorf("an expired acceptance must fail: %+v", expired)
	}
}

func TestDebtStepReportsUnreadableCodeAsAFailure(t *testing.T) {
	t.Parallel()

	got := runDebtStep(t, map[string]string{"tools/main.go": "package main\n\nfunc {"}, "")
	if got.Pass {
		t.Error("code the scan cannot read hides debt, so it must fail the step")
	}
}

func TestDebtStepKeepsLongListsShort(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for i := range 40 {
		sb.WriteString("// TODO: item ")
		sb.WriteString(strings.Repeat("x", i+1))
		sb.WriteString("\nfunc f")
		sb.WriteString(strings.Repeat("a", i+1))
		sb.WriteString("() {}\n\n")
	}
	got := runDebtStep(t, map[string]string{"tools/main.go": sb.String()}, "")

	if lines := strings.Count(got.Detail, "\n"); lines > 30 {
		t.Errorf("detail is %d lines; a first run on a big repo must not flood the reader", lines)
	}
	if !strings.Contains(got.Detail, "more") {
		t.Errorf("a cut list says how much it dropped: %q", got.Detail)
	}
}
