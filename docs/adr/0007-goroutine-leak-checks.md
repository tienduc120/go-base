# 0007 - Goroutine-leak checks in tests

- **Status**: accepted
- **Date**: 2026-10-18

## Context

A goroutine that nothing stops is a leak: it holds memory, file descriptors, and whatever it references, and a service that starts one per request runs out of something eventually. The pprof loop (ADR-0006) finds leaks in a running service. They are cheaper to catch before then, in the test that exercised the code.

## Decision

1. **Every package whose code starts goroutines, subprocesses, or servers verifies in `TestMain` that its tests left nothing running**, with `go.uber.org/goleak` (MIT, Uber): `goleak.VerifyTestMain(m)`. A leak fails the package's tests and names the goroutine and the code that started it.
2. **It is a dependency of the `tools` module only.** Services take the same dependency in their own tests when they exist, since the pattern is the point.
3. **The tooling is the first user.** `proc`, `perf`, and `agentctl` carry the check, so the bounded-command runner is itself proven not to leak a pipe copier or a timer callback on its kill, idle, and deadline paths.
4. **Time in tests is controlled, not slept.** `time.Sleep` is already forbidden by lint. Tests that need to move time use `testing/synctest` (standard library from Go 1.25), which runs a test in a bubble with a fake clock and reports a deadlock instead of hanging. Adopting it where a test waits on a timer is done case by case, as the code that needs it appears.
5. **Runtime detection stays on in services.** The `goroutineleak` profile (Go 1.27) is captured by `agentctl perf capture`, so a leak the tests missed is still caught under load.

## Consequences

- Positive: a leak fails at the commit that introduces it, with the creator's stack in the message.
- Negative: a test that deliberately leaves a goroutine running must clean it up, or the package fails.
- Neutral: goleak waits briefly for goroutines that are about to exit, so a slow shutdown is not a false alarm.

## Alternatives considered

- **`goleak.VerifyNone(t)` per test**: more precise, but every author has to remember it. One `TestMain` per package cannot be forgotten once added.
- **Relying on the runtime's leak profile alone**: it needs the leak to happen under load in a running service, which is later and costlier than a failing test.
