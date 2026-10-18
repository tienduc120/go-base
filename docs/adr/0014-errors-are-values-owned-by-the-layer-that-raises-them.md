# 0014 - Errors are values owned by the layer that raises them

- **Status**: accepted
- **Date**: 2026-10-18

## Context

A strict layered service fails in three different ways at once: a rule of the business is broken, a use case cannot do what it was asked, and a dependency misbehaves. If they share one error type, the delivery layer ends up deciding what a storage failure means, or the domain ends up knowing about HTTP. If errors are thrown instead of returned, an expected failure becomes a crash that nothing in the type system points at.

The domain may import only a small standard-library set, so it cannot share an error package with the other layers. The generated service already follows a shape; this record states it so a tool and a reviewer can hold the next service to it.

## Decision

1. **Failure is a returned value.** A function that can fail returns `(T, error)` or `error`, in Go's own form. An expected failure is never a panic. There is no generic `Result[T]` wrapper: it would fight `errcheck`, `errorlint`, `nilerr`, and every library, and add a second way to say what `error` already says.
2. **Each layer owns the errors it raises.** The domain declares sentinel errors for broken invariants (`ErrEmptyName`). The application declares the outcomes of a use case that its callers branch on (`ErrInvalidCommand`, `ErrAlreadyExists`, `ErrNotFound`). Infrastructure declares none of its own that other layers need; it returns the application's errors for conditions the application understands.
3. **A layer translates at its boundary and keeps the cause.** The application wraps a domain error as `fmt.Errorf("%w: %w", ErrInvalidCommand, err)`, so `errors.Is` finds both the outcome and the reason. An adapter turns a storage condition into the application's error (a unique-key violation becomes `application.ErrAlreadyExists`) and wraps everything else with context and `%w` (`save note: %w`). No layer returns another layer's error unwrapped as its own meaning.
4. **Only `interfaces` maps an error to a status.** A request that is invalid is 400, unauthenticated 401, forbidden 403, absent 404, a taken ID or a state conflict 409. Anything the handler does not recognise is 500 with a generic body; the detail is logged, never returned. `net/http` cannot be imported below `interfaces`, so this is enforced by the layer rules.
5. **Absence is the use case's call.** A repository's `Find` reports "not there" as `(zero, false, nil)`. The use case decides whether that is `ErrNotFound` or an ordinary answer.
6. **A handler test pins each mapping**, including that an internal error's text never reaches the client. The generated service has these tests.

## Consequences

- Positive: the domain stays free of transport and storage, a new transport (gRPC, a queue consumer) maps the same application errors without touching a use case, and a status code is decided in exactly one layer.
- Negative: every boundary writes a small translation, and a use case's error set must be kept accurate. The `errors.Is` chain makes that cheap to test but not free to write.
- Neutral: nothing new is enforced mechanically. What already is: `errcheck`, `errorlint` (wrap with `%w`, compare with `errors.Is`), `errname` (`ErrX` and `XError` naming), `nilerr` and `nilnil`, and the layer rules that keep `net/http` out of the domain, application, and infrastructure.

## Alternatives considered

- **A generic `Result[T]` type**: familiar from other languages, but it replaces the one convention every Go tool understands.
- **One shared errors package that every layer imports**: simple, but the domain would then import it, and the domain would carry transport kinds it must not know about. It would also stop the layer rules from being as strict as they are.
- **The domain returns application or transport errors directly**: removes the translation, and makes the domain depend on its callers.
- **Panics for failures the caller cannot handle**: reasonable for a bug, wrong for a taken ID.
