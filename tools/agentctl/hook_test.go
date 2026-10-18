package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func payload(t *testing.T, cwd string, toolInput map[string]string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"cwd":             cwd,
		"tool_input":      toolInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func hookEnv(cwd, stdin string, approved bool) environment {
	getenv := func(string) string { return "" }
	if approved {
		getenv = func(key string) string {
			if key == "AGENTCTL_ALLOW_PROTECTED" {
				return "1"
			}
			return ""
		}
	}
	return environment{cwd: cwd, stdin: strings.NewReader(stdin), getenv: getenv}
}

// hookRepo is a git repository whose rule file and guide are already committed, which is what makes them protected.
func hookRepo(t *testing.T) string {
	t.Helper()
	repo := newRepo(t)
	for _, name := range []string{".golangci.yml", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init")
	return repo
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestHookPreEditAllowsAProtectedFileThatIsNotCommittedYet(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	// A gate still being written in this session has not been accepted by anyone yet; only committed rules are
	// protected, so weakening one is blocked while adding or finishing one is not.
	fresh := filepath.Join(repo, "lefthook.yml")
	if err := os.WriteFile(fresh, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := payload(t, repo, map[string]string{"file_path": fresh})

	if got := invoke(t, hookEnv(repo, in, false), "hook", "pre-edit"); got.code != 0 {
		t.Errorf("exit = %d, want 0 for a file that is not in HEAD\n%s", got.code, got.stderr)
	}
}

func TestHookPreEditBlocksACommittedFileThatHasLocalChanges(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	rule := filepath.Join(repo, ".golangci.yml")
	if err := os.WriteFile(rule, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := payload(t, repo, map[string]string{"file_path": rule})

	if got := invoke(t, hookEnv(repo, in, false), "hook", "pre-edit"); got.code != 2 {
		t.Errorf("exit = %d, want 2: staging or editing does not make a committed rule editable", got.code)
	}
}

func TestHookPreEditOutsideAGitRepositoryFailsOpen(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	rule := filepath.Join(repo, ".golangci.yml")
	if err := os.WriteFile(rule, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := payload(t, repo, map[string]string{"file_path": rule})

	if got := invoke(t, hookEnv(repo, in, false), "hook", "pre-edit"); got.code != 0 {
		t.Errorf("exit = %d, want 0: with no git history nothing is committed, so nothing is protected", got.code)
	}
}

func TestHookPreEditBlocksAProtectedFile(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	in := payload(t, repo, map[string]string{"file_path": filepath.Join(repo, ".golangci.yml")})

	got := invoke(t, hookEnv(repo, in, false), "hook", "pre-edit")

	if got.code != 2 {
		t.Fatalf("exit = %d, want 2 (the code that blocks a tool call)\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "BLOCKED") || !strings.Contains(got.stderr, ".golangci.yml") {
		t.Errorf("the agent reads stderr, so it must say what and why: %q", got.stderr)
	}
}

func TestHookPreEditAllowsOrdinaryAndNewFiles(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	for _, target := range []string{
		filepath.Join(repo, "AGENTS.md"),
		filepath.Join(repo, "testdata", "violations", "brand-new", "lib.go"),
	} {
		in := payload(t, repo, map[string]string{"file_path": target})
		if got := invoke(t, hookEnv(repo, in, false), "hook", "pre-edit"); got.code != 0 {
			t.Errorf("%s: exit = %d, want 0\n%s", target, got.code, got.stderr)
		}
	}
}

func TestHookPreEditHonoursTheUsersApproval(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	in := payload(t, repo, map[string]string{"file_path": filepath.Join(repo, ".golangci.yml")})

	if got := invoke(t, hookEnv(repo, in, true), "hook", "pre-edit"); got.code != 0 {
		t.Errorf("exit = %d, want 0 once the user approved", got.code)
	}
}

func TestHookPreEditReadsNotebookPaths(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	in := payload(t, repo, map[string]string{"notebook_path": filepath.Join(repo, ".golangci.yml")})

	if got := invoke(t, hookEnv(repo, in, false), "hook", "pre-edit"); got.code != 2 {
		t.Errorf("exit = %d, want 2: a notebook edit is an edit", got.code)
	}
}

func TestHookPreEditWithNoPathAllows(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	if got := invoke(
		t,
		hookEnv(repo, payload(t, repo, map[string]string{}), false),
		"hook",
		"pre-edit",
	); got.code != 0 {
		t.Errorf("exit = %d, want 0", got.code)
	}
}

func TestHookPreBashBlocksAndAllows(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	tests := []struct {
		command string
		want    int
	}{
		{"git commit --no-verify -m x", 2},
		{"go test ./...", 2},
		{"sed -i s/a/b/ .golangci.yml", 2},
		{"go test -timeout 1m ./...", 0},
		{"go run ./tools/agentctl run -- go test ./...", 0},
		{"git status", 0},
	}
	for _, tc := range tests {
		in := payload(t, repo, map[string]string{"command": tc.command})
		got := invoke(t, hookEnv(repo, in, false), "hook", "pre-bash")
		if got.code != tc.want {
			t.Errorf("%q: exit = %d, want %d\n%s", tc.command, got.code, tc.want, got.stderr)
		}
	}
}

func TestHookApprovalDoesNotLiftTheOtherBashRules(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	for command, want := range map[string]int{"sed -i s/a/b/ .golangci.yml": 0, "git commit --no-verify -m x": 2} {
		in := payload(t, repo, map[string]string{"command": command})
		if got := invoke(t, hookEnv(repo, in, true), "hook", "pre-bash"); got.code != want {
			t.Errorf("%q: exit = %d, want %d", command, got.code, want)
		}
	}
}

func TestHookFailsOpenOnGarbage(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	// Exit 2 would block the tool call, so a broken guard must exit 1, which lets it through with a visible error.
	got := invoke(t, hookEnv(repo, "not json", false), "hook", "pre-edit")
	if got.code != 1 {
		t.Errorf("exit = %d, want 1 (fail open)", got.code)
	}
	if !strings.Contains(got.stderr, "agentctl hook") {
		t.Errorf("the error must be visible: %q", got.stderr)
	}
}

func TestHookUnknownEventFailsOpen(t *testing.T) {
	t.Parallel()

	repo := hookRepo(t)
	for _, args := range [][]string{{"hook"}, {"hook", "post-edit"}} {
		if got := invoke(t, hookEnv(repo, "{}", false), args...); got.code != 1 {
			t.Errorf("%v: exit = %d, want 1, never 2: a typo must not block every tool call", args, got.code)
		}
	}
}
