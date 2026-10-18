# Architecture decisions

One file per decision, numbered, never edited after acceptance (supersede it with a new record instead).

| #                                            | Title                         | Status   |
| -------------------------------------------- | ----------------------------- | -------- |
| [0001](0001-foundations.md)                   | Foundations                   | accepted |
| [0002](0002-static-and-architecture-gates.md) | Static and architecture gates | accepted |
| [0003](0003-git-hooks.md)                     | Git hooks                     | accepted |
| [0004](0004-bounded-command-runs.md)          | Bounded command runs          | accepted |
| [0005](0005-agent-guard.md) | Agent guard | accepted |
| [0006](0006-pprof-diagnosis.md) | pprof diagnosis for agents | accepted |
| [0007](0007-goroutine-leak-checks.md) | Goroutine-leak checks in tests | accepted |
| [0008](0008-technical-debt-loop.md) | The technical-debt loop | accepted |
| [0009](0009-supervised-goroutines-and-the-debug-listener.md) | Supervised goroutines and the debug listener | accepted |
| [0010](0010-service-template.md) | The service template | accepted |
| [0011](0011-query-gates.md) | Query gates | accepted |
| [0012](0012-postgres-runtime-guards.md) | Postgres runtime guards | accepted |

Needs an ADR first: a new external dependency, a hosted service, a pinned major version moving, or a change to a published contract.
