package proc_test

import (
	"testing"

	"go.uber.org/goleak"
)

// A run that leaves a goroutine behind (a pipe copier, a timer callback) would pile up across thousands of commands.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
