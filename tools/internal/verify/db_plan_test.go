package verify_test

import (
	"slices"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/verify"
)

func TestPlanRunsDBCheckAfterTheAgentctlBuildItUses(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	for _, level := range []verify.Level{verify.Fast, verify.Full} {
		names := stepNames(verify.Plan(env(t, root, &fakeProc{}), level))

		built, db := slices.Index(names, "build agentctl"), slices.Index(names, "db check")
		if db < 0 || built < 0 || db < built {
			t.Errorf("level %v: db check must run, and after the build that produces its binary: %v", level, names)
		}
	}
}

func TestPlanLeavesDBCheckOutWhenThereIsNoGoCode(t *testing.T) {
	t.Parallel()

	root := workspace(t, nil)
	if slices.Contains(stepNames(verify.Plan(env(t, root, &fakeProc{}), verify.Fast)), "db check") {
		t.Error("with no Go code there is no agentctl binary to run, and nothing to check")
	}
}
