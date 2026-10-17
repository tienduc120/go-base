# go-base

A Go base for strict DDD and clean-architecture cloud microservices, extended by AI agents. It ships almost no
features; it ships rules a tool checks, skills an agent follows, and feedback loops for performance and tech debt.
The decisions behind it: [docs/adr/0001-foundations.md](docs/adr/0001-foundations.md).

## Status

Phase 0 (foundations) only. What exists: the module layout and `agentctl doctor`. Everything under "Target
architecture" is planned and not yet enforced. Do not report a rule as enforced unless a tool in this repo checks it.

## Commands

Run from the repository root. There is no Make; `agentctl` is the command surface.

| Need | Command |
|---|---|
| Check the toolchain and pinned tools | `go run ./tools/agentctl doctor` (`--json` for machines) |
| Tests | `go test -race -timeout 120s -count=1 ./tools/...` |
| Format check | `gofumpt -l tools` (prints files that need formatting) |
| Vet | `go vet ./tools/...` |
| Lint | `cd tools && golangci-lint run ./...` |

Two modules joined by `go.work`: the root module (services, platform, contracts, none yet) and `tools/` (agentctl).
Tool dependencies must never enter the root module.

## Hard rules

- **Test first.** No production code without a test that failed first, for the right reason.
- **Evidence, not claims.** "Tests pass" means you ran them and saw the result. Name every step you skipped and why.
- **Never weaken a gate to make a change pass.** Do not loosen lint config, delete or skip tests, or raise a
  threshold without the user's explicit approval. Fix the cause.
- **Never bypass git hooks** (`--no-verify`).
- **Anything that can run long gets a timeout.** Tests always run with `-timeout`; servers never run in the
  foreground of an agent session.
- **Do not commit `.local/`** (gitignored scratch and notes) and do not install software system-wide without asking.
- **Credit what you reference.** When a design, config, or code comes from a public source, add it to the README
  "Credits" in the same change. If any text or code is copied or adapted, also keep the upstream license text and
  record upstream and version in `NOTICE.md`. Only MIT, BSD, and Apache-2.0 material comes in.
- **Comments say why**, never what, and stay short.
- Conventional Commits: `type(scope): subject`, imperative, lowercase.

## Target architecture (planned)

One bounded context per service under `services/<name>/`, layered `domain` -> `application` ->
`infrastructure` / `interfaces` inside a compiler-private `internal/`.

- `domain` imports the standard library only; no `context`, no I/O, no `time.Now()`, no struct tags.
- A service never imports another service; cross-service imports are `contracts/` and `platform/` only.
- Aggregates have unexported fields and `New` / `Restore` constructors; one repository per aggregate root.
- Cross-aggregate effects go through domain events and an outbox; consumers are idempotent.
- Every outbound call has a deadline; every list query has a limit; no `SELECT *`; no query inside a loop.

Each rule above gets an enforcer and a fixture proving it fires before it is relied on.
