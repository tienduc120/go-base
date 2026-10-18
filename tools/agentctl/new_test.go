package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServiceCreatesTheServiceAndNamesTheNextStep(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	got := invoke(t, environment{cwd: repo}, "new", "service", "billing", "--aggregate", "Invoice")

	if got.code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	domain := filepath.Join(repo, "services", "billing", "internal", "domain", "invoice.go")
	data, err := os.ReadFile(domain)
	if err != nil {
		t.Fatalf("the domain file was not created: %v", err)
	}
	if !strings.Contains(string(data), "type Invoice struct") {
		t.Errorf("the aggregate name was not used:\n%s", data)
	}
	main, _ := os.ReadFile(filepath.Join(repo, "services", "billing", "cmd", "billing-api", "main.go"))
	if !strings.Contains(string(main), `"example.com/x/platform/async"`) {
		t.Errorf("imports should start at the root module path from go.mod:\n%s", main)
	}
	for _, want := range []string{"created services/billing/cmd/billing-api/main.go", "agentctl verify"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, got.stdout)
		}
	}
}

func TestNewServiceAcceptsTheFlagBeforeTheName(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	got := invoke(t, environment{cwd: repo}, "new", "service", "--aggregate", "Invoice", "billing")

	if got.code != 0 {
		t.Fatalf("exit = %d\n%s", got.code, got.stderr)
	}
	if _, err := os.Stat(filepath.Join(repo, "services", "billing", "internal", "domain", "invoice.go")); err != nil {
		t.Errorf("flag-first form should work too: %v", err)
	}
}

func TestNewServiceRefusesAnExistingService(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	if got := invoke(t, environment{cwd: repo}, "new", "service", "billing"); got.code != 0 {
		t.Fatalf("first run: %d %s", got.code, got.stderr)
	}
	got := invoke(t, environment{cwd: repo}, "new", "service", "billing")

	if got.code != 1 || !strings.Contains(got.stderr, "already exists") {
		t.Errorf("exit = %d, stderr = %q, want a refusal that says the file exists", got.code, got.stderr)
	}
}

func TestNewServiceUsageErrors(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"no subcommand":   {"new"},
		"unknown kind":    {"new", "widget", "x"},
		"no name":         {"new", "service"},
		"two names":       {"new", "service", "a", "b"},
		"bad name":        {"new", "service", "Billing"},
		"bad aggregate":   {"new", "service", "billing", "--aggregate", "invoice"},
		"unknown flag":    {"new", "service", "billing", "--nope"},
		"path traversal":  {"new", "service", "../evil"},
		"underscore name": {"new", "service", "order_intake"},
	} {
		repo := newRepo(t)
		if got := invoke(t, environment{cwd: repo}, args...); got.code != 2 {
			t.Errorf("%s: exit = %d, want 2\n%s", name, got.code, got.stderr)
		}
		if _, err := os.Stat(filepath.Join(repo, "services")); err == nil {
			t.Errorf("%s: something was written for a request that was refused", name)
		}
	}
}

func TestNewServiceOutsideTheRepository(t *testing.T) {
	t.Parallel()

	if got := invoke(t, environment{cwd: t.TempDir()}, "new", "service", "billing"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}
