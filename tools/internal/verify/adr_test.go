package verify_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/verify"
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

func adrRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "adr")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestADRStepPassesAWellFormedSet(t *testing.T) {
	t.Parallel()

	root := adrRoot(t, map[string]string{
		"0001-first.md": "# 0001 - First\n" + adrBody,
		"README.md":     "# Decisions\n\n| # | Title | Status |\n| --- | --- | --- |\n| [0001](0001-first.md) | First | accepted |\n",
	})

	step := verify.ADRStep(verify.Env{Root: root})
	out := step.Run(t.Context())

	if step.Name != "adr check" || !out.Pass {
		t.Errorf("name = %q, outcome = %+v", step.Name, out)
	}
}

func TestADRStepFailsWithEveryProblem(t *testing.T) {
	t.Parallel()

	root := adrRoot(t, map[string]string{
		"0001-first.md": "# 0001 - First\n" + adrBody,
		"0003-skips.md": "# 0003 - Skips\n" + adrBody,
		"README.md":     "# Decisions\n\n| # | Title | Status |\n| --- | --- | --- |\n| [0001](0001-first.md) | First | accepted |\n",
	})

	out := verify.ADRStep(verify.Env{Root: root}).Run(t.Context())

	if out.Pass || !strings.Contains(out.Detail, "0002") || !strings.Contains(out.Detail, "0003") {
		t.Errorf("outcome = %+v, want the gap and the unindexed record named", out)
	}
}

func TestADRStepIsSkippedWhereThereAreNoRecords(t *testing.T) {
	t.Parallel()

	out := verify.ADRStep(verify.Env{Root: t.TempDir()}).Run(t.Context())

	if !out.Skipped || out.Detail == "" {
		t.Errorf("outcome = %+v, want a skip that says why: a repository without docs/adr has nothing to check", out)
	}
}
