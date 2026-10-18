package dbcheck_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

// requireTools skips when sqlc or Docker is missing, loudly, unless AGENTCTL_REQUIRE_DB is set, which CI sets so a
// gate that cannot run fails instead of passing by absence.
func requireTools(t *testing.T) {
	t.Helper()

	problem := ""
	if _, err := exec.LookPath("sqlc"); err != nil {
		problem = "sqlc is not installed"
	} else if _, err = exec.LookPath("docker"); err != nil {
		problem = "docker is not installed"
	} else if res, runErr := (proc.Exec{}).Run(t.Context(), proc.Spec{
		Name: "docker", Args: []string{"info", "--format", "{{.ServerVersion}}"}, Timeout: 30 * time.Second,
	}); runErr != nil || res.ExitCode != 0 {
		problem = "the docker daemon is not running"
	}

	switch {
	case problem == "":
	case os.Getenv("AGENTCTL_REQUIRE_DB") != "":
		t.Fatalf("%s, and AGENTCTL_REQUIRE_DB is set: the query gate cannot be proven here", problem)
	default:
		t.Skipf(
			"%s: the query gate's fixtures were NOT run (set AGENTCTL_REQUIRE_DB=1 to make this a failure)",
			problem,
		)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(to, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// stage lays every fixture out as its own service under one root, so one container serves the whole run. A
// violation fixture's directory name is the rule it must trip.
func stage(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()

	place := func(id, queries string) string {
		pg := filepath.Join(root, "services", id, "internal", "infrastructure", "postgres")
		copyFile(t, queries, filepath.Join(pg, "queries", "queries.sql"))
		copyFile(
			t,
			filepath.Join("testdata", "schema", "0001_init.sql"),
			filepath.Join(pg, "migrations", "0001_init.sql"),
		)
		return pg
	}
	place("clean", filepath.Join("testdata", "clean", "queries.sql"))
	place("allowed", filepath.Join("testdata", "allowed", "queries.sql"))

	entries, err := os.ReadDir(filepath.Join("testdata", "violations"))
	if err != nil {
		t.Fatal(err)
	}
	var violations []string
	for _, e := range entries {
		dir := filepath.Join("testdata", "violations", e.Name())
		pg := place(e.Name(), filepath.Join(dir, "queries.sql"))
		// A fixture may bring its own sqlc.yaml, for the rules about generated code.
		if _, statErr := os.Stat(filepath.Join(dir, "sqlc.yaml")); statErr == nil {
			copyFile(t, filepath.Join(dir, "sqlc.yaml"), filepath.Join(pg, "sqlc.yaml"))
		}
		violations = append(violations, e.Name())
	}
	return root, violations
}

// inService keeps the findings of one staged service, whose files all live under services/<id>/.
func inService(list []dbcheck.Finding, id string) []dbcheck.Finding {
	var out []dbcheck.Finding
	for _, f := range list {
		if strings.HasPrefix(f.File, "services/"+id+"/") {
			out = append(out, f)
		}
	}
	return out
}

// TestFixturesAgainstRealSqlcAndPostgres is the proof that each rule fires, that the compliant twin stays clean, and
// that the plan analysis works on real EXPLAIN output and not only on the JSON we imagined.
func TestFixturesAgainstRealSqlcAndPostgres(t *testing.T) {
	t.Parallel()
	requireTools(t)

	root, violations := stage(t)
	report, err := dbcheck.Run(t.Context(), proc.Exec{}, dbcheck.Options{Root: root, Plan: true})
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range violations {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			if !slices.ContainsFunc(
				inService(report.Findings, id),
				func(f dbcheck.Finding) bool { return f.Rule == id },
			) {
				t.Errorf("rule %s did not fire on its fixture; findings: %v", id, inService(report.Findings, id))
			}
		})
	}
	t.Run("clean twin", func(t *testing.T) {
		t.Parallel()
		if got := inService(report.Findings, "clean"); len(got) != 0 {
			t.Errorf("the compliant queries must pass every rule and every plan check: %v", got)
		}
	})
	t.Run("an explained allow is accepted and still reported", func(t *testing.T) {
		t.Parallel()
		checkAccepted(t, report)
	})
	t.Run("a hidden full index walk is caught too", func(t *testing.T) {
		t.Parallel()
		checkIndexWalk(t, report)
	})
}

func checkAccepted(t *testing.T, report dbcheck.Report) {
	t.Helper()
	if got := inService(report.Findings, "allowed"); len(got) != 0 {
		t.Errorf("findings = %v", got)
	}
	if got := inService(report.Allowed, "allowed"); len(got) != 1 || got[0].Rule != dbcheck.RuleSeqScan {
		t.Errorf("allowed = %v", got)
	}
}

func checkIndexWalk(t *testing.T, report dbcheck.Report) {
	t.Helper()
	hits := map[string]bool{}
	for _, f := range inService(report.Findings, "seq-scan") {
		hits[f.Query] = f.Rule == dbcheck.RuleSeqScan
	}
	if !hits["ListNotesByName"] || !hits["ListNotesByNameOrdered"] {
		t.Errorf("both the plain scan and the ordered primary-key walk must be flagged: %v", hits)
	}
}
