package dbcheck_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

// fakeRunner answers every command through handle and records the calls, so no test needs Docker or sqlc.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []proc.Spec
	handle func(proc.Spec) (proc.Result, error)
}

func (f *fakeRunner) Run(_ context.Context, spec proc.Spec) (proc.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, spec)
	f.mu.Unlock()
	if f.handle == nil {
		return proc.Result{}, nil
	}
	return f.handle(spec)
}

func (f *fakeRunner) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Name+" "+strings.Join(c.Args, " "))
	}
	return out
}

func (f *fakeRunner) find(substr string) (string, bool) {
	for _, c := range f.commands() {
		if strings.Contains(c, substr) {
			return c, true
		}
	}
	return "", false
}

func TestStartPostgresRunsAThrowawayContainerThatExpiresByItself(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	pg, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine")
	if err != nil {
		t.Fatal(err)
	}

	run, ok := runner.find("run -d")
	if !ok {
		t.Fatalf("no docker run in %v", runner.commands())
	}
	for _, want := range []string{"--rm", "--label gobase.dbcheck=1", "--tmpfs", "postgres:18-alpine", "timeout 600"} {
		if !strings.Contains(run, want) {
			t.Errorf(
				"docker run lacks %q: a container must be disposable and must end on its own if we are killed\n%s",
				want,
				run,
			)
		}
	}
	if strings.Contains(run, " -p ") || strings.Contains(run, "--publish") {
		t.Errorf("the database must not be reachable from the network: %s", run)
	}
	if pg.Name() == "" || !strings.Contains(run, pg.Name()) {
		t.Errorf("name %q is not in the command: %s", pg.Name(), run)
	}
}

func TestStartPostgresWaitsUntilTheServerAnswers(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	probes := 0
	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if slices.Contains(spec.Args, "pg_isready") {
			mu.Lock()
			defer mu.Unlock()
			probes++
			if probes < 3 {
				return proc.Result{ExitCode: 2, Output: "no response"}, nil
			}
		}
		return proc.Result{}, nil
	}}

	if _, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine"); err != nil {
		t.Fatal(err)
	}
	if probes != 3 {
		t.Errorf("probed %d times, want 3: it must keep asking until the server is up", probes)
	}
}

func TestStartPostgresRemovesTheContainerWhenItNeverBecomesReady(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if slices.Contains(spec.Args, "pg_isready") {
			return proc.Result{ExitCode: 2, Output: "no response"}, nil
		}
		return proc.Result{}, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := dbcheck.StartPostgres(ctx, runner, "postgres:18-alpine"); err == nil {
		t.Fatal("a cancelled start must be an error")
	}
	if _, ok := runner.find("rm -f"); !ok {
		t.Errorf("a half-started container must still be removed: %v", runner.commands())
	}
}

func TestStartPostgresExplainsAMissingDocker(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: func(proc.Spec) (proc.Result, error) { return proc.Result{}, exec.ErrNotFound }}
	_, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine")

	if err == nil || !strings.Contains(err.Error(), "docker") {
		t.Errorf("err = %v, want a message that names docker", err)
	}
}

func TestStopRemovesTheContainerEvenWhenTheCallerIsCancelled(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	pg, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pg.Stop(ctx)

	if _, ok := runner.find("rm -f " + pg.Name()); !ok {
		t.Fatalf("no rm -f in %v", runner.commands())
	}
}

func TestApplyMigrationsRunsEachFileInOrderAndStopsOnTheFirstError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"0002_b.sql", "0001_a.sql", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("select 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if slices.Contains(spec.Args, "/migrations/0001_a.sql") {
			return proc.Result{ExitCode: 3, Output: "ERROR: syntax error at line 4"}, nil
		}
		return proc.Result{}, nil
	}}
	pg, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine")
	if err != nil {
		t.Fatal(err)
	}

	err = pg.ApplyMigrations(t.Context(), "svc_a", dir)

	if err == nil || !strings.Contains(err.Error(), "0001_a.sql") || !strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("err = %v, want the failing file and psql's message", err)
	}
	if _, ran := runner.find("/migrations/0002_b.sql"); ran {
		t.Error("a later migration must not run after an earlier one failed")
	}
	if _, ran := runner.find("notes.txt"); ran {
		t.Error("only .sql files are migrations")
	}
}

func TestExplainSetsSeqscanOffAndReturnsThePlan(t *testing.T) {
	t.Parallel()

	const plan = `[{"Plan":{"Node Type":"Index Scan","Relation Name":"notes"}}]`
	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if slices.ContainsFunc(spec.Args, func(a string) bool { return strings.HasPrefix(a, "EXPLAIN") }) {
			return proc.Result{Output: "NOTICE: x\n" + plan + "\n"}, nil
		}
		return proc.Result{}, nil
	}}
	pg, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine")
	if err != nil {
		t.Fatal(err)
	}

	got, err := pg.Explain(t.Context(), "svc_a", "SELECT id\nFROM   notes WHERE id = $1")
	if err != nil {
		t.Fatal(err)
	}

	if got != plan {
		t.Errorf("plan = %q", got)
	}
	cmd, _ := runner.find("EXPLAIN")
	for _, want := range []string{"PGOPTIONS=-c enable_seqscan=off", "GENERIC_PLAN", "FORMAT JSON", "svc_a", "SELECT id FROM notes WHERE id = $1"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("explain command lacks %q: %s", want, cmd)
		}
	}
}

func TestExplainReportsWhatPostgresSaid(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if slices.ContainsFunc(spec.Args, func(a string) bool { return strings.HasPrefix(a, "EXPLAIN") }) {
			return proc.Result{ExitCode: 1, Output: `ERROR: relation "ghost" does not exist`}, nil
		}
		return proc.Result{}, nil
	}}
	pg, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine")
	if err != nil {
		t.Fatal(err)
	}

	_, err = pg.Explain(t.Context(), "svc_a", "SELECT 1 FROM ghost")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Error("wrong error kind")
	}
}

func TestAPublishedServerListensOnLoopbackOnly(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	if _, err := dbcheck.StartPostgres(
		t.Context(),
		runner,
		"postgres:18-alpine",
		dbcheck.WithPublishedPort(),
	); err != nil {
		t.Fatal(err)
	}

	run, _ := runner.find("run -d")
	if !strings.Contains(run, "-p 127.0.0.1::5432") {
		t.Errorf("a test server must publish on loopback with a random port, never on every interface: %s", run)
	}
}

func TestURLReadsThePortDockerChose(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if len(spec.Args) > 0 && spec.Args[0] == "port" {
			return proc.Result{Output: "127.0.0.1:55432\n[::1]:55432\n"}, nil
		}
		return proc.Result{}, nil
	}}
	pg, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine", dbcheck.WithPublishedPort())
	if err != nil {
		t.Fatal(err)
	}

	got, err := pg.URL(t.Context(), "postgres")
	if err != nil {
		t.Fatal(err)
	}
	if got != "postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable" {
		t.Errorf("url = %q", got)
	}
}

func TestURLRefusesAnUnparsableAnswer(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if len(spec.Args) > 0 && spec.Args[0] == "port" {
			return proc.Result{Output: "no public port\n"}, nil
		}
		return proc.Result{}, nil
	}}
	pg, err := dbcheck.StartPostgres(t.Context(), runner, "postgres:18-alpine", dbcheck.WithPublishedPort())
	if err != nil {
		t.Fatal(err)
	}

	if _, err = pg.URL(t.Context(), "postgres"); err == nil {
		t.Error("a missing port must be an error, not a URL to nowhere")
	}
}
