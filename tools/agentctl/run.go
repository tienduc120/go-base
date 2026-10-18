package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/runcmd"
)

const (
	defaultRunTimeout = 10 * time.Minute
	defaultRunIdle    = 2 * time.Minute
	defaultRunTail    = 25

	// Exit codes follow the shell conventions callers already know.
	exitRunTimeout  = 124
	exitRunNotFound = 127

	roundRun = 100 * time.Millisecond
)

const runUsage = `usage: agentctl run [--timeout 10m] [--idle 2m] [--name slug] [--tail 25] -- <command> [args...]

Runs a command under a hard deadline and an idle limit, kills its whole process tree if either is hit, keeps the full
output in .agentctl/logs, and prints one line (plus the tail on failure). Exit code: the command's own, 124 on a
timeout, 127 when the command does not exist. "go test" gets its own -timeout just under the deadline so a hang
produces a goroutine dump.
`

func runRun(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	timeout := fs.Duration("timeout", defaultRunTimeout, "hard deadline for the whole run")
	idle := fs.Duration("idle", defaultRunIdle, "kill the run after this long without any output")
	name := fs.String("name", "", "log file name (default: derived from the command)")
	tailLines := fs.Int("tail", defaultRunTail, "lines of output to show on failure")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	cmd := fs.Args()
	if len(cmd) == 0 {
		_, _ = fmt.Fprint(stderr, runUsage)
		return exitUsage
	}
	cmd = runcmd.InjectGoTestTimeout(cmd, *timeout)

	runner := env.procRunner
	if runner == nil {
		runner = proc.Exec{}
	}
	started := time.Now()
	res, err := runner.Run(ctx, proc.Spec{
		Dir: env.cwd, Name: cmd[0], Args: cmd[1:], Timeout: *timeout, IdleTimeout: *idle,
	})
	elapsed := time.Since(started).Round(roundRun)

	switch {
	case errors.Is(err, exec.ErrNotFound):
		_, _ = fmt.Fprintf(stderr, "agentctl run: command not found: %s\n", cmd[0])
		return exitRunNotFound
	case err != nil:
		_, _ = fmt.Fprintln(stderr, "agentctl run:", err)
		return exitUsage
	}

	logName := "run-" + runcmd.Slug(cmd)
	if *name != "" {
		logName = "run-" + runcmd.Slug([]string{*name})
	}
	logPath := runcmd.WriteLog(logDirFor(env.cwd), logName, res.Output)
	display := strings.Join(cmd, " ")

	switch {
	case res.TimedOut:
		_, _ = fmt.Fprintf(stdout, "TIMEOUT  %s  %s after %s; killed the whole process tree\n%s\n",
			display, timeoutReason(res.Reason, *timeout, *idle), elapsed, runcmd.Tail(res.Output, *tailLines))
		printLog(stdout, logPath)
		return exitRunTimeout
	case res.ExitCode != 0:
		_, _ = fmt.Fprintf(
			stdout,
			"FAIL  %s  exit %d in %s\n%s\n",
			display,
			res.ExitCode,
			elapsed,
			runcmd.Tail(res.Output, *tailLines),
		)
		printLog(stdout, logPath)
		return max(res.ExitCode, 1)
	}
	_, _ = fmt.Fprintf(stdout, "ok  %s  %s  (log: %s)\n", display, elapsed, logPath)
	return exitOK
}

// logDirFor keeps logs at the repository root when there is one, so every session finds them in one place.
func logDirFor(cwd string) string {
	if root, err := doctor.FindRoot(cwd); err == nil {
		return filepath.Join(root, ".agentctl", "logs")
	}
	return filepath.Join(cwd, ".agentctl", "logs")
}

func timeoutReason(reason string, timeout, idle time.Duration) string {
	switch reason {
	case proc.ReasonIdle:
		return fmt.Sprintf("no output for %s (idle limit)", idle)
	case proc.ReasonDeadline:
		return fmt.Sprintf("exceeded the %s deadline", timeout)
	}
	return "was cancelled"
}

func printLog(w io.Writer, path string) {
	if path != "" {
		_, _ = fmt.Fprintf(w, "full log: %s\n", path)
	}
}
