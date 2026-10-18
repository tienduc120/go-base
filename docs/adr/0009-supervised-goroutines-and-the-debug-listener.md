# 0009 - Supervised goroutines and the debug listener

- **Status**: accepted
- **Date**: 2026-10-18

## Context

Two rules were already in force with nothing for services to use. The `barego` analyzer forbids a `go` statement anywhere but `platform/async`, and the pprof loop (ADR-0006) needs a profiling endpoint on every service. Without the package the first rule only blocks, and without the endpoint the second has nothing to capture from.

## Decision

1. **`platform/async.Run` is how a service runs things concurrently.** It starts each task in a goroutine, gives each a context that is cancelled when any task fails or the parent ends, and returns when all have finished. Only a failure stops the others; a task that returns nil does not. A task that returns `context.Canceled` was told to stop and is not reported. A panic becomes an error that carries its stack. Every failure is returned, joined.
2. **It is the only package that may start a goroutine.** The `barego` analyzer enforces this everywhere else, and test files are exempt.
3. **`platform/debugserver` serves pprof, and only pprof, on a loopback-only listener of its own.** It refuses any address that is not loopback, binds at construction so a taken port is a startup error and port 0 reports the real port, serves from its own mux (not the default one), sets timeouts, and shuts down gracefully when its context ends. Its write timeout (45 seconds) exceeds the longest CPU profile `agentctl perf capture` will ask for (30 seconds), or profiles would be cut off.
4. **`EnableContentionProfiles` turns on mutex and block sampling**, which the Go runtime leaves off, so `lock-contention` and `blocking` findings have data.
5. **`platform/clock` is the time port.** The domain never reads the clock; the application declares a `Clock` port and `clock.System` is the adapter.
6. **The root module takes `go.uber.org/goleak` for its tests**, as ADR-0007 anticipated. Every platform package that starts goroutines or servers verifies in `TestMain` that it left nothing running.

## Consequences

- Positive: a service has one obvious way to run a server and a debug listener together, with graceful shutdown and no lost panics, and the perf loop works on every generated service from the first run.
- Negative: anything that needs a goroutine must go through `async.Run`, which fits tasks that live as long as a service better than a fire-and-forget worker. A pool or pipeline gets its own helper in `platform/async` when one is needed.
- Neutral: `net/http/pprof` still registers its handlers on the default mux when imported; nothing serves that mux, so they are unreachable.

## Alternatives considered

- **`golang.org/x/sync/errgroup`**: close to `async.Run` but adds a dependency for a few lines, and has no panic handling or cancellation filtering.
- **Bare `go` statements with a lint exception per site**: every site becomes a reviewed suppression and the ownership problem returns.
- **pprof on the serving port**: simple, and exactly how profiles end up public.
