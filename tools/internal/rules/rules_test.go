package rules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tienduc120/go-base/tools/internal/rules"
)

func repoFS() fstest.MapFS {
	return fstest.MapFS{
		".golangci.yml": {Data: []byte("version: \"2\"\n")},
		"arch-go.yml":   {Data: []byte("version: 1\n")},
		"testdata/clean/lib/lib.go": {
			Data: []byte("// Package lib is compliant.\npackage lib\n"),
		},
		"testdata/violations/forbidigo-sleep/rule.json": {
			Data: []byte(`{"enforcer":"golangci-lint","contains":"time.Sleep","doc":"no sleeping"}`),
		},
		"testdata/violations/forbidigo-sleep/lib/lib.go": {Data: []byte("package lib\n")},
		"testdata/violations/arch-domain/rule.json": {
			Data: []byte(`{"enforcer":"arch-go","contains":"application"}`),
		},
		"testdata/violations/arch-domain/services/a/internal/domain/d.go": {Data: []byte("package domain\n")},
	}
}

func TestDiscover(t *testing.T) {
	t.Parallel()

	got, err := rules.Discover(repoFS())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d fixtures, want 2: %+v", len(got), got)
	}
	if got[0].ID != "arch-domain" || got[1].ID != "forbidigo-sleep" {
		t.Errorf("fixtures must be sorted by id, got %q then %q", got[0].ID, got[1].ID)
	}
	if got[1].Spec.Enforcer != rules.GolangciLint || got[1].Spec.Contains != "time.Sleep" {
		t.Errorf("spec not parsed: %+v", got[1].Spec)
	}
}

func TestDiscoverRejectsBadFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(fstest.MapFS)
		want   string
	}{
		{"missing rule.json", func(m fstest.MapFS) {
			m["testdata/violations/orphan/x.go"] = &fstest.MapFile{Data: []byte("package x\n")}
		}, "orphan"},
		{"unknown enforcer", func(m fstest.MapFS) {
			m["testdata/violations/bad/rule.json"] = &fstest.MapFile{
				Data: []byte(`{"enforcer":"magic","contains":"x"}`),
			}
		}, "magic"},
		{"empty contains matches any finding", func(m fstest.MapFS) {
			m["testdata/violations/bad/rule.json"] = &fstest.MapFile{
				Data: []byte(`{"enforcer":"arch-go","contains":""}`),
			}
		}, "contains"},
		{"malformed json", func(m fstest.MapFS) {
			m["testdata/violations/bad/rule.json"] = &fstest.MapFile{Data: []byte(`{`)}
		}, "bad"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := repoFS()
			tc.mutate(m)
			_, err := rules.Discover(m)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestDiscoverNeedsTheViolationsDir(t *testing.T) {
	t.Parallel()

	if _, err := rules.Discover(fstest.MapFS{}); err == nil {
		t.Fatal("want an error when there are no fixtures at all")
	}
}

func TestStage(t *testing.T) {
	t.Parallel()

	fsys := repoFS()
	fixtures, err := rules.Discover(fsys)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()

	if err = rules.Stage(fsys, fixtures, dest, "1.27.2"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"go.mod",
		".golangci.yml",
		"arch-go.yml",
		"clean/lib/lib.go",
		"forbidigo-sleep/lib/lib.go",
		"arch-domain/services/a/internal/domain/d.go",
	}
	for _, rel := range want {
		if _, statErr := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); statErr != nil {
			t.Errorf("staged module lacks %s: %v", rel, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dest, "forbidigo-sleep", "rule.json")); statErr == nil {
		t.Error("rule.json is metadata and must not be staged as source")
	}

	mod, err := os.ReadFile(filepath.Join(dest, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "module fixture.test") || !strings.Contains(string(mod), "go 1.27.2") {
		t.Errorf("unexpected go.mod:\n%s", mod)
	}
}

func TestStageRequiresEnforcerConfig(t *testing.T) {
	t.Parallel()

	fsys := repoFS()
	delete(fsys, ".golangci.yml")
	fixtures, err := rules.Discover(fsys)
	if err != nil {
		t.Fatal(err)
	}

	// A fixture run against a missing config would silently use defaults and prove nothing.
	if err = rules.Stage(fsys, fixtures, t.TempDir(), "1.27.2"); err == nil {
		t.Fatal("want an error when .golangci.yml is missing")
	}
}

func TestParseGolangci(t *testing.T) {
	t.Parallel()

	data := []byte(`{"Issues":[
	  {"FromLinter":"forbidigo","Text":"use of ` + "`context.Background`" + ` forbidden","Pos":{"Filename":"forbidigo-ctx\\lib\\lib.go","Line":5}},
	  {"FromLinter":"revive","Text":"package-comments: should have a package comment","Pos":{"Filename":"clean/x.go","Line":1}}
	],"Report":{}}`)

	got, err := rules.ParseGolangci(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
	if got[0].Path != "forbidigo-ctx/lib/lib.go" {
		t.Errorf("path = %q, want forward slashes", got[0].Path)
	}
	if !strings.HasPrefix(got[0].Message, "forbidigo: ") || !strings.Contains(got[0].Message, "context.Background") {
		t.Errorf("message = %q, want the linter name then its text", got[0].Message)
	}
}

func TestParseGolangciEmpty(t *testing.T) {
	t.Parallel()

	got, err := rules.ParseGolangci([]byte(`{"Issues":null,"Report":{}}`))
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want no findings and no error", got, err)
	}
}

func TestParseGolangciRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := rules.ParseGolangci([]byte("not json")); err == nil {
		t.Fatal("want an error for output that is not JSON")
	}
}

func TestParseVet(t *testing.T) {
	t.Parallel()

	out := "# fixture.test/barego-x\n" +
		"barego-x\\x.go:7:2: bare go statement; use the supervised helper\n" +
		".\\clean\\y.go:3:1: something else\n" +
		"exit status 1\n"

	got := rules.ParseVet(out)
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(got), got)
	}
	if got[0].Path != "barego-x/x.go" || !strings.Contains(got[0].Message, "bare go statement") {
		t.Errorf("first finding = %+v", got[0])
	}
	if got[1].Path != "clean/y.go" {
		t.Errorf("leading ./ must be stripped, got %q", got[1].Path)
	}
}

func TestParseArchGo(t *testing.T) {
	t.Parallel()

	data := []byte(`{"compliance":{"details":{"dependenciesRules":{"details":[
	  {"rule":"domain rule","pass":false,"packageDetails":[
	    {"package":"fixture.test/arch-domain/services/a/internal/domain","pass":false,
	     "details":["ShouldNotDependsOn.Internal rule contains imported package 'x/application'"]},
	    {"package":"fixture.test/clean/services/a/internal/domain","pass":true}
	  ]}
	]}}}}`)

	got, err := rules.ParseArchGo(data, "fixture.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d findings, want only the failing package: %+v", len(got), got)
	}
	if got[0].Path != "arch-domain/services/a/internal/domain" {
		t.Errorf("path = %q, want the module prefix stripped", got[0].Path)
	}
	if !strings.Contains(got[0].Message, "application") {
		t.Errorf("message = %q", got[0].Message)
	}
}

func sleepFixture(contains string) rules.Fixture {
	return rules.Fixture{ID: "sleep", Spec: rules.Spec{Enforcer: rules.GolangciLint, Contains: contains}}
}

func golangciReported(list ...rules.Finding) map[rules.Enforcer][]rules.Finding {
	m := make(map[rules.Enforcer][]rules.Finding)
	m[rules.GolangciLint] = list
	return m
}

func TestEvaluatePassesWhenTheRuleFires(t *testing.T) {
	t.Parallel()

	fixtures := []rules.Fixture{sleepFixture("time.Sleep")}
	got := rules.Evaluate(fixtures, golangciReported(
		rules.Finding{Path: "sleep/lib/lib.go", Message: "forbidigo: use of `time.Sleep` forbidden"}))
	if len(got) != 2 {
		t.Fatalf("want the fixture outcome plus the clean-twin outcome, got %+v", got)
	}
	for _, o := range got {
		if !o.Pass {
			t.Errorf("%s should pass: %s", o.ID, o.Detail)
		}
	}
}

func TestEvaluateFailsWhenTheRuleStaysSilent(t *testing.T) {
	t.Parallel()

	fixtures := []rules.Fixture{sleepFixture("time.Sleep")}
	// Another fixture's finding, and a finding from the wrong enforcer, must not count.
	wrongPlace := golangciReported(
		rules.Finding{Path: "other/lib.go", Message: "forbidigo: use of `time.Sleep` forbidden"})
	wrongPlace[rules.ArchGo] = []rules.Finding{{Path: "sleep/x", Message: "time.Sleep"}}

	got := rules.Evaluate(fixtures, wrongPlace)
	if got[0].Pass {
		t.Fatal("a rule that did not fire on its own fixture must fail")
	}
	if !strings.Contains(got[0].Detail, "time.Sleep") {
		t.Errorf("detail should say what was expected: %q", got[0].Detail)
	}
}

func TestEvaluateFailsWhenTheMessageDiffers(t *testing.T) {
	t.Parallel()

	fixtures := []rules.Fixture{sleepFixture("time.Sleep")}
	got := rules.Evaluate(fixtures, golangciReported(
		rules.Finding{Path: "sleep/lib.go", Message: "revive: unrelated complaint"}))
	if got[0].Pass {
		t.Fatal("firing for a different reason is not proof the rule works")
	}
	if !strings.Contains(got[0].Detail, "unrelated complaint") {
		t.Errorf("detail should show what was seen instead: %q", got[0].Detail)
	}
}

func TestEvaluateFailsWhenTheCleanTwinIsFlagged(t *testing.T) {
	t.Parallel()

	fixtures := []rules.Fixture{sleepFixture("time.Sleep")}
	got := rules.Evaluate(fixtures, golangciReported(
		rules.Finding{Path: "sleep/lib.go", Message: "forbidigo: time.Sleep"},
		rules.Finding{Path: "clean/lib/lib.go", Message: "revive: false positive on good code"}))
	clean := got[len(got)-1]
	if clean.ID != rules.CleanID || clean.Pass {
		t.Fatalf("clean twin must fail when any enforcer flags it: %+v", clean)
	}
	if !strings.Contains(clean.Detail, "false positive") {
		t.Errorf("detail should list the findings: %q", clean.Detail)
	}
}

func TestEvaluatePrefixIsExactOnDirectoryBoundary(t *testing.T) {
	t.Parallel()

	fixtures := []rules.Fixture{sleepFixture("x")}
	notMine := golangciReported(rules.Finding{Path: "sleeping/lib.go", Message: "x"})

	if got := rules.Evaluate(fixtures, notMine); got[0].Pass {
		t.Error("fixture sleep must not claim findings under sleeping/")
	}
}
