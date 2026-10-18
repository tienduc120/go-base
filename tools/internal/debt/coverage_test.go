package debt_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/debt"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

const goTestOutput = `ok  	github.com/acme/shop/internal/billing	0.412s	coverage: 91.5% of statements
ok  	github.com/acme/shop/internal/orders	1.101s	coverage: 58.3% of statements
?   	github.com/acme/shop/cmd/billing-api	[no test files]
	github.com/acme/shop/internal/ghost		coverage: 0.0% of statements
ok  	github.com/acme/shop/internal/edge	0.2s	coverage: 70.0% of statements
FAIL	github.com/acme/shop/internal/broken [build failed]
`

func TestParseCoverageReadsGoTestOutput(t *testing.T) {
	t.Parallel()

	got := map[string]debt.Package{}
	for _, p := range debt.ParseCoverage(goTestOutput) {
		got[p.Path] = p
	}

	if p := got["github.com/acme/shop/internal/orders"]; p.Coverage != 58.3 || !p.HasTests {
		t.Errorf("orders = %+v", p)
	}
	if p := got["github.com/acme/shop/cmd/billing-api"]; p.HasTests {
		t.Errorf("a package with no test files has no tests: %+v", p)
	}
	if p := got["github.com/acme/shop/internal/ghost"]; p.Coverage != 0 || p.HasTests {
		t.Errorf("ghost reports 0%% and no test files: %+v", p)
	}
}

func TestCoverageItemsFlagWeakAndMissingTestsOnly(t *testing.T) {
	t.Parallel()

	items := debt.CoverageItems(debt.ParseCoverage(goTestOutput), 70)

	byPath := map[string]debt.Item{}
	for _, it := range items {
		byPath[it.Subject] = it
	}
	if len(items) != 3 {
		t.Fatalf(
			"items = %+v, want orders (58.3%%), billing-api and ghost (no tests); billing and edge are fine",
			items,
		)
	}
	if o := byPath["github.com/acme/shop/internal/orders"]; !strings.Contains(o.Evidence, "58.3%") ||
		o.Kind != debt.KindCoverage {
		t.Errorf("orders = %+v", o)
	}
	if api := byPath["github.com/acme/shop/cmd/billing-api"]; !strings.Contains(api.Evidence, "no test files") {
		t.Errorf("billing-api = %+v", api)
	}
	if byPath["github.com/acme/shop/cmd/billing-api"].Score <= byPath["github.com/acme/shop/internal/orders"].Score {
		t.Error("code with no tests at all is worse than code with weak tests")
	}
}

func TestCoverageIDsDoNotDependOnThePercentage(t *testing.T) {
	t.Parallel()

	a := debt.CoverageItems([]debt.Package{{Path: "p", Coverage: 40, HasTests: true}}, 70)
	b := debt.CoverageItems([]debt.Package{{Path: "p", Coverage: 55, HasTests: true}}, 70)
	if a[0].ID != b[0].ID {
		t.Errorf("a decision about a package must survive its coverage changing: %s vs %s", a[0].ID, b[0].ID)
	}
}

type fakeRunner struct {
	res proc.Result
	err error
	got proc.Spec
}

func (f *fakeRunner) Run(_ context.Context, spec proc.Spec) (proc.Result, error) {
	f.got = spec
	return f.res, f.err
}

func TestCoverageRunsTheTestsInTheModule(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{res: proc.Result{Output: goTestOutput, ExitCode: 1}}
	// Exit 1 is how go test reports the failing package in the sample output, so the percentages still count.
	items, err := debt.Coverage(t.Context(), r, "/repo/tools", 70)
	if err != nil {
		t.Fatal(err)
	}

	if r.got.Dir != "/repo/tools" || r.got.Name != "go" || r.got.Args[0] != "test" {
		t.Errorf("ran %+v", r.got)
	}
	if len(items) == 0 {
		t.Error("coverage items expected")
	}
}

func TestCoverageFailsWhenTheTestRunItselfIsCutShort(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{res: proc.Result{TimedOut: true, Reason: proc.ReasonDeadline}}
	if _, err := debt.Coverage(t.Context(), r, ".", 70); err == nil {
		t.Error("a timed-out measurement says nothing about coverage")
	}
}

func TestPlaybookCoversEveryKindAndSaysHowToStaySafe(t *testing.T) {
	t.Parallel()

	for kind, must := range map[string]string{
		debt.KindSuppression:   "remove",
		debt.KindUntrackedTodo: "reference",
		debt.KindComplexity:    "characterization",
		debt.KindCoverage:      "test",
	} {
		got := debt.Playbook(kind)
		if !strings.Contains(strings.ToLower(got), must) {
			t.Errorf("playbook for %s lacks %q:\n%s", kind, must, got)
		}
		if !strings.Contains(got, "one item per commit") {
			t.Errorf("every playbook keeps changes small:\n%s", got)
		}
	}
	if debt.Playbook("mystery") == "" {
		t.Error("an unknown kind still gets general advice")
	}
}
