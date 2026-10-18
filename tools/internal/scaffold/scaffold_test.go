package scaffold_test

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/scaffold"
)

const testModule = "example.com/mono"

func paths(files []scaffold.File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func find(t *testing.T, files []scaffold.File, suffix string) string {
	t.Helper()
	for _, f := range files {
		if strings.HasSuffix(f.Path, suffix) {
			return string(f.Content)
		}
	}
	t.Fatalf("no generated file ends in %q, have %v", suffix, paths(files))
	return ""
}

func TestGenerateProducesTheWholeLayeredService(t *testing.T) {
	t.Parallel()

	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "billing", Aggregate: "Invoice"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"services/billing/cmd/billing-api/main.go",
		"services/billing/cmd/billing-api/main_test.go",
		"services/billing/internal/domain/invoice.go",
		"services/billing/internal/domain/invoice_test.go",
		"services/billing/internal/application/create_invoice.go",
		"services/billing/internal/application/create_invoice_test.go",
		"services/billing/internal/infrastructure/memory/invoice_repository.go",
		"services/billing/internal/infrastructure/memory/invoice_repository_test.go",
		"services/billing/internal/infrastructure/postgres/migrations/0001_init.sql",
		"services/billing/internal/infrastructure/postgres/queries/invoice.sql",
		"services/billing/internal/infrastructure/postgres/sqlc.yaml",
		"services/billing/internal/infrastructure/postgres/invoice_repository.go",
		"services/billing/internal/infrastructure/postgres/invoice_repository_test.go",
		"services/billing/cmd/billing-load/main.go",
		"services/billing/cmd/billing-load/main_test.go",
		"services/billing/cmd/billing-load/scenario.go",
		"services/billing/cmd/billing-load/scenario_test.go",
		"services/billing/internal/interfaces/rest/handler.go",
		"services/billing/internal/interfaces/rest/handler_test.go",
	}
	got := paths(files)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("files:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEveryProductionFileHasATestBesideIt(t *testing.T) {
	t.Parallel()

	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, f := range files {
		have[f.Path] = true
	}
	for _, f := range files {
		if strings.HasSuffix(f.Path, "_test.go") || !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		if !have[strings.TrimSuffix(f.Path, ".go")+"_test.go"] {
			t.Errorf("%s has no test file beside it: a generated service starts with its logic covered", f.Path)
		}
	}
}

func TestGenerateRootsImportsAtTheGivenModule(t *testing.T) {
	t.Parallel()

	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	main := find(t, files, "cmd/billing-api/main.go")
	for _, want := range []string{
		`"example.com/mono/platform/async"`,
		`"example.com/mono/platform/debugserver"`,
		`"example.com/mono/services/billing/internal/application"`,
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go lacks import %s", want)
		}
	}
	if strings.Contains(main, "[[") || strings.Contains(main, "]]") {
		t.Error("an unreplaced template action is left in main.go")
	}
}

func TestAggregateNamingFollowsTheName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		aggregate, file, route, words string
	}{
		{"Invoice", "invoice.go", "/invoices", "invoice id"},
		{"OrderLine", "order_line.go", "/order-lines", "order line id"},
		{"Category", "category.go", "/categories", "category id"},
		{"Box", "box.go", "/boxes", "box id"},
		{"Day", "day.go", "/days", "day id"},
	} {
		files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "shop", Aggregate: tc.aggregate})
		if err != nil {
			t.Fatalf("%s: %v", tc.aggregate, err)
		}
		if !strings.Contains(find(t, files, "internal/domain/"+tc.file), tc.words) {
			t.Errorf("%s: domain does not say %q", tc.aggregate, tc.words)
		}
		if !strings.Contains(find(t, files, "rest/handler.go"), `"POST `+tc.route+`"`) {
			t.Errorf("%s: handler does not route %s", tc.aggregate, tc.route)
		}
	}
}

func TestTheAggregateDefaultsToTheServiceName(t *testing.T) {
	t.Parallel()

	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "order-intake"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(find(t, files, "internal/domain/order_intake.go"), "type OrderIntake struct") {
		t.Error("with no aggregate named, the service name in PascalCase should be used")
	}
}

func TestGenerateRefusesBadNames(t *testing.T) {
	t.Parallel()

	for name, spec := range map[string]scaffold.Spec{
		"no module":         {Service: "billing"},
		"empty service":     {Module: testModule},
		"uppercase service": {Module: testModule, Service: "Billing"},
		"underscore":        {Module: testModule, Service: "order_intake"},
		"path traversal":    {Module: testModule, Service: "../evil"},
		"leading hyphen":    {Module: testModule, Service: "-billing"},
		"digit first":       {Module: testModule, Service: "1billing"},
		"lowercase agg":     {Module: testModule, Service: "billing", Aggregate: "invoice"},
		"agg with space":    {Module: testModule, Service: "billing", Aggregate: "Invoice Line"},
		"agg with dot":      {Module: testModule, Service: "billing", Aggregate: "Invoice.X"},
	} {
		if files, err := scaffold.Generate(spec); err == nil {
			t.Errorf("%s: generated %d files, want an error", name, len(files))
		}
	}
}

func TestWriteCreatesTheFilesAndRefusesToOverwrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if err = scaffold.Write(root, files); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
		if readErr != nil || !bytes.Equal(data, f.Content) {
			t.Errorf("%s was not written as generated: %v", f.Path, readErr)
		}
	}

	edited := filepath.Join(root, "services", "billing", "internal", "domain", "billing.go")
	if err = os.WriteFile(edited, []byte("package domain // hand-written\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = scaffold.Write(root, files); err == nil {
		t.Error("a second Write must refuse: generated code never replaces hand-written code")
	}
	if data, _ := os.ReadFile(edited); !strings.Contains(string(data), "hand-written") {
		t.Error("the existing file was overwritten")
	}
}

func TestWriteChangesNothingWhenOneFileAlreadyExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	last := files[len(files)-1]
	blocker := filepath.Join(root, filepath.FromSlash(last.Path))
	if err = os.MkdirAll(filepath.Dir(blocker), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(blocker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err = scaffold.Write(root, files); err == nil {
		t.Fatal("want an error for the existing file")
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(files[0].Path))); statErr == nil {
		t.Error("files were written before the conflict was found: a refused scaffold must leave nothing half-made")
	}
}

var moduleLine = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// TestCommittedWitnessMatchesTheTemplates keeps services/example a true product of the templates. The witness runs
// through every gate with the rest of the repository, so a template that stops meeting a gate fails there, and a
// template edited without regenerating the witness fails here.
func TestCommittedWitnessMatchesTheTemplates(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..", "..")
	gomod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	match := moduleLine.FindSubmatch(gomod)
	if match == nil {
		t.Fatal("no module line in the root go.mod")
	}

	files, err := scaffold.Generate(scaffold.Spec{Module: string(match[1]), Service: "example", Aggregate: "Note"})
	if err != nil {
		t.Fatal(err)
	}
	generated := map[string][]byte{}
	for _, f := range files {
		generated[f.Path] = f.Content
		committed, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(f.Path)))
		if readErr != nil {
			t.Errorf(
				"%s is missing from the witness: delete services/example, then run `agentctl new service example --aggregate Note`",
				f.Path,
			)
			continue
		}
		if !bytes.Equal(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n")), f.Content) {
			t.Errorf("%s differs from what the templates generate; regenerate the witness", f.Path)
		}
	}

	walkErr := fs.WalkDir(os.DirFS(repo), "services/example", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, ok := generated[path.Clean(p)]; ok {
			return nil
		}
		// sqlc, not a template, writes the query code; it must still say so, or someone has edited it by hand.
		if strings.HasPrefix(p, "services/example/"+scaffold.PostgresDir+"/db/") {
			if head, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(p))); readErr != nil ||
				!bytes.HasPrefix(head, []byte("// Code generated by sqlc.")) {
				t.Errorf("%s is in the db directory but is not sqlc output", p)
			}
			return nil
		}
		t.Errorf("%s is in the witness but no template makes it", p)
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
}

func TestTheTableAndRouteFollowTheAggregate(t *testing.T) {
	t.Parallel()

	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "shop", Aggregate: "OrderLine"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(find(t, files, "migrations/0001_init.sql"), "CREATE TABLE order_lines (") {
		t.Error("the table should be the plural snake case of the aggregate")
	}
	queries := find(t, files, "queries/order_line.sql")
	for _, want := range []string{"-- name: CreateOrderLine :exec", "-- name: GetOrderLine :one", "FROM order_lines WHERE id = $1"} {
		if !strings.Contains(queries, want) {
			t.Errorf("queries lack %q:\n%s", want, queries)
		}
	}
}

func TestGeneratedSQLAndYAMLAreNotRunThroughTheGoFormatter(t *testing.T) {
	t.Parallel()

	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(find(t, files, "sqlc.yaml"), "version:") {
		t.Error("sqlc.yaml should be plain YAML")
	}
}

func TestTheLoadScenarioIsAPlaceholderThatFailsUntilItIsWritten(t *testing.T) {
	t.Parallel()

	files, err := scaffold.Generate(scaffold.Spec{Module: testModule, Service: "billing"})
	if err != nil {
		t.Fatal(err)
	}

	scenario := find(t, files, "cmd/billing-load/scenario.go")
	if !strings.Contains(scenario, "scenario not written") {
		t.Error("a load run before the scenario exists must fail, or it reports a healthy run of nothing")
	}
	if strings.Contains(scenario, "http.NewRequest") && !strings.Contains(scenario, "//   - build each request") {
		t.Error("the skeleton must not ship a sample call against the example API; projects write their own")
	}
}
