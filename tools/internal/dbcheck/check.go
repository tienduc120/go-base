package dbcheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

// Options says what to check.
type Options struct {
	// Root is the repository root.
	Root string
	// Services limits the check to these names; empty means every service that has queries.
	Services []string
	// Plan also runs each query on a throwaway Postgres and reads its plan. It needs Docker.
	Plan bool
	// Image is the Postgres image for plans; empty means DefaultImage.
	Image string
}

// Report is the outcome of a check.
type Report struct {
	Services []string
	Queries  int
	Findings []Finding
	// Allowed are findings accepted with an explained agent:allow, kept visible.
	Allowed []Finding
}

type serviceDB struct {
	name string
	// dir is the postgres directory holding migrations/ and queries/.
	dir string
}

func (s serviceDB) database() string { return "svc_" + strings.ReplaceAll(s.name, "-", "_") }

// Run checks the queries of every selected service.
func Run(ctx context.Context, runner proc.Runner, opts Options) (Report, error) {
	services, err := discover(opts.Root, opts.Services)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if len(services) == 0 {
		return report, nil
	}

	var pg *Postgres
	if opts.Plan {
		image := opts.Image
		if image == "" {
			image = DefaultImage
		}
		if pg, err = StartPostgres(ctx, runner, image); err != nil {
			return Report{}, err
		}
		defer pg.Stop(ctx)
	}

	for _, svc := range services {
		report.Services = append(report.Services, svc.name)
		if err = checkService(ctx, runner, pg, opts.Root, svc, &report); err != nil {
			return Report{}, fmt.Errorf("service %s: %w", svc.name, err)
		}
	}
	return report, nil
}

func discover(root string, only []string) ([]serviceDB, error) {
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read services: %w", err)
	}

	var found []serviceDB
	for _, e := range entries {
		dir := filepath.Join(root, "services", e.Name(), "internal", "infrastructure", "postgres")
		if info, statErr := os.Stat(filepath.Join(dir, "queries")); statErr == nil && info.IsDir() {
			found = append(found, serviceDB{name: e.Name(), dir: dir})
		}
	}
	if len(only) == 0 {
		return found, nil
	}

	var picked []serviceDB
	for _, name := range only {
		i := slices.IndexFunc(found, func(s serviceDB) bool { return s.name == name })
		if i < 0 {
			return nil, fmt.Errorf(
				"service %q has no queries directory at services/%s/internal/infrastructure/postgres/queries",
				name,
				name,
			)
		}
		picked = append(picked, found[i])
	}
	return picked, nil
}

func checkService(
	ctx context.Context,
	runner proc.Runner,
	pg *Postgres,
	root string,
	svc serviceDB,
	report *Report,
) error {
	queries, err := readQueries(root, svc)
	if err != nil {
		return err
	}
	compiled, err := compile(ctx, runner, svc.dir)
	if err != nil {
		return err
	}
	stale, err := checkGenerated(ctx, runner, root, svc)
	if err != nil {
		return err
	}
	report.Findings = append(report.Findings, stale...)
	for i := range queries {
		c, ok := compiled[queries[i].Name]
		if !ok {
			return fmt.Errorf("%s:%d: sqlc did not report query %s", queries[i].File, queries[i].Line, queries[i].Name)
		}
		queries[i].Cmd, queries[i].SQL = c.Cmd, c.Text
	}
	report.Queries += len(queries)

	for _, q := range queries {
		violations, accepted := Evaluate(q)
		report.Findings = append(report.Findings, violations...)
		report.Allowed = append(report.Allowed, accepted...)
	}
	if pg == nil {
		return nil
	}
	return planService(ctx, pg, svc, queries, report)
}

// checkGenerated has sqlc compare the generated code with what the queries and migrations now produce. A service with
// no sqlc.yaml of its own has no generated code to compare.
func checkGenerated(ctx context.Context, runner proc.Runner, root string, svc serviceDB) ([]Finding, error) {
	config := filepath.Join(svc.dir, "sqlc.yaml")
	_, err := os.Stat(config)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("check sqlc.yaml: %w", err)
	}
	res, err := runner.Run(ctx, proc.Spec{Dir: svc.dir, Name: "sqlc", Args: []string{"diff"}, Timeout: sqlcTimeout})
	if err != nil {
		return nil, fmt.Errorf("sqlc diff: %w", err)
	}
	if !res.TimedOut && res.ExitCode == 0 {
		return nil, nil
	}

	rel, relErr := filepath.Rel(root, config)
	if relErr != nil {
		rel = config
	}
	return []Finding{{
		File: filepath.ToSlash(rel), Line: 1, Query: "sqlc generate", Rule: RuleGeneratedStale,
		Message: "the generated code differs from what sqlc makes of the queries and migrations; " +
			"run `sqlc generate` in " + filepath.ToSlash(filepath.Dir(rel)) + " and commit the result.",
	}}, nil
}

func readQueries(root string, svc serviceDB) ([]Query, error) {
	files, err := os.ReadDir(filepath.Join(svc.dir, "queries"))
	if err != nil {
		return nil, fmt.Errorf("read queries: %w", err)
	}

	var queries []Query
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".sql") {
			continue
		}
		path := filepath.Join(svc.dir, "queries", f.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name(), readErr)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		blocks, parseErr := ParseSource(filepath.ToSlash(rel), string(data))
		if parseErr != nil {
			return nil, parseErr
		}
		for _, b := range blocks {
			queries = append(queries, Query{File: b.File, Line: b.Line, Name: b.Name, Raw: b.Raw, Allow: b.Allow})
		}
	}
	return queries, nil
}

func planService(ctx context.Context, pg *Postgres, svc serviceDB, queries []Query, report *Report) error {
	db := svc.database()
	if err := pg.CreateDatabase(ctx, db); err != nil {
		return err
	}
	if err := pg.ApplyMigrations(ctx, db, filepath.Join(svc.dir, "migrations")); err != nil {
		return err
	}

	for _, q := range queries {
		findings, err := planQuery(ctx, pg, db, q)
		if err != nil {
			return err
		}
		for _, f := range findings {
			if accepted, ok := q.Resolve(f); ok {
				report.Allowed = append(report.Allowed, accepted)
			} else {
				report.Findings = append(report.Findings, f)
			}
		}
	}
	return nil
}

// planQuery returns the plan findings for q. Postgres rejecting the statement is a finding, since a query that
// cannot be planned cannot be judged; Docker failing is an error.
func planQuery(ctx context.Context, pg *Postgres, db string, q Query) ([]Finding, error) {
	out, err := pg.Explain(ctx, db, q.SQL)
	if rejected, ok := errors.AsType[*SQLError](err); ok {
		return []Finding{
			q.finding(RulePlanFailed, "postgres could not plan this query: "+firstLine(rejected.Output)),
		}, nil
	}
	if err != nil {
		return nil, err
	}

	found, analyzeErr := AnalyzePlan([]byte(out))
	if analyzeErr == nil {
		for i := range found {
			found[i].File, found[i].Line, found[i].Query = q.File, q.Line, q.Name
		}
		return found, nil
	}
	// An unreadable plan is itself the finding: the query could not be judged.
	return []Finding{q.finding(RulePlanFailed, analyzeErr.Error())}, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
