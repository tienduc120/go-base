---
name: new-service
description: Use when adding a new service or bounded context under services/. Generates the layered skeleton with agentctl so it already passes every gate, then guides replacing the sample aggregate with the real domain.
---

# Add a service

Never create a service directory by hand. The generator produces the layout, ports, composition root, graceful shutdown, debug listener, and tests that the gates require, so you start from something that already passes.

## Steps

1. **Name it.** The service is one bounded context: lowercase words joined by hyphens (`billing`, `order-intake`). The first aggregate is PascalCase (`Invoice`). If you cannot name the aggregate, you do not yet know what the service owns; ask the user.
2. **Generate.** `go run ./tools/agentctl new service <name> --aggregate <Name>`. It refuses to touch a service that exists. Read the file list it prints.
3. **Commit the untouched skeleton first** (`feat(<name>): add the service skeleton`). The pre-commit hook proves it meets the gates, and the next diff then shows only your domain.
4. **Change the domain test first**, then the domain. The sample aggregate has an ID, a name, and a creation time. Replace the fields and rules with the real invariants; keep the constructor the only way to build a valid value and keep the time an argument, never `time.Now`.
5. **Grow outward.** New behaviour is a use case in `application` with a test; a new dependency is a port declared there and an adapter in `infrastructure`; a new endpoint is a route in `interfaces/rest`. Do not import across layers; `arch-go` will say which import broke the rule.
6. **Load.** `cmd/<name>-load` is a skeleton: its scenario fails on purpose until you write the requests your users make. Do that before you profile (skill: `perf-diagnose`).
7. **Storage.** The service runs in memory until `DATABASE_URL` is set, then on Postgres. Change the schema with a new migration and the queries in `internal/infrastructure/postgres`, run `sqlc generate` there, and follow the `writing-queries` skill. `go run ./tools/agentctl db test ./services/<name>/...` runs the Postgres adapter and the service against a real server.

## Do not

- Copy `services/example` to start a service. It is generated output and is compared with the templates by a test; a hand copy drifts.
- Edit `services/example` to experiment. Change the templates in `tools/internal/scaffold/templates`, delete `services/example`, and regenerate it with `go run ./tools/agentctl new service example --aggregate Note`.
- Start a goroutine in the service. Use `async.Run`.
- Put profiling on the serving port. The debug listener exists and is loopback only.
