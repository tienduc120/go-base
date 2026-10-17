# go-base

A Go base for strict DDD and clean-architecture cloud microservices that AI agents extend. It ships almost no
features. It ships enforcement: rules a tool checks, skills an agent follows, and feedback loops that let an agent
find and fix performance problems and tech debt on its own.

Status: foundations. Read [AGENTS.md](AGENTS.md) first, then [docs/adr/](docs/adr/README.md).

## Quick start

You need Go (the version in `go.mod` is fetched automatically), Git, and Docker.

```sh
go run ./tools/agentctl doctor     # checks the toolchain and pinned tools, prints install hints
go test -race -timeout 120s ./tools/...
```

There is no Make. `agentctl` is the single command surface.

## License

MIT, Copyright (c) 2026 Luu Tien Duc. Free to use, modify, and use commercially. See [LICENSE](LICENSE).

## Credits

This base stands on public work. Material that is copied or adapted is listed in [NOTICE.md](NOTICE.md) with its
license; this section thanks everyone whose work shaped a decision, whether or not any text was copied. A source
moves from "referenced" to "used" the moment code or configuration from it lands here.

**Used**

- The Go team and community: the language, toolchain, standard library, and `go vet`.
- Developer tools we run, not copy: [golangci-lint](https://github.com/golangci/golangci-lint),
  [gofumpt](https://github.com/mvdan/gofumpt), [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck),
  [sqlc](https://github.com/sqlc-dev/sqlc), [buf](https://github.com/bufbuild/buf) (Buf Technologies),
  [lefthook](https://github.com/evilmartians/lefthook) (Evil Martians),
  [go-arch-lint](https://github.com/fe3dback/go-arch-lint) (fe3dback),
  [arch-go](https://github.com/arch-go/arch-go).

**Referenced for design** (all MIT unless noted; no code copied yet)

| Source | By | What shaped us |
|---|---|---|
| [golangci-lint-config](https://github.com/maratori/golangci-lint-config) | maratori | the strict, documented shape of a golangci-lint v2 config |
| [cc-skills-golang](https://github.com/samber/cc-skills-golang) | samber | the set of Go topics worth an agent skill |
| [goleak](https://github.com/uber-go/goleak) | Uber | goroutine-leak checks in `TestMain` |
| [wild-workouts-go-ddd-example](https://github.com/ThreeDotsLabs/wild-workouts-go-ddd-example) | Three Dots Labs | DDD, clean architecture, and CQRS layering in Go |
| [watermill](https://github.com/ThreeDotsLabs/watermill) | Three Dots Labs | broker-agnostic messaging behind a port |
| [go-clean-template](https://github.com/evrone/go-clean-template) | Evrone | service wiring, OpenTelemetry, and test layout |
| [pprof-analyzer-mcp](https://github.com/ZephyrDeng/pprof-analyzer-mcp) | ZephyrDeng | machine-readable pprof findings for agents |
| [yet-another-agent-harness](https://github.com/dirien/yet-another-agent-harness) | dirien | command guards and secret scanning as agent hooks |
| [agents-md-cookbook](https://github.com/Taiizor/agents-md-cookbook) | Taiizor | shape of a Go `AGENTS.md` |
| [buf](https://github.com/bufbuild/buf) (Apache-2.0) | Buf Technologies | breaking-change detection as a contract gate |

Also: [AGENTS.md](https://agents.md) as the shared agent instruction format, and Factory's article
[Using linters to direct agents](https://www.factory.ai/using-linters-to-direct-agents).
