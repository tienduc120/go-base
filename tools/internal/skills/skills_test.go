package skills_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/skills"
)

// repo makes a repository root with one skill in the source directory.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := filepath.Join(root, ".agents", "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("---\nname: demo\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLinkMakesTheSkillsVisibleWhereClaudeCodeLooks(t *testing.T) {
	t.Parallel()

	root := repo(t)
	if err := skills.Link(t.Context(), root); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "demo", "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "name: demo") {
		t.Fatalf("the skill is not reachable through .claude/skills: %v", err)
	}
	if err = skills.Check(root); err != nil {
		t.Errorf("Check after Link: %v", err)
	}
}

func TestLinkIsIdempotent(t *testing.T) {
	t.Parallel()

	root := repo(t)
	for range 2 {
		if err := skills.Link(t.Context(), root); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLinkSeesASkillAddedAfterwards(t *testing.T) {
	t.Parallel()

	root := repo(t)
	if err := skills.Link(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(root, ".agents", "skills", "later", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(added), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(added, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "later", "SKILL.md")); err != nil {
		t.Errorf("a link is one directory, so a new skill needs no second step: %v", err)
	}
}

func TestLinkRefusesToReplaceARealDirectory(t *testing.T) {
	t.Parallel()

	root := repo(t)
	old := filepath.Join(root, ".claude", "skills", "stale", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(old), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := skills.Link(t.Context(), root)

	if err == nil || !strings.Contains(err.Error(), ".claude/skills") {
		t.Errorf("err = %v, want a refusal that names the directory: it may hold skills that exist nowhere else", err)
	}
	if _, statErr := os.Stat(old); statErr != nil {
		t.Error("nothing may be deleted to make room for the link")
	}
}

func TestLinkNeedsTheSourceDirectory(t *testing.T) {
	t.Parallel()

	if err := skills.Link(t.Context(), t.TempDir()); err == nil {
		t.Error("with no .agents/skills there is nothing to link to")
	}
}

func TestCheckSaysWhatIsWrongAndHowToFixIt(t *testing.T) {
	t.Parallel()

	root := repo(t)
	err := skills.Check(root)

	if err == nil || !strings.Contains(err.Error(), "agentctl skills link") {
		t.Errorf("err = %v, want the command that fixes it", err)
	}
}

func TestCheckRejectsALinkToTheWrongPlace(t *testing.T) {
	t.Parallel()

	root := repo(t)
	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o750); err != nil {
		t.Fatal(err)
	}
	// A real directory standing where the link should be is the wrong shape too.
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := skills.Check(root); err == nil {
		t.Error("a directory that merely exists at .claude/skills is not the link: it would drift from .agents/skills")
	}
}
