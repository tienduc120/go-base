package runcmd_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tienduc120/go-base/tools/internal/runcmd"
)

func TestInjectGoTestTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmd     []string
		timeout time.Duration
		want    []string
	}{
		{
			"adds a limit just under the wrapper's",
			[]string{"go", "test", "./..."},
			10 * time.Minute,
			[]string{"go", "test", "-timeout", "590s", "./..."},
		},
		{
			"keeps flags that were already there",
			[]string{"go", "test", "-race", "-run", "TestX", "./pkg"},
			5 * time.Minute,
			[]string{"go", "test", "-timeout", "290s", "-race", "-run", "TestX", "./pkg"},
		},
		{
			"a short wrapper limit halves instead of subtracting",
			[]string{"go", "test", "./..."},
			15 * time.Second,
			[]string{"go", "test", "-timeout", "7s", "./..."},
		},
		{
			"respects an explicit limit",
			[]string{"go", "test", "-timeout=2m", "./..."},
			10 * time.Minute,
			[]string{"go", "test", "-timeout=2m", "./..."},
		},
		{
			"respects an explicit limit given as two words",
			[]string{"go", "test", "-timeout", "30s", "./..."},
			10 * time.Minute,
			[]string{"go", "test", "-timeout", "30s", "./..."},
		},
		{"leaves go build alone", []string{"go", "build", "./..."}, time.Minute, []string{"go", "build", "./..."}},
		{"leaves other tools alone", []string{"golangci-lint", "run"}, time.Minute, []string{"golangci-lint", "run"}},
		{"leaves a bare go alone", []string{"go"}, time.Minute, []string{"go"}},
		{"no wrapper limit means no injection", []string{"go", "test", "./..."}, 0, []string{"go", "test", "./..."}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runcmd.InjectGoTestTimeout(tc.cmd, tc.timeout)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInjectGoTestTimeoutDoesNotMutateItsInput(t *testing.T) {
	t.Parallel()

	in := []string{"go", "test", "./..."}
	_ = runcmd.InjectGoTestTimeout(in, time.Minute)
	if !slices.Equal(in, []string{"go", "test", "./..."}) {
		t.Errorf("input was modified: %v", in)
	}
}

func TestTailKeepsTheLastLines(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := range 50 {
		sb.WriteString("line ")
		sb.WriteRune(rune('a' + i%26))
		sb.WriteString("\n")
	}
	sb.WriteString("LAST\n")

	got := runcmd.Tail(sb.String(), 5)
	lines := strings.Split(got, "\n")

	if !strings.Contains(got, "LAST") {
		t.Errorf("the tail must end with the real last line: %q", got)
	}
	if len(lines) > 7 {
		t.Errorf("got %d lines, want the marker plus 5", len(lines))
	}
	if !strings.Contains(lines[0], "earlier") {
		t.Errorf("a cut tail should say how much was dropped: %q", lines[0])
	}
}

func TestTailLeavesShortOutputAlone(t *testing.T) {
	t.Parallel()

	if got := runcmd.Tail("a\nb\n", 5); got != "a\nb" {
		t.Errorf("got %q", got)
	}
	if got := runcmd.Tail("", 5); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"go test ./...":            "go-test",
		"golangci-lint run":        "golangci-lint-run",
		"C:/Program Files/x.exe a": "c-program-files-x-exe-a",
	}
	for in, want := range tests {
		if got := runcmd.Slug(strings.Fields(in)); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
