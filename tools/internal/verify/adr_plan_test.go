package verify_test

import (
	"slices"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/verify"
)

func TestPlanChecksTheDecisionRecordsOnEveryVerify(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	for _, level := range []verify.Level{verify.Fast, verify.Full} {
		names := stepNames(verify.Plan(env(t, root, &fakeProc{}), level))
		if !slices.Contains(names, "adr check") {
			t.Errorf(
				"level %v: a decision record that drifts from its index must fail the commit that drifts it",
				level,
			)
		}
	}
}
