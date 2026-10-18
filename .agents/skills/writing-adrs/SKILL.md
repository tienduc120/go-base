---
name: writing-adrs
description: Use when a change needs a recorded decision (a new dependency, a hosted service, a pinned major version moving, a published contract, or a rule a tool will enforce), or when asked to write, review, or supersede an architecture decision record.
---

# Write an architecture decision record

A record exists so that the next person, or the next agent, does not reopen a settled question or quietly undo it. Write it before the change that depends on it, not after.

## When one is required

- A new external dependency, a hosted service, or a pinned major version moving.
- A change to a published contract.
- A rule that a tool will enforce, or a gate that is added, loosened, or removed.
- Anything the user decided in conversation that a future change could reasonably contradict.

A bug fix, a refactor, and an implementation detail inside one package do not need one.

## Steps

1. **State the question in one sentence**: "This record decides X." If you cannot, the question is still vague; stop and sharpen it.
2. **List the real options** (at least two) and what separates them. Do not invent a weak option so the chosen one looks strong.
3. **Confirm with the user before you draft** when the choice is theirs to make: say the question, the options, and your recommendation, and wait. Draft first only when they have already decided.
4. **Start the file**: `go run ./tools/agentctl adr new "<title>"`. It numbers the record, writes the template as a `proposed` record, and adds its row to the index.
5. **Fill every section.** Context says what forces the decision. Decision is numbered sentences a tool or a reviewer can check. Consequences name what gets easier, harder, and different elsewhere. Alternatives say why each lost.
6. **Set the status.** Leave it `proposed` until the user agrees; set `accepted` (in the file and in the index row) when they do, or when they made the decision themselves.
7. **Run `go run ./tools/agentctl adr check`.** It fails on a gap in the numbering, a missing section, template text left in an accepted record, or an index row that disagrees with the file.

## Writing it well

- Write for someone who was not in the conversation and has only this file.
- Put the rule in a form that can be checked: "every pool is opened by `platform/postgres.Connect`", not "use good connection practices".
- Name the cost honestly. A record with no negative consequence has not been thought through.
- Link what it depends on or replaces by number (`ADR-0011`).
- One decision per record. If the Decision section needs an "and" between two unrelated rules, split it.

## After acceptance

- **Never edit an accepted record's reasoning.** To change a decision, write a new record that supersedes it, then change the old one's status line (and its index row) to `superseded by NNNN`. Fixing a typo is fine; changing what it decided is not.
- When a rule starts being enforced by a tool, say so in `AGENTS.md` and move it out of the "Planned" line there.
