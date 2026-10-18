package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFmtMdUnwrapsAndListsWhatChanged(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	wrapped := filepath.Join(repo, "docs", "a.md")
	if err := os.MkdirAll(filepath.Dir(wrapped), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrapped, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := invoke(t, environment{cwd: repo}, "fmt", "md")

	if got.code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "a.md") {
		t.Errorf("should name the rewritten file: %q", got.stdout)
	}
	if data, _ := os.ReadFile(wrapped); string(data) != "one two\n" {
		t.Errorf("a.md = %q", data)
	}
}

func TestFmtMdWithNothingToDo(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{cwd: newRepo(t)}, "fmt", "md")
	if got.code != 0 || !strings.Contains(got.stdout, "nothing to change") {
		t.Errorf("exit = %d, stdout = %q", got.code, got.stdout)
	}
}

func TestFmtNeedsAKnownTarget(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"fmt"}, {"fmt", "go"}} {
		if got := invoke(t, environment{cwd: newRepo(t)}, args...); got.code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, got.code)
		}
	}
}

func TestFmtMdOutsideRepo(t *testing.T) {
	t.Parallel()

	if got := invoke(t, environment{cwd: t.TempDir()}, "fmt", "md"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestFmtMdAcceptsExplicitFiles(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	// .local is skipped by default; naming the file is how you opt in.
	scratch := filepath.Join(repo, ".local", "plan.md")
	if err := os.MkdirAll(filepath.Dir(scratch), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratch, []byte("wrapped\ntext\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := invoke(t, environment{cwd: repo}, "fmt", "md", scratch); got.code != 0 {
		t.Fatalf("exit = %d\n%s", got.code, got.stderr)
	}
	if data, _ := os.ReadFile(scratch); string(data) != "wrapped text\n" {
		t.Errorf("plan.md = %q", data)
	}
}
