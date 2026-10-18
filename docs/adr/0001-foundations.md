# 0001 - Foundations

- **Status**: accepted
- **Date**: 2026-10-17

## Context

AI agents extend this base, so correctness must come from tools, not from prose an agent may skip. The target is strict DDD and clean-architecture cloud microservices that stay maintainable as teams and services multiply.

## Decision

1. **Enforcement first.** The base ships almost no features. Every rule has an enforcer and a fixture that proves the enforcer fires; a rule without both is advice, not a rule.
2. **Go, latest stable.** `go 1.27.2` in `go.mod`; `GOTOOLCHAIN=auto` fetches it.
3. **No Make.** All commands go through `agentctl`, a Go program in `tools/`. It behaves the same on Windows, Linux, and CI, is unit tested, and gives agents one self-describing command surface.
4. **Two modules joined by `go.work`.** The root module holds services, platform, and contracts. `tools/` has its own `go.mod` so tool dependencies never reach a service.
5. **One bounded context per service**, one deployable per context, inside a compiler-private `internal/`. Layers: `domain`, `application`, `infrastructure`, `interfaces`. Cross-service imports are limited to `contracts/` and `platform/`.
6. **Protobuf contracts checked by buf** between services, with `buf breaking` against main as a gate. REST only at the edge.
7. **Cloud-agnostic runtime:** containers, Kubernetes manifests, OTLP telemetry, 12-factor configuration. PostgreSQL through pgx and sqlc, one schema per service.
8. **Pinned developer tools** are checked by `agentctl doctor`, which reports and never installs. golangci-lint is installed as a prebuilt binary, not through `go install` or `go tool`, because its documentation advises against both.
9. **AI tooling:** `AGENTS.md` is the shared source and `CLAUDE.md` imports it. Hard gates arrive as agent hooks that call `agentctl`; tools without hooks get git hooks and CI.

## Consequences

- Positive: one toolchain, no shell dialect differences, rules that cannot silently erode.
- Negative: `agentctl` is code to maintain, and `go run` adds a fraction of a second per call.
- Rules named in `AGENTS.md` under "Target architecture" are not enforced until their phase lands.

## Alternatives considered

- **Make or Taskfile:** an extra install on Windows, shell-dialect differences, logic that cannot be unit tested.
- **One module per service with `go.work`:** strongest isolation, but every tool must run per module. Each service stays self-contained so this remains a mechanical change later.
- **OpenAPI between services:** weaker typing and no built-in breaking-change gate.
- **A cloud-specific runtime:** faster to a first deploy, but couples the base to one provider.
