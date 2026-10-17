package proc_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

const helperEnv = "PROC_TEST_HELPER"

// TestHelperProcess is re-executed as a child by the tests below; it is not a test of its own.
//
//nolint:paralleltest // runs as a child process and exits, there is nothing to parallelise
func TestHelperProcess(_ *testing.T) {
	switch os.Getenv(helperEnv) {
	case "":
		return
	case "exit3":
		os.Exit(3)
	case "echo":
		_, _ = os.Stdout.WriteString("hello from child")
		os.Exit(0)
	case "hang":
		<-time.After(time.Minute)
		os.Exit(0)
	}
}

func helper(mode string, timeout time.Duration) proc.Spec {
	return proc.Spec{
		Name:    os.Args[0],
		Args:    []string{"-test.run=^TestHelperProcess$"},
		Env:     []string{helperEnv + "=" + mode},
		Timeout: timeout,
	}
}

func TestRunCapturesOutputAndZeroExit(t *testing.T) {
	t.Parallel()

	res, err := proc.Exec{}.Run(t.Context(), helper("echo", 30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.TimedOut {
		t.Errorf("exit = %d, timedOut = %v, want 0 and false", res.ExitCode, res.TimedOut)
	}
	if res.Output != "hello from child" {
		t.Errorf("output = %q", res.Output)
	}
}

func TestRunReportsNonZeroExitWithoutError(t *testing.T) {
	t.Parallel()

	res, err := proc.Exec{}.Run(t.Context(), helper("exit3", 30*time.Second))
	if err != nil {
		t.Fatalf("a non-zero exit is a result, not an error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("exit = %d, want 3", res.ExitCode)
	}
}

func TestRunKillsAHungChildAtTheDeadline(t *testing.T) {
	t.Parallel()

	start := time.Now()
	res, err := proc.Exec{}.Run(t.Context(), helper("hang", 300*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Error("want TimedOut")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("took %s: the timeout did not stop the child", elapsed)
	}
}

func TestRunStopsWhenTheCallerCancels(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	res, err := proc.Exec{}.Run(ctx, helper("hang", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Error("a cancelled run must be reported as cut short")
	}
}

func TestRunMissingExecutable(t *testing.T) {
	t.Parallel()

	_, err := proc.Exec{}.Run(t.Context(), proc.Spec{Name: "definitely-not-a-real-binary-xyz", Timeout: time.Second})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("err = %v, want exec.ErrNotFound", err)
	}
}
