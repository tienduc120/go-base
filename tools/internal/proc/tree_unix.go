//go:build !windows

package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// killTreeOnCancel puts the child in its own process group and kills the whole group on cancel, so
// grandchildren such as the test binary under `go test` cannot outlive their parent.
func killTreeOnCancel(_ context.Context, cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
