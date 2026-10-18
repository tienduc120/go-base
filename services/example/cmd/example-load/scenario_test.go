package main

import (
	"context"
	"testing"

	"github.com/tienduc120/go-base/platform/loadgen"
)

// This holds for the placeholder and for any real scenario: a step must not outlive its context, or the load program
// cannot stop at its deadline.
func TestTheStepReturnsWhenItsContextIsAlreadyCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_ = scenario("http://127.0.0.1:1")(ctx, loadgen.Worker{ID: 0, Iteration: 0})
}
