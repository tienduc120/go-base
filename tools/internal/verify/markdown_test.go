package verify_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/verify"
)

func TestPlanIncludesTheMarkdownStyleStep(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"tools/main.go": "package main\n"})
	got := stepNames(verify.Plan(env(t, root, &fakeProc{}), verify.Fast))

	if !slices.Contains(got, "markdown style") {
		t.Errorf("fast plan lacks the markdown style step: %v", got)
	}
}

func TestMarkdownStyleFailsOnWrappedProseAndNamesTheLine(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{
		"tools/main.go": "package main\n",
		"docs/guide.md": "# Guide\n\nA paragraph that was wrapped\nby hand.\n",
	})
	e := env(t, root, &fakeProc{})

	var got verify.Result
	for _, r := range verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast)) {
		if r.Name == "markdown style" {
			got = r
		}
	}

	if got.Pass {
		t.Fatal("hard-wrapped prose must fail the step")
	}
	if !strings.Contains(got.Detail, "guide.md:4") {
		t.Errorf("detail should name the file and line, got %q", got.Detail)
	}
	if !strings.Contains(got.Detail, "agentctl fmt md") {
		t.Errorf("detail should say how to fix it, got %q", got.Detail)
	}
}

func TestMarkdownStylePassesOnTidyDocs(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{
		"tools/main.go": "package main\n",
		"docs/guide.md": "# Guide\n\nOne paragraph on one line.\n\n- a list item\n",
	})
	e := env(t, root, &fakeProc{})

	for _, r := range verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast)) {
		if r.Name == "markdown style" && !r.Pass {
			t.Errorf("tidy docs should pass: %+v", r)
		}
	}
}

func TestMarkdownStyleRunsEvenWithoutGoCode(t *testing.T) {
	t.Parallel()

	root := workspace(t, map[string]string{"README.md": "wrapped\ntext\n"})
	e := env(t, root, &fakeProc{})

	found := false
	for _, r := range verify.Execute(t.Context(), e, verify.Plan(e, verify.Fast)) {
		if r.Name == "markdown style" {
			found = true
			if r.Pass {
				t.Error("docs are checked whether or not the module has Go packages")
			}
		}
	}
	if !found {
		t.Error("no markdown style step")
	}
}
