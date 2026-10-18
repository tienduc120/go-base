// Package loadgen applies a bounded amount of load and reports how the service coped.
//
// It is the engine under a service's own load program: a project supplies the Step (the requests its users really
// make) and this package supplies the workers, the limits, and the numbers. Every run has a hard duration, a request
// cap, and an error cap, because a load test with no bound is how a test becomes an outage.
package loadgen

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tienduc120/go-base/platform/async"
)

const (
	// MaxWorkers and MaxDuration are the most a run may ask for; a deliberate larger test needs a deliberate tool.
	MaxWorkers  = 256
	MaxDuration = 10 * time.Minute

	// DefaultMaxRequests and DefaultMaxErrors apply when the config leaves them zero. Zero never means unlimited.
	DefaultMaxRequests = 1_000_000
	DefaultMaxErrors   = 50

	percentileMedian = 0.50
	percentileP95    = 0.95
	percentileP99    = 0.99
)

// Worker identifies one step. (ID, Iteration) is unique within a run, so a step can build ids that never collide.
type Worker struct {
	ID        int
	Iteration int
}

// Step is one request or one short flow. It returns the failure, if any, and Run times it.
type Step func(ctx context.Context, w Worker) error

// Config bounds a run.
type Config struct {
	// Workers is how many steps run at once, between 1 and MaxWorkers.
	Workers int
	// Duration is how long load is applied, up to MaxDuration.
	Duration time.Duration
	// MaxRequests stops the run after this many finished steps; zero means DefaultMaxRequests.
	MaxRequests int
	// MaxErrors stops the run once this many steps have failed, so a dead service is not hammered; zero means
	// DefaultMaxErrors. With several workers the run may overshoot by the steps already in flight.
	MaxErrors int
}

// Stats is the outcome of a run. Latencies cover the steps that succeeded.
type Stats struct {
	Requests   int
	Errors     int
	Elapsed    time.Duration
	RPS        float64
	P50        time.Duration
	P95        time.Duration
	P99        time.Duration
	Max        time.Duration
	FirstError string
	// Stopped says why the run ended: the duration, a cap, too many errors, or an interrupt.
	Stopped string
}

// Summary is the compact form an agent or a person compares between two runs.
func (s Stats) Summary() string {
	var b strings.Builder
	fmt.Fprintf(
		&b,
		"%d requests, %d errors in %s (%.1f req/s)\n",
		s.Requests,
		s.Errors,
		s.Elapsed.Round(time.Millisecond),
		s.RPS,
	)
	fmt.Fprintf(&b, "latency p50 %s, p95 %s, p99 %s, max %s\n", s.P50, s.P95, s.P99, s.Max)
	if s.FirstError != "" {
		fmt.Fprintf(&b, "first error: %s\n", s.FirstError)
	}
	if s.Stopped != "" {
		fmt.Fprintf(&b, "stopped: %s\n", s.Stopped)
	}
	return b.String()
}

func (c Config) validate(step Step) error {
	switch {
	case step == nil:
		return errors.New("loadgen: step is nil")
	case c.Workers < 1 || c.Workers > MaxWorkers:
		return fmt.Errorf("loadgen: Workers must be between 1 and %d, got %d", MaxWorkers, c.Workers)
	case c.Duration <= 0 || c.Duration > MaxDuration:
		return fmt.Errorf("loadgen: Duration must be positive and at most %s, got %s", MaxDuration, c.Duration)
	case c.MaxRequests < 0 || c.MaxErrors < 0:
		return errors.New("loadgen: MaxRequests and MaxErrors must not be negative")
	}
	return nil
}

// run is the state the workers share.
type run struct {
	step        Step
	maxRequests int64
	maxErrors   int64
	stop        context.CancelFunc

	claimed atomic.Int64
	errors  atomic.Int64

	mu         sync.Mutex
	firstError string
	stopped    string

	// latencies and finished are indexed by worker, so no worker writes another's slot.
	latencies [][]time.Duration
	finished  []int
}

// Run applies load until the duration ends, a cap is reached, or ctx is cancelled. A step that fails is counted, not
// returned: the error is for a run that could not be carried out, such as a bad config or a step that panicked.
func Run(ctx context.Context, cfg Config, step Step) (Stats, error) {
	if err := cfg.validate(step); err != nil {
		return Stats{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()
	r := &run{
		step:        step,
		maxRequests: int64(cmp.Or(cfg.MaxRequests, DefaultMaxRequests)),
		maxErrors:   int64(cmp.Or(cfg.MaxErrors, DefaultMaxErrors)),
		stop:        cancel,
		latencies:   make([][]time.Duration, cfg.Workers),
		finished:    make([]int, cfg.Workers),
	}

	tasks := make([]func(context.Context) error, cfg.Workers)
	for id := range tasks {
		tasks[id] = func(workerCtx context.Context) error { return r.worker(workerCtx, id) }
	}
	started := time.Now()
	err := async.Run(runCtx, tasks...)

	stats := r.stats(time.Since(started))
	stats.Stopped = r.reason(ctx)
	if err != nil {
		return stats, fmt.Errorf("loadgen: the scenario failed: %w", err)
	}
	return stats, nil
}

func (r *run) worker(ctx context.Context, id int) error {
	for iteration := 0; ctx.Err() == nil; iteration++ {
		if r.claimed.Add(1) > r.maxRequests {
			r.end("request cap reached")
			return nil
		}
		began := time.Now()
		err := r.step(ctx, Worker{ID: id, Iteration: iteration})
		took := time.Since(began)

		// A step the deadline or an interrupt cut short did not fail; it simply did not finish.
		if err != nil && ctx.Err() != nil && isContextError(err) {
			return nil
		}
		r.finished[id]++
		if err == nil {
			r.latencies[id] = append(r.latencies[id], took)
			continue
		}
		r.failed(err)
	}
	return nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (r *run) failed(err error) {
	r.mu.Lock()
	if r.firstError == "" {
		r.firstError = err.Error()
	}
	r.mu.Unlock()

	if r.errors.Add(1) >= r.maxErrors {
		r.end(fmt.Sprintf("%d errors", r.maxErrors))
	}
}

// end records why the run is stopping, if nothing has said so already, and stops the workers.
func (r *run) end(reason string) {
	r.mu.Lock()
	if r.stopped == "" {
		r.stopped = reason
	}
	r.mu.Unlock()
	r.stop()
}

func (r *run) reason(parent context.Context) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.stopped != "":
		return r.stopped
	case parent.Err() != nil:
		return "interrupted"
	}
	return "duration reached"
}

func (r *run) stats(elapsed time.Duration) Stats {
	var all []time.Duration
	total := 0
	for id := range r.latencies {
		all = append(all, r.latencies[id]...)
		total += r.finished[id]
	}
	slices.Sort(all)

	r.mu.Lock()
	first := r.firstError
	r.mu.Unlock()

	s := Stats{Requests: total, Errors: int(r.errors.Load()), Elapsed: elapsed, FirstError: first}
	if elapsed > 0 {
		s.RPS = float64(total) / elapsed.Seconds()
	}
	s.P50, s.P95, s.P99 = percentile(
		all,
		percentileMedian,
	), percentile(
		all,
		percentileP95,
	), percentile(
		all,
		percentileP99,
	)
	if len(all) > 0 {
		s.Max = all[len(all)-1]
	}
	return s
}

// percentile is the nearest-rank value of sorted, which is zero when nothing succeeded.
func percentile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(q * float64(len(sorted))))
	return sorted[min(max(rank, 1), len(sorted))-1]
}
