package verify_test

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/verify"
)

type call struct {
	dir  string
	name string
	args []string
}

type fakeProc struct {
	mu    sync.Mutex
	calls []call
	reply func(spec proc.Spec) (proc.Result, error)
}

func (f *fakeProc) Run(_ context.Context, spec proc.Spec) (proc.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{dir: spec.Dir, name: spec.Name, args: spec.Args})
	f.mu.Unlock()
	if f.reply == nil {
		return proc.Result{}, nil
	}
	return f.reply(spec)
}

func (f *fakeProc) names() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.name+" "+strings.Join(c.args, " "))
	}
	return out
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func workspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	base := map[string]string{
		"go.work":      "go 1.27.2\n\nuse (\n\t.\n\t./tools\n)\n",
		"go.mod":       "module example.com/root\n\ngo 1.27.2\n",
		"tools/go.mod": "module example.com/root/tools\n\ngo 1.27.2\n",
	}
	maps.Copy(base, files)
	writeFiles(t, root, base)
	return root
}

func TestModules(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{
		"tools/main.go":             "package main\n",
		"testdata/clean/x.go":       "package x\n",
		"arch-go.yml":               "version: 1\n",
		"tools/testdata/src/y/y.go": "package y\n",
	})

	mods, err := verify.Modules(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 {
		t.Fatalf("got %d modules, want 2: %+v", len(mods), mods)
	}
	if mods[0].Rel != "." || mods[0].HasGo {
		t.Errorf("root has no Go code outside testdata, got %+v", mods[0])
	}
	if !mods[0].HasArchConfig {
		t.Errorf("root carries arch-go.yml: %+v", mods[0])
	}
	if mods[1].Rel != "tools" || !mods[1].HasGo || mods[1].HasArchConfig {
		t.Errorf("tools has Go code and no arch config, got %+v", mods[1])
	}
}

func TestModulesIgnoreNestedModules(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	mods, err := verify.Modules(root)
	if err != nil {
		t.Fatal(err)
	}
	if mods[0].HasGo {
		t.Error("tools/main.go belongs to the tools module, not the root module")
	}
}

func TestModulesNeedsAWorkspace(t *testing.T) {
	t.Parallel()

	if _, err := verify.Modules(t.TempDir()); err == nil {
		t.Fatal("want an error when go.work is missing")
	}
}

func env(t *testing.T, root string, runner proc.Runner) verify.Env {
	t.Helper()
	mods, err := verify.Modules(root)
	if err != nil {
		t.Fatal(err)
	}
	return verify.Env{Root: root, Modules: mods, Runner: runner, LogDir: filepath.Join(root, ".agentctl", "logs")}
}

func stepNames(steps []verify.Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.Name)
	}
	return out
}

func TestPlanFast(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	steps := verify.Plan(env(t, root, &fakeProc{}), verify.Fast)
	got := strings.Join(stepNames(steps), "\n")

	for _, want := range []string{
		"go vet (tools)", "build agentlint", "agentlint (tools)", "golangci-lint (tools)", "go test (tools)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("fast plan lacks %q:\n%s", want, got)
		}
	}
	for _, never := range []string{"govulncheck", "rules test"} {
		if strings.Contains(got, never) {
			t.Errorf("fast plan must not include %q:\n%s", never, got)
		}
	}
}

func TestPlanFullAddsTheSlowGates(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	got := strings.Join(stepNames(verify.Plan(env(t, root, &fakeProc{}), verify.Full)), "\n")

	for _, want := range []string{"govulncheck (tools)", "rules test"} {
		if !strings.Contains(got, want) {
			t.Errorf("full plan lacks %q:\n%s", want, got)
		}
	}
}

func TestPlanRunsLayerRulesOnlyWhereTheConfigLives(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{
		"tools/main.go":           "package main\n",
		"arch-go.yml":             "version: 1\n",
		"services/billing/x/x.go": "package x\n",
	})
	got := stepNames(verify.Plan(env(t, root, &fakeProc{}), verify.Fast))

	if !slices.Contains(got, "arch-go (.)") {
		t.Errorf("root has Go code and arch-go.yml, want an arch-go step: %v", got)
	}
	if slices.Contains(got, "arch-go (tools)") {
		t.Errorf("tools has no arch-go.yml: %v", got)
	}
}

func TestEmptyModulesAreReportedAsSkippedNotSilentlyDropped(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	results := verify.Execute(
		t.Context(),
		env(t, root, &fakeProc{}),
		verify.Plan(env(t, root, &fakeProc{}), verify.Fast),
	)

	var skipped []verify.Result
	for _, r := range results {
		if r.Skipped {
			skipped = append(skipped, r)
		}
	}
	if len(skipped) == 0 {
		t.Fatal("the root module has no Go code; that must be named as a skipped step")
	}
	if !strings.Contains(skipped[0].Detail, "no Go packages") {
		t.Errorf("skip reason = %q", skipped[0].Detail)
	}
}

func TestExecuteRunsEveryStepEvenAfterAFailure(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	fake := &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "golangci-lint" {
			return proc.Result{ExitCode: 1, Output: "main.go:1:1: something is wrong\n"}, nil
		}
		return proc.Result{}, nil
	}}
	e := env(t, root, fake)

	results := verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast))

	var failed []string
	ran := map[string]bool{}
	for _, r := range results {
		ran[r.Name] = true
		if !r.Pass && !r.Skipped {
			failed = append(failed, r.Name)
		}
	}
	if !slices.Equal(failed, []string{"golangci-lint (tools)"}) {
		t.Errorf("failed = %v, want only the lint step", failed)
	}
	if !ran["go test (tools)"] {
		t.Error("a failing step must not hide the steps after it; the agent wants every problem in one pass")
	}
}

func TestFailureDetailIsTheTailAndTheFullLogIsOnDisk(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i%7))
		sb.WriteString("\n")
	}
	sb.WriteString("FINAL FAILURE LINE\n")

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	fake := &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "golangci-lint" {
			return proc.Result{ExitCode: 1, Output: sb.String()}, nil
		}
		return proc.Result{}, nil
	}}
	e := env(t, root, fake)

	results := verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast))
	var lint verify.Result
	for _, r := range results {
		if r.Name == "golangci-lint (tools)" {
			lint = r
		}
	}

	if !strings.Contains(lint.Detail, "FINAL FAILURE LINE") {
		t.Errorf("detail must end with the real failure: %q", lint.Detail)
	}
	if strings.Count(lint.Detail, "\n") > 40 {
		t.Errorf("detail is %d lines; a failure summary must stay short", strings.Count(lint.Detail, "\n"))
	}
	data, err := os.ReadFile(lint.LogPath)
	if err != nil {
		t.Fatalf("the full output must be on disk: %v", err)
	}
	if strings.Count(string(data), "\n") < 100 {
		t.Errorf("log holds %d lines, want the whole output", strings.Count(string(data), "\n"))
	}
}

func TestMissingToolFailsWithAHint(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	fake := &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "golangci-lint" {
			return proc.Result{}, exec.ErrNotFound
		}
		return proc.Result{}, nil
	}}
	e := env(t, root, fake)

	for _, r := range verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast)) {
		if r.Name != "golangci-lint (tools)" {
			continue
		}
		if r.Pass || !strings.Contains(r.Detail, "agentctl doctor") {
			t.Errorf("a missing tool is a failure that points at doctor, got %+v", r)
		}
		return
	}
	t.Fatal("lint step not found")
}

func TestTimeoutIsAFailureNamingTheLimit(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	fake := &fakeProc{reply: func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "golangci-lint" {
			return proc.Result{ExitCode: -1, TimedOut: true, Output: "partial"}, nil
		}
		return proc.Result{}, nil
	}}
	e := env(t, root, fake)

	for _, r := range verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast)) {
		if r.Name == "golangci-lint (tools)" {
			if r.Pass || !strings.Contains(r.Detail, "timed out") {
				t.Errorf("want a timeout failure, got %+v", r)
			}
			return
		}
	}
	t.Fatal("lint step not found")
}

func TestStepsRunInTheirModuleDirectory(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	fake := &fakeProc{}
	e := env(t, root, fake)

	verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast))

	want := filepath.Join(root, "tools")
	for _, c := range fake.calls {
		if c.name == "golangci-lint" && c.dir != want {
			t.Errorf("golangci-lint ran in %q, want %q", c.dir, want)
		}
	}
	if !slices.ContainsFunc(
		fake.names(),
		func(s string) bool { return strings.HasPrefix(s, "go test -race -timeout") },
	) {
		t.Errorf("tests must run with the race detector and an explicit timeout: %v", fake.names())
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()

	results := []verify.Result{
		{Name: "go vet (tools)", Pass: true, Duration: 1200 * time.Millisecond},
		{
			Name:     "golangci-lint (tools)",
			Detail:   "main.go:1:1: bad",
			Duration: 3 * time.Second,
			LogPath:  "x.log",
		},
		{Name: "arch-go (.)", Skipped: true, Detail: "no Go packages yet"},
	}
	var sb strings.Builder
	ok := verify.Summarize(&sb, results)
	out := sb.String()

	if ok {
		t.Error("a failed step means the run failed")
	}
	for _, want := range []string{"ok", "FAIL", "SKIP", "main.go:1:1: bad", "no Go packages yet", "x.log", "1 passed, 1 failed, 1 skipped"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
}
