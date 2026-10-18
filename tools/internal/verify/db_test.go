package verify_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/verify"
)

func runDBStep(
	t *testing.T,
	level verify.Level,
	reply func(proc.Spec) (proc.Result, error),
) (*fakeProc, verify.Outcome) {
	t.Helper()
	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	runner := &fakeProc{reply: reply}

	step := verify.DBStep(env(t, root, runner), level)
	if step.Name != "db check" {
		t.Fatalf("name = %q", step.Name)
	}
	return runner, step.Run(t.Context())
}

func TestDBStepJudgesQueriesWithoutDockerInTheFastGate(t *testing.T) {
	t.Parallel()

	runner, out := runDBStep(t, verify.Fast, nil)

	if !out.Pass || len(runner.calls) != 1 {
		t.Fatalf("outcome = %+v, calls = %v", out, runner.names())
	}
	got := runner.calls[0]
	if !strings.HasPrefix(filepath.Base(got.name), "agentctl") ||
		!slices.Equal(got.args, []string{"db", "check", "--no-plan"}) {
		t.Errorf("ran %s %v: every commit checks the query rules and generated code, and none of that needs Docker",
			got.name, got.args)
	}
}

func TestDBStepAlsoReadsPlansInTheFullGate(t *testing.T) {
	t.Parallel()

	runner, out := runDBStep(t, verify.Full, nil)

	if !out.Pass || len(runner.calls) != 1 || !slices.Equal(runner.calls[0].args, []string{"db", "check"}) {
		t.Errorf(
			"outcome = %+v, calls = %v: before a push the plans are read on a throwaway Postgres",
			out,
			runner.names(),
		)
	}
}

func TestDBStepFailsWithTheFindingsTail(t *testing.T) {
	t.Parallel()

	_, out := runDBStep(t, verify.Fast, func(proc.Spec) (proc.Result, error) {
		return proc.Result{
			ExitCode: 1,
			Output:   "q.sql:3: ListNotes: list-needs-limit: add a LIMIT\ndb check: 1 query in 1 service: 1 finding\n",
		}, nil
	})

	if out.Pass || !strings.Contains(out.Detail, "list-needs-limit") {
		t.Errorf("outcome = %+v, want a failure that shows the finding", out)
	}
}
