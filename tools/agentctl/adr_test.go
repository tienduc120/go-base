package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const adrBody = `
- **Status**: accepted
- **Date**: 2026-10-18

## Context

Why.

## Decision

1. What.

## Consequences

- Positive: this.

## Alternatives considered

- **Other**: no.
`

func repoWithADRs(t *testing.T) string {
	t.Helper()
	repo := newRepo(t)
	dir := filepath.Join(repo, "docs", "adr")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"0001-first.md": "# 0001 - First\n" + adrBody,
		"README.md": "# Decisions\n\n| # | Title | Status |\n| --- | --- | --- |\n" +
			"| [0001](0001-first.md) | First | accepted |\n\nNeeds an ADR first: a thing.\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func TestAdrCheckPassesAGoodSet(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{cwd: repoWithADRs(t)}, "adr", "check")

	if got.code != 0 || !strings.Contains(got.stdout, "adr: ok") {
		t.Errorf("exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
}

func TestAdrCheckListsEveryProblem(t *testing.T) {
	t.Parallel()

	repo := repoWithADRs(t)
	if err := os.WriteFile(
		filepath.Join(repo, "docs", "adr", "0003-skips.md"),
		[]byte("# 0003 - Skips\n"+adrBody),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	got := invoke(t, environment{cwd: repo}, "adr", "check")

	if got.code != 1 || !strings.Contains(got.stdout, "0002") || !strings.Contains(got.stdout, "0003") {
		t.Errorf("exit = %d, stdout = %q, want the gap and the unindexed record", got.code, got.stdout)
	}
}

func TestAdrNewWritesAProposalAndIndexesIt(t *testing.T) {
	t.Parallel()

	repo := repoWithADRs(t)
	at := time.Date(2026, time.November, 3, 9, 0, 0, 0, time.UTC)

	got := invoke(t, environment{cwd: repo, now: func() time.Time { return at }}, "adr", "new", "Use", "a", "queue")

	if got.code != 0 || !strings.Contains(got.stdout, "0002-use-a-queue.md") {
		t.Fatalf("exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	data, err := os.ReadFile(filepath.Join(repo, "docs", "adr", "0002-use-a-queue.md"))
	if err != nil || !strings.Contains(string(data), "- **Date**: 2026-11-03") {
		t.Errorf("record: %v\n%s", err, data)
	}
	if again := invoke(t, environment{cwd: repo}, "adr", "check"); again.code != 0 {
		t.Errorf("a new proposal must pass the check at once: %s", again.stdout)
	}
}

func TestAdrUsageErrors(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"none": {"adr"}, "unknown": {"adr", "list"}, "new without a title": {"adr", "new"},
	} {
		if got := invoke(t, environment{cwd: repoWithADRs(t)}, args...); got.code != 2 {
			t.Errorf("%s: exit = %d, want 2", name, got.code)
		}
	}
	if got := invoke(t, environment{cwd: t.TempDir()}, "adr", "check"); got.code != 2 {
		t.Errorf("outside a repository: exit = %d, want 2", got.code)
	}
}
