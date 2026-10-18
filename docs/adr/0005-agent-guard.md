# 0005 - Agent guard

- **Status**: accepted
- **Date**: 2026-10-18

## Context

The gates only mean something if the agent they judge cannot quietly change them. The easiest way to make a failing build pass is to loosen the rule, delete the fixture, skip the hook, or run the command in a way that hides the failure. Prose in `AGENTS.md` asks agents not to; a tool should stop them.

## Decision

1. **Claude Code runs `agentctl hook` before every edit and shell command** (`.claude/settings.json`). Exit 2 blocks the call and the reason on stderr goes to the agent, so it learns what to do instead. Any other failure exits 1, which lets the call through: a broken guard must not brick a session.
2. **Protected means committed.** A file is protected once it is in HEAD, because that is when you and CI have accepted it. Rule configs, the gate code, the CI workflow, the hook wiring, fixtures, and licenses are protected. A new file, including a new fixture or analyzer, is always allowed, and so is finishing a gate that has not been committed yet. This adds rules freely and makes weakening a committed one deliberate.
3. **The shell is checked too.** The guard refuses `--no-verify`, `HUSKY=0`, `LEFTHOOK=0`, `core.hooksPath`, and `lefthook uninstall`; shell writes, moves, and deletes aimed at a protected path; and commands that never return (`go test` without a timeout, a service in the foreground, `docker compose up` without `-d`, `tail -f`). Commit messages and heredoc bodies that merely mention these are not mistaken for commands.
4. **The user approves a protected change by starting the session with `AGENTCTL_ALLOW_PROTECTED=1`.** It lives in the environment that launches the session, so the agent cannot set it from inside. It lifts only the protected-file rule, never the hook bypass or the hang rules.
5. **The hooks call a prebuilt binary** in `.agentctl/bin`, refreshed by every `verify`, because `go run` costs about a second per call. If the binary is missing the command builds it first, since `go run` swallows the exit code and would turn a block into an allow.
6. **It is a guard rail, not a security boundary.** String matching can be routed around, and Codex or Cursor do not run these hooks. `CODEOWNERS` over the same paths, with code-owner review required, plus the git hooks and CI, are the boundary.

## Consequences

- Positive: weakening a rule, skipping a gate, or hanging a session is stopped at the moment it is attempted, with the fix in the message.
- Negative: changing a committed rule needs a session restart with the approval variable. If that proves too heavy, a prompt-based approval is the alternative, but whether a hook's `ask` decision still prompts in auto-accept modes is not documented.
- Negative: an unusual edit to a protected file is a false alarm until approved.

## Alternatives considered

- **Protect every file ever created**: blocks finishing a new gate in the same session, which is when it needs the most edits.
- **Deny without an approval path**: makes legitimate rule changes impossible for the owner's own agent.
- **Fail closed when the guard breaks**: a typo in the guard would block every tool call.
