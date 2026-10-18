---
name: writing-use-cases
description: Use when adding or changing a use case in a service's application layer, especially one that writes to storage, sends an email, calls another service, or publishes an event. Follows ADR-0015 and the query rules.
---

# Write a use case

A use case is one operation the service offers, written as one method in `internal/application`. It orchestrates; the rules of the business live in `domain`, the storage in `infrastructure`.

## Shape

1. **Take a command, return a result and an error.** Plain structs with primitive fields; no HTTP types, no domain types in the command.
2. **Validate by building domain values.** Call the domain constructors (`NewInvoiceID`, `NewInvoice`); a failure becomes `ErrInvalidCommand` wrapping the cause (see the `handling-errors` skill).
3. **Declare what you need as ports in this package**: repositories, a clock, an outbox. Infrastructure implements them. Name a repository port `…Repository`; the transaction check recognises it by that suffix.
4. **Take time from the `Clock` port.** The domain never reads the clock.

## The transaction

- **One use case is one transaction**, opened once through the `Transactor` port: `tx.InTransaction(ctx, func(ctx context.Context) error { ... })`. No second transaction inside, none opened anywhere else.
- **Inside it use only repositories, the outbox, and the clock**, plus domain values. The `txboundary` check reports the rest.
- **Never call another service, send mail, publish to a broker, or touch the network inside it.** It cannot be rolled back and it holds the locks while it waits.

## Side effects

| You need to | Do |
|---|---|
| Notify, email, publish | Write an outbox row in the same transaction; a relay sends it after commit and retries |
| Call a dependency whose answer you need | Call it **before** the transaction and pass the answer in |
| Call something after the data is committed | Do it after `InTransaction` returns; if it fails, log and retry it, never return it as the use case's failure |
| Undo a call you could not avoid putting first | Register a compensating action |

The outbox, the relay, and the transaction runner are your project's to provide; the rules above apply to whatever you build.

## Queries

- A use case that touches the database asserts a query budget in its test: `ctx, budget := postgres.WithBudget(...)`, run it, `budget.Err()` is nil. A query per row fails there.
- Fetch the set you need in one query (`= ANY($1)` or a join), not one by one.

## Retries and duplicates

- Assume the caller will retry. Take the new ID from the command, so a retry hits `ErrAlreadyExists` instead of creating a second copy.
- A consumer of an outbox message deduplicates on its message ID.

## Testing

Test it with hand-written fakes of its ports and a fixed clock, and count the calls (see `writing-go-tests`). Assert the stored state through the fake repository, and that nothing external was called when the transaction failed.
