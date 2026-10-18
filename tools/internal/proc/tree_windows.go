//go:build windows

package proc

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

// taskkillLimit bounds the kill itself; the run's context is already cancelled when Cancel fires.
const taskkillLimit = 10 * time.Second

// killTreeOnCancel kills the whole process tree on cancel. Process.Kill ends only the direct child, so
// grandchildren such as the test binary under `go test` would keep running; taskkill /T walks the tree.
func killTreeOnCancel(ctx context.Context, cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		killCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), taskkillLimit)
		defer stop()

		pid := strconv.Itoa(cmd.Process.Pid)
		if err := exec.CommandContext(killCtx, "taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
			// taskkill fails when the process is already gone; Kill then reports os.ErrProcessDone.
			return cmd.Process.Kill()
		}
		return nil
	}
}
