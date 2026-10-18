//go:build !windows

package proc_test

import "syscall"

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func killPID(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
