package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

func capture(spec *proc.Spec, res proc.Result, err error) procFunc {
	return func(s proc.Spec) (proc.Result, error) {
		*spec = s
		return res, err
	}
}

func TestRunSucceedsQuietly(t *testing.T) {
	t.Parallel()

	var seen proc.Spec
	repo := newRepo(t)
	env := environment{cwd: repo, procRunner: capture(&seen, proc.Result{Output: "all good\n"}, nil)}

	got := invoke(t, env, "run", "--", "echo", "hi")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	if !strings.HasPrefix(got.stdout, "ok ") {
		t.Errorf("a success is one short line, got %q", got.stdout)
	}
	if strings.Contains(got.stdout, "all good") {
		t.Errorf("a success must not echo the command's output: %q", got.stdout)
	}
	logs, err := filepath.Glob(filepath.Join(repo, ".agentctl", "logs", "run-*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("want one log under .agentctl/logs, got %v (%v)", logs, err)
	}
	if data, _ := os.ReadFile(logs[0]); !strings.Contains(string(data), "all good") {
		t.Errorf("the log must hold the full output, got %q", data)
	}
	if seen.Name != "echo" || !slices.Equal(seen.Args, []string{"hi"}) {
		t.Errorf("ran %s %v", seen.Name, seen.Args)
	}
}

func TestRunPassesTheCommandsOwnExitCodeAndShowsTheTail(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for range 100 {
		sb.WriteString("noise\n")
	}
	sb.WriteString("the actual failure\n")

	var seen proc.Spec
	env := environment{cwd: newRepo(t), procRunner: capture(&seen, proc.Result{ExitCode: 3, Output: sb.String()}, nil)}
	got := invoke(t, env, "run", "--", "make-it-fail")

	if got.code != 3 {
		t.Errorf("exit = %d, want the command's own 3 so callers can branch on it", got.code)
	}
	if !strings.Contains(got.stdout, "FAIL") || !strings.Contains(got.stdout, "the actual failure") {
		t.Errorf("a failure shows the tail:\n%s", got.stdout)
	}
	if strings.Count(got.stdout, "noise") > 40 {
		t.Errorf("the summary must stay short, got %d noise lines", strings.Count(got.stdout, "noise"))
	}
}

func TestRunReportsATimeoutAs124(t *testing.T) {
	t.Parallel()

	var seen proc.Spec
	res := proc.Result{ExitCode: -1, TimedOut: true, Reason: proc.ReasonIdle, Output: "last words\n"}
	env := environment{cwd: newRepo(t), procRunner: capture(&seen, res, nil)}
	got := invoke(t, env, "run", "--", "hang-forever")

	if got.code != 124 {
		t.Errorf("exit = %d, want 124 (the timeout(1) convention)", got.code)
	}
	for _, want := range []string{"TIMEOUT", "idle", "process tree", "last words"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, got.stdout)
		}
	}
}

func TestRunMissingCommandExits127(t *testing.T) {
	t.Parallel()

	var seen proc.Spec
	env := environment{cwd: newRepo(t), procRunner: capture(&seen, proc.Result{}, exec.ErrNotFound)}
	got := invoke(t, env, "run", "--", "no-such-tool")

	if got.code != 127 {
		t.Errorf("exit = %d, want 127", got.code)
	}
	if !strings.Contains(got.stderr, "no-such-tool") {
		t.Errorf("stderr should name the command: %q", got.stderr)
	}
}

func TestRunNeedsACommand(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"run"}, {"run", "--"}, {"run", "--timeout", "5s"}} {
		if got := invoke(t, environment{cwd: newRepo(t)}, args...); got.code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, got.code)
		}
	}
}

func TestRunAppliesTheLimitsAndInjectsAGoTestTimeout(t *testing.T) {
	t.Parallel()

	var seen proc.Spec
	env := environment{cwd: newRepo(t), procRunner: capture(&seen, proc.Result{}, nil)}
	invoke(t, env, "run", "--timeout", "5m", "--idle", "30s", "--", "go", "test", "./...")

	if seen.Timeout != 5*time.Minute || seen.IdleTimeout != 30*time.Second {
		t.Errorf("limits = %s / %s, want 5m / 30s", seen.Timeout, seen.IdleTimeout)
	}
	if !slices.Equal(seen.Args, []string{"test", "-timeout", "290s", "./..."}) {
		t.Errorf("go test must get its own limit just under the wrapper's, got %v", seen.Args)
	}
}

func TestRunHasSaneDefaults(t *testing.T) {
	t.Parallel()

	var seen proc.Spec
	env := environment{cwd: newRepo(t), procRunner: capture(&seen, proc.Result{}, nil)}
	invoke(t, env, "run", "--", "something")

	if seen.Timeout <= 0 || seen.IdleTimeout <= 0 {
		t.Errorf("an unbounded run is exactly what this command prevents: %s / %s", seen.Timeout, seen.IdleTimeout)
	}
}

func TestRunNamesTheLogAndWorksOutsideARepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var seen proc.Spec
	env := environment{cwd: dir, procRunner: capture(&seen, proc.Result{Output: "x\n"}, nil)}
	got := invoke(t, env, "run", "--name", "mycheck", "--", "tool")

	if got.code != 0 {
		t.Fatalf("exit = %d\n%s", got.code, got.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agentctl", "logs", "run-mycheck.log")); err != nil {
		t.Errorf("want the named log beside the working directory when there is no repo: %v", err)
	}
}
