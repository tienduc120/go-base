package adr_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tienduc120/go-base/tools/internal/adr"
)

const body = `
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

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// good lays out two valid records and an index that lists them.
func good(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "0001-first.md", "# 0001 - First one\n"+body)
	write(t, dir, "0002-second.md", "# 0002 - Second\n"+body)
	write(t, dir, "README.md", "# Architecture decisions\n\n| # | Title | Status |\n| --- | --- | --- |\n"+
		"| [0001](0001-first.md) | First one | accepted |\n| [0002](0002-second.md) | Second | accepted |\n\nNeeds an ADR first: a thing.\n")
	return dir
}

func problems(t *testing.T, dir string) string {
	t.Helper()
	found, err := adr.Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(found, "\n")
}

func TestCheckPassesAWellFormedSet(t *testing.T) {
	t.Parallel()

	if got := problems(t, good(t)); got != "" {
		t.Errorf("problems:\n%s", got)
	}
}

func TestCheckCatchesEachKindOfDefect(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		edit func(t *testing.T, dir string)
		want string
	}{
		"a gap in the numbers": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "0004-skips-three.md", "# 0004 - Skips three\n"+body)
		}, "0003"},
		"a duplicate number": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "0002-again.md", "# 0002 - Again\n"+body)
		}, "0002"},
		"a heading that disagrees with the file": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "0002-second.md", "# 0009 - Second\n"+body)
		}, "heading"},
		"a bad status": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "0002-second.md", "# 0002 - Second\n"+strings.Replace(body, "accepted", "maybe", 1))
		}, "Status"},
		"a missing date": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "0002-second.md", "# 0002 - Second\n"+strings.Replace(body, "- **Date**: 2026-10-18\n", "", 1))
		}, "Date"},
		"an impossible date": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "0002-second.md", "# 0002 - Second\n"+strings.Replace(body, "2026-10-18", "2026-13-40", 1))
		}, "Date"},
		"a missing section": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "0002-second.md", "# 0002 - Second\n"+strings.Replace(body, "## Alternatives considered", "## Other", 1))
		}, "Alternatives considered"},
		"a badly named file": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "notes.md", "# notes\n")
		}, "notes.md"},
		"a record missing from the index": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "0003-third.md", "# 0003 - Third\n"+body)
		}, "index"},
		"an index row with the wrong title": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "README.md", strings.Replace(read(t, dir, "README.md"), "| Second |", "| Something else |", 1))
		}, "title"},
		"an index row with the wrong status": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "README.md", strings.Replace(read(t, dir, "README.md"), "Second | accepted", "Second | proposed", 1))
		}, "status"},
		"an index row for a record that does not exist": {func(t *testing.T, dir string) {
			t.Helper()
			write(t, dir, "README.md", strings.Replace(read(t, dir, "README.md"), "\nNeeds", "| [0007](0007-ghost.md) | Ghost | accepted |\n\nNeeds", 1))
		}, "0007"},
	} {
		dir := good(t)
		tc.edit(t, dir)

		got := problems(t, dir)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: problems = %q, want one that mentions %q", name, got, tc.want)
		}
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCheckAcceptsASupersededStatusThatNamesAnExistingRecord(t *testing.T) {
	t.Parallel()

	dir := good(t)
	write(t, dir, "0001-first.md", "# 0001 - First one\n"+strings.Replace(body, "accepted", "superseded by 0002", 1))
	write(
		t,
		dir,
		"README.md",
		strings.Replace(read(t, dir, "README.md"), "First one | accepted", "First one | superseded by 0002", 1),
	)
	if got := problems(t, dir); got != "" {
		t.Errorf("problems:\n%s", got)
	}

	write(t, dir, "0001-first.md", "# 0001 - First one\n"+strings.Replace(body, "accepted", "superseded by 0099", 1))
	if got := problems(t, dir); !strings.Contains(got, "0099") {
		t.Errorf("a record superseded by one that does not exist is a dead end: %q", got)
	}
}

func TestCheckRefusesAnAcceptedRecordThatStillHoldsTemplateText(t *testing.T) {
	t.Parallel()

	dir := good(t)
	path, err := adr.New(dir, "Third thing", time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := problems(t, dir); got != "" {
		t.Fatalf("a freshly made record is a proposal and should pass: %s", got)
	}

	data, _ := os.ReadFile(path)
	if err = os.WriteFile(path, []byte(strings.Replace(string(data), "proposed", "accepted", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte(strings.Replace(read(t, dir, "README.md"), "| proposed |", "| accepted |", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := problems(t, dir); !strings.Contains(got, "template") {
		t.Errorf("accepting a record nobody filled in must be refused: %q", got)
	}
}

func TestNewNumbersTheNextRecordAndIndexesIt(t *testing.T) {
	t.Parallel()

	dir := good(t)
	path, err := adr.New(dir, "Use a queue, not a thread!", time.Date(2026, time.November, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(path) != "0003-use-a-queue-not-a-thread.md" {
		t.Errorf("file = %s, want the next number and a slug", filepath.Base(path))
	}
	text := read(t, dir, "0003-use-a-queue-not-a-thread.md")
	for _, want := range []string{
		"# 0003 - Use a queue, not a thread!", "- **Status**: proposed", "- **Date**: 2026-11-03",
		"## Context", "## Decision", "## Consequences", "## Alternatives considered",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("record lacks %q:\n%s", want, text)
		}
	}
	index := read(t, dir, "README.md")
	if !strings.Contains(
		index,
		"| [0003](0003-use-a-queue-not-a-thread.md) | Use a queue, not a thread! | proposed |",
	) {
		t.Errorf("index:\n%s", index)
	}
	if strings.Index(index, "0003") > strings.Index(index, "Needs an ADR first") {
		t.Error("the new row belongs inside the table, before the text that follows it")
	}
}

func TestNewRefusesAnEmptyTitleAndANumberItWouldReuse(t *testing.T) {
	t.Parallel()

	dir := good(t)
	if _, err := adr.New(dir, "   ", time.Now()); err == nil {
		t.Error("a record needs a title")
	}
	if _, err := adr.New(dir, "!!!", time.Now()); err == nil {
		t.Error("a title with no letters or digits has no slug")
	}
}
