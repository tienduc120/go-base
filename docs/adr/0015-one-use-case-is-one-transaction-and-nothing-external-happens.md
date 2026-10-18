# 0015 - One use case is one transaction and nothing external happens inside it

- **Status**: accepted
- **Date**: 2026-10-18

## Context

A database transaction can be rolled back. An email, a payment, a message to a broker, or any call to another service cannot. When one is made inside a transaction, a later failure rolls the data back and leaves the effect behind; and even when nothing fails, the transaction holds its locks for as long as the call takes, so a slow dependency becomes a stalled database.

These mistakes are easy to write, pass every test that uses fakes, and show up only under load or failure. They are exactly what a reviewer misses and a tool can see.

## Decision

1. **A use case is one transaction, opened through a port declared in `application`.** The port is a single method, `InTransaction(ctx, fn func(ctx context.Context) error) error`, which runs `fn` in a transaction and commits when it returns nil. A transaction is never opened in `domain`, `infrastructure`, or `interfaces`, and never inside another.
2. **Inside the function passed to `InTransaction`, only repositories, the outbox, and the clock are used**, plus the domain's own values and the standard library. A call on any other interface is an external effect, and a call into `net/http`, `net`, `net/smtp`, or `net/rpc` is a network call.
3. **An external effect is a row, not a call.** The use case writes an outbox row in the same transaction; a relay dispatches it after commit, retries from the database, and consumers deduplicate. When a call cannot be avoided it happens before the transaction; if it must come after, a failure there is logged and retried, never returned as if the use case had failed.
4. **The `txboundary` analyzer enforces 1 and 2** in `agentlint`: it reports a transaction opened outside `application`, a nested transaction, a network call inside one, and a call on an interface whose name does not end in `Repository`, `Outbox`, or `Clock`. The method name and the allowed suffixes are flags. A repository port is recognised by that name, which every generated service already follows.
5. **Nothing here provides the relay, the outbox table, or the transaction runner.** They depend on the project's broker and database, and a project adds them. The rules and the checker are what every project shares.

## Consequences

- Positive: the usual transaction mistakes fail at commit, with the line and the reason, before they meet a load that exposes them.
- Negative: the check goes by names, so a port called `UserStore` is treated as external until it is renamed `UserRepository` or the flag is widened. A call that reaches the network through a repository's implementation is not seen, because the analyzer reads the use case, not the adapter.
- Neutral: a project that does not use transactions never meets the analyzer; it looks only for calls to the runner method.

## Alternatives considered

- **Review only**: the rule is old and widely known, and still broken in most code bases that rely on reading.
- **A transaction helper that forbids side effects at run time**: it cannot see a call to a type it does not know, and it fails in production instead of at commit.
- **Allow external calls inside a transaction with a compensating action**: compensation is the fallback for a call that cannot be moved, not a licence to put every call there.
- **Provide the outbox and relay here**: couples the base to one broker and one schema, which a real project will want to choose.
