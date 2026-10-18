# 0013 - Load programs live in the service

- **Status**: accepted
- **Date**: 2026-10-18

## Context

The pprof loop (ADR-0006) ends with "run your load" and gives an agent no way to do it. Left alone, an agent captures an idle service and reads a profile with nothing in it, or writes a throwaway script whose numbers cannot be compared with the next run. What traffic means is different in every project, so the repository can supply the mechanism but not the requests.

## Decision

1. **`platform/loadgen` is the engine.** `loadgen.Run` applies a `Step` from a bounded number of workers and reports requests, errors, throughput, and p50/p95/p99/max latency. A run always has a duration (at most ten minutes), a request cap, and an error cap, and zero never means unlimited: a load test with no bound is how a test becomes an outage. Workers go through `platform/async`. A step the deadline cut short is dropped, not counted as a failure. A panicking step is an error, so a broken scenario cannot look like a healthy run.
2. **Each generated service gets a `cmd/<name>-load` skeleton.** It parses `-url`, `-duration`, `-workers`, and `-max-requests`, refuses a target that is not on this machine unless `-allow-remote` is passed, runs the engine, prints the summary, and exits non-zero when any request failed.
3. **The scenario is a placeholder that fails on purpose.** It makes no call to the example API. A project replaces `scenario.go` with the requests its users make. Until then a run stops after 50 identical errors and says what to write, because an empty scenario that reports success would hand the profiler an idle service and a clean bill of health.
4. **No load runner inside `agentctl`.** The agent starts the service's own program in the background and runs `perf capture` beside it, as the `perf-diagnose` skill says.

## Consequences

- Positive: two runs of the same scenario produce comparable numbers, and the engine is tested with exact timings under `testing/synctest`.
- Negative: until a project writes its scenario, the profiling loop has no load. That is deliberate, but it is a step an agent must not skip.
- Neutral: the engine speaks no protocol. A scenario can use HTTP, gRPC, or anything a Go client can do.

## Alternatives considered

- **A scenario file format and a runner in `agentctl`**: more to maintain, and it fixes the shape of a request before any real service has shown what shape it needs.
- **An external tool such as k6, vegeta, or hey**: capable, but a second language or binary to install and pin, and no tie to the typed requests and ids a service already has.
- **A sample scenario against the example API**: it would look like a working load test and measure nothing a real project cares about.
