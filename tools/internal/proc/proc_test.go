package proc_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
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
	case "once":
		_, _ = os.Stdout.WriteString("started\n")
		<-time.After(time.Minute)
		os.Exit(0)
	case "chatty":
		for range 15 {
			_, _ = os.Stdout.WriteString("still working\n")
			<-time.After(200 * time.Millisecond)
		}
		os.Exit(0)
	case "spawn":
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		child.Env = append(os.Environ(), helperEnv+"=hang")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		_, _ = fmt.Fprintf(os.Stdout, "grandchild-pid=%d\n", child.Process.Pid)
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
	if !res.TimedOut || res.Reason != proc.ReasonDeadline {
		t.Errorf("timedOut = %v, reason = %q, want the deadline", res.TimedOut, res.Reason)
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
	if !res.TimedOut || res.Reason != proc.ReasonCancelled {
		t.Errorf("timedOut = %v, reason = %q, want a cancelled run", res.TimedOut, res.Reason)
	}
}

func TestRunMissingExecutable(t *testing.T) {
	t.Parallel()

	_, err := proc.Exec{}.Run(t.Context(), proc.Spec{Name: "definitely-not-a-real-binary-xyz", Timeout: time.Second})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("err = %v, want exec.ErrNotFound", err)
	}
}

func TestRunKillsTheWholeProcessTree(t *testing.T) {
	t.Parallel()

	res, err := proc.Exec{}.Run(t.Context(), helper("spawn", 2*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	m := regexp.MustCompile(`grandchild-pid=(\d+)`).FindStringSubmatch(res.Output)
	if m == nil {
		t.Fatalf("the helper did not report its grandchild: %q", res.Output)
	}
	pid, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	// A surviving grandchild would outlive the test run, so clean up even when the assertion fails.
	t.Cleanup(func() { killPID(pid) })

	deadline := time.Now().Add(10 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d survived the timeout: only the direct child was killed", pid)
		}
		<-time.After(100 * time.Millisecond)
	}
}

func TestRunKillsAProcessThatWentSilent(t *testing.T) {
	t.Parallel()

	spec := helper("once", time.Minute)
	spec.IdleTimeout = 500 * time.Millisecond

	start := time.Now()
	res, err := proc.Exec{}.Run(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.Reason != proc.ReasonIdle {
		t.Errorf("timedOut = %v, reason = %q, want the idle limit", res.TimedOut, res.Reason)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("took %s: the idle limit did not stop the process well before the deadline", elapsed)
	}
}

func TestRunLeavesAChattyProcessAlone(t *testing.T) {
	t.Parallel()

	// The helper runs about 3s in total with 200ms between lines; the limit is well above the gaps but below the
	// total, so only the timer reset on activity lets it finish. The slack absorbs a slow start under load.
	spec := helper("chatty", time.Minute)
	spec.IdleTimeout = 2 * time.Second

	res, err := proc.Exec{}.Run(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if res.TimedOut || res.ExitCode != 0 {
		t.Errorf("a process that keeps printing is working, not stuck: %+v", res)
	}
}
