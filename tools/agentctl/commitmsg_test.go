package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
