package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

// sqlcStub plays sqlc: it writes the compiled queries the test gives it, and refuses to touch Docker.
type sqlcStub struct {
	compiled string
	ran      []string
}

func (s *sqlcStub) Run(_ context.Context, spec proc.Spec) (proc.Result, error) {
	s.ran = append(s.ran, spec.Name)
	if spec.Name != "sqlc" {
		return proc.Result{ExitCode: 1, Output: "docker must not be called"}, nil
	}
	out := filepath.Join(spec.Dir, ".dbcheck", "out")
	if err := os.MkdirAll(out, 0o750); err != nil {
		return proc.Result{}, err
	}
	return proc.Result{}, os.WriteFile(filepath.Join(out, "codegen_request.json"), []byte(s.compiled), 0o600)
}

func repoWithQueries(t *testing.T, queries string) string {
	t.Helper()
	repo := newRepo(t)
	pg := filepath.Join(repo, "services", "billing", "internal", "infrastructure", "postgres")
	for name, content := range map[string]string{
		"queries/notes.sql":        queries,
		"migrations/0001_init.sql": "CREATE TABLE notes (id text PRIMARY KEY);\n",
	} {
		path := filepath.Join(pg, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func TestDbCheckPassesCompliantQueries(t *testing.T) {
	t.Parallel()

	repo := repoWithQueries(t, "-- name: GetNote :one\nSELECT id FROM notes WHERE id = $1;\n")
	stub := &sqlcStub{
		compiled: `{"queries":[{"name":"GetNote","cmd":":one","text":"SELECT id FROM notes WHERE id = $1"}]}`,
	}

	got := invoke(t, environment{cwd: repo, procRunner: stub}, "db", "check", "--no-plan")

	if got.code != 0 || !strings.Contains(got.stdout, "1 query") || !strings.Contains(got.stdout, "ok") {
		t.Errorf("exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
}

func TestDbCheckFailsWithTheFindingAndHowToFixIt(t *testing.T) {
	t.Parallel()

	repo := repoWithQueries(t, "-- name: ListNotes :many\nSELECT id FROM notes ORDER BY id;\n")
	stub := &sqlcStub{
		compiled: `{"queries":[{"name":"ListNotes","cmd":":many","text":"SELECT id FROM notes ORDER BY id"}]}`,
	}

	got := invoke(t, environment{cwd: repo, procRunner: stub}, "db", "check", "--no-plan")

	if got.code != 1 {
		t.Fatalf("exit = %d, want 1\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"notes.sql:1", "ListNotes", "list-needs-limit", "LIMIT", "1 finding"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, got.stdout)
		}
	}
}

func TestDbCheckShowsAcceptedFindingsWithTheirReason(t *testing.T) {
	t.Parallel()

	src := "-- name: ListKinds :many\n-- agent:allow list-needs-limit: at most ten rows ever\nSELECT id FROM notes ORDER BY id;\n"
	repo := repoWithQueries(t, src)
	stub := &sqlcStub{
		compiled: `{"queries":[{"name":"ListKinds","cmd":":many","text":"SELECT id FROM notes ORDER BY id"}]}`,
	}

	got := invoke(t, environment{cwd: repo, procRunner: stub}, "db", "check", "--no-plan")

	if got.code != 0 {
		t.Fatalf("exit = %d\n%s", got.code, got.stdout)
	}
	if !strings.Contains(got.stdout, "accepted") || !strings.Contains(got.stdout, "at most ten rows ever") {
		t.Errorf("an accepted finding is a decision and must stay in the output:\n%s", got.stdout)
	}
}

func TestDbCheckWithNothingToCheck(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{cwd: newRepo(t), procRunner: &sqlcStub{}}, "db", "check")

	if got.code != 0 || !strings.Contains(got.stdout, "no queries") {
		t.Errorf("exit = %d, stdout = %q", got.code, got.stdout)
	}
}

func TestDbCheckReportsAnInfrastructureFailureAsUsageError(t *testing.T) {
	t.Parallel()

	repo := repoWithQueries(t, "-- name: GetNote :one\nSELECT id FROM notes WHERE id = $1;\n")
	failing := specRunner(func(proc.Spec) (proc.Result, error) {
		return proc.Result{ExitCode: 1, Output: "queries/notes.sql:2:8: relation \"ghost\" does not exist"}, nil
	})

	got := invoke(t, environment{cwd: repo, procRunner: failing}, "db", "check", "--no-plan")

	if got.code != 2 || !strings.Contains(got.stderr, "ghost") {
		t.Errorf("exit = %d, stderr = %q: a check that could not run is not a pass", got.code, got.stderr)
	}
}

type specRunner func(proc.Spec) (proc.Result, error)

func (f specRunner) Run(_ context.Context, spec proc.Spec) (proc.Result, error) { return f(spec) }

func TestDbUsageErrors(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"no subcommand": {"db"},
		"unknown":       {"db", "migrate"},
		"unknown flag":  {"db", "check", "--nope"},
	} {
		if got := invoke(t, environment{cwd: newRepo(t)}, args...); got.code != 2 {
			t.Errorf("%s: exit = %d, want 2", name, got.code)
		}
	}
	if got := invoke(t, environment{cwd: t.TempDir()}, "db", "check"); got.code != 2 {
		t.Errorf("outside a repository: exit = %d, want 2", got.code)
	}
}

// testStack plays Docker and go test for `db test`.
type testStack struct {
	goExit   int
	goOutput string
	calls    []string
}

func (s *testStack) Run(_ context.Context, spec proc.Spec) (proc.Result, error) {
	s.calls = append(s.calls, spec.Name+" "+strings.Join(spec.Args, " "))
	switch {
	case spec.Name == "docker" && len(spec.Args) > 0 && spec.Args[0] == "port":
		return proc.Result{Output: "127.0.0.1:55432\n"}, nil
	case spec.Name == "go":
		return proc.Result{ExitCode: s.goExit, Output: s.goOutput}, nil
	}
	return proc.Result{}, nil
}

func TestDbTestRunsTheTestsAgainstAThrowawayServer(t *testing.T) {
	t.Parallel()

	stack := &testStack{goOutput: "ok  \tgithub.com/x/platform/postgres\t1.2s\n"}
	got := invoke(t, environment{cwd: newRepo(t), procRunner: stack}, "db", "test", "./platform/postgres/...")

	if got.code != 0 || !strings.Contains(got.stdout, "db test: ok") {
		t.Fatalf("exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	joined := strings.Join(stack.calls, "\n")
	if !strings.Contains(joined, "go test -race") || !strings.Contains(joined, "./platform/postgres/...") ||
		!strings.Contains(joined, "rm -f") {
		t.Errorf("calls:\n%s", joined)
	}
}

func TestDbTestShowsTheTailOfAFailingRun(t *testing.T) {
	t.Parallel()

	stack := &testStack{
		goExit:   1,
		goOutput: "--- FAIL: TestTheServerCancels (0.31s)\n    integration_test.go:41: err = nil\nFAIL\n",
	}
	got := invoke(t, environment{cwd: newRepo(t), procRunner: stack}, "db", "test")

	if got.code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", got.code, got.stdout)
	}
	if !strings.Contains(got.stdout, "TestTheServerCancels") || !strings.Contains(got.stdout, "db test: failed") {
		t.Errorf("a failure must show what failed:\n%s", got.stdout)
	}
}

func TestDbTestReportsMissingDockerAsAUsageError(t *testing.T) {
	t.Parallel()

	missing := specRunner(func(proc.Spec) (proc.Result, error) { return proc.Result{}, exec.ErrNotFound })
	got := invoke(t, environment{cwd: newRepo(t), procRunner: missing}, "db", "test")

	if got.code != 2 || !strings.Contains(got.stderr, "docker") {
		t.Errorf("exit = %d, stderr = %q", got.code, got.stderr)
	}
}
