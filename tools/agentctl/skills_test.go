package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoWithSkill(t *testing.T) string {
	t.Helper()
	repo := newRepo(t)
	skill := filepath.Join(repo, ".agents", "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestSkillsLinkThenCheck(t *testing.T) {
	t.Parallel()

	repo := repoWithSkill(t)
	env := environment{cwd: repo}

	if got := invoke(
		t,
		env,
		"skills",
		"check",
	); got.code != 1 ||
		!strings.Contains(got.stderr, "agentctl skills link") {
		t.Errorf("before linking: exit = %d, stderr = %q, want a failure that names the fix", got.code, got.stderr)
	}
	if got := invoke(t, env, "skills", "link"); got.code != 0 {
		t.Fatalf("link: exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	if got := invoke(t, env, "skills", "check"); got.code != 0 || !strings.Contains(got.stdout, "ok") {
		t.Errorf("after linking: exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("the skill should be reachable where Claude Code looks: %v", err)
	}
}

func TestSkillsLinkRefusesARealDirectory(t *testing.T) {
	t.Parallel()

	repo := repoWithSkill(t)
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "skills", "old"), 0o750); err != nil {
		t.Fatal(err)
	}

	got := invoke(t, environment{cwd: repo}, "skills", "link")

	if got.code != 1 || !strings.Contains(got.stderr, "real directory") {
		t.Errorf("exit = %d, stderr = %q", got.code, got.stderr)
	}
}

func TestSkillsUsageErrors(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{"none": {"skills"}, "unknown": {"skills", "sync"}} {
		if got := invoke(t, environment{cwd: newRepo(t)}, args...); got.code != 2 {
			t.Errorf("%s: exit = %d, want 2", name, got.code)
		}
	}
	if got := invoke(t, environment{cwd: t.TempDir()}, "skills", "check"); got.code != 2 {
		t.Errorf("outside a repository: exit = %d, want 2", got.code)
	}
}
