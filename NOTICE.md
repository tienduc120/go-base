# Third-party material

Only MIT, BSD, and Apache-2.0 material is brought in. For each item keep the upstream license text in the
repository, record the upstream and version below, and prefer a dependency to a copy. A new dependency needs an ADR.

## Vendored or adapted

None yet.

## Developer tools (installed, not copied)

golangci-lint, gofumpt, govulncheck, sqlc, buf, lefthook, go-arch-lint, arch-go. Each keeps its own license; they are
checked by `go run ./tools/agentctl doctor`.
