package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

func debtDay() time.Time { return time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC) }

func debtEnv(cwd string) environment {
	return environment{cwd: cwd, now: debtDay}
}

func debtRepo(t *testing.T) string {
	t.Helper()
	repo := verifyRepo(t)
	writeGo(t, repo, "tools/sample.go", `package main

// TODO: handle the empty case
func read() {
	run() //nolint:gosec // the path is a fixed constant
}

func run() {}
`)
	return repo
}

func writeGo(t *testing.T, repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type scanJSON struct {
	Items []struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	} `json:"items"`
	Resolved []struct {
		ID string `json:"id"`
	} `json:"resolved"`
}

func scanOf(t *testing.T, repo string) scanJSON {
	t.Helper()
	args := []string{"debt", "scan", "--json"}
	got := invoke(t, debtEnv(repo), args...)
	if got.code != 0 {
		t.Fatalf("scan: exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	var out scanJSON
	if err := json.Unmarshal([]byte(got.stdout), &out); err != nil {
		t.Fatalf("scan --json is not JSON: %v\n%s", err, got.stdout)
	}
	return out
}

func TestDebtScanListsWhatTheGatesAllow(t *testing.T) {
	t.Parallel()

	repo := debtRepo(t)
	got := invoke(t, debtEnv(repo), "debt", "scan")

	if got.code != 0 {
		t.Fatalf("exit = %d\n%s", got.code, got.stderr)
	}
	for _, want := range []string{"2 items", "2 unconfirmed", "tools/sample.go:3", "tools/sample.go:5", "suppression", "untracked-todo"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("scan output lacks %q:\n%s", want, got.stdout)
		}
	}
}

func TestDebtCheckFailsOnUntriagedDebtAndPassesAfterABaseline(t *testing.T) {
	t.Parallel()

	repo := debtRepo(t)

	first := invoke(t, debtEnv(repo), "debt", "check")
	if first.code != 1 || !strings.Contains(first.stdout, "tools/sample.go") {
		t.Fatalf("an untriaged repo must fail the check: exit = %d\n%s", first.code, first.stdout)
	}

	base := invoke(t, debtEnv(repo), "debt", "baseline", "--reason", "present when debt tracking was adopted")
	if base.code != 0 || !strings.Contains(base.stdout, "2") {
		t.Fatalf("baseline: exit = %d\n%s\n%s", base.code, base.stdout, base.stderr)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs", "debt", "register.json")); err != nil {
		t.Fatalf("the register must be a file in the repo: %v", err)
	}
	if after := invoke(t, debtEnv(repo), "debt", "check"); after.code != 0 {
		t.Errorf("after the baseline the check passes: exit = %d\n%s", after.code, after.stdout)
	}

	writeGo(t, repo, "tools/fresh.go", "package main\n\n// FIXME: this is new debt\nfunc fresh() {}\n")
	again := invoke(t, debtEnv(repo), "debt", "check")
	if again.code != 1 || !strings.Contains(again.stdout, "tools/fresh.go") ||
		strings.Contains(again.stdout, "tools/sample.go") {
		t.Errorf("only the new debt fails: exit = %d\n%s", again.code, again.stdout)
	}
}

func TestDebtBaselineNeedsAReason(t *testing.T) {
	t.Parallel()

	if got := invoke(t, debtEnv(debtRepo(t)), "debt", "baseline"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestDebtExpiredAcceptanceFailsTheCheck(t *testing.T) {
	t.Parallel()

	repo := debtRepo(t)
	invoke(t, debtEnv(repo), "debt", "baseline", "--reason", "present when debt tracking was adopted")

	later := environment{cwd: repo, now: func() time.Time { return debtDay().AddDate(0, 0, 200) }}
	got := invoke(t, later, "debt", "check")

	if got.code != 1 || !strings.Contains(got.stdout, "expired") {
		t.Errorf("accepted debt must come back for review: exit = %d\n%s", got.code, got.stdout)
	}
}

func TestDebtConfirmThenNextShowsThePlaybook(t *testing.T) {
	t.Parallel()

	repo := debtRepo(t)
	var suppression string
	for _, it := range scanOf(t, repo).Items {
		if it.Kind == "suppression" {
			suppression = it.ID
		}
	}

	if got := invoke(t, debtEnv(repo), "debt", "next"); !strings.Contains(got.stdout, "nothing is confirmed") {
		t.Errorf("recovery starts from confirmed debt only:\n%s", got.stdout)
	}

	confirm := invoke(t, debtEnv(repo), "debt", "confirm", suppression, "--status", "confirmed")
	if confirm.code != 0 {
		t.Fatalf("confirm: exit = %d\n%s\n%s", confirm.code, confirm.stdout, confirm.stderr)
	}

	next := invoke(t, debtEnv(repo), "debt", "next")
	for _, want := range []string{suppression, "tools/sample.go:5", "Remove the //nolint", "one item per commit"} {
		if !strings.Contains(next.stdout, want) {
			t.Errorf("next lacks %q:\n%s", want, next.stdout)
		}
	}
}

func TestDebtConfirmRefusesWeakDecisionsAndUnknownIDs(t *testing.T) {
	t.Parallel()

	repo := debtRepo(t)
	id := scanOf(t, repo).Items[0].ID
	for name, args := range map[string][]string{
		"accepted without an expiry": {"debt", "confirm", id, "--status", "accepted", "--reason", "waiting on the vendor fix"},
		"accepted without a reason":  {"debt", "confirm", id, "--status", "accepted", "--expires", "2026-12-01"},
		"an unknown id":              {"debt", "confirm", "suppression:doesnotexist", "--status", "confirmed"},
		"no status":                  {"debt", "confirm", id},
	} {
		if got := invoke(t, debtEnv(repo), args...); got.code == 0 {
			t.Errorf("%s: must be refused\n%s", name, got.stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "docs", "debt", "register.json")); err == nil {
		t.Error("a refused decision must not create or change the register")
	}
}

func TestDebtPruneRemovesPaidOffEntries(t *testing.T) {
	t.Parallel()

	repo := debtRepo(t)
	invoke(t, debtEnv(repo), "debt", "baseline", "--reason", "present when debt tracking was adopted")

	// Pay one debt off: remove the TODO.
	writeGo(
		t,
		repo,
		"tools/sample.go",
		"package main\n\nfunc read() {\n\trun() //nolint:gosec // the path is a fixed constant\n}\n\nfunc run() {}\n",
	)
	if resolved := scanOf(t, repo).Resolved; len(resolved) != 1 {
		t.Fatalf("resolved = %+v, want the TODO's entry", resolved)
	}

	got := invoke(t, debtEnv(repo), "debt", "prune")
	if got.code != 0 || !strings.Contains(got.stdout, "1") {
		t.Fatalf("prune: exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	if resolved := scanOf(t, repo).Resolved; len(resolved) != 0 {
		t.Errorf("nothing should remain to prune: %+v", resolved)
	}
}

func TestDebtScanCanMeasureCoverageThroughTheRunner(t *testing.T) {
	t.Parallel()

	runner := procFunc(func(proc.Spec) (proc.Result, error) {
		return proc.Result{Output: "ok  \texample.com/root/tools/thin\t0.1s\tcoverage: 40.0% of statements\n"}, nil
	})
	env := debtEnv(debtRepo(t))
	env.procRunner = runner

	got := invoke(t, env, "debt", "scan", "--coverage")
	if !strings.Contains(got.stdout, "coverage 40.0%") {
		t.Errorf("weak coverage should be listed:\n%s", got.stdout)
	}
}

func TestDebtNeedsASubcommandAndARepo(t *testing.T) {
	t.Parallel()

	repo := debtRepo(t)
	for _, args := range [][]string{{"debt"}, {"debt", "bogus"}} {
		if got := invoke(t, debtEnv(repo), args...); got.code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, got.code)
		}
	}
	if got := invoke(t, debtEnv(t.TempDir()), "debt", "scan"); got.code != 2 {
		t.Errorf("outside a repo: exit = %d, want 2", got.code)
	}
}
