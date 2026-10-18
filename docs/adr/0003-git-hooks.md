# 0003 - Git hooks

- **Status**: accepted
- **Date**: 2026-10-17

## Context

Gates that run only when someone remembers them are skipped by the author who most needs them. Git hooks are the one layer every tool and every human passes through, whether or not their tool supports agent hooks.

## Decision

1. **lefthook** manages the hooks (`lefthook.yml`): it is a single Go binary and the same on Windows and Linux.
2. **pre-commit** runs `agentctl verify` (about 12 seconds): vet, analyzers, strict lint, race tests, layer rules.
3. **commit-msg** runs `agentctl commitmsg`: Conventional Commits, imperative, lowercase subject, no trailing period, at most 72 characters, every problem reported at once.
4. **pre-push** runs `agentctl verify --full`: adds govulncheck and the rules proof.
5. Hooks are installed per clone with `lefthook install`. `--no-verify` is forbidden by `AGENTS.md`; the hooks are a guard rail, not a security boundary, so CI must run the same `verify --full` once a pipeline exists.

## Consequences

- Positive: a failing gate stops the commit, with the same output an agent would see running `verify` by hand.
- Negative: every commit costs about 12 seconds.
- Neutral: agent-level hooks (blocking a command before it runs) are a separate, later layer.

## Alternatives considered

- **Husky or pre-commit (Python)**: a second runtime for a Go repository.
- **CI only**: feedback arrives after the push, and an agent loop wants it before the commit.
