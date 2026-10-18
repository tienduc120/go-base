package dbcheck_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

const (
	pgRel      = "services/billing/internal/infrastructure/postgres"
	migration  = "CREATE TABLE notes (id text PRIMARY KEY, name text NOT NULL, created_at timestamptz NOT NULL);\n"
	goodPlan   = `[{"Plan":{"Node Type":"Index Scan","Relation Name":"notes"}}]`
	seqPlan    = `[{"Plan":{"Node Type":"Limit","Plans":[{"Node Type":"Seq Scan","Relation Name":"notes"}]}}]`
	queryFile  = "-- name: GetNote :one\nSELECT id, name FROM notes WHERE id = $1;\n\n-- name: ListByName :many\nSELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2;\n"
	starSource = "-- name: GetNote :one\nSELECT " + star + " FROM notes WHERE id = $1;\n"
)

// writeService lays out a service's postgres directory under a fresh repository root.
func writeService(t *testing.T, queries string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(pgRel))
	for name, content := range map[string]string{
		"migrations/0001_init.sql": migration,
		"queries/notes.sql":        queries,
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// stack answers sqlc and docker. compiled is what `sqlc generate` would write; plans maps a query's SQL to its plan.
func stack(t *testing.T, compiled map[string]string, plans map[string]string) *fakeRunner {
	t.Helper()
	return &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		switch spec.Name {
		case "sqlc":
			return proc.Result{}, writeCompiled(spec.Dir, compiled)
		case "docker":
			if plan, ok := planFor(spec.Args, plans); ok {
				return proc.Result{Output: plan}, nil
			}
		}
		return proc.Result{}, nil
	}}
}

// writeCompiled does what `sqlc generate` does for the check: leaves the compiled queries as JSON.
func writeCompiled(pgDir string, compiled map[string]string) error {
	type query struct {
		Name string `json:"name"`
		Cmd  string `json:"cmd"`
		Text string `json:"text"`
	}
	var out struct {
		Queries []query `json:"queries"`
	}
	for name, text := range compiled {
		cmd := ":one"
		if strings.HasPrefix(name, "List") {
			cmd = ":many"
		}
		out.Queries = append(out.Queries, query{Name: name, Cmd: cmd, Text: text})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	target := filepath.Join(pgDir, ".dbcheck", "out")
	if err = os.MkdirAll(target, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(target, "codegen_request.json"), data, 0o600)
}

// planFor answers an EXPLAIN command: the plan whose key appears in the statement, or an indexed plan.
func planFor(args []string, plans map[string]string) (string, bool) {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "EXPLAIN") {
			continue
		}
		for text, plan := range plans {
			if strings.Contains(arg, text) {
				return plan, true
			}
		}
		return goodPlan, true
	}
	return "", false
}

func TestRunReportsNothingForAServiceWithoutQueries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runner := &fakeRunner{}
	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root, Plan: true})

	if err != nil || report.Queries != 0 || len(report.Findings) != 0 {
		t.Errorf("report = %+v, err = %v", report, err)
	}
	if len(runner.commands()) != 0 {
		t.Errorf("no queries means no containers and no sqlc: %v", runner.commands())
	}
}

func TestRunPassesCompliantQueriesWithAnIndexedPlan(t *testing.T) {
	t.Parallel()

	root := writeService(t, queryFile)
	runner := stack(t, map[string]string{
		"GetNote":    "SELECT id, name FROM notes WHERE id = $1",
		"ListByName": "SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2",
	}, nil)

	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root, Plan: true})
	if err != nil {
		t.Fatal(err)
	}

	if report.Queries != 2 || len(report.Findings) != 0 {
		t.Errorf("report = %+v", report)
	}
	if _, ok := runner.find("rm -f"); !ok {
		t.Errorf("the container must be removed when the run ends: %v", runner.commands())
	}
}

func TestRunFindsStaticViolationsWithTheirRepositoryRelativeLocation(t *testing.T) {
	t.Parallel()

	root := writeService(t, starSource)
	runner := stack(t, map[string]string{"GetNote": "SELECT id, name, created_at FROM notes WHERE id = $1"}, nil)

	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Findings) != 1 {
		t.Fatalf("findings = %v", report.Findings)
	}
	f := report.Findings[0]
	if f.Rule != dbcheck.RuleSelectStar || f.Query != "GetNote" || f.Line != 1 ||
		f.File != pgRel+"/queries/notes.sql" {
		t.Errorf("finding = %+v", f)
	}
}

func TestRunWithoutPlanNeverTouchesDocker(t *testing.T) {
	t.Parallel()

	root := writeService(t, queryFile)
	runner := stack(t, map[string]string{
		"GetNote":    "SELECT id, name FROM notes WHERE id = $1",
		"ListByName": "SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2",
	}, nil)

	if _, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root, Plan: false}); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(runner.commands(), func(c string) bool { return strings.HasPrefix(c, "docker") }) {
		t.Errorf("--no-plan must not need Docker: %v", runner.commands())
	}
}

func TestRunAttributesASequentialScanToItsQuery(t *testing.T) {
	t.Parallel()

	root := writeService(t, queryFile)
	runner := stack(t, map[string]string{
		"GetNote":    "SELECT id, name FROM notes WHERE id = $1",
		"ListByName": "SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2",
	}, map[string]string{"WHERE name = $1": seqPlan})

	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root, Plan: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Findings) != 1 {
		t.Fatalf("findings = %v", report.Findings)
	}
	f := report.Findings[0]
	if f.Rule != dbcheck.RuleSeqScan || f.Query != "ListByName" || f.Line != 4 {
		t.Errorf("finding = %+v, want the seq-scan on ListByName at the line of its header", f)
	}
}

func TestRunHonoursAnExplainedAllowOnAPlanFinding(t *testing.T) {
	t.Parallel()

	src := strings.Replace(queryFile, "-- name: ListByName :many\n",
		"-- name: ListByName :many\n-- agent:allow seq-scan: the notes table holds at most fifty rows\n", 1)
	root := writeService(t, src)
	runner := stack(t, map[string]string{
		"GetNote":    "SELECT id, name FROM notes WHERE id = $1",
		"ListByName": "SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2",
	}, map[string]string{"WHERE name = $1": seqPlan})

	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root, Plan: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Errorf("findings = %v", report.Findings)
	}
	if len(report.Allowed) != 1 || report.Allowed[0].Rule != dbcheck.RuleSeqScan {
		t.Errorf("an accepted finding must stay visible, as an allowed one: %+v", report.Allowed)
	}
}

func TestRunReportsAQueryPostgresCannotPlan(t *testing.T) {
	t.Parallel()

	root := writeService(t, "-- name: GetNote :one\nSELECT id FROM notes WHERE id = $1;\n")
	runner := stack(t, map[string]string{"GetNote": "SELECT id FROM notes WHERE id = $1"}, nil)
	inner := runner.handle
	runner.handle = func(spec proc.Spec) (proc.Result, error) {
		if slices.ContainsFunc(spec.Args, func(a string) bool { return strings.HasPrefix(a, "EXPLAIN") }) {
			return proc.Result{ExitCode: 1, Output: "ERROR: could not determine data type of parameter $1"}, nil
		}
		return inner(spec)
	}

	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root, Plan: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Rule != dbcheck.RulePlanFailed {
		t.Errorf("a query that cannot be planned is not a query that is fine: %v", report.Findings)
	}
}

func TestRunSurfacesASqlcFailure(t *testing.T) {
	t.Parallel()

	root := writeService(t, queryFile)
	runner := &fakeRunner{handle: func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "sqlc" {
			return proc.Result{ExitCode: 1, Output: "queries/notes.sql:2:8: column \"nope\" does not exist"}, nil
		}
		return proc.Result{}, nil
	}}

	_, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root})
	if err == nil || !strings.Contains(err.Error(), `column "nope" does not exist`) {
		t.Errorf("err = %v, want sqlc's own message", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(pgRel), ".dbcheck")); statErr == nil {
		t.Error("the scratch directory must be removed even when sqlc fails")
	}
}

func TestRunRefusesAServiceNameWithNoQueries(t *testing.T) {
	t.Parallel()

	root := writeService(t, queryFile)
	_, err := dbcheck.Run(t.Context(), &fakeRunner{}, dbcheck.Options{Root: root, Services: []string{"ghost"}})

	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("err = %v, want the unknown service named", err)
	}
}

// withDiff answers `sqlc diff` with the given exit code and output, and everything else as stack does.
func withDiff(t *testing.T, compiled map[string]string, exit int, output string) *fakeRunner {
	t.Helper()
	runner := stack(t, compiled, nil)
	inner := runner.handle
	runner.handle = func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "sqlc" && len(spec.Args) > 0 && spec.Args[0] == "diff" {
			return proc.Result{ExitCode: exit, Output: output}, nil
		}
		return inner(spec)
	}
	return runner
}

func addSqlcConfig(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(pgRel), "sqlc.yaml")
	if err := os.WriteFile(path, []byte("version: \"2\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunFlagsGeneratedCodeThatNoLongerMatchesItsQueries(t *testing.T) {
	t.Parallel()

	root := writeService(t, queryFile)
	addSqlcConfig(t, root)
	runner := withDiff(t, map[string]string{
		"GetNote":    "SELECT id, name FROM notes WHERE id = $1",
		"ListByName": "SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2",
	}, 1, "--- a/db/notes.sql.go\n+++ b/db/notes.sql.go\n")

	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Findings) != 1 {
		t.Fatalf("findings = %v", report.Findings)
	}
	f := report.Findings[0]
	if f.Rule != dbcheck.RuleGeneratedStale || f.File != pgRel+"/sqlc.yaml" ||
		!strings.Contains(f.Message, "sqlc generate") {
		t.Errorf("finding = %+v, want generated-stale on the sqlc.yaml with the command that fixes it", f)
	}
}

func TestRunAcceptsGeneratedCodeThatIsCurrent(t *testing.T) {
	t.Parallel()

	root := writeService(t, queryFile)
	addSqlcConfig(t, root)
	runner := withDiff(t, map[string]string{
		"GetNote":    "SELECT id, name FROM notes WHERE id = $1",
		"ListByName": "SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2",
	}, 0, "")

	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root})
	if err != nil || len(report.Findings) != 0 {
		t.Errorf("findings = %v, err = %v", report.Findings, err)
	}
	if _, ran := runner.find("sqlc diff"); !ran {
		t.Errorf("a service with a sqlc.yaml must have its generated code compared: %v", runner.commands())
	}
}

func TestRunSkipsTheDiffForAServiceWithoutASqlcConfig(t *testing.T) {
	t.Parallel()

	root := writeService(t, queryFile)
	runner := withDiff(t, map[string]string{
		"GetNote":    "SELECT id, name FROM notes WHERE id = $1",
		"ListByName": "SELECT id FROM notes WHERE name = $1 ORDER BY id LIMIT $2",
	}, 1, "should not be asked")

	report, err := dbcheck.Run(t.Context(), runner, dbcheck.Options{Root: root})
	if err != nil || len(report.Findings) != 0 {
		t.Errorf("findings = %v, err = %v", report.Findings, err)
	}
}
