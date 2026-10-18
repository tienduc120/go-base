// Package proc runs subprocesses under a hard deadline so no tool run can hang a session. A run that overruns, goes
// silent, or is cancelled takes its whole process tree with it, not just the direct child.
package proc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"time"
)

// killGrace bounds how long a child that keeps our pipes open can delay Run after the deadline.
const killGrace = 2 * time.Second

type Spec struct {
	Dir  string
	Name string
	Args []string
	// Env entries are added to the parent's environment.
	Env []string
	// Timeout is the hard deadline; zero means only the caller's context bounds the run.
	Timeout time.Duration
	// IdleTimeout kills a process that prints nothing for this long; zero disables the check.
	IdleTimeout time.Duration
}

// Why a run was cut short.
const (
	ReasonDeadline  = "deadline"
	ReasonIdle      = "idle"
	ReasonCancelled = "cancelled"
)

type Result struct {
	ExitCode int
	Output   string
	// TimedOut is true when the run was cut short; Reason says why.
	TimedOut bool
	Reason   string
}

type Runner interface {
	// Run returns an error only when the process could not be started; a non-zero exit is a Result.
	Run(ctx context.Context, spec Spec) (Result, error)
}

type Exec struct{}

func (Exec) Run(ctx context.Context, spec Spec) (Result, error) {
	path, err := exec.LookPath(spec.Name)
	if err != nil {
		return Result{}, err
	}

	// Our own cancel lets the idle timer stop the run without waiting for the deadline.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if spec.Timeout > 0 {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, spec.Timeout)
		defer cancelTimeout()
	}

	cmd := exec.CommandContext(ctx, path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.WaitDelay = killGrace
	killTreeOnCancel(ctx, cmd)

	var idleFired atomic.Bool
	out := &activityBuffer{}
	if spec.IdleTimeout > 0 {
		idle := time.AfterFunc(spec.IdleTimeout, func() {
			idleFired.Store(true)
			cancel()
		})
		defer idle.Stop()
		out.onWrite = func() { idle.Reset(spec.IdleTimeout) }
	}
	cmd.Stdout, cmd.Stderr = out, out

	err = cmd.Run()
	res := Result{Output: out.String()}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case ctx.Err() != nil:
		res.TimedOut = true
		res.ExitCode = -1
		res.Reason = reasonFor(ctx.Err(), idleFired.Load())
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		return res, err
	}
	return res, nil
}

func reasonFor(ctxErr error, idle bool) string {
	switch {
	case idle:
		return ReasonIdle
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return ReasonDeadline
	}
	return ReasonCancelled
}

// activityBuffer collects output and reports each write, which is what keeps a working process from looking idle.
// exec serialises writes when stdout and stderr are the same pointer, so no lock is needed.
type activityBuffer struct {
	buf     bytes.Buffer
	onWrite func()
}

func (a *activityBuffer) Write(p []byte) (int, error) {
	n, err := a.buf.Write(p)
	if a.onWrite != nil {
		a.onWrite()
	}
	return n, err
}

func (a *activityBuffer) String() string { return a.buf.String() }
