package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

type procFunc func(spec proc.Spec) (proc.Result, error)

func (f procFunc) Run(_ context.Context, spec proc.Spec) (proc.Result, error) { return f(spec) }

func rulesRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	files := map[string]string{
		".golangci.yml":             "version: \"2\"\n",
		"arch-go.yml":               "version: 1\n",
		"testdata/clean/lib/lib.go": "// Package lib is compliant.\npackage lib\n",
		"testdata/violations/forbidigo-sleep/rule.json":  `{"enforcer":"golangci-lint","contains":"time.Sleep"}`,
		"testdata/violations/forbidigo-sleep/lib/lib.go": "package lib\n",
	}
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func golangciWrites(issues string, exit int) procFunc {
	return func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "go" {
			return proc.Result{}, nil
		}
		err := os.WriteFile(filepath.Join(spec.Dir, "issues.json"), []byte(issues), 0o600)
		return proc.Result{ExitCode: exit}, err
	}
}

const sleepFires = `{"Issues":[{"FromLinter":"forbidigo","Text":"use of time.Sleep forbidden",` +
	`"Pos":{"Filename":"forbidigo-sleep/lib/lib.go"}}]}`

func TestRulesTestPasses(t *testing.T) {
	t.Parallel()

	env := environment{cwd: rulesRepo(t), procRunner: golangciWrites(sleepFires, 1)}
	got := invoke(t, env, "rules", "test")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"PASS", "forbidigo-sleep", "clean", "2 passed, 0 failed"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, got.stdout)
		}
	}
}

func TestRulesTestFailsWhenARuleStaysSilent(t *testing.T) {
	t.Parallel()

	env := environment{cwd: rulesRepo(t), procRunner: golangciWrites(`{"Issues":null}`, 0)}
	got := invoke(t, env, "rules", "test")

	if got.code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", got.code, got.stdout)
	}
	if !strings.Contains(got.stdout, "FAIL") || !strings.Contains(got.stdout, "time.Sleep") {
		t.Errorf("output should name the silent rule and what it expected:\n%s", got.stdout)
	}
}

func TestRulesTestExitsTwoWhenAnEnforcerCannotRun(t *testing.T) {
	t.Parallel()

	env := environment{cwd: rulesRepo(t), procRunner: golangciWrites(`{}`, 3)}
	got := invoke(t, env, "rules", "test")

	if got.code != 2 {
		t.Fatalf("exit = %d, want 2: a crash is not a failed rule", got.code)
	}
	if !strings.Contains(got.stderr, "golangci-lint") {
		t.Errorf("stderr should name the enforcer: %q", got.stderr)
	}
}

func TestRulesNeedsASubcommand(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"rules"}, {"rules", "bogus"}} {
		if got := invoke(t, environment{cwd: rulesRepo(t)}, args...); got.code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, got.code)
		}
	}
}

func TestRulesTestNamesABrokenFixture(t *testing.T) {
	t.Parallel()

	runner := procFunc(func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "go" {
			return proc.Result{ExitCode: 1, Output: "forbidigo-sleep/lib/lib.go:3:1: syntax error"}, nil
		}
		return proc.Result{}, nil
	})
	got := invoke(t, environment{cwd: rulesRepo(t), procRunner: runner}, "rules", "test")

	if got.code != 2 {
		t.Fatalf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "syntax error") || !strings.Contains(got.stderr, "compile") {
		t.Errorf("stderr should show the compiler output and say a fixture must compile: %q", got.stderr)
	}
}
