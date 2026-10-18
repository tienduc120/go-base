---
name: handling-errors
description: Use when a function can fail, when adding an error, a use case outcome, or an HTTP status mapping, or when reviewing how an error crosses a layer. Follows ADR-0014.
---

# Handle errors

Failure is a returned value, and each layer owns the errors it raises. The generated service shows the whole shape in a few files; copy it.

## What each layer does

| Layer | Declares | Does with an error |
|---|---|---|
| `domain` | Sentinels for broken invariants: `ErrEmptyName` | Returns it from the constructor; knows no other layer |
| `application` | Outcomes callers branch on: `ErrInvalidCommand`, `ErrAlreadyExists`, `ErrNotFound` | Wraps a domain error with an outcome; passes a port's error through with context |
| `infrastructure` | Nothing other layers need | Turns a storage condition into the application's error; wraps the rest with context |
| `interfaces` | Nothing | Maps an application error to a status; logs the rest |

## Rules

1. **Return `(T, error)`.** Never panic for a failure a caller could expect, and never invent a `Result[T]`.
2. **Wrap with `%w` and say what failed**: `fmt.Errorf("save invoice: %w", err)`. The message names the operation, not the layer's name or "error".
3. **Keep the cause when you name an outcome**: `fmt.Errorf("%w: %w", ErrInvalidCommand, err)`. `errors.Is` then finds both.
4. **Compare with `errors.Is` and `errors.As`** (`errors.AsType` for a type), never `==` or a type assertion.
5. **Translate at the boundary, once.** A unique-key violation becomes `application.ErrAlreadyExists` inside the adapter; the use case and handler never see a driver error.
6. **"Not found" is not an error in a repository.** `Find` returns `(zero, false, nil)`. The use case chooses `ErrNotFound` when absence is wrong.
7. **Only `interfaces` picks a status**: invalid 400, unauthenticated 401, forbidden 403, absent 404, conflict 409, anything else 500 with a generic body. The detail goes to the log, never the response.
8. **Name them well**: `ErrSomething` for a sentinel, `SomethingError` for a type (`errname` checks).
9. **Never ignore one.** `_ = f()` needs a reason in a comment, and a deferred `Close` on a reader is the only usual case.

## Adding an outcome

1. Write the handler test first: the use case returns `application.ErrX`, expect status N, expect no internal text in the body.
2. Declare `ErrX` in `application` with a comment saying when it happens.
3. Return it from the use case (or from the adapter, if only the storage knows).
4. Add the status case to the handler's `switch`, before the default.

## Before you finish

Walk one failing path from the place it starts to the response, and check that each layer boundary is a translation you can point to.
