package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

func messageFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "COMMIT_EDITMSG")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCommitmsgAcceptsAGoodMessage(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{}, "commitmsg", messageFile(t, "feat(tools): add the thing\n"))
	if got.code != 0 {
		t.Errorf("exit = %d, want 0\n%s", got.code, got.stderr)
	}
}

func TestCommitmsgRejectsABadMessageAndListsEveryProblem(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{}, "commitmsg", messageFile(t, "Add the thing.\n"))
	if got.code != 1 {
		t.Fatalf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stderr, "type(scope): subject") {
		t.Errorf("stderr should explain the expected format: %q", got.stderr)
	}
}

func TestCommitmsgNeedsAFile(t *testing.T) {
	t.Parallel()

	if got := invoke(t, environment{}, "commitmsg"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if got := invoke(t, environment{}, "commitmsg", filepath.Join(t.TempDir(), "missing")); got.code != 2 {
		t.Errorf("exit = %d, want 2 for an unreadable file", got.code)
	}
}

const editedAssertionDiff = "diff --git a/pkg/a_test.go b/pkg/a_test.go\n--- a/pkg/a_test.go\n+++ b/pkg/a_test.go\n@@ -12 +12 @@\n-\tif got != 3 {\n+\tif got != 4 {\n"

const addedTestDiff = "diff --git a/pkg/a_test.go b/pkg/a_test.go\n--- a/pkg/a_test.go\n+++ b/pkg/a_test.go\n@@ -12,0 +13,3 @@\n+func TestPinned(t *testing.T) {}\n"

func TestCommitmsgRefusesARefactorThatEditsAnExistingTest(t *testing.T) {
	t.Parallel()

	runner := procFunc(func(proc.Spec) (proc.Result, error) { return proc.Result{Output: editedAssertionDiff}, nil })
	file := messageFile(t, "refactor(tools): split the long function\n")

	got := invoke(t, environment{procRunner: runner, cwd: t.TempDir()}, "commitmsg", file)

	if got.code != 1 {
		t.Fatalf("exit = %d, want 1: a refactor must not change what the tests expect\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "pkg/a_test.go") || !strings.Contains(got.stderr, "feat") {
		t.Errorf("stderr should name the file and say what to do instead: %q", got.stderr)
	}
}

func TestCommitmsgAllowsARefactorThatOnlyAddsTests(t *testing.T) {
	t.Parallel()

	runner := procFunc(func(proc.Spec) (proc.Result, error) { return proc.Result{Output: addedTestDiff}, nil })
	file := messageFile(t, "refactor(tools): split the long function\n")

	if got := invoke(t, environment{procRunner: runner, cwd: t.TempDir()}, "commitmsg", file); got.code != 0 {
		t.Errorf("exit = %d, want 0\n%s", got.code, got.stderr)
	}
}

func TestCommitmsgLooksAtTheDiffOnlyForRefactors(t *testing.T) {
	t.Parallel()

	called := false
	runner := procFunc(func(proc.Spec) (proc.Result, error) {
		called = true
		return proc.Result{Output: editedAssertionDiff}, nil
	})
	file := messageFile(t, "fix(tools): correct the expectation\n")

	if got := invoke(t, environment{procRunner: runner, cwd: t.TempDir()}, "commitmsg", file); got.code != 0 {
		t.Errorf("a fix may change a test: exit = %d\n%s", got.code, got.stderr)
	}
	if called {
		t.Error("git must not be consulted for a message that is not a refactor")
	}
}

func TestCommitmsgDoesNotBlockACommitWhenGitCannotBeAsked(t *testing.T) {
	t.Parallel()

	runner := procFunc(
		func(proc.Spec) (proc.Result, error) { return proc.Result{}, errors.New("git is not installed") },
	)
	file := messageFile(t, "refactor(tools): split the long function\n")

	got := invoke(t, environment{procRunner: runner, cwd: t.TempDir()}, "commitmsg", file)
	if got.code != 0 || !strings.Contains(got.stderr, "could not check") {
		t.Errorf("a tooling failure warns and lets the commit through: exit = %d, stderr = %q", got.code, got.stderr)
	}
}
