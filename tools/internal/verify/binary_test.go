package verify_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/verify"
)

func TestPlanBuildsTheBinaryTheAgentHooksCall(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	fake := &fakeProc{}
	e := env(t, root, fake)

	if !slices.Contains(stepNames(verify.Plan(e, verify.Fast)), "build agentctl") {
		t.Fatalf("the plan must refresh the hook binary: %v", stepNames(verify.Plan(e, verify.Fast)))
	}

	verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast))

	var built bool
	for _, c := range fake.calls {
		line := strings.Join(c.args, " ")
		if c.name == "go" && strings.HasPrefix(line, "build -o") &&
			strings.Contains(line, ".agentctl") && strings.HasSuffix(line, "/tools/agentctl") {
			built = true
			if c.dir != root {
				t.Errorf("built in %q, want the repository root %q", c.dir, root)
			}
		}
	}
	if !built {
		t.Errorf("no `go build -o .agentctl/bin/agentctl ./tools/agentctl` call among %v", fake.names())
	}
}

func TestPlanSkipsTheBinaryBuildWhenThereIsNoGoCode(t *testing.T) {
	t.Parallel()

	root := workspace(t, nil)
	if slices.Contains(stepNames(verify.Plan(env(t, root, &fakeProc{}), verify.Fast)), "build agentctl") {
		t.Error("with no Go code anywhere there is nothing to build")
	}
}
