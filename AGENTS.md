# go-base

A Go base for strict DDD and clean-architecture cloud microservices, extended by AI agents. It ships almost no features; it ships rules a tool checks, skills an agent follows, and feedback loops for performance and tech debt.

The decisions behind it: [docs/adr/](docs/adr/README.md).

## Status

Phase 1 (static and architecture gates) is done, and the bounded-command runner (`agentctl run`) from phase 4 is in. Enforced today: the lint rules in `.golangci.yml`, the layer rules in `arch-go.yml`, and the analyzers in `tools/analyzers`, each proven by a fixture, plus Markdown style (one paragraph per line). The agent guard (below) is in. The pprof diagnosis loop (`agentctl perf`, ADR-0006) is in. Goroutine-leak checks in tests (ADR-0007) are in. The technical-debt loop (`agentctl debt`, ADR-0008) is in, and `verify` runs its ratchet. The service template (`agentctl new service`, ADR-0010) is in, with `services/example` as its checked witness. The query gates (`agentctl db check`, ADR-0011) are in. The runtime database layer (`platform/postgres`: bounded pools and a per-request query budget, ADR-0012) is in. Not built yet: a Postgres adapter in the service template, and a scenario runner that drives load for you. Everything marked "planned" below has no enforcer yet. Do not report a rule as enforced unless a tool in this repo checks it.

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
| Find what owns the time, memory, locks, or goroutines | `go run ./tools/agentctl perf capture --url http://127.0.0.1:<pprof port>` while load runs; `perf analyze [--base before.pprof] <file>` for one profile (skill: `perf-diagnose`) |
| Find, triage, and pay off the debt the gates allow | `go run ./tools/agentctl debt scan`, then `debt confirm <id>`, `debt next`, `debt check` (skill: `debt-recovery`) |
| Start a new service | `go run ./tools/agentctl new service <name> --aggregate <Name>` (skill: `new-service`); never create `services/<name>` by hand |
| Judge a service's SQL: query rules and each query's plan | `go run ./tools/agentctl db check [--no-plan] [service...]` (needs Docker for plans; skill: `writing-queries`) |
| Run the tests that need a database | `go run ./tools/agentctl db test [package...]` starts a throwaway Postgres and sets `TEST_DATABASE_URL`; without it those tests skip loudly |
| Run anything that could hang                                      | `go run ./tools/agentctl run -- <command> [args...]` (`--timeout`, `--idle`, `--name`) |
| Tests (the TDD loop)                                              | `go test -race -timeout 120s -count=1 ./tools/<package>/`                                    |
| Format Go (apply)                                                 | `cd tools && golangci-lint fmt ./...`                                                        |
| Format Markdown (apply)                                           | `go run ./tools/agentctl fmt md [file...]`                                                   |
| Layer rules on real code                                          | `arch-go` (reads `arch-go.yml` from the module root)                                       |

Two modules joined by `go.work`: the root module (services, platform, contracts) and `tools/` (agentctl). Tool dependencies must never enter the root module.

## Hard rules

- **Test first.** No production code without a test that failed first, for the right reason.
- **Start a service with the generator.** `agentctl new service` gives a layout that already passes every gate; `services/example` is its generated witness, so change the templates in `tools/internal/scaffold/templates` and regenerate it, never edit it (ADR-0010).
- **SQL goes in sqlc query files and passes `agentctl db check`.** Queries live in `services/<name>/internal/infrastructure/postgres/queries`, migrations beside them as forward-only `NNNN_name.sql`. Fix a finding at its cause; accepting one (`-- agent:allow <rule>: <why>`) is debt, so tell the user. Follow the `writing-queries` skill (ADR-0011).
- **Open a database pool only with `platform/postgres.Connect`.** It refuses a zero timeout and sets the server-side statement, lock, and idle-in-transaction limits. A use-case test asserts `postgres.WithBudget(...).Err()` is nil, so a query in a loop fails the test (ADR-0012). Tests that need a database use `platform/postgres/postgrestest` and run with `agentctl db test`.
- **Start goroutines only through `platform/async`** (`async.Run`): it cancels, waits, and turns a panic into an error. A service serves pprof on a loopback-only port with `platform/debugserver`, never on the serving port (ADR-0009).
- **A package that starts goroutines, subprocesses, or servers checks for leaks.** Its `TestMain` calls `goleak.VerifyTestMain(m)` (ADR-0007), so a test that leaves something running fails the package. Never `time.Sleep` in a test.
- **New debt is triaged, not hidden.** A `//nolint`, a TODO with no reference, or a function near the complexity limit is debt: fix the cause, or ask the user to accept it with a reason and an expiry. A `refactor:` commit may only add tests, never edit one (the commit-msg hook enforces it). Follow the `debt-recovery` skill.
- **Never optimise without a profile.** Follow the `perf-diagnose` skill in `.claude/skills/`: capture, read the findings, make one change, capture again, and quote the numbers before and after.
- **Evidence, not claims.** "Tests pass" means you ran them and saw the result. Name every step you skipped and why.
- **Never weaken a gate to make a change pass.** `.golangci.yml`, `arch-go.yml`, and everything under `testdata/` are the rules that judge you: change them only with the user's explicit approval. Fix the cause instead.
- **Every rule has a fixture.** A new or changed rule gets a violation fixture in `testdata/violations/<id>/` (with a `rule.json` naming the enforcer and the text it must report) and `rules test` must stay green. The compliant twin in `testdata/clean/` must stay free of findings.
- **Never bypass git hooks** (`--no-verify`). `lefthook.yml` runs `verify` before each commit, `commitmsg` on the message, and `verify --full` before a push.
- **Anything that can run long goes through `agentctl run`.** It enforces a deadline and an idle limit, kills the whole process tree, and keeps the full output in `.agentctl/logs` so only a short summary comes back (exit 124 means it was killed). It also gives `go test` its own `-timeout`, so a hang ends with a goroutine dump showing where it is stuck. Servers never run in the foreground of an agent session.
- **Do not commit `.local/`** (gitignored scratch and notes) and do not install software system-wide without asking.
- **Credit what you reference.** When a design, config, or code comes from a public source, add it to the README "Credits" in the same change. If any text or code is copied or adapted, also keep the upstream license text and record upstream and version in `NOTICE.md`. Only MIT, BSD, and Apache-2.0 material comes in.
- **Markdown: one paragraph or list item per line.** Never hard-wrap prose; editors wrap for themselves and hand-wrapped lines show as arbitrary breaks. `agentctl fmt md` fixes it and `verify` checks it.
- **Comments say why**, never what, and stay short.
- Conventional Commits: `type(scope): subject`, imperative, lowercase.

## Agent guard

In Claude Code, `.claude/settings.json` runs `agentctl hook` before every edit and shell command and refuses, with a reason you can act on:

- changing a committed rule file: `.golangci.yml`, `arch-go.yml`, `lefthook.yml`, the CI workflow, `.claude/settings.json`, the gate code (`tools/analyzers`, `tools/agentlint`, `tools/internal/{rules,verify,commitmsg,guard}`), anything under `testdata/`, and the licenses. New files are always allowed, so adding a fixture or an analyzer is fine; protection starts once a file is committed.
- skipping the git hooks: `--no-verify`, `HUSKY=0`, `LEFTHOOK=0`, `core.hooksPath`, `lefthook uninstall`.
- writing, moving, or deleting a protected path through the shell.
- commands that never return: `go test` without `-timeout`, a service run in the foreground, `docker compose up` without `-d`, `tail -f`. Use `agentctl run` instead.

When you are blocked, fix the cause. Do not look for another way to make the same change. If the rule really should change, ask the user: they approve by starting the session with `AGENTCTL_ALLOW_PROTECTED=1`, which only lifts the protected-file block. The guard is a guard rail, not a security boundary; `CODEOWNERS`, the git hooks, and CI are the boundary. Codex and Cursor do not run these hooks. Decision record: [ADR-0005](docs/adr/0005-agent-guard.md).

## Architecture

One bounded context per service under `services/<name>/`, layered inside a compiler-private `internal/`: `domain`, `application`, `infrastructure`, `interfaces`; `cmd/<binary>` is the composition root.

Enforced by `arch-go.yml` (fixture per rule):

- `domain` imports only other domain packages and a small pure standard-library set: no `context`, no I/O, no frameworks.
- `application` imports domain, application, `platform`, `contracts`, and a restricted standard-library set. It declares the ports (repository and clock interfaces) that infrastructure implements.
- `infrastructure` imports domain, application, itself, `platform`, `contracts`; never `interfaces`.
- `interfaces` imports application, itself, `platform`, `contracts`; never domain or infrastructure.
- `platform` never imports a service or `contracts`; `contracts` never imports a service or `platform`.

Enforced by `.golangci.yml` (fixture per rule): no `context.Background`/`TODO` outside `main`; no `http.Get`/`DefaultClient`/`DefaultTransport`; no `time.Sleep`; no `fmt.Print*`; no `log` (use `slog`); no global logger; no mutable package state; HTTP requests carry a context; rows and response bodies are closed; no `SELECT *`; every `//nolint` names its linter and gives a reason.

Enforced by `agentctl db check` on every service's query files (fixture per rule, ADR-0011): no `SELECT *`; `UPDATE`/`DELETE` need `WHERE`; a `:many` query needs `LIMIT` and `ORDER BY`; no `OFFSET` or `ORDER BY random()`; at most three joins; query names start with Create, Get, List, Update, Delete, Count or Exists; and on a throwaway Postgres, no sequential scan and no index walked end to end only to filter.

Enforced by the analyzers in `tools/analyzers` (run as `go vet -vettool`, fixture per rule): no `go` statement outside `platform/async`; domain types have no exported fields (opt out per type with `//agent:allow-exported-fields <reason>`), no struct tags, and never call `time.Now`/`Since`/`Until`.

The Go compiler already forbids importing another service's `internal/` packages.

Planned, not enforced yet: aggregates have `New` / `Restore` constructors; cross-aggregate effects go through events and an outbox; no query inside a loop is enforced by the query budget in tests that assert it, not yet by a tool across the repository.

## Things that will surprise you

Grow this by correction: when any tool gets something wrong, add the line.

- **golangci-lint dedups per line by default**; here `uniq-by-line` is off, so one line can carry several findings. If a rule seems silent, check that it is not hidden behind another finding on the same line.
- **One fixture that does not compile silences every finding** from a linter, so `rules test` type-checks the staged module first and names the broken fixture.
- **Fixture import paths are relative to the staged module `fixture.test/<fixture-id>/...`**, not this repository's module path.
- **On Windows, Python's `write_text` writes CRLF** and `.gitattributes` forces LF; write bytes with explicit `\n`. Shell heredocs can also swallow double backslashes: write files that contain escapes with the editor tools.
- **`rm` with a variable or wildcard target is refused by the sandbox**; remove by literal path.
- **Never name a package `httpapi`.** On the maintainer's Windows machine Bitdefender quarantines a test binary called `httpapi.test.exe` (a heuristic detection, `Gen:Variant.Yogi`), and `go test` fails with `Access is denied`. The binary's name is the trigger, not its content; the delivery package is `interfaces/rest`. If a test build fails like this, rename the package rather than rebuilding in a loop.
