# 0006 - pprof diagnosis for agents

- **Status**: accepted
- **Date**: 2026-10-18

## Context

An agent asked to "make it faster" tends to guess, and a guess about where the time goes is often wrong. The tools that tell the truth, the pprof profiles, are written for people: a flame graph or a ranked list of leaf functions that are mostly the runtime (`mallocgc`, `gopark`). What an agent needs is the function in *our* code that owns the cost, a number it can quote, and a next step, in a few lines it can read in one go.

## Decision

1. **Parse profiles with `github.com/google/pprof/profile`** (Apache-2.0, the same library the `go tool pprof` command uses). It is a dependency of the `tools` module only, so no service ever imports it.
2. **Charge every sample to the nearest frame in our own code.** Leaf functions are reported too, but the owner table is the one that answers "who in our code is responsible". Our code means the workspace modules plus package `main`.
3. **Diff snapshots instead of reading absolutes.** Leaks and growth are visible only as change, so heap, allocations, goroutines, locks, and blocking are snapshotted before and after a CPU window and the difference is analysed. Negative values (things that shrank) are dropped.
4. **Findings are few and evidenced.** A hotspot is reported when one owner holds at least 30% of a profile; goroutines when at least 50 are parked at one stack (any number for the runtime's own leak profile). Each finding names the function, the share, and a next step. "Outside our code" is reported only for CPU and allocation churn, where it means garbage collection pressure, and is suppressed for retained memory, locks, and blocking, where it is the runtime's bookkeeping.
5. **The analyzer is proven against real defects, not just synthetic numbers.** Tests plant a quadratic loop, string concatenation in a loop, a regexp compiled per call, a slow critical section, a consumer blocked on a slow producer, goroutines that pile up, and goroutines the runtime proves leaked, profile each for real, and require the report to name the guilty function.
6. **Capture cannot hurt the service it inspects.** `agentctl perf capture` accepts only loopback addresses unless told otherwise, caps the CPU window at 30 seconds, puts a deadline on every request, and bounds response sizes.
7. **Never profile CPU on a `-race` build.** The profiler cannot unwind through the race instrumentation and every sample loses its caller. The CPU defect test therefore runs as its own program built without the race detector.
8. **The skill `perf-diagnose` fixes the procedure**: reproduce, capture, read the findings, one hypothesis, one change, compare, and stop after three rounds.

## Consequences

- Positive: an agent gets "main.findDuplicates holds 77% of CPU" instead of a wall of runtime frames, and can prove its fix with a second capture.
- Negative: the thresholds are heuristics. A cost spread across many functions yields no finding, which is the honest answer but not a fix.
- Neutral: N+1 queries are invisible to pprof. They are caught by the query-count and `EXPLAIN` gates, which come with the database layer.

## Alternatives considered

- **An MCP server for pprof** (such as ZephyrDeng/pprof-analyzer-mcp): a small project we would depend on for the core of the feature, and it does not charge samples to our own code or diff snapshots.
- **Shelling out to `go tool pprof -top`**: text meant for people, no ownership view, and no way to diff goroutine stacks.
- **Continuous profiling (Pyroscope)**: valuable in production, but a service to run; this is the local, per-change loop.
