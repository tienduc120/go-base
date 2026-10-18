---
name: debt-recovery
description: Use when asked to pay down technical debt, reduce complexity, remove lint suppressions or TODOs, or improve test coverage, and when `agentctl debt check` fails. Finds the debt the gates allow, confirms it with evidence, and pays off one confirmed item per commit without changing behaviour.
---

# Find, confirm, and pay off technical debt

The lint gate stops what it can name. Debt is what slips through it: every `//nolint`, every TODO nobody owns, functions drifting towards the complexity limit, and packages with thin tests. This skill is how to deal with that debt without making things worse.

## The loop

1. **Scan.** `go run ./tools/agentctl debt scan` lists the debt, worst first, each with a stable id and the evidence it was found on. Add `--coverage` to run the tests and include weak or missing coverage.
2. **Confirm.** Before acting on an item, record what it is: `debt confirm <id> --status confirmed`. This only works if the scan still finds the item, and it stores the scan's evidence, so a claim cannot be made up.
3. **Pick.** `go run ./tools/agentctl debt next` shows the worst confirmed item and the safe way to pay it off. Recovery starts from confirmed debt only.
4. **Pay it off**, following the playbook it printed. One item per commit, with a `refactor:` or `test:` subject.
5. **Scan again.** The item must be gone and nothing new may have appeared.
6. **Prune.** `debt prune` removes register entries whose debt is paid, so the register only ever lists live debt.

## Who may decide what

- You may mark an item `confirmed`, or `false-positive` with a reason someone else could check (for example, "this is inside a string, not a directive").
- You may **not** accept debt on your own. `accepted` means a person decided to live with it. When an item is not worth fixing now, say so and let the user run `debt confirm <id> --status accepted --reason ... --expires YYYY-MM-DD`.
- An acceptance needs a reason and an expiry within 180 days. When it runs out the item comes back, which is the point: accepted debt that never expires is ignored debt.

## When `debt check` fails

It fails on **new debt nobody has decided about**, and on **acceptances that expired**. This is the ratchet: debt cannot grow silently.

1. Read which items it names.
2. Prefer to fix the cause so the item disappears. A `//nolint` or a TODO added to get past a gate is itself new debt.
3. If it cannot be fixed now, ask the user to accept it, with a reason and an expiry.

## Rules that keep a recovery honest

- **Pin behaviour before you change it.** Write characterization tests that pass on the current code first, run them, then refactor.
- **A `refactor:` commit may only add tests.** The `commit-msg` hook rejects a refactor that edits or deletes an existing test line, because an edited expectation means behaviour changed. If behaviour really changes, it is a `fix:` or `feat:`.
- **Never hide debt to make it go away.** No new `//nolint`, no raised threshold, no deleted test, no marker rewritten so the scan misses it.
- **Do not edit protected files** (the lint and layer configs, the gate code, fixtures). If paying off an item needs one, stop and ask.
- **One item per commit.** A commit that mixes several is hard to review and hard to revert.
- Quote the scan before and after in your summary: the item, its evidence, and that it is gone.

## What each kind of debt is

- `suppression`: a `//nolint` directive. Fix what it hides so the directive can be removed.
- `untracked-todo`: a TODO, FIXME, HACK, or XXX with no reference. Do it, give it a reference such as `TODO(#123)` or `TODO(adr-0008)`, or delete it if nobody would.
- `complexity`: a function near the lint limits. Extract named steps under characterization tests.
- `coverage`: a package with weak or no tests. Add tests through the public API, one behaviour at a time.
