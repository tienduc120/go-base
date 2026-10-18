package dbcheck_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

func portAnswering(spec proc.Spec) (proc.Result, error) {
	if len(spec.Args) > 0 && spec.Args[0] == "port" {
		return proc.Result{Output: "127.0.0.1:55432\n"}, nil
	}
	return proc.Result{Output: "ok"}, nil
}

func goTestCall(runner *fakeRunner) (proc.Spec, bool) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	for _, c := range runner.calls {
		if c.Name == "go" {
			return c, true
		}
	}
	return proc.Spec{}, false
}

func TestRunTestsHandsTheServerToGoTestAndBoundsTheRun(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: portAnswering}
	res, err := dbcheck.RunTests(t.Context(), runner, dbcheck.TestOptions{Root: "/repo"})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}

	call, ok := goTestCall(runner)
	if !ok {
		t.Fatalf("go test was not run: %v", runner.commands())
	}
	if call.Dir != "/repo" || call.Timeout <= 0 || call.IdleTimeout <= 0 {
		t.Errorf("the run needs a directory, a deadline and an idle limit: %+v", call)
	}
	for _, want := range []string{"test", "-race", "-count=1", "-timeout", "./..."} {
		if !slices.Contains(call.Args, want) {
			t.Errorf("args %v lack %q", call.Args, want)
		}
	}
	for _, want := range []string{
		"TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable",
		"AGENTCTL_REQUIRE_DB=1",
	} {
		if !slices.Contains(call.Env, want) {
			t.Errorf("env %v lacks %q", call.Env, want)
		}
	}
}

func TestRunTestsRunsOnlyTheNamedPackages(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: portAnswering}
	if _, err := dbcheck.RunTests(t.Context(), runner, dbcheck.TestOptions{
		Root: "/repo", Packages: []string{"./platform/postgres/..."},
	}); err != nil {
		t.Fatal(err)
	}

	call, _ := goTestCall(runner)
	if slices.Contains(call.Args, "./...") || !slices.Contains(call.Args, "./platform/postgres/...") {
		t.Errorf("args = %v", call.Args)
	}
}

func TestRunTestsRemovesTheServerWhenTheTestsFail(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "go" {
			return proc.Result{ExitCode: 1, Output: "--- FAIL: TestX"}, nil
		}
		return portAnswering(spec)
	}}

	res, err := dbcheck.RunTests(t.Context(), runner, dbcheck.TestOptions{Root: "/repo"})

	if err != nil || res.ExitCode != 1 || !strings.Contains(res.Output, "FAIL") {
		t.Errorf("a failing test run is a result, not an error: %+v, %v", res, err)
	}
	if _, ok := runner.find("rm -f"); !ok {
		t.Errorf("the container must not outlive the run: %v", runner.commands())
	}
}
