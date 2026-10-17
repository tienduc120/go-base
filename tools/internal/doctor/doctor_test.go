package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type fakeResult struct {
	out string
	err error
}

type fakeRunner map[string]fakeResult

func (f fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	res, ok := f[strings.Join(append([]string{name}, args...), " ")]
	if !ok {
		return "", exec.ErrNotFound
	}
	return res.out, res.err
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.27.2", "1.27.2", 0},
		{"patch newer", "1.27.3", "1.27.2", 1},
		{"minor older", "1.26.9", "1.27.0", -1},
		{"numeric not lexical", "1.9.0", "1.10.0", -1},
		{"missing patch is zero", "1.27", "1.27.0", 0},
		{"leading v ignored", "v2.15.0", "2.15.0", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compareVersions(tc.a, tc.b)
			if err != nil {
				t.Fatalf("compareVersions(%q, %q) error: %v", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestCompareVersionsRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := compareVersions("abc", "1.0.0"); err == nil {
		t.Fatal("want an error for a non-numeric version")
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	tool := Tool{Name: "demo", Command: []string{"demo", "--version"}, Min: "2.0.0", Required: true, Install: "get demo"}

	tests := []struct {
		name   string
		runner fakeRunner
		tool   Tool
		status Status
		found  string
	}{
		{"exact version", fakeRunner{"demo --version": {out: "demo 2.0.0"}}, tool, StatusOK, "2.0.0"},
		{"newer version", fakeRunner{"demo --version": {out: "demo 2.3.1"}}, tool, StatusOK, "2.3.1"},
		{"older version", fakeRunner{"demo --version": {out: "demo 1.9.9"}}, tool, StatusOutdated, "1.9.9"},
		{"not installed", fakeRunner{}, tool, StatusMissing, ""},
		{"command fails", fakeRunner{"demo --version": {err: errors.New("boom")}}, tool, StatusError, ""},
		{"deadline exceeded", fakeRunner{"demo --version": {err: context.DeadlineExceeded}}, tool, StatusError, ""},
		{"unparseable output", fakeRunner{"demo --version": {out: "no digits here"}}, tool, StatusError, ""},
		{
			"presence only skips parsing",
			fakeRunner{"demo --help": {out: "usage"}},
			Tool{Name: "demo", Command: []string{"demo", "--help"}, Required: true, Install: "get demo"},
			StatusOK, "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Check(context.Background(), tc.runner, []Tool{tc.tool}, time.Second)
			if len(got) != 1 {
				t.Fatalf("got %d results, want 1", len(got))
			}
			if got[0].Status != tc.status {
				t.Errorf("status = %q, want %q (detail: %s)", got[0].Status, tc.status, got[0].Detail)
			}
			if got[0].Found != tc.found {
				t.Errorf("found = %q, want %q", got[0].Found, tc.found)
			}
		})
	}
}

func TestCheckMissingCarriesInstallHint(t *testing.T) {
	t.Parallel()

	tool := Tool{Name: "demo", Command: []string{"demo", "--version"}, Min: "1.0.0", Install: "scoop install demo"}
	got := Check(context.Background(), fakeRunner{}, []Tool{tool}, time.Second)

	if !strings.Contains(got[0].Detail, "scoop install demo") {
		t.Errorf("detail %q does not carry the install hint", got[0].Detail)
	}
}

func TestCheckCustomPattern(t *testing.T) {
	t.Parallel()

	tool := Tool{
		Name:    "vuln",
		Command: []string{"vuln", "-version"},
		Pattern: regexp.MustCompile(`scanner@v(\d+\.\d+\.\d+)`),
		Min:     "1.0.0",
	}
	runner := fakeRunner{"vuln -version": {out: "Go: go1.27.1 Scanner: scanner@v1.9.0"}}

	got := Check(context.Background(), runner, []Tool{tool}, time.Second)
	if got[0].Found != "1.9.0" {
		t.Errorf("found = %q, want 1.9.0 (must not pick the Go version)", got[0].Found)
	}
}

func TestFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []Result
		want    bool
	}{
		{"all ok", []Result{{Status: StatusOK, Required: true}}, false},
		{"required missing", []Result{{Status: StatusMissing, Required: true}}, true},
		{"required outdated", []Result{{Status: StatusOutdated, Required: true}}, true},
		{"required error", []Result{{Status: StatusError, Required: true}}, true},
		{"optional missing", []Result{{Status: StatusMissing, Required: false}}, false},
		{"empty", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Failed(tc.results); got != tc.want {
				t.Errorf("Failed() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Samples are the real --version outputs of the pinned tools on the author's machine.
func TestDefaultToolsParseRealOutput(t *testing.T) {
	t.Parallel()

	samples := map[string]struct{ out, want string }{
		"go":            {"go version go1.27.2 windows/amd64", "1.27.2"},
		"golangci-lint": {"golangci-lint has version 2.15.0 built with go1.27.0 from 25c5369a on 2026-10-16T11:03:09Z", "2.15.0"},
		"sqlc":          {"v1.31.1", "1.31.1"},
		"buf":           {"1.74.0", "1.74.0"},
		"lefthook":      {"2.2.2", "2.2.2"},
		"gofumpt":       {"v0.12.0 (go1.27.1)", "0.12.0"},
		"govulncheck":   {"Go: go1.27.1 Scanner: govulncheck@v1.9.0", "1.9.0"},
		"go-arch-lint":  {"Linter version: \x1b[33mv1.19.0\x1b[0m Supported go arch file versions: \x1b[33m1 .. 3\x1b[0m", "1.19.0"},
	}

	for _, tool := range DefaultTools("1.27.2") {
		sample, ok := samples[tool.Name]
		if !ok {
			continue
		}
		t.Run(tool.Name, func(t *testing.T) {
			t.Parallel()
			runner := fakeRunner{strings.Join(tool.Command, " "): {out: sample.out}}
			got := Check(context.Background(), runner, []Tool{tool}, time.Second)
			if got[0].Found != sample.want {
				t.Errorf("found = %q, want %q (status %s, detail %s)", got[0].Found, sample.want, got[0].Status, got[0].Detail)
			}
		})
	}
}

func TestDefaultToolsAreWellFormed(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, tool := range DefaultTools("1.27.2") {
		if seen[tool.Name] {
			t.Errorf("duplicate tool %q", tool.Name)
		}
		seen[tool.Name] = true
		if len(tool.Command) == 0 {
			t.Errorf("%s has no command", tool.Name)
		}
		if tool.Install == "" {
			t.Errorf("%s has no install hint", tool.Name)
		}
	}
	if !seen["go"] {
		t.Error("the Go toolchain must be checked")
	}
}

func TestDefaultToolsPinGoToTheDirective(t *testing.T) {
	t.Parallel()

	for _, tool := range DefaultTools("1.27.2") {
		if tool.Name == "go" && tool.Min != "1.27.2" {
			t.Errorf("go min = %q, want the go.mod directive 1.27.2", tool.Min)
		}
	}
}

func TestGoDirective(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{"plain", "module x\n\ngo 1.27.2\n", "1.27.2", false},
		{"with toolchain line", "module x\n\ngo 1.27.2\ntoolchain go1.27.3\n", "1.27.2", false},
		{"no directive", "module x\n", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := GoDirective(dir)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("GoDirective = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.27.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := FindRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
}

func TestFindRootNotFound(t *testing.T) {
	t.Parallel()

	if _, err := FindRoot(t.TempDir()); err == nil {
		t.Fatal("want an error when no go.work exists above the start directory")
	}
}

func TestRender(t *testing.T) {
	t.Parallel()

	results := []Result{
		{Name: "go", Status: StatusOK, Found: "1.27.2", Want: "1.27.2", Required: true},
		{Name: "sqlc", Status: StatusMissing, Required: true, Detail: "install: scoop install sqlc"},
	}
	var sb strings.Builder
	if err := Render(&sb, results); err != nil {
		t.Fatal(err)
	}
	out := sb.String()

	for _, want := range []string{"go", "1.27.2", "sqlc", "missing", "scoop install sqlc"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
