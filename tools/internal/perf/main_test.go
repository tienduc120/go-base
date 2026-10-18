package perf_test

import (
	"testing"

	"go.uber.org/goleak"
)

// Capture talks to a live service, so a response body or connection left open would show up here first.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
