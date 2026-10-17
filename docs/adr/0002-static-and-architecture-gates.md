# 0002 - Static and architecture gates

- **Status**: accepted
- **Date**: 2026-10-17

## Context

Agents write most of the code, so a rule that depends on being remembered will be broken. Each rule needs a tool that
fails the build, and each tool needs proof that it still fires, because a linter config can be loosened by accident
and a linter that cannot load one package silently drops every finding.

## Decision

1. **One command runs the gates.** `agentctl verify` runs `go vet`, the repository's own analyzers, golangci-lint,
   and race-enabled tests per module, plus arch-go where an `arch-go.yml` lives. `--full` adds govulncheck and the
   rules proof. Every step runs even after a failure, the full output goes to `.agentctl/logs`, and only the tail of a
   failure comes back. A module with no Go code is reported as skipped, never dropped. Fast takes about 12 seconds.
2. **Rules are proven, not assumed.** `agentctl rules test` stages every fixture under `testdata/violations/` plus the
   compliant twin in `testdata/clean/` as one throwaway module and runs each enforcer once. A fixture passes only when
   its own enforcer reports the expected text under its directory; the twin passes only when nothing flags it. A staged
   module that does not compile is rejected first, and an enforcer that cannot run is an error, never a pass.
3. **Lint config adapted from maratori/golangci-lint-config** (MIT), with repository rules on top. Reporting is
   complete: golangci-lint's per-line dedup and per-linter caps are off, because the default hides a finding behind
   another on the same line and an agent then meets it one run later.
4. **Layer rules use arch-go**, not go-arch-lint. arch-go works on Windows, can allowlist the standard library per
   layer, and reports JSON. go-arch-lint fails on Windows glob paths and cannot say "depends on nothing".
5. **Two custom analyzers** for what configuration cannot say, built on `golang.org/x/tools/go/analysis` and shipped as
   a `go vet -vettool`: `barego` (no `go` statement outside `platform/async`) and `domainpurity` (no exported fields or
   struct tags in domain types, no clock reads). `x/tools` is a dependency of the `tools` module only. Pure data
   carriers opt out of the exported-field rule with `//agent:allow-exported-fields <reason>`.
6. **Ports live in the application layer.** The domain imports only a small pure standard-library set (no `context`),
   so repository and clock interfaces are declared by the use cases that need them.
7. **Delivery sees application only.** `interfaces` imports neither domain nor infrastructure; use cases return their
   own result types and a sentinel error for invalid input.

## Consequences

- Positive: a weakened rule fails `rules test` by name; a new rule cannot land without a fixture; agents get one short,
  complete report per run.
- Negative: every rule costs a fixture, and the fixtures must compile.
- Neutral: the engine adds about ten seconds to the full gate.

## Alternatives considered

- **Trust the config**: it erodes silently, and a typo in one fixture can hide every finding.
- **go-arch-lint**: see decision 4.
- **Custom analyzers as golangci-lint plugins**: needs a custom golangci-lint build; a vet tool is simpler and cached.
