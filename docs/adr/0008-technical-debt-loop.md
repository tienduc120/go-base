# 0008 - The technical-debt loop

- **Status**: accepted
- **Date**: 2026-10-18

## Context

The gates stop what they can name, so a clean build says nothing about the debt they allow: a suppression, a TODO nobody owns, a function creeping towards the complexity limit, a package with thin tests. Left alone that debt grows with every change, and an agent asked to "clean up" tends either to do nothing measurable or to make a risky change it cannot prove safe.

## Decision

1. **Detect what the gates allow.** `agentctl debt scan` reads the code with the Go parser (so a directive inside a string is not mistaken for one) and lists four kinds: `//nolint` suppressions, TODO/FIXME/HACK/XXX markers with no reference, functions at 15 cyclomatic complexity or 80 lines (below the lint limits of 30 and 100), and, on request because it runs the tests, packages with under 70% coverage or no tests. Items are ranked by a documented score.
2. **Items have stable ids.** An id is a hash of the kind, file, enclosing function, and the thing found, never of a line number, so a decision recorded about an item survives edits that only move code.
3. **Decisions live in a committed register** (`docs/debt/register.json`): `confirmed`, `accepted`, or `false-positive`, each with the evidence the decision was made on. Items it does not mention are new.
4. **A claim needs evidence.** `debt confirm` only records a decision about an item the fresh scan still finds, and stores the scan's evidence rather than the caller's words. A reason is required for `accepted` and `false-positive`.
5. **Acceptance expires.** Accepted debt needs a reason and an expiry within 180 days; when it passes, the item is `expired` and fails the check. Agents may confirm and dismiss; accepting debt is a person's decision.
6. **The ratchet is `debt check`.** It fails on any item with no decision and on any expired acceptance, naming the item. Existing debt is adopted once with `debt baseline`, which accepts everything currently untriaged for the maximum 180 days. After that only new debt fails.
7. **Recovery starts from confirmed debt.** `debt next` prints the worst confirmed item with a playbook for its kind. The `debt-recovery` skill adds the rules: pin behaviour first, one item per commit, never hide debt with a new suppression or a raised threshold.
8. **A refactor may only add tests.** The `commit-msg` hook inspects the staged diff of a `refactor:` commit and refuses one that edits or deletes an existing test line, because an edited expectation means behaviour changed. Blank lines and comments are ignored. If git cannot be asked, it warns and allows the commit.

## Consequences

- Positive: debt is visible, ranked, and cannot grow unnoticed; an agent cleaning up works from evidence and cannot claim a refactor while changing behaviour.
- Negative: the first run on a repository with a lot of debt needs a baseline decision; and the register is one more file to review.
- Neutral: the scan is conservative about what counts as debt. It does not try to be a general code-smell detector, because the lint gate already is one.

## Wiring

The commands, the register, the refactor guard, and the `DebtStep` that runs the ratchet inside `agentctl verify` exist. `verify` is protected gate code, so adding `steps = append(steps, DebtStep(env))` after the markdown step in `Plan` needs the owner's approval. Until then `agentctl debt check` is run by hand, and in CI once it is added to the workflow.

## Alternatives considered

- **Counts only** (a ratchet on how many suppressions exist): cannot say *which* one is new, and lets one be swapped for another.
- **A general code-quality service** (SonarQube and similar): a service to run, and it reports far more than a person or an agent will triage.
- **Fail on every item**: makes adoption impossible; a baseline with an expiry gives the same discipline going forward.
