package guard_test

import (
	"path/filepath"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/guard"
)

// CODEOWNERS names who must approve a change to every other protected file, so it is protected itself.
func TestCheckEditProtectsCodeowners(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	abs := filepath.Join(root, ".github", "CODEOWNERS")

	got := guard.CheckEdit(root, root, abs, exists(abs))
	if !got.Block {
		t.Fatal("an agent must not be able to edit the file that decides who reviews its changes")
	}
}

func TestCheckEditLeavesTheRestOfDotGithubAlone(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repo")
	for _, rel := range []string{".github/PULL_REQUEST_TEMPLATE.md", ".github/ISSUE_TEMPLATE/bug.md", ".github/dependabot.yml"} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if got := guard.CheckEdit(root, root, abs, exists(abs)); got.Block {
			t.Errorf("%s is not a rule that judges changes: %q", rel, got.Reason)
		}
	}
}
