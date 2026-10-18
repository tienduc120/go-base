---
name: perf-diagnose
description: Use when a service is slow, uses too much memory, has lock contention or goroutines piling up, or before changing a hot path. Finds which function in our code owns the cost from pprof profiles, in a reproduce, capture, read, one change, compare loop.
---

# Diagnose performance with pprof

Never optimise without a profile, and never claim an improvement without a second profile to compare. A guess about where the time goes is wrong often enough that a fix built on it is a coin flip.

## The loop

1. **Reproduce.** Run the service with its pprof server on a loopback port (never the serving port, never a public address), and drive the load that shows the problem with the service's own load program, `go run ./tools/agentctl run --timeout 2m -- go run ./services/<name>/cmd/<name>-load -duration 40s`, started in the background so it overlaps the capture. The program refuses a target that is not on this machine and ends at its duration, a request cap, or 50 errors. A fresh service's scenario in `cmd/<name>-load/scenario.go` is a placeholder that fails on purpose: write the requests its users really make first, because a capture of an idle service says nothing. Start every long-running process through `agentctl run` so it cannot outlive the session.
2. **Capture.** While the load runs: `go run ./tools/agentctl perf capture --url http://127.0.0.1:<pprof port> --seconds 10`. It snapshots the heap, allocations, goroutines, locks, and blocking before and after a CPU window, so what *grew* is separated from what merely exists.
3. **Read the findings, not the profile.** Each finding names a function in our code, the share it holds, and a next step. Start with the biggest share. `findings.json` in the output directory has the same data for machines.
4. **Form one hypothesis** from the finding and the code of the named function. Open the function and the code that calls it.
5. **Make one change.** One, so the next capture can say whether it worked.
6. **Capture again under the same load and compare**: `go run ./tools/agentctl perf analyze --base <old>.pprof <new>.pprof`, or read both finding lists. Report the share and the totals before and after.
7. **Stop after three iterations** and report what you found, what you changed, and what is left, even if the numbers are still not where you want them.

## What the findings mean

- `cpu-hotspot`: CPU time is concentrated in that function. Look for work repeated per call or an algorithm that scales badly.
- `outside-our-code` on CPU: the runtime owns the time, usually the garbage collector. The cause is allocation churn, so capture and read the allocations next.
- `alloc-hotspot`: that function allocates most of the bytes. Preallocate, reuse buffers, hoist work out of loops (a regexp built on every call is the classic).
- `memory-retention` or `memory-growth`: something keeps memory reachable. Look for unbounded caches and goroutines that hold references.
- `lock-contention`: goroutines wait for a lock held in that function. Shorten the critical section or shard the lock.
- `blocking`: goroutines wait on a channel, pool, or call there. Give the wait a deadline and a bound.
- `goroutine-growth` and `goroutine-pileup`: goroutines are parked at one stack and the count rises. Find who starts them and what should stop them.
- `goroutine-leak`: the Go runtime itself proved these goroutines can never run again. Fix every one; it needs Go 1.27 or later.

## Rules that save you a wrong turn

- **Never profile CPU on a `-race` build.** The profiler cannot unwind through the race instrumentation, so every sample loses its caller and the whole profile reads "outside our code".
- Profile a real build under realistic load. A profile of an idle service shows nothing.
- A finding is evidence, not a verdict. Read the function before changing it, and say what you checked.
- `perf capture` refuses non-loopback addresses and windows over 30 seconds on purpose. Do not point it at production, and do not pass `--allow-remote` unless the user asked.
- Do not leave `net/http/pprof` on a port that serves users. The pprof server belongs on its own internal listener.
- Quote numbers from the report in your summary: the function, its share, and the totals before and after.
