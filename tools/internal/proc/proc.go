// Package proc runs subprocesses under a hard deadline so no tool run can hang a session.
package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
}

type Result struct {
	ExitCode int
	Output   string
	// TimedOut is true when the deadline or the caller's cancellation cut the run short.
	TimedOut bool
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

	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.WaitDelay = killGrace

	out, err := cmd.CombinedOutput()
	res := Result{Output: string(out)}

	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		res.TimedOut = true
		res.ExitCode = -1
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	case err != nil:
		return res, err
	}
	return res, nil
}
