# Third-party material

Only MIT, BSD, and Apache-2.0 material is brought in. For each item keep the upstream license text in the repository, record the upstream and version below, and prefer a dependency to a copy. A new dependency needs an ADR.

## Vendored or adapted

| Item                        | Upstream                                         | Version                                               | License                                    | Where                                                                             |
| --------------------------- | ------------------------------------------------ | ----------------------------------------------------- | ------------------------------------------ | --------------------------------------------------------------------------------- |
| golangci-lint configuration | https://github.com/maratori/golangci-lint-config | main as of 2026-10-17 (targets golangci-lint v2.14.0) | MIT, Copyright (c) 2021-2025 Marat Reimers | `.golangci.yml`; license text in `LICENSES/maratori-golangci-lint-config.txt` |

## Go dependencies (used as modules, not copied)

- `golang.org/x/tools` (BSD-3-Clause), the analysis framework behind our analyzers.
- `github.com/google/pprof` (Apache-2.0), the profile parser and diff behind `agentctl perf`.
- `go.uber.org/goleak` (MIT, Copyright (c) 2018 Uber Technologies, Inc.), the goroutine-leak check in `TestMain`.
- `github.com/jackc/pgx/v5` v5.11.0 (MIT, Copyright (c) 2013-2021 Jack Christensen), the Postgres driver and pool behind `platform/postgres`, with its dependencies `github.com/jackc/pgpassfile` (MIT, 2019), `github.com/jackc/pgservicefile` (MIT, 2020), `github.com/jackc/puddle/v2` (MIT, 2018), all Jack Christensen, and `golang.org/x/sync` and `golang.org/x/text` (BSD-3-Clause, The Go Authors).

`golang.org/x/tools` and `github.com/google/pprof` are dependencies of the `tools` module only. `goleak` and `pgx` belong to the root module, which holds the platform and the services.

## Developer tools (installed, not copied)

golangci-lint, gofumpt, govulncheck, sqlc, buf, lefthook, arch-go. Each keeps its own license; they are checked by `go run ./tools/agentctl doctor`.
