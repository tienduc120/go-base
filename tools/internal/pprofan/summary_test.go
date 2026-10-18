package pprofan_test

import (
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/pprofan"
)

func TestSummaryLeadsWithTheFindingAndItsNextStep(t *testing.T) {
	t.Parallel()

	p := build(cpuTypes(), "", []stack{
		{[]string{ours + ".slow"}, []int64{9, 90_000_000}},
		{[]string{ours + ".fine"}, []int64{1, 10_000_000}},
	})
	out := analyze(t, p, pprofan.Options{}).Summary(5)

	for _, want := range []string{"cpu-hotspot", ours + ".slow", "90%", "next:", "100ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "cpu-hotspot") > strings.Index(out, "owners") {
		t.Errorf("findings must come before the tables:\n%s", out)
	}
}

func TestSummarySaysSoWhenThereIsNothingToReport(t *testing.T) {
	t.Parallel()

	var stacks []stack
	for _, name := range []string{".a", ".b", ".c", ".d"} {
		stacks = append(stacks, stack{[]string{ours + name}, []int64{1, 25}})
	}
	out := analyze(t, build(cpuTypes(), "", stacks), pprofan.Options{}).Summary(5)

	if !strings.Contains(out, "no findings") {
		t.Errorf("an empty answer must be explicit, not silent:\n%s", out)
	}
}

func TestSummaryRespectsTheLineBudget(t *testing.T) {
	t.Parallel()

	var stacks []stack
	for _, name := range []string{".a", ".b", ".c", ".d", ".e", ".f", ".g", ".h"} {
		stacks = append(stacks, stack{[]string{ours + name}, []int64{1, 12}})
	}
	out := analyze(t, build(cpuTypes(), "", stacks), pprofan.Options{Top: 8}).Summary(3)

	if strings.Contains(out, ours+".h") {
		t.Errorf("only the top 3 rows belong in the summary:\n%s", out)
	}
}

func TestFormatValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		v    int64
		unit string
		want string
	}{
		{1_500_000_000, "nanoseconds", "1.5s"},
		{2_500_000, "nanoseconds", "2.5ms"},
		{4_000, "nanoseconds", "4µs"},
		{3 * 1024 * 1024, "bytes", "3MB"},
		{2048, "bytes", "2KB"},
		{12, "bytes", "12B"},
		{200, "count", "200"},
	}
	for _, tc := range tests {
		if got := pprofan.FormatValue(tc.v, tc.unit); got != tc.want {
			t.Errorf("FormatValue(%d, %q) = %q, want %q", tc.v, tc.unit, got, tc.want)
		}
	}
}
