package rules_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/rules"
)

type fakeProc struct {
	mu    sync.Mutex
	calls []proc.Spec
	// reply writes whatever files the real tool would and returns its result.
	reply func(spec proc.Spec) (proc.Result, error)
}

func (f *fakeProc) Run(_ context.Context, spec proc.Spec) (proc.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, spec)
	f.mu.Unlock()
	return f.reply(spec)
}

func golangciReply(issues string, exit int) func(proc.Spec) (proc.Result, error) {
	return func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "go" {
			return proc.Result{}, nil
		}
		if strings.HasSuffix(spec.Name, "golangci-lint") {
			err := os.WriteFile(filepath.Join(spec.Dir, "issues.json"), []byte(issues), 0o600)
			return proc.Result{ExitCode: exit}, err
		}
		return proc.Result{}, errors.New("unexpected tool " + spec.Name)
	}
}

const firesOnSleep = `{"Issues":[{"FromLinter":"forbidigo","Text":"use of time.Sleep forbidden",` +
	`"Pos":{"Filename":"forbidigo-sleep\\lib\\lib.go"}}]}`

func golangciOnly(t *testing.T) rules.Deps {
	t.Helper()
	fsys := repoFS()
	delete(fsys, "testdata/violations/arch-domain/rule.json")
	delete(fsys, "testdata/violations/arch-domain/services/a/internal/domain/d.go")
	return rules.Deps{FS: fsys, GoVersion: "1.27.2", TempDir: t.TempDir()}
}

func TestRunPassesWhenTheEnforcerFires(t *testing.T) {
	t.Parallel()

	deps := golangciOnly(t)
	deps.Runner = &fakeProc{reply: golangciReply(firesOnSleep, 1)}

	outcomes, err := rules.Run(t.Context(), deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		if !o.Pass {
			t.Errorf("%s: %s", o.ID, o.Detail)
		}
	}
}

func TestRunTreatsExitCodeOneAsFindingsAndOtherCodesAsFailure(t *testing.T) {
	t.Parallel()

	deps := golangciOnly(t)
	deps.Runner = &fakeProc{reply: golangciReply(`{"Issues":null}`, 3)}

	_, err := rules.Run(t.Context(), deps)
	if !errors.Is(err, rules.ErrEnforcerFailed) {
		t.Fatalf("err = %v, want ErrEnforcerFailed: a crashed linter must not look like a rule that fired", err)
	}
}

func TestRunReportsATimeoutAsAFailure(t *testing.T) {
	t.Parallel()

	deps := golangciOnly(t)
	deps.Runner = &fakeProc{reply: func(proc.Spec) (proc.Result, error) {
		return proc.Result{ExitCode: -1, TimedOut: true}, nil
	}}

	if _, err := rules.Run(t.Context(), deps); !errors.Is(err, rules.ErrEnforcerFailed) {
		t.Fatalf("err = %v, want ErrEnforcerFailed", err)
	}
}

func TestRunOnlyInvokesEnforcersThatFixturesUse(t *testing.T) {
	t.Parallel()

	deps := golangciOnly(t)
	fake := &fakeProc{reply: golangciReply(firesOnSleep, 1)}
	deps.Runner = fake

	if _, err := rules.Run(t.Context(), deps); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.calls {
		if strings.Contains(c.Name, "arch-go") {
			t.Errorf("arch-go has no fixtures here and must not run: %+v", c)
		}
	}
}

func TestRunRemovesItsStagingDirectory(t *testing.T) {
	t.Parallel()

	deps := golangciOnly(t)
	var staged string
	deps.Runner = &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		staged = spec.Dir
		return golangciReply(firesOnSleep, 1)(spec)
	}}

	if _, err := rules.Run(t.Context(), deps); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); err == nil {
		t.Errorf("staging directory %s was left behind", staged)
	}
}

func TestRunStagesEverythingBeforeRunning(t *testing.T) {
	t.Parallel()

	deps := golangciOnly(t)
	var sawConfig, sawFixture, sawClean bool
	deps.Runner = &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		_, e1 := os.Stat(filepath.Join(spec.Dir, ".golangci.yml"))
		_, e2 := os.Stat(filepath.Join(spec.Dir, "forbidigo-sleep", "lib", "lib.go"))
		_, e3 := os.Stat(filepath.Join(spec.Dir, "clean", "lib", "lib.go"))
		sawConfig, sawFixture, sawClean = e1 == nil, e2 == nil, e3 == nil
		return golangciReply(firesOnSleep, 1)(spec)
	}}

	if _, err := rules.Run(t.Context(), deps); err != nil {
		t.Fatal(err)
	}
	if !sawConfig || !sawFixture || !sawClean {
		t.Errorf("config=%v fixture=%v clean=%v at run time", sawConfig, sawFixture, sawClean)
	}
}

func TestRunRejectsFixturesThatDoNotCompile(t *testing.T) {
	t.Parallel()

	deps := golangciOnly(t)
	var enforcerRan bool
	deps.Runner = &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "go" {
			return proc.Result{ExitCode: 1, Output: "fixture.test/sleep/lib: undefined: nope"}, nil
		}
		enforcerRan = true
		return golangciReply(firesOnSleep, 1)(spec)
	}}

	_, err := rules.Run(t.Context(), deps)
	if !errors.Is(err, rules.ErrFixtureBroken) {
		t.Fatalf("err = %v, want ErrFixtureBroken: one bad fixture makes a linter drop every finding", err)
	}
	if !strings.Contains(err.Error(), "undefined: nope") {
		t.Errorf("error should carry the compiler output: %v", err)
	}
	if enforcerRan {
		t.Error("enforcers must not run on a module that does not compile")
	}
}

func agentlintDeps(t *testing.T) rules.Deps {
	t.Helper()
	fsys := repoFS()
	delete(fsys, "testdata/violations/forbidigo-sleep/rule.json")
	delete(fsys, "testdata/violations/forbidigo-sleep/lib/lib.go")
	delete(fsys, "testdata/violations/arch-domain/rule.json")
	delete(fsys, "testdata/violations/arch-domain/services/a/internal/domain/d.go")
	fsys["testdata/violations/barego-x/rule.json"] = &fstest.MapFile{
		Data: []byte(`{"enforcer":"agentlint","contains":"bare go statement"}`),
	}
	fsys["testdata/violations/barego-x/x.go"] = &fstest.MapFile{Data: []byte("package x\n")}
	return rules.Deps{FS: fsys, GoVersion: "1.27.2", TempDir: t.TempDir(), RepoRoot: "/repo"}
}

func TestRunBuildsThenRunsAgentlintAsAVetTool(t *testing.T) {
	t.Parallel()

	deps := agentlintDeps(t)
	var buildDir, vetDir, vetTool string
	deps.Runner = &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		switch {
		case spec.Name == "go" && spec.Args[0] == "build" && slices.Contains(spec.Args, "-o"):
			buildDir = spec.Dir
			return proc.Result{}, nil
		case spec.Name == "go" && spec.Args[0] == "vet" && len(spec.Args) > 2:
			vetDir, vetTool = spec.Dir, spec.Args[1]
			return proc.Result{
				ExitCode: 1,
				Output:   "# fixture.test/barego-x\nbarego-x/x.go:3:2: bare go statement: use platform/async\n",
			}, nil
		}
		return proc.Result{}, nil
	}}

	outcomes, err := rules.Run(t.Context(), deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		if !o.Pass {
			t.Errorf("%s: %s", o.ID, o.Detail)
		}
	}
	if buildDir != "/repo" {
		t.Errorf("the vettool must be built from the repo root, built in %q", buildDir)
	}
	if !strings.HasPrefix(vetTool, "-vettool=") {
		t.Errorf("go vet must be pointed at the built tool, got %q", vetTool)
	}
	if vetDir == "/repo" || vetDir == "" {
		t.Errorf("go vet must run in the staged module, ran in %q", vetDir)
	}
}

func TestRunFailsWhenTheVetToolCannotBeBuilt(t *testing.T) {
	t.Parallel()

	deps := agentlintDeps(t)
	deps.Runner = &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "go" && spec.Args[0] == "build" && slices.Contains(spec.Args, "-o") {
			return proc.Result{ExitCode: 1, Output: "agentlint: build failed"}, nil
		}
		return proc.Result{}, nil
	}}

	if _, err := rules.Run(t.Context(), deps); !errors.Is(err, rules.ErrEnforcerFailed) {
		t.Fatalf("err = %v, want ErrEnforcerFailed", err)
	}
}
