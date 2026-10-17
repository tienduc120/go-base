package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type runnerFunc func(name string, args ...string) (string, error)

func (f runnerFunc) Run(_ context.Context, name string, args ...string) (string, error) {
	return f(name, args...)
}

// healthy answers every probe with a version far above any minimum.
func healthy(string, ...string) (string, error) {
	return "govulncheck@v99.99.99 99.99.99", nil
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.work": "go 1.27.2\n",
		"go.mod":  "module example.com/x\n\ngo 1.27.2\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func invoke(t *testing.T, env environment, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb, env)
	return code, out.String(), errb.String()
}

func TestNoArgsPrintsUsage(t *testing.T) {
	t.Parallel()

	code, _, stderr := invoke(t, environment{})
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "doctor") {
		t.Errorf("usage should list doctor, got %q", stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()

	code, _, stderr := invoke(t, environment{}, "frobnicate")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "frobnicate") {
		t.Errorf("error should name the command, got %q", stderr)
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()

	code, stdout, _ := invoke(t, environment{}, "help")
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "doctor") {
		t.Errorf("help should list doctor, got %q", stdout)
	}
}

func TestDoctorHealthy(t *testing.T) {
	t.Parallel()

	env := environment{runner: runnerFunc(healthy), cwd: newRepo(t)}
	code, stdout, stderr := invoke(t, env, "doctor")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "golangci-lint") {
		t.Errorf("report should list golangci-lint:\n%s", stdout)
	}
}

func TestDoctorFailsWhenRequiredToolMissing(t *testing.T) {
	t.Parallel()

	runner := runnerFunc(func(name string, args ...string) (string, error) {
		if name == "sqlc" {
			return "", exec.ErrNotFound
		}
		return healthy(name, args...)
	})
	env := environment{runner: runner, cwd: newRepo(t)}
	code, stdout, _ := invoke(t, env, "doctor")

	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stdout, "missing") || !strings.Contains(stdout, "scoop install sqlc") {
		t.Errorf("report should show the missing tool and how to install it:\n%s", stdout)
	}
}

func TestDoctorIgnoresMissingOptionalTool(t *testing.T) {
	t.Parallel()

	runner := runnerFunc(func(name string, args ...string) (string, error) {
		if name == "arch-go" {
			return "", exec.ErrNotFound
		}
		return healthy(name, args...)
	})
	env := environment{runner: runner, cwd: newRepo(t)}

	if code, stdout, _ := invoke(t, env, "doctor"); code != 0 {
		t.Errorf("exit = %d, want 0 for an optional tool\n%s", code, stdout)
	}
}

func TestDoctorJSON(t *testing.T) {
	t.Parallel()

	env := environment{runner: runnerFunc(healthy), cwd: newRepo(t)}
	code, stdout, _ := invoke(t, env, "doctor", "--json")

	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var results []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(results) == 0 || results[0].Name != "go" || results[0].Status != "ok" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestDoctorOutsideRepo(t *testing.T) {
	t.Parallel()

	env := environment{runner: runnerFunc(healthy), cwd: t.TempDir()}
	code, _, stderr := invoke(t, env, "doctor")

	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "go.work") {
		t.Errorf("error should explain the missing go.work, got %q", stderr)
	}
}

func TestDoctorRejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	env := environment{runner: runnerFunc(healthy), cwd: newRepo(t)}
	if code, _, _ := invoke(t, env, "doctor", "--bogus"); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}
