//go:build windows

package proc_test

import (
	"os/exec"
	"strconv"
	"strings"
)

func processAlive(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH", "/FO", "CSV").Output()
	return err == nil && strings.Contains(string(out), `"`+strconv.Itoa(pid)+`"`)
}

func killPID(pid int) {
	_ = exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
}
