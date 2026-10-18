package loadgen_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.uber.org/goleak"

	"github.com/tienduc120/go-base/platform/loadgen"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// after waits d on the (fake, inside a synctest bubble) clock or until ctx ends, as a request that takes d would.
func after(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRunStopsAtTheRequestCap(t *testing.T) {
	t.Parallel()

	stats, err := loadgen.Run(t.Context(), loadgen.Config{Workers: 4, Duration: time.Minute, MaxRequests: 40},
		func(context.Context, loadgen.Worker) error { return nil })

	if err != nil || stats.Requests != 40 || stats.Errors != 0 {
		t.Errorf("stats = %+v, err = %v, want exactly 40 requests: the cap is a bound, not a hint", stats, err)
	}
}

func TestEveryStepGetsAWorkerAndIterationNoOtherStepHas(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	seen := map[string]bool{}
	_, err := loadgen.Run(t.Context(), loadgen.Config{Workers: 4, Duration: time.Minute, MaxRequests: 100},
		func(_ context.Context, w loadgen.Worker) error {
			mu.Lock()
			defer mu.Unlock()
			seen[fmt.Sprintf("%d/%d", w.ID, w.Iteration)] = true
			if w.ID < 0 || w.ID >= 4 {
				return fmt.Errorf("worker id %d out of range", w.ID)
			}
			return nil
		})

	if err != nil || len(seen) != 100 {
		t.Errorf(
			"%d distinct (worker, iteration) pairs for 100 steps, err = %v: ids built from them would collide",
			len(seen),
			err,
		)
	}
}

func TestRunMeasuresLatencyPercentilesOnTheClock(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// One worker, nine steps taking 10, 20, 30 ms in turn, so every figure is exact.
		stats, err := loadgen.Run(
			t.Context(),
			loadgen.Config{Workers: 1, Duration: loadgen.MaxDuration, MaxRequests: 9},
			func(ctx context.Context, w loadgen.Worker) error {
				return after(ctx, time.Duration(w.Iteration%3+1)*10*time.Millisecond)
			},
		)
		if err != nil {
			t.Fatal(err)
		}

		if stats.P50 != 20*time.Millisecond || stats.P95 != 30*time.Millisecond || stats.Max != 30*time.Millisecond {
			t.Errorf("p50 = %v, p95 = %v, max = %v", stats.P50, stats.P95, stats.Max)
		}
		if stats.Elapsed != 180*time.Millisecond || int(stats.RPS) != 50 {
			t.Errorf("elapsed = %v, rps = %.1f, want 180ms and 50", stats.Elapsed, stats.RPS)
		}
	})
}

func TestRunStopsAtTheDurationAndDropsTheRequestsItCutShort(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// A step takes 300ms, so each worker finishes three in a second and the fourth is cut off.
		stats, err := loadgen.Run(t.Context(), loadgen.Config{Workers: 3, Duration: time.Second},
			func(ctx context.Context, _ loadgen.Worker) error { return after(ctx, 300*time.Millisecond) })
		if err != nil {
			t.Fatal(err)
		}

		if stats.Requests != 9 || stats.Errors != 0 {
			t.Errorf(
				"stats = %+v, want 9 finished requests and no errors: a request the deadline cut short is not a failure",
				stats,
			)
		}
		if stats.Elapsed != time.Second {
			t.Errorf("elapsed = %v, want the full second", stats.Elapsed)
		}
	})
}

func TestRunCountsFailuresAndKeepsTheFirst(t *testing.T) {
	t.Parallel()

	stats, err := loadgen.Run(t.Context(), loadgen.Config{Workers: 1, Duration: time.Minute, MaxRequests: 10},
		func(_ context.Context, w loadgen.Worker) error {
			if w.Iteration%2 == 1 {
				return fmt.Errorf("status 500 on step %d", w.Iteration)
			}
			return nil
		})

	if err != nil || stats.Requests != 10 || stats.Errors != 5 {
		t.Fatalf("stats = %+v, err = %v", stats, err)
	}
	if !strings.Contains(stats.FirstError, "step 1") {
		t.Errorf("first error = %q, want the first one, which is the one that explains the rest", stats.FirstError)
	}
}

func TestRunGivesUpOnAServiceThatIsFailingEveryRequest(t *testing.T) {
	t.Parallel()

	stats, err := loadgen.Run(t.Context(), loadgen.Config{Workers: 1, Duration: time.Minute, MaxErrors: 3},
		func(context.Context, loadgen.Worker) error { return errors.New("connection refused") })

	if err != nil || stats.Errors != 3 || stats.Requests != 3 {
		t.Errorf(
			"stats = %+v, err = %v, want it to stop after 3 failures instead of hammering a dead service",
			stats,
			err,
		)
	}
	if !strings.Contains(stats.Stopped, "errors") {
		t.Errorf("stopped = %q, want the reason", stats.Stopped)
	}
}

func TestRunStopsWhenTheCallerCancels(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(500*time.Millisecond, cancel)

		stats, err := loadgen.Run(ctx, loadgen.Config{Workers: 2, Duration: loadgen.MaxDuration},
			func(ctx context.Context, _ loadgen.Worker) error { return after(ctx, 100*time.Millisecond) })

		if err != nil || stats.Errors != 0 || !strings.Contains(stats.Stopped, "interrupted") {
			t.Errorf("stats = %+v, err = %v, want a clean stop that says it was interrupted", stats, err)
		}
	})
}

func TestAPanickingStepIsAnErrorNotACrash(t *testing.T) {
	t.Parallel()

	_, err := loadgen.Run(t.Context(), loadgen.Config{Workers: 2, Duration: time.Minute, MaxRequests: 10},
		func(context.Context, loadgen.Worker) error { panic("scenario bug") })

	if err == nil || !strings.Contains(err.Error(), "scenario bug") {
		t.Errorf("err = %v, want the panic reported, since a broken scenario must not look like a healthy run", err)
	}
}

func TestRunRefusesAnUnboundedOrSillyConfig(t *testing.T) {
	t.Parallel()

	step := func(context.Context, loadgen.Worker) error { return nil }
	for name, cfg := range map[string]loadgen.Config{
		"no workers":      {Duration: time.Second},
		"too many":        {Workers: loadgen.MaxWorkers + 1, Duration: time.Second},
		"no duration":     {Workers: 1},
		"negative":        {Workers: 1, Duration: -time.Second},
		"unbounded":       {Workers: 1, Duration: loadgen.MaxDuration + time.Second},
		"negative cap":    {Workers: 1, Duration: time.Second, MaxRequests: -1},
		"negative errors": {Workers: 1, Duration: time.Second, MaxErrors: -1},
	} {
		if _, err := loadgen.Run(t.Context(), cfg, step); err == nil {
			t.Errorf("%s: want an error: a load run with no bound is how a test becomes an outage", name)
		}
	}
	if _, err := loadgen.Run(t.Context(), loadgen.Config{Workers: 1, Duration: time.Second}, nil); err == nil {
		t.Error("a nil step must be an error")
	}
}

func TestStatsSummaryShowsWhatAnAgentNeedsToCompareTwoRuns(t *testing.T) {
	t.Parallel()

	stats := loadgen.Stats{
		Requests: 900, Errors: 3, Elapsed: 10 * time.Second, RPS: 90,
		P50: 12 * time.Millisecond, P95: 40 * time.Millisecond, P99: 90 * time.Millisecond, Max: 210 * time.Millisecond,
		FirstError: "status 500",
	}

	out := stats.Summary()
	for _, want := range []string{"900 requests", "3 errors", "90.0 req/s", "p50 12ms", "p95 40ms", "p99 90ms", "max 210ms", "status 500"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
}
