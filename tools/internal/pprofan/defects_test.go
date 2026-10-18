package pprofan_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/pprof/profile"

	"github.com/tienduc120/go-base/tools/internal/pprofan"
)

// These tests plant real, typical performance bugs, capture genuine runtime profiles of them, and check that the
// analyzer names the guilty function. They are the proof that an agent reading the report is told the truth. The
// profilers and their rates are process-wide, so none of these tests may run in parallel.

const testPkg = "github.com/tienduc120/go-base/tools/internal/pprofan_test."

func opts(hint string) pprofan.Options {
	return pprofan.Options{Prefixes: []string{testPkg}, Hint: hint}
}

func parseNamed(t *testing.T, name string) *profile.Profile {
	t.Helper()
	var buf bytes.Buffer
	prof := pprof.Lookup(name)
	if prof == nil {
		t.Skipf("this Go version has no %s profile", name)
	}
	if err := prof.WriteTo(&buf, 0); err != nil {
		t.Fatal(err)
	}
	p, err := pprofan.Parse(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// snapshotHeap makes the allocation profile current: it only reflects allocations up to the last garbage collection.
func snapshotHeap(t *testing.T) *profile.Profile {
	t.Helper()
	runtime.GC()
	return parseNamed(t, "allocs")
}

func finding(t *testing.T, r pprofan.Report, kind string) pprofan.Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Kind == kind {
			return f
		}
	}
	t.Fatalf("no %s finding; the report says %+v", kind, r.Findings)
	return pprofan.Finding{}
}

func spinFor(d time.Duration) {
	for start := time.Now(); time.Since(start) < d; { //nolint:revive // a deliberate busy-wait that burns CPU for d
	}
}

// The CPU defect runs as its own program, built without the race detector: with -race the profiler cannot unwind
// through the instrumentation, so every sample loses its caller and the whole profile lands "outside our code".
// Agents must therefore never profile CPU on a -race build.
func TestNamesAQuadraticLoopInTheCPUProfile(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "cpu.pprof")
	cmd := exec.CommandContext(t.Context(), "go", "run", "./testdata/cpudefect", out)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go run: %v\n%s", err, output)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pprofan.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	r, err := pprofan.Analyze(p, opts(""))
	if err != nil {
		t.Fatal(err)
	}

	f := finding(t, r, "cpu-hotspot")
	if f.Where != "main.hasDuplicates" {
		t.Errorf("blamed %s, want main.hasDuplicates", f.Where)
	}
}

// compilePerCall is the classic bug: building a regexp inside the request path instead of once.
//
//go:noinline
func compilePerCall(inputs []string) int {
	matched := 0
	for _, in := range inputs {
		if regexp.MustCompile(`^[a-z]+-\d+(\.\d+)*$`).MatchString(in) {
			matched++
		}
	}
	return matched
}

// The CPU profile of this bug mostly shows the garbage collector (so "outside our code"), because the cost is the
// allocation churn. The allocation profile is the one that names the function, which is why a diagnosis needs both.
//
//nolint:paralleltest // the allocation profile is process-wide
func TestNamesARegexpCompiledOnEveryCallInTheAllocationProfile(t *testing.T) {
	inputs := []string{"alpha-1", "beta-22.3", "not valid", "gamma-4.5.6"}

	before := snapshotHeap(t)
	for range 6000 {
		compilePerCall(inputs)
	}
	after := snapshotHeap(t)

	r, err := pprofan.AnalyzeDiff(before, after, opts(""))
	if err != nil {
		t.Fatal(err)
	}

	f := finding(t, r, "alloc-hotspot")
	if !strings.HasSuffix(f.Where, "compilePerCall") {
		t.Errorf("blamed %s, want compilePerCall", f.Where)
	}
}

// buildReport grows a string one piece at a time, copying it on every append.
//
//go:noinline
func buildReport(rows int) string {
	out := ""
	for i := range rows {
		out += "row " + string(rune('a'+i%26)) + "\n" //nolint:perfsprint // the planted defect is this concatenation
	}
	return out
}

//nolint:paralleltest // the allocation profile is process-wide
func TestNamesStringConcatenationInALoop(t *testing.T) {
	before := snapshotHeap(t)
	total := 0
	for range 10 {
		total += len(buildReport(3000))
	}
	after := snapshotHeap(t)
	if total == 0 {
		t.Fatal("the workload produced nothing")
	}

	r, err := pprofan.AnalyzeDiff(before, after, opts(""))
	if err != nil {
		t.Fatal(err)
	}

	f := finding(t, r, "alloc-hotspot")
	if !strings.HasSuffix(f.Where, "buildReport") {
		t.Errorf("blamed %s, want buildReport", f.Where)
	}
}

// incrementUnderSlowLock does its slow work while holding the lock, so everyone else queues behind it.
//
//go:noinline
func incrementUnderSlowLock(mu *sync.Mutex, n *int) {
	mu.Lock()
	spinFor(500 * time.Microsecond)
	*n++
	mu.Unlock()
}

//nolint:paralleltest // the mutex profile rate is process-wide
func TestNamesASlowCriticalSection(t *testing.T) {
	previous := runtime.SetMutexProfileFraction(1)
	t.Cleanup(func() { runtime.SetMutexProfileFraction(previous) })
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("lock contention needs at least two processors")
	}

	before := parseNamed(t, "mutex")
	var (
		mu sync.Mutex
		n  int
		wg sync.WaitGroup
	)
	for range 8 {
		wg.Go(func() {
			for range 20 {
				incrementUnderSlowLock(&mu, &n)
			}
		})
	}
	wg.Wait()
	after := parseNamed(t, "mutex")

	r, err := pprofan.AnalyzeDiff(before, after, opts("mutex"))
	if err != nil {
		t.Fatal(err)
	}

	f := finding(t, r, "lock-contention")
	if !strings.HasSuffix(f.Where, "incrementUnderSlowLock") {
		t.Errorf("blamed %s, want incrementUnderSlowLock", f.Where)
	}
}

// waitForSlowProducer blocks on a channel whose sender is slow.
//
//go:noinline
func waitForSlowProducer(ch <-chan int) int { return <-ch }

//nolint:paralleltest // the block profile rate is process-wide
func TestNamesAGoroutineBlockedOnASlowProducer(t *testing.T) {
	runtime.SetBlockProfileRate(1)
	t.Cleanup(func() { runtime.SetBlockProfileRate(0) })

	before := parseNamed(t, "block")
	ch := make(chan int)
	for i := range 20 {
		// The producer spins rather than sleeping, so only the consumer shows up as blocked.
		go func() {
			spinFor(5 * time.Millisecond)
			ch <- i
		}()
		waitForSlowProducer(ch)
	}
	after := parseNamed(t, "block")

	r, err := pprofan.AnalyzeDiff(before, after, opts("block"))
	if err != nil {
		t.Fatal(err)
	}

	f := finding(t, r, "blocking")
	if !strings.HasSuffix(f.Where, "waitForSlowProducer") {
		t.Errorf("blamed %s, want waitForSlowProducer", f.Where)
	}
}

// leakyWorker waits for a signal that, in the buggy program, nobody ever sends.
//
//go:noinline
func leakyWorker(started *sync.WaitGroup, release <-chan struct{}) {
	started.Done()
	<-release
}

//nolint:paralleltest // goroutine profiles are compared across the whole process
func TestNamesGoroutinesThatPileUp(t *testing.T) {
	before := parseNamed(t, "goroutine")

	release := make(chan struct{})
	var started, finished sync.WaitGroup
	started.Add(200)
	for range 200 {
		finished.Go(func() { leakyWorker(&started, release) })
	}
	started.Wait()
	after := parseNamed(t, "goroutine")

	// Let them all end so this test does not leave 200 goroutines behind for the others.
	close(release)
	finished.Wait()

	r, err := pprofan.AnalyzeDiff(before, after, opts(""))
	if err != nil {
		t.Fatal(err)
	}

	f := finding(t, r, "goroutine-growth")
	if !strings.HasSuffix(f.Where, "leakyWorker") || f.Count < 200 {
		t.Errorf("blamed %s with %d goroutines, want leakyWorker with at least 200", f.Where, f.Count)
	}
}

// orphanWorker waits on a channel nothing else holds, so it can never be woken.
//
//go:noinline
func orphanWorker() {
	ch := make(chan int)
	<-ch
}

//nolint:paralleltest // the runtime's leak detection works on the whole process
func TestNamesGoroutinesTheRuntimeProvesAreLeaked(t *testing.T) {
	for range 5 {
		go orphanWorker()
	}

	// The runtime finds a leak during garbage collection, so give it a few cycles.
	var found bool
	for range 10 {
		runtime.GC()
		<-time.After(50 * time.Millisecond)

		p := parseNamed(t, "goroutineleak")
		r, err := pprofan.Analyze(p, opts("goroutineleak"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range r.Findings {
			if f.Kind == "goroutine-leak" && strings.HasSuffix(f.Where, "orphanWorker") {
				found = true
			}
		}
		if found {
			return
		}
	}
	t.Error("the runtime's leak profile never reported orphanWorker")
}
