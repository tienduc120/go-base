# 0016 - Aggregate roots are marked and have a typed id, New, and Restore

- **Status**: accepted
- **Date**: 2026-10-18

## Context

The domain package already forbids exported fields, struct tags, and clock reads, so an aggregate cannot be changed from outside. Two ways round that remain. An id that is a bare `string` can be passed for any other id, and a value built by hand skips the checks that make it valid. Nothing yet says which types are aggregates, so nothing can hold them to a shape.

## Decision

1. **An aggregate root is marked with a `//agent:aggregate` line in its doc comment**, in the style of `//agent:allow-exported-fields`. The marker names the root; value objects and plain structs carry none and are outside the rule.
2. **A marked root has a typed id**: an unexported field `id` whose type is a defined type (`InvoiceID`), not a predeclared one.
3. **A marked root has `New<T>`, which checks the invariants, and `Restore<T>`, which does not.** Both are package-level functions whose first result is `T` or `*T`.
4. **Only infrastructure calls `Restore<T>`.** A call from any other package, outside tests, is reported: a value rebuilt without the checks can break an invariant the rest of the code relies on. Repositories call it because what they read was valid when it was saved.
5. **The `aggregateshape` analyzer enforces 2 to 4** in `agentlint`. The package segments are flags. The generated service follows all three, with `NewNote` and `RestoreNote`.

## Consequences

- Positive: an aggregate cannot be built any way but through its constructor, outside storage, and ids cannot be swapped without a compile error.
- Negative: the rule applies only to types that carry the marker, so an unmarked aggregate escapes it. A person or the review has to notice a missing marker.
- Neutral: value objects, events, and anything else in the domain are untouched; a rule for them can follow the same marker approach when a project needs one.

## Alternatives considered

- **Guess the roots by shape**: a type with an `ID` method and a constructor is not necessarily a root, and a wrong guess would block valid code.
- **Recognise a root by the presence of `Restore`**: circular, since the rule exists to demand `Restore`.
- **Review only**: the swapped id and the hand-built value are the mistakes a reviewer reads past.
