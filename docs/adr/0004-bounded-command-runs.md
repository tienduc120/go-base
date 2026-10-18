# 0004 - Bounded command runs

- **Status**: accepted
- **Date**: 2026-10-18

## Context

An agent session that waits on a command that never returns is lost, and so is a CI minute budget. The cause is rarely
the command itself: it is a child it started. On Windows, killing a process leaves its children running, so a timed-out
`go test` can leave the test binary behind holding the terminal's pipes. A hung test also leaves the person who killed
it with nothing to read.

## Decision

1. **Every long command runs through `agentctl run`.** It applies a hard deadline (default 10 minutes) and an idle
   limit (default 2 minutes without any output). Either one ends the run.
2. **The whole process tree dies, not just the direct child.** On Unix the child leads its own process group and the
   group is killed; on Windows `taskkill /T` walks the tree. A test proves a grandchild does not survive.
3. **Activity resets the idle timer.** A process that keeps printing is working; one that prints once and goes quiet is
   stuck. The idle limit therefore catches hangs long before the deadline.
4. **`go test` gets its own `-timeout`** just under the wrapper's deadline (10 seconds less, or half for short limits)
   unless the caller set one. Go then prints every goroutine's stack before the wrapper has to kill anything, so a hang
   comes with the place it is blocked.
5. **Output is kept, not shown.** The full output goes to `.agentctl/logs`; the summary is one line, plus the tail of a
   failure. Exit codes follow the shell: the command's own, 124 for a timeout, 127 when the command does not exist.
6. `agentctl verify` and `agentctl rules test` use the same runner, so they cannot hang either.

## Consequences

- Positive: a stuck command costs at most the idle limit, leaves nothing behind, and says where it was stuck.
- Negative: an orphan whose parent exited before the kill is not reachable through the tree on Windows.
- Neutral: the guard that refuses to let an agent run a long command *without* this wrapper is a separate, later layer.

## Alternatives considered

- **Windows job objects** (kill-on-close): robust against orphans, but needs the `x/sys` dependency and care around
  the window between process start and job assignment; `taskkill /T` is built in and enough for now.
- **Only `-timeout` on `go test`**: covers one command; the rest of the toolchain can still hang.
