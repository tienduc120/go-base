package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

func verifyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.work":       "go 1.27.2\n\nuse (\n\t.\n\t./tools\n)\n",
		"go.mod":        "module example.com/root\n\ngo 1.27.2\n",
		"tools/go.mod":  "module example.com/root/tools\n\ngo 1.27.2\n",
		"tools/main.go": "package main\n",
	}
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func allPass(proc.Spec) (proc.Result, error) { return proc.Result{}, nil }

func TestVerifyPasses(t *testing.T) {
	t.Parallel()

	env := environment{cwd: verifyRepo(t), procRunner: procFunc(allPass)}
	got := invoke(t, env, "verify")

	if got.code != 0 {
		t.Fatalf("exit = %d, want 0\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"go test (tools)", "golangci-lint (tools)", "SKIP", "0 failed"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, got.stdout)
		}
	}
	if strings.Contains(got.stdout, "govulncheck") {
		t.Errorf("--fast is the default and must not run govulncheck:\n%s", got.stdout)
	}
}

func TestVerifyFailsWhenAGateFails(t *testing.T) {
	t.Parallel()

	runner := procFunc(func(spec proc.Spec) (proc.Result, error) {
		if spec.Name == "golangci-lint" {
			return proc.Result{ExitCode: 1, Output: "x.go:1:1: bad\n"}, nil
		}
		return proc.Result{}, nil
	})
	got := invoke(t, environment{cwd: verifyRepo(t), procRunner: runner}, "verify")

	if got.code != 1 {
		t.Fatalf("exit = %d, want 1", got.code)
	}
	if !strings.Contains(got.stdout, "FAIL") || !strings.Contains(got.stdout, "x.go:1:1: bad") {
		t.Errorf("output should show the failure tail:\n%s", got.stdout)
	}
}

func TestVerifyFullAddsTheSlowGates(t *testing.T) {
	t.Parallel()

	env := environment{cwd: verifyRepo(t), procRunner: procFunc(allPass)}
	got := invoke(t, env, "verify", "--full")

	if !strings.Contains(got.stdout, "govulncheck (tools)") {
		t.Errorf("--full should run govulncheck:\n%s", got.stdout)
	}
}

func TestVerifyOutsideRepo(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{cwd: t.TempDir(), procRunner: procFunc(allPass)}, "verify")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}

func TestVerifyRejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	got := invoke(t, environment{cwd: verifyRepo(t), procRunner: procFunc(allPass)}, "verify", "--bogus")
	if got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}
