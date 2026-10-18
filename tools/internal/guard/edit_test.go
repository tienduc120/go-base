package guard_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/guard"
)

func exists(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[filepath.Clean(p)] = true
	}
	return func(p string) bool { return set[filepath.Clean(p)] }
}

func TestCheckEditBlocksExistingProtectedFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	protected := []string{
		".golangci.yml",
		"arch-go.yml",
		"lefthook.yml",
		".github/workflows/ci.yml",
		".claude/settings.json",
		"LICENSE",
		"LICENSES/maratori-golangci-lint-config.txt",
		"testdata/violations/forbidigo-time-sleep/lib/lib.go",
		"testdata/clean/platform/clock/clock.go",
		"tools/analyzers/barego/barego.go",
		"tools/analyzers/barego/testdata/src/bare/bare.go",
		"tools/agentlint/main.go",
		"tools/internal/rules/rules.go",
		"tools/internal/verify/verify.go",
		"tools/internal/dbcheck/rules.go",
		"tools/internal/dbcheck/testdata/violations/seq-scan/queries.sql",
		"tools/internal/commitmsg/commitmsg.go",
		"tools/internal/guard/edit.go",
	}
	for _, rel := range protected {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			abs := filepath.Join(root, filepath.FromSlash(rel))
			got := guard.CheckEdit(root, root, abs, exists(abs))
			if !got.Block {
				t.Fatalf("%s must be protected", rel)
			}
			if !strings.Contains(got.Reason, "AGENTCTL_ALLOW_PROTECTED") {
				t.Errorf("the reason must say how the user approves a change: %q", got.Reason)
			}
		})
	}
}

func TestCheckEditAllowsCreatingNewFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	for _, rel := range []string{
		"testdata/violations/new-rule/lib/lib.go",
		"tools/analyzers/newcheck/newcheck.go",
		".claude/settings.json",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if got := guard.CheckEdit(root, root, abs, exists()); got.Block {
			t.Errorf("creating %s adds to the rules, it does not weaken them: %q", rel, got.Reason)
		}
	}
}

func TestCheckEditAllowsOrdinaryFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	for _, rel := range []string{
		"AGENTS.md",
		"README.md",
		"tools/agentctl/run.go",
		"tools/internal/proc/proc.go",
		"services/billing/internal/domain/invoice.go",
		"docs/adr/0005-new.md",
	} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if got := guard.CheckEdit(root, root, abs, exists(abs)); got.Block {
			t.Errorf("%s is ordinary code: %q", rel, got.Reason)
		}
	}
}

func TestCheckEditResolvesRelativePathsAgainstTheWorkingDirectory(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	tools := filepath.Join(root, "tools")
	abs := filepath.Join(root, "tools", "internal", "guard", "edit.go")

	got := guard.CheckEdit(root, tools, filepath.Join("internal", "guard", "edit.go"), exists(abs))
	if !got.Block {
		t.Error("a relative path from a subdirectory reaches the same protected file")
	}
}

func TestCheckEditIgnoresFilesOutsideTheRepository(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "elsewhere", ".golangci.yml")

	if got := guard.CheckEdit(root, root, outside, exists(outside)); got.Block {
		t.Errorf("a file outside the repository is not ours to protect: %q", got.Reason)
	}
}

func TestCheckEditDoesNotBlockLookalikes(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	for _, rel := range []string{"LICENSE-notes.md", "docs/golangci.yml", "tools/internal/guardian/x.go", "mytestdata/x.go"} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if got := guard.CheckEdit(root, root, abs, exists(abs)); got.Block {
			t.Errorf("%s only looks like a protected path: %q", rel, got.Reason)
		}
	}
}
