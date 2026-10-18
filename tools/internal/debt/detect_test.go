package debt_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/debt"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func scan(t *testing.T, root string, opts debt.Options) []debt.Item {
	t.Helper()
	items, err := debt.Scan(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func ofKind(items []debt.Item, kind string) []debt.Item {
	var out []debt.Item
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

func TestSuppressionsAreDebt(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{"pkg/a.go": `package pkg

func Read() {
	run() //nolint:gosec // the path is a fixed constant
}

func Other() {
	run() //nolint:errcheck,gosec // best effort
	run() //nolint
}

func run() {}
`})
	got := ofKind(scan(t, root, debt.Options{}), debt.KindSuppression)

	if len(got) != 3 {
		t.Fatalf("got %d suppressions, want 3: %+v", len(got), got)
	}
	var blanket, reasoned *debt.Item
	for i := range got {
		switch got[i].Subject {
		case "(all linters)":
			blanket = &got[i]
		case "gosec":
			reasoned = &got[i]
		}
	}
	if blanket == nil || reasoned == nil {
		t.Fatalf("expected a blanket and a gosec suppression: %+v", got)
	}
	if blanket.Score <= reasoned.Score {
		t.Errorf("a suppression that silences every linter is worse: %v vs %v", blanket.Score, reasoned.Score)
	}
	if !strings.Contains(reasoned.Evidence, "the path is a fixed constant") || reasoned.Where != "pkg/a.go:4" {
		t.Errorf("evidence and place must be exact: %+v", reasoned)
	}
}

func TestADirectiveInsideAStringIsNotASuppression(t *testing.T) {
	t.Parallel()

	root := tree(
		t,
		map[string]string{
			"a.go": "package a\n\nconst text = \"//nolint:gosec // only text\"\n\n// Note that \"TODO\" in a string below is not a marker.\nvar _ = \"TODO: nothing\"\n",
		},
	)
	items := scan(t, root, debt.Options{})

	if got := ofKind(items, debt.KindSuppression); len(got) != 0 {
		t.Errorf("a string is not a directive: %+v", got)
	}
	if got := ofKind(items, debt.KindUntrackedTodo); len(got) != 0 {
		t.Errorf("a string is not a marker: %+v", got)
	}
}

func TestIDsSurviveEditsThatOnlyMoveCode(t *testing.T) {
	t.Parallel()

	body := "func Read() {\n\trun() //nolint:gosec // fixed path\n}\n\nfunc run() {}\n"
	before := scan(t, tree(t, map[string]string{"a.go": "package a\n\n" + body}), debt.Options{})
	after := scan(
		t,
		tree(t, map[string]string{"a.go": "package a\n\n// An unrelated comment.\n\nvar unused = 1\n\n" + body}),
		debt.Options{},
	)

	b, a := ofKind(before, debt.KindSuppression), ofKind(after, debt.KindSuppression)
	if len(b) != 1 || len(a) != 1 || b[0].ID != a[0].ID {
		t.Fatalf("the same suppression must keep its id when lines shift: %+v vs %+v", b, a)
	}
	if b[0].Where == a[0].Where {
		t.Errorf("the line number should have moved: %s", b[0].Where)
	}
}

func TestIdenticalSuppressionsInOneFunctionGetDistinctIDs(t *testing.T) {
	t.Parallel()

	root := tree(
		t,
		map[string]string{
			"a.go": "package a\n\nfunc Read() {\n\trun() //nolint:gosec // same reason\n\trun() //nolint:gosec // same reason\n}\n\nfunc run() {}\n",
		},
	)
	got := ofKind(scan(t, root, debt.Options{}), debt.KindSuppression)

	if len(got) != 2 || got[0].ID == got[1].ID {
		t.Errorf("two directives are two debts: %+v", got)
	}
}

func TestUntrackedMarkersAreDebtAndTrackedOnesAreNot(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{"a.go": `package a

// TODO: handle the empty case
func A() {}

// TODO(#123): handle the empty case
func B() {}

// FIXME(adr-0008): wrong rounding
func C() {}

// HACK works around the driver
func D() {}

// XXX
func E() {}
`})
	got := ofKind(scan(t, root, debt.Options{}), debt.KindUntrackedTodo)

	var subjects []string
	for _, it := range got {
		subjects = append(subjects, it.Subject)
	}
	slices.Sort(subjects)
	if want := []string{"HACK", "TODO", "XXX"}; !slices.Equal(subjects, want) {
		t.Errorf("untracked markers = %v, want %v (a marker with a reference is already tracked)", subjects, want)
	}
}

func TestComplexFunctionsAreFlaggedBeforeTheyBreakTheLimit(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{"a.go": `package a

func Simple(x int) int { return x }

func Branchy(a, b, c int) int {
	n := 0
	if a > 0 && b > 0 {
		n++
	} else if c > 0 || a < 0 {
		n--
	}
	for i := 0; i < a; i++ {
		switch {
		case i > 1:
			n += i
		case i > 2:
			n -= i
		default:
			n = 0
		}
	}
	return n
}
`})
	got := ofKind(scan(t, root, debt.Options{ComplexityMin: 5}), debt.KindComplexity)

	if len(got) != 1 || got[0].Subject != "Branchy" {
		t.Fatalf("only Branchy is complex enough: %+v", got)
	}
	// 1 + if + else-if + && + || + for + two non-default cases = 8
	if !strings.Contains(got[0].Evidence, "cyclomatic 8") {
		t.Errorf("evidence = %q, want cyclomatic 8", got[0].Evidence)
	}
}

func TestLongFunctionsAreFlaggedToo(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	sb.WriteString("package a\n\nfunc Long() int {\n\tn := 0\n")
	for range 90 {
		sb.WriteString("\tn++\n")
	}
	sb.WriteString("\treturn n\n}\n")
	got := ofKind(scan(t, tree(t, map[string]string{"a.go": sb.String()}), debt.Options{}), debt.KindComplexity)

	if len(got) != 1 || !strings.Contains(got[0].Evidence, "lines") {
		t.Errorf("a 90-line function is long even when it is simple: %+v", got)
	}
}

func TestMethodsAreNamedWithTheirReceiver(t *testing.T) {
	t.Parallel()

	root := tree(
		t,
		map[string]string{
			"a.go": "package a\n\ntype T struct{}\n\nfunc (t *T) Busy(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\tif a < 0 {\n\t\treturn 2\n\t}\n\treturn 0\n}\n",
		},
	)
	got := ofKind(scan(t, root, debt.Options{ComplexityMin: 3}), debt.KindComplexity)

	if len(got) != 1 || got[0].Subject != "T.Busy" {
		t.Errorf("a method is named Receiver.Method: %+v", got)
	}
}

func TestTestFilesCountForSuppressionsButNotForComplexity(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{"a_test.go": `package a

import "testing"

func TestBusy(t *testing.T) { //nolint:paralleltest // a process-wide resource
	for i := 0; i < 3; i++ {
		if i > 0 && i < 2 {
			t.Log(i)
		}
	}
}
`})
	items := scan(t, root, debt.Options{ComplexityMin: 2})

	if len(ofKind(items, debt.KindSuppression)) != 1 {
		t.Errorf("a suppression in a test is still a suppression: %+v", items)
	}
	if len(ofKind(items, debt.KindComplexity)) != 0 {
		t.Errorf("long table tests are not complexity debt: %+v", items)
	}
}

func TestScanSkipsWhatIsNotOurs(t *testing.T) {
	t.Parallel()

	marker := "package a\n\n// TODO: x\nfunc A() {}\n"
	root := tree(t, map[string]string{
		"keep/a.go":           marker,
		"testdata/a.go":       marker,
		"vendor/a.go":         marker,
		".local/a.go":         marker,
		".agentctl/bin/a.go":  marker,
		"node_modules/a/a.go": marker,
	})
	got := ofKind(scan(t, root, debt.Options{}), debt.KindUntrackedTodo)

	if len(got) != 1 || got[0].Where != "keep/a.go:3" {
		t.Errorf("only keep/ is ours: %+v", got)
	}
}

func TestScanIsDeterministicAndRanked(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"a.go": "package a\n\n// TODO: later\nfunc A() {\n\tb() //nolint\n}\n\nfunc b() {}\n",
		"b.go": "package a\n\n// TODO: also later\nfunc C() {}\n",
	})
	first := scan(t, root, debt.Options{})
	second := scan(t, root, debt.Options{})

	if len(first) != len(second) {
		t.Fatal("two scans of the same tree must agree")
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("order differs at %d: %s vs %s", i, first[i].ID, second[i].ID)
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i].Score > first[i-1].Score {
			t.Errorf("not ranked by score: %v after %v", first[i].Score, first[i-1].Score)
		}
	}
}

func TestUnparseableFilesAreReportedNotIgnored(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{"broken.go": "package a\n\nfunc {"})
	if _, err := debt.Scan(root, debt.Options{}); err == nil {
		t.Error("a file the scan cannot read hides debt, so it must be an error")
	}
}
