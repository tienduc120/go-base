package pprofan_test

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/google/pprof/profile"

	"github.com/tienduc120/go-base/tools/internal/pprofan"
)

const ours = "github.com/acme/shop"

type stack struct {
	frames []string // leaf first
	values []int64
}

func build(sampleTypes [][2]string, defaultType string, stacks []stack) *profile.Profile {
	p := &profile.Profile{DefaultSampleType: defaultType}
	for _, st := range sampleTypes {
		p.SampleType = append(p.SampleType, &profile.ValueType{Type: st[0], Unit: st[1]})
	}

	locs := map[string]*profile.Location{}
	next := uint64(1)
	locationFor := func(name string) *profile.Location {
		if l, ok := locs[name]; ok {
			return l
		}
		fn := &profile.Function{ID: next, Name: name, Filename: "file.go", StartLine: 10}
		loc := &profile.Location{ID: next, Line: []profile.Line{{Function: fn, Line: 12}}}
		p.Function = append(p.Function, fn)
		p.Location = append(p.Location, loc)
		locs[name] = loc
		next++
		return loc
	}
	for _, s := range stacks {
		var ls []*profile.Location
		for _, f := range s.frames {
			ls = append(ls, locationFor(f))
		}
		p.Sample = append(p.Sample, &profile.Sample{Location: ls, Value: s.values})
	}
	return p
}

func cpuTypes() [][2]string { return [][2]string{{"samples", "count"}, {"cpu", "nanoseconds"}} }

func heapTypes() [][2]string {
	return [][2]string{
		{"alloc_objects", "count"}, {"alloc_space", "bytes"}, {"inuse_objects", "count"}, {"inuse_space", "bytes"},
	}
}

func delayTypes() [][2]string { return [][2]string{{"contentions", "count"}, {"delay", "nanoseconds"}} }

func analyze(t *testing.T, p *profile.Profile, opts pprofan.Options) pprofan.Report {
	t.Helper()
	if opts.Prefixes == nil {
		opts.Prefixes = []string{ours}
	}
	r, err := pprofan.Analyze(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.05 }

func TestFlatCumulativeAndOwners(t *testing.T) {
	t.Parallel()

	p := build(cpuTypes(), "", []stack{
		{[]string{"runtime.mallocgc", ours + ".parse", ours + ".handle"}, []int64{6, 60}},
		{[]string{ours + ".render", ours + ".handle"}, []int64{3, 30}},
		{[]string{"runtime.gcBgMarkWorker"}, []int64{1, 10}},
	})
	r := analyze(t, p, pprofan.Options{})

	if r.Profile.Kind != "cpu" || r.Profile.Unit != "nanoseconds" || r.Profile.Total != 100 {
		t.Fatalf("profile = %+v, want cpu / nanoseconds / 100", r.Profile)
	}

	// Flat: the runtime leaf is the biggest, even though it is not ours.
	if r.Hotspots[0].Function != "runtime.mallocgc" || r.Hotspots[0].Ours || !near(r.Hotspots[0].FlatPct, 60) {
		t.Errorf("top hotspot = %+v", r.Hotspots[0])
	}
	// Cumulative: handle sits under 90 of the 100.
	var handle *pprofan.Hotspot
	for i := range r.Hotspots {
		if r.Hotspots[i].Function == ours+".handle" {
			handle = &r.Hotspots[i]
		}
	}
	if handle == nil || !near(handle.CumPct, 90) || handle.Flat != 0 || !handle.Ours {
		t.Errorf("handle = %+v, want cum 90%%, flat 0, ours", handle)
	}

	// Owners blame the nearest frame in our code, so runtime time is charged to its caller.
	if r.Owners[0].Function != ours+".parse" || !near(r.Owners[0].Pct, 60) {
		t.Errorf("top owner = %+v, want parse at 60%%", r.Owners[0])
	}
	last := r.Owners[len(r.Owners)-1]
	if last.Function != pprofan.OutsideOurs || !near(last.Pct, 10) {
		t.Errorf("last owner = %+v, want the outside bucket at 10%%", last)
	}
}

func TestMainPackageCountsAsOurs(t *testing.T) {
	t.Parallel()

	p := build(cpuTypes(), "", []stack{{[]string{"main.work"}, []int64{1, 10}}})
	r := analyze(t, p, pprofan.Options{})

	if !r.Hotspots[0].Ours || r.Owners[0].Function != "main.work" {
		t.Errorf("main is the program's own code: %+v / %+v", r.Hotspots[0], r.Owners[0])
	}
}

func TestKindDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		types [][2]string
		hint  string
		want  string
	}{
		{"cpu", cpuTypes(), "", "cpu"},
		{"heap", heapTypes(), "", "heap"},
		{"mutex by hint", delayTypes(), "mutex", "mutex"},
		{"block by hint", delayTypes(), "block", "block"},
		{"delay without a hint", delayTypes(), "", "contention"},
		{"goroutine", [][2]string{{"goroutine", "count"}}, "", "goroutine"},
		{"goroutine leak by hint", [][2]string{{"goroutine", "count"}}, "goroutineleak", "goroutineleak"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := build(tc.types, "", []stack{{[]string{ours + ".f"}, make([]int64, len(tc.types))}})
			r := analyze(t, p, pprofan.Options{Hint: tc.hint})
			if r.Profile.Kind != tc.want {
				t.Errorf("kind = %q, want %q", r.Profile.Kind, tc.want)
			}
		})
	}
}

func TestUnknownProfileIsAnError(t *testing.T) {
	t.Parallel()

	p := build([][2]string{{"mystery", "widgets"}}, "", nil)
	if _, err := pprofan.Analyze(p, pprofan.Options{}); err == nil {
		t.Fatal("a profile of no known kind must not be guessed at")
	}
}

func TestDefaultSampleTypeDecidesTheMeasure(t *testing.T) {
	t.Parallel()

	stacks := []stack{
		{[]string{ours + ".cache"}, []int64{1, 100, 1, 900}},
		{[]string{ours + ".churn"}, []int64{9, 900, 0, 100}},
	}

	inuse := analyze(t, build(heapTypes(), "inuse_space", stacks), pprofan.Options{})
	if inuse.Owners[0].Function != ours+".cache" || inuse.Profile.Measure != "inuse_space" {
		t.Errorf("in-use view should blame the retainer: %+v / %s", inuse.Owners[0], inuse.Profile.Measure)
	}
	alloc := analyze(t, build(heapTypes(), "alloc_space", stacks), pprofan.Options{})
	if alloc.Owners[0].Function != ours+".churn" || alloc.Profile.Measure != "alloc_space" {
		t.Errorf("allocation view should blame the churner: %+v / %s", alloc.Owners[0], alloc.Profile.Measure)
	}
}

func kinds(r pprofan.Report) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Kind)
	}
	return out
}

func TestFindingsNameTheOwnerAndSuggestTheNextStep(t *testing.T) {
	t.Parallel()

	p := build(cpuTypes(), "", []stack{
		{[]string{ours + ".slow"}, []int64{7, 70}},
		{[]string{ours + ".fine"}, []int64{3, 30}},
	})
	r := analyze(t, p, pprofan.Options{})

	if len(r.Findings) != 1 || r.Findings[0].Kind != "cpu-hotspot" {
		t.Fatalf("findings = %v, want one cpu-hotspot", kinds(r))
	}
	f := r.Findings[0]
	if f.Where != ours+".slow" || !near(f.Share, 70) {
		t.Errorf("finding = %+v", f)
	}
	if !strings.Contains(f.Message, ours+".slow") || f.Next == "" {
		t.Errorf("a finding must say where and what to do next: %+v", f)
	}
}

func TestNoFindingWhenTheCostIsSpreadOut(t *testing.T) {
	t.Parallel()

	var stacks []stack
	for _, name := range []string{".a", ".b", ".c", ".d"} {
		stacks = append(stacks, stack{[]string{ours + name}, []int64{1, 25}})
	}
	if r := analyze(t, build(cpuTypes(), "", stacks), pprofan.Options{}); len(r.Findings) != 0 {
		t.Errorf("an even profile has no hotspot, got %v", kinds(r))
	}
}

func TestFindingKindFollowsTheProfile(t *testing.T) {
	t.Parallel()

	hot := func(types [][2]string, def string) *profile.Profile {
		values := make([]int64, len(types))
		for i := range values {
			values[i] = 100
		}
		return build(types, def, []stack{{[]string{ours + ".hot"}, values}})
	}
	tests := []struct {
		name string
		p    *profile.Profile
		hint string
		want string
	}{
		{"alloc churn", hot(heapTypes(), "alloc_space"), "", "alloc-hotspot"},
		{"retained memory", hot(heapTypes(), "inuse_space"), "", "memory-retention"},
		{"mutex", hot(delayTypes(), ""), "mutex", "lock-contention"},
		{"block", hot(delayTypes(), ""), "block", "blocking"},
		{"unlabelled delay", hot(delayTypes(), ""), "", "contention"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := analyze(t, tc.p, pprofan.Options{Hint: tc.hint})
			if len(r.Findings) != 1 || r.Findings[0].Kind != tc.want {
				t.Errorf("findings = %v, want [%s]", kinds(r), tc.want)
			}
		})
	}
}

func TestGoroutinePileupsAreGroupedByStack(t *testing.T) {
	t.Parallel()

	p := build([][2]string{{"goroutine", "count"}}, "", []stack{
		{[]string{"runtime.gopark", "runtime.chanrecv", ours + ".worker"}, []int64{200}},
		{[]string{"runtime.gopark", "net/http.(*conn).serve"}, []int64{3}},
	})
	r := analyze(t, p, pprofan.Options{})

	if len(r.Goroutines) != 2 || r.Goroutines[0].Count != 200 || r.Goroutines[0].Owner != ours+".worker" {
		t.Fatalf("groups = %+v", r.Goroutines)
	}
	if len(r.Findings) != 1 || r.Findings[0].Kind != "goroutine-pileup" || r.Findings[0].Where != ours+".worker" {
		t.Errorf("findings = %+v, want one pile-up at worker", r.Findings)
	}
	if r.Profile.Total != 203 {
		t.Errorf("total goroutines = %d, want 203", r.Profile.Total)
	}
}

func TestGoroutineLeakProfileFlagsEveryLeak(t *testing.T) {
	t.Parallel()

	p := build([][2]string{{"goroutine", "count"}}, "", []stack{
		{[]string{"runtime.gopark", ours + ".forgotten"}, []int64{1}},
	})
	r := analyze(t, p, pprofan.Options{Hint: "goroutineleak"})

	if len(r.Findings) != 1 || r.Findings[0].Kind != "goroutine-leak" {
		t.Errorf("the runtime already says these are leaks, so even one counts: %v", kinds(r))
	}
}

func TestOutsideOurCodeIsOnlyReportedWhereItMeansSomething(t *testing.T) {
	t.Parallel()

	runtimeOnly := func(types [][2]string, def string) *profile.Profile {
		values := make([]int64, len(types))
		for i := range values {
			values[i] = 100
		}
		return build(types, def, []stack{{[]string{"runtime.gcBgMarkWorker"}, values}})
	}
	tests := []struct {
		name string
		p    *profile.Profile
		hint string
		want bool
	}{
		{"cpu dominated by the runtime points at GC pressure", runtimeOnly(cpuTypes(), ""), "", true},
		{"allocation churn dominated by the runtime", runtimeOnly(heapTypes(), "alloc_space"), "", true},
		{"retained memory owned by the runtime is noise", runtimeOnly(heapTypes(), "inuse_space"), "", false},
		{"lock delay owned by the runtime is noise", runtimeOnly(delayTypes(), ""), "mutex", false},
		{"blocking owned by the runtime is noise", runtimeOnly(delayTypes(), ""), "block", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := analyze(t, tc.p, pprofan.Options{Hint: tc.hint})
			if got := len(r.Findings) == 1 && r.Findings[0].Kind == "outside-our-code"; got != tc.want {
				t.Errorf("reported = %v, want %v (findings %v)", got, tc.want, kinds(r))
			}
		})
	}
}

func TestTheRuntimesLeakProfileIsRecognisedWithoutAHint(t *testing.T) {
	t.Parallel()

	// This is what Go 1.27 writes for /debug/pprof/goroutineleak: a single "goroutineleak" sample type.
	p := build([][2]string{{"goroutineleak", "count"}}, "", []stack{
		{[]string{"runtime.gopark", ours + ".orphan"}, []int64{3}},
	})
	r := analyze(t, p, pprofan.Options{})

	if r.Profile.Kind != "goroutineleak" {
		t.Fatalf("kind = %q, want goroutineleak", r.Profile.Kind)
	}
	if len(r.Findings) != 1 || r.Findings[0].Kind != "goroutine-leak" || r.Findings[0].Count != 3 {
		t.Errorf("findings = %+v", r.Findings)
	}
}

func TestDiffShowsOnlyGrowth(t *testing.T) {
	t.Parallel()

	types := [][2]string{{"goroutine", "count"}}
	before := build(types, "", []stack{
		{[]string{ours + ".worker"}, []int64{10}},
		{[]string{ours + ".shrinking"}, []int64{500}},
	})
	after := build(types, "", []stack{
		{[]string{ours + ".worker"}, []int64{210}},
		{[]string{ours + ".shrinking"}, []int64{100}},
	})

	r, err := pprofan.AnalyzeDiff(before, after, pprofan.Options{Prefixes: []string{ours}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Findings) != 1 || r.Findings[0].Kind != "goroutine-growth" || r.Findings[0].Where != ours+".worker" {
		t.Fatalf("findings = %+v, want growth at worker only", r.Findings)
	}
	if r.Goroutines[0].Count != 200 {
		t.Errorf("growth = %d, want 200", r.Goroutines[0].Count)
	}
	for _, g := range r.Goroutines {
		if g.Owner == ours+".shrinking" {
			t.Errorf("a stack that shrank is not growth: %+v", g)
		}
	}
}

func TestTopLimitsTheLists(t *testing.T) {
	t.Parallel()

	var stacks []stack
	for _, name := range []string{".a", ".b", ".c", ".d", ".e"} {
		stacks = append(stacks, stack{[]string{ours + name}, []int64{1, 20}})
	}
	r := analyze(t, build(cpuTypes(), "", stacks), pprofan.Options{Top: 3})

	if len(r.Hotspots) != 3 || len(r.Owners) != 3 {
		t.Errorf("hotspots = %d, owners = %d, want 3 each", len(r.Hotspots), len(r.Owners))
	}
}

func TestAnEmptyProfileHasNoFindingsAndNoPanic(t *testing.T) {
	t.Parallel()

	r := analyze(t, build(cpuTypes(), "", nil), pprofan.Options{})
	if len(r.Findings) != 0 || r.Profile.Total != 0 {
		t.Errorf("report = %+v", r)
	}
}

func TestReportJSONNeverHasNullLists(t *testing.T) {
	t.Parallel()

	r := analyze(t, build(cpuTypes(), "", nil), pprofan.Options{})
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("an agent parsing this wants [] not null: %s", data)
	}
}

func TestParseReadsWhatWriteProduced(t *testing.T) {
	t.Parallel()

	orig := build(cpuTypes(), "", []stack{{[]string{ours + ".f"}, []int64{1, 10}}})
	var buf bytes.Buffer
	if err := orig.Write(&buf); err != nil {
		t.Fatal(err)
	}

	got, err := pprofan.Parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sample) != 1 {
		t.Errorf("samples = %d", len(got.Sample))
	}
	if _, err = pprofan.Parse([]byte("not a profile")); err == nil {
		t.Error("garbage must be an error, not an empty profile")
	}
}
