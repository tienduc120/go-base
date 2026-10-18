---
name: reviewing-code
description: Use when asked to review a change, before reporting work as finished, or when checking a pull request. A checklist of what the gates cannot see, grouped by what to look at, with the skill to follow for each.
---

# Review a change

The gates catch what a tool can name: layer imports, lint, races, leaks, query rules. A review is for what they cannot: whether the change is the right one, named well, and honest about what it did not check. Start with the gate output, then read the change in this order.

## 1. Is it the right change

- It does what was asked and nothing else. Extra features, speculative options, and drive-by renames are separate changes.
- It was written test first: the test fails without the code. Read the tests before the code (`writing-go-tests`).
- A decision that a future change could contradict has a record (`writing-adrs`).
- No gate was loosened, no `//nolint` or `agent:allow` added without a reason and the user told. A suppression is debt (`debt-recovery`).

## 2. Names and shape

- A name says what the thing is for in the domain's words, not how it works or which layer it sits in. Units are in the name when a value has one (`timeoutSeconds`).
- A function does one thing at one level. If its comment needs "and", it is two functions.
- A public API, an error message, or a log line exposes no internal jargon.
- Comments say why, never what, and stay short.

## 3. Layers and domain

- Business rules are in `domain`; use cases only orchestrate; adapters only translate; `interfaces` only maps. A rule found in a handler or a repository is in the wrong place.
- An aggregate is built through `New…` and rebuilt by a repository through `Restore…` only; ids are typed (`aggregateshape`).
- Time and randomness arrive as ports or arguments, never read inside the domain.
- Delete, create, and update go through the aggregate, not around it.

## 4. Failure

- Every error is wrapped with context and `%w`, handled once, and translated at its layer boundary; only `interfaces` picks a status (`handling-errors`).
- Nothing internal reaches a client. Expected failures are returned, never panics.
- A new outcome has a handler test that pins its status.

## 5. Transactions and effects

- A use case is one transaction, opened once, containing only repositories, the outbox, and the clock (`writing-use-cases`, `txboundary`).
- An external effect is an outbox row or happens after commit, and a retry cannot do it twice.

## 6. Data and queries

- Each query was run through `agentctl db check`: no star, bounded and ordered lists, keyset pagination, an index for every filter (`writing-queries`).
- A use case that touches the database asserts a query budget. There is no query per row.
- A migration is new and forward-only, and the old and new code can run together during a deploy.

## 7. Concurrency and resources

- Goroutines start only through `platform/async`; every pool, server, and client has a timeout; every long call takes the caller's context.
- Tests leave nothing running and sleep nowhere.

## 8. Evidence

- The author says which commands were run and what they printed, and names what was skipped and why. "It should work" is not evidence.
- For a performance claim, two profiles under the same load (`perf-diagnose`).

## How to report

Lead with whether it should merge. Then list findings, worst first, each with the file and line, what is wrong, and what to do. Mark what you did not read. Do not pad a clean review with praise or invented nits.
