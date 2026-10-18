# 0010 - The service template

- **Status**: accepted
- **Date**: 2026-10-18

## Context

Every rule so far judges code after it is written. An agent starting a new service from a blank directory makes the same first mistakes each time: a flat package, a port declared in infrastructure, a `main` with no shutdown, no debug listener, a handler with no body cap. The gates catch each one, but the cheapest fix is to never write them.

## Decision

1. **`agentctl new service <name> [--aggregate Name]` generates a whole layered service** under `services/<name>/`: a domain aggregate with a constructor that checks its invariants, a create use case with the repository and clock ports declared in the application layer, an in-memory adapter, an HTTP handler, and a `cmd/<name>-api` composition root. Every file with logic has a test beside it, and the composition root has a test that starts both listeners and shuts them down.
2. **The templates are the source of truth and are embedded in `agentctl`** (`tools/internal/scaffold/templates`). Output goes through `go/format`. The generator refuses to write anything if any target file already exists, because a generator that overwrites hand-written code is worse than none.
3. **A committed witness, `services/example`, is the generated output for `example` / `Note`.** It lives in the root module, so it goes through every gate with the rest of the code: lint, vet, the analyzers, `arch-go`, and its own tests under the race detector. A test regenerates it and fails on any difference, so a template cannot drift from the gates, and a gate change that the template no longer meets fails on the witness first.
4. **`platform/httpserver` is how a service listens.** It requires every timeout to be set (a zero is a bug, not "no limit"), binds at construction, refuses a nil handler, and drains in-flight requests when its context ends. `platform/debugserver` is built on it.
5. **The generated `main` reads `ADDR` and `DEBUG_ADDR`, handles SIGTERM, and runs both servers through `async.Run`**, so a failure of either stops the other. The debug address defaults to loopback and is refused otherwise.

## Consequences

- Positive: a new service meets every gate with zero edits, and the first thing an agent changes is domain logic rather than plumbing. A gate that a generated service fails is a gate bug found immediately.
- Negative: the witness is a real service in the repository that does nothing useful, and a template change means regenerating it. The drift test names the command.
- Neutral: the template has one aggregate and one use case. It shows the shape; it does not model a domain.

## Alternatives considered

- **A copyable example directory with no generator**: it rots, and renaming by hand is where mistakes enter.
- **`cookiecutter` or `copier`**: a Python dependency for what `text/template` and `go/format` do in a few hundred lines, and the templates could not be tested with the Go toolchain here.
- **Generating from a protobuf or OpenAPI spec**: contracts are not built yet; the template can take a contract input when they are.
