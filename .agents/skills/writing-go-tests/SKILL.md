---
name: writing-go-tests
description: Use when writing or changing a Go test, or before writing any production code. Covers the test-first loop, how a test is laid out, what each layer tests and with what, and the things this repository's gates reject.
---

# Write Go tests

A test that cannot fail proves nothing, and a test written after the code only describes it. The gates (race detector, leak check, linters) enforce much of this; the rest is yours.

## The loop

1. **Write the failing test first**, run that package's tests, and see it fail for the right reason: the behaviour is missing, not the code failing to compile on a typo.
2. Write the least code that passes it, and run again. That is the only checking you do while working; the pre-commit hook runs the gates.
3. Run a package with `go test -race -timeout 120s -count=1 ./path/to/pkg/`. Anything that can hang goes through `go run ./tools/agentctl run -- <command>`. A `go test` with no `-timeout` is refused.
4. **Never edit an existing test to make a change pass.** A test is the specification; change it only when the specification changes, and say so. A `refactor:` commit may add tests, never edit them.

## How a test reads

- **Name it for the behaviour**, in plain Go: `TestSaveRefusesATakenID`, not `TestSave2`. The name is what a failure shows first.
- **Arrange, act, assert** as three blocks separated by blank lines.
- **Compare whole values**, not one field, so a field added later breaks the test that should notice. Use `reflect.DeepEqual` or `slices.Equal`, and print both sides with `%+v`.
- **After a create, update, or delete, assert the stored state too**, not only the response: read it back through the repository.
- **Table tests** use `t.Parallel()` in the test and in each subtest (`paralleltest` and `tparallel` enforce it).
- **Say what a failure means** in the message: `t.Errorf("got %d, want 409: a taken ID is a conflict, not a server error", code)`.

## What each layer tests, and with what

| Layer | Test with | Never |
|---|---|---|
| `domain` | Plain values; no fakes, no context | A clock: time arrives as an argument |
| `application` | Hand-written fakes of its own ports; a fixed clock | A real database or network |
| `infrastructure` | A real Postgres through `platform/postgres/postgrestest` and `agentctl db test` | Mocks of the driver |
| `interfaces` | The handler called directly with a recorded request | A running server |
| `cmd` | The assembled service on port 0, shut down by cancelling its context | A fixed port |

A fake counts its calls and the test asserts the exact number, not "at least once".

## What the gates reject

- **A leaked goroutine.** A package that starts goroutines, subprocesses, or servers calls `goleak.VerifyTestMain(m)` in `TestMain`.
- **`time.Sleep`.** Wait on a channel, a timer, or the context. For code that depends on time, run the test inside `synctest.Test`, where the clock is fake and exact.
- **A race.** Tests run with `-race`; share nothing between parallel tests.
- **A query in a loop.** Assert `postgres.WithBudget(ctx, limits)` then `budget.Err()` is nil for a use case that touches the database.
- **A test that depends on another.** Each test builds its own state, or it cannot run in parallel.

## Before you finish

State which tests you ran and what they printed. If you skipped one, say which and why.
