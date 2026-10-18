package verify_test

import (
	"slices"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/verify"
)

func TestPlanRunsTheDebtRatchetRightAfterTheMarkdownCheck(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	names := stepNames(verify.Plan(env(t, root, &fakeProc{}), verify.Fast))

	debtAt := slices.Index(names, "debt ratchet")
	markdownAt := slices.Index(names, "markdown style")
	if debtAt < 0 {
		t.Fatalf("the ratchet must run on every verify, or debt grows unnoticed: %v", names)
	}
	if debtAt != markdownAt+1 {
		t.Errorf("the cheap, code-free checks run together first: %v", names)
	}
}

func TestPlanRunsTheDebtRatchetEvenWithNoGoCode(t *testing.T) {
	t.Parallel()

	root := workspace(t, nil)
	if !slices.Contains(stepNames(verify.Plan(env(t, root, &fakeProc{}), verify.Fast)), "debt ratchet") {
		t.Error("a repository that has not got its first Go package yet still has a register to keep honest")
	}
}

func TestPlanRunsTheDebtRatchetInTheFullGateToo(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	if !slices.Contains(stepNames(verify.Plan(env(t, root, &fakeProc{}), verify.Full)), "debt ratchet") {
		t.Error("the full gate is a superset of the fast one")
	}
}
