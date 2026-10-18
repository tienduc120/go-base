# go-base

A Go base for strict DDD and clean-architecture cloud microservices, extended by AI agents. It ships almost no features; it ships rules a tool checks, skills an agent follows, and feedback loops for performance and tech debt.

The decisions behind it: [docs/adr/](docs/adr/README.md).

## Status

Phase 1 (static and architecture gates) is done. Enforced today: the lint rules in `.golangci.yml`, the layer rules in `arch-go.yml`, and the analyzers in `tools/analyzers`, each proven by a fixture. Everything marked "planned" below has no enforcer yet. Do not report a rule as enforced unless a tool in this repo checks it.

## How to work

- **Test first, and run the tests.** Write the failing test, run that package's tests and see it fail for the right reason, make it pass, run them again. That is the only checking you do while working.
- **Do not run lint, format, `verify`, `rules test`, or a review between steps.** The pre-commit hook runs `verify` and CI runs `verify --full`; a rejected commit tells you exactly what to fix. Run `verify` by hand only to read the details of a failure the hook already reported.

## Commands

Run from the repository root. There is no Make; `agentctl` is the command surface.

| Need                                                              | Command                                                                                        |
| ----------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Run the gates (hooks and CI do this; by hand only to investigate) | `go run ./tools/agentctl verify` (about 12s); `--full` adds govulncheck and `rules test` |
| Check a commit message                                            | `go run ./tools/agentctl commitmsg <file>` (the commit-msg hook calls it)                    |
| Install the git hooks (once per clone)                            | `lefthook install`                                                                           |
| Check the toolchain and pinned tools                              | `go run ./tools/agentctl doctor` (`--json` for machines)                                   |
| Prove every rule fires on its fixture                             | `go run ./tools/agentctl rules test`                                                         |
| Run anything that could hang                                      | `go run ./tools/agentctl run -- <command> [args...]` (`--timeout`, `--idle`, `--name`) |
| Tests (the TDD loop)                                              | `go test -race -timeout 120s -count=1 ./tools/<package>/`                                    |
| Format (apply)                                                    | `cd tools && golangci-lint fmt ./...`                                                        |
| Layer rules on real code                                          | `arch-go` (reads `arch-go.yml` from the module root)                                       |

Two modules joined by `go.work`: the root module (services, platform, contracts, none yet) and `tools/` (agentctl).
Tool dependencies must never enter the root module.

## Hard rules

- **Test first.** No production code without a test that failed first, for the right reason.
- **Evidence, not claims.** "Tests pass" means you ran them and saw the result. Name every step you skipped and why.
- **Never weaken a gate to make a change pass.** `.golangci.yml`, `arch-go.yml`, and everything under `testdata/` are the rules that judge you: change them only with the user's explicit approval. Fix the cause instead.
- **Every rule has a fixture.** A new or changed rule gets a violation fixture in `testdata/violations/<id>/` (with a `rule.json` naming the enforcer and the text it must report) and `rules test` must stay green. The compliant twin in
  `testdata/clean/` must stay free of findings.
- **Never bypass git hooks** (`--no-verify`). `lefthook.yml` runs `verify` before each commit, `commitmsg` on the message, and `verify --full` before a push.
- **Anything that can run long goes through `agentctl run`.** It enforces a deadline and an idle limit, kills the whole process tree, and keeps the full output in `.agentctl/logs` so only a short summary comes back (exit 124 means it was killed). It also gives `go test` its own `-timeout`, so a hang ends with a goroutine dump showing where it is stuck. Servers never run in the foreground of an agent session.
- **Do not commit `.local/`** (gitignored scratch and notes) and do not install software system-wide without asking.
- **Credit what you reference.** When a design, config, or code comes from a public source, add it to the README "Credits" in the same change. If any text or code is copied or adapted, also keep the upstream license text and record upstream and version in `NOTICE.md`. Only MIT, BSD, and Apache-2.0 material comes in.
- **Comments say why**, never what, and stay short.
- Conventional Commits: `type(scope): subject`, imperative, lowercase.

## Architecture

One bounded context per service under `services/<name>/`, layered inside a compiler-private `internal/`: `domain`, `application`, `infrastructure`, `interfaces`; `cmd/<binary>` is the composition root.

Enforced by `arch-go.yml` (fixture per rule):

- `domain` imports only other domain packages and a small pure standard-library set: no `context`, no I/O, no frameworks.
- `application` imports domain, application, `platform`, `contracts`, and a restricted standard-library set. It declares the ports (repository and clock interfaces) that infrastructure implements.
- `infrastructure` imports domain, application, itself, `platform`, `contracts`; never `interfaces`.
- `interfaces` imports application, itself, `platform`, `contracts`; never domain or infrastructure.
- `platform` never imports a service or `contracts`; `contracts` never imports a service or `platform`.

Enforced by `.golangci.yml` (fixture per rule): no `context.Background`/`TODO` outside `main`; no
`http.Get`/`DefaultClient`/`DefaultTransport`; no `time.Sleep`; no `fmt.Print*`; no `log` (use `slog`); no global logger; no mutable package state; HTTP requests carry a context; rows and response bodies are closed; no `SELECT *`; every `//nolint` names its linter and gives a reason.

Enforced by the analyzers in `tools/analyzers` (run as `go vet -vettool`, fixture per rule): no `go` statement outside `platform/async`; domain types have no exported fields (opt out per type with
`//agent:allow-exported-fields <reason>`), no struct tags, and never call `time.Now`/`Since`/`Until`.

The Go compiler already forbids importing another service's `internal/` packages.

Planned, not enforced yet: aggregates have `New` / `Restore` constructors; cross-aggregate effects go through events and an outbox; every list query has a limit; no query inside a loop.

## Things that will surprise you

Grow this by correction: when any tool gets something wrong, add the line.

- **golangci-lint dedups per line by default**; here `uniq-by-line` is off, so one line can carry several findings. If a rule seems silent, check that it is not hidden behind another finding on the same line.
- **One fixture that does not compile silences every finding** from a linter, so `rules test` type-checks the staged module first and names the broken fixture.
- **Fixture import paths are relative to the staged module `fixture.test/<fixture-id>/...`**, not this repository's module path.
- **On Windows, Python's `write_text` writes CRLF** and `.gitattributes` forces LF; write bytes with explicit `\n`.
  Shell heredocs can also swallow double backslashes: write files that contain escapes with the editor tools.
- **`rm` with a variable or wildcard target is refused by the sandbox**; remove by literal path.
