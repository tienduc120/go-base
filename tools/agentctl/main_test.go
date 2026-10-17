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

type outcome struct {
	code   int
	stdout string
	stderr string
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

func invoke(t *testing.T, env environment, args ...string) outcome {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(t.Context(), args, &out, &errb, env)
	return outcome{code: code, stdout: out.String(), stderr: errb.String()}
}

func TestNoArgsPrintsUsage(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{})
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "doctor") {
		t.Errorf("usage should list doctor, got %q", got.stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{}, "frobnicate")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "frobnicate") {
		t.Errorf("error should name the command, got %q", got.stderr)
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{}, "help")
	if got.code != 0 {
		t.Errorf("exit = %d, want 0", got.code)
	}
	if !strings.Contains(got.stdout, "doctor") {
		t.Errorf("help should list doctor, got %q", got.stdout)
	}
}

func TestDoctorHealthy(t *testing.T) {
	t.Parallel()

	env := environment{runner: runnerFunc(healthy), cwd: newRepo(t)}
	got := invoke(t, env, "doctor")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "golangci-lint") {
		t.Errorf("report should list golangci-lint:\n%s", got.stdout)
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
	got := invoke(t, environment{runner: runner, cwd: newRepo(t)}, "doctor")

	if got.code != 1 {
		t.Errorf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stdout, "missing") || !strings.Contains(got.stdout, "scoop install sqlc") {
		t.Errorf("report should show the missing tool and how to install it:\n%s", got.stdout)
	}
}

func TestDoctorJSON(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{runner: runnerFunc(healthy), cwd: newRepo(t)}, "doctor", "--json")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0", got.code)
	}
	var results []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &results); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, got.stdout)
	}
	if len(results) == 0 || results[0].Name != "go" || results[0].Status != "ok" {
		t.Errorf("unexpected results: %+v", results)
	}
}

func TestDoctorOutsideRepo(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{runner: runnerFunc(healthy), cwd: t.TempDir()}, "doctor")

	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "go.work") {
		t.Errorf("error should explain the missing go.work, got %q", got.stderr)
	}
}

func TestDoctorRejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{runner: runnerFunc(healthy), cwd: newRepo(t)}, "doctor", "--bogus")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestDoctorStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	runner := runnerFunc(func(string, ...string) (string, error) { return "", ctx.Err() })
	var out, errb bytes.Buffer
	code := run(ctx, []string{"doctor"}, &out, &errb, environment{runner: runner, cwd: newRepo(t)})

	if code != 1 {
		t.Errorf("exit = %d, want 1 (every probe cancelled)\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "timed out") && !strings.Contains(out.String(), "cancel") {
		t.Errorf("report should say the probes were cut short:\n%s", out.String())
	}
}
