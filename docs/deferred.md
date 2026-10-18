# Deferred work

Three things are left out on purpose until a real project needs them. Each entry says what triggers it and what must be true before it counts as done. Do not start one without that trigger, and do not report it as enforced until a tool in this repository checks it.

A short table of smaller gaps follows them.

Every item follows the repository's own rules: an ADR first (`writing-adrs`), tests first, a fixture for every new rule, a credit in the README for any public source, and a pinned tool in `agentctl doctor`.

## Worker template

Trigger: the first project that consumes messages or runs scheduled jobs, with no API of its own.

Done when:

- `agentctl new service <name> --kind worker` writes a worker, and `--kind api` stays the default. The template lives in `tools/internal/scaffold/templates`.
- A generated witness (`services/example-worker`) is checked the way `services/example` is, and passes every gate untouched.
- The layers are the same four. `interfaces` holds the consumer or job entry and imports only `application`, so `arch-go.yml` needs no change; if it does, that is a sign the shape is wrong.
- Every goroutine starts through `platform/async`, pprof sits on the loopback port from `platform/debugserver`, and `TestMain` calls `goleak.VerifyTestMain`.
- A handler is safe to run twice: a redelivered message cannot repeat its effect (`writing-use-cases`).
- The shutdown path is tested with `testing/synctest`, not with sleeps.
- The `new-service` skill says when to pick a worker, and the load skeleton drives the consumer instead of an HTTP client.

Needs first: the transaction runner and outbox that each project provides today, because a worker without them has nothing safe to consume from.

## Buf contract gates

Trigger: the first project that publishes or consumes a protobuf contract. There is no `contracts/` directory yet.

Done when:

- `buf` is pinned and reported by `agentctl doctor`.
- `verify` has a step that runs `buf lint` on `contracts/`, and a project with no contracts skips it with a reason, the way `adr check` does.
- A breaking change against `main` fails (`buf breaking --against` the base branch). Decide in the ADR how the hook gets a base ref when there is no remote.
- Generated code that no longer matches the `.proto` files fails, as `sqlc diff` does for queries.
- Each rule has a violation fixture in `testdata/violations/<id>/`, and the compliant twin stays clean.
- `contracts` still imports no service and no `platform` code, which `arch-go.yml` already says.
- A `writing-contracts` skill says how to add a field, deprecate one, and version a message without breaking a consumer.

Credit: buf is already in the README Credits as the planned breaking-change gate; keep that line true.

## Deploy gates

Trigger: the first project that ships a service to a cluster. The target stays plain Kubernetes with no cloud provider named.

Done when:

- The service template writes a `Dockerfile` and Kubernetes manifests, and the `services/example` witness is regenerated with them.
- A gate builds the image and fails on a base image that is not pinned by digest, a root user, or a missing health check. Building needs Docker, so it runs in `verify --full` and CI, not in the pre-commit hook, as the query plans do.
- A manifest linter, pinned in `agentctl doctor`, fails on a container with no resource limits, no readiness and liveness probes, or no security context, and on a manifest that does not validate against the Kubernetes schema.
- Probes point at the endpoints `platform/httpserver` already serves, and the pprof port is never exposed outside the pod.
- Each rule has a violation fixture and a clean twin.
- No gate calls a cloud provider's CLI. Pushing an image or applying a manifest is a project decision and always asks the user first.

## Smaller gaps

Each is cheap to add and was left out because nothing has needed it yet. Do it when the trigger happens, with an ADR and a fixture like any other rule.

| Gap | Trigger | Done when |
| --- | --- | --- |
| Guard blocks destructive SQL and `gh` comment writes in shell commands | An agent runs SQL or posts to a pull request from the shell | `guard` denies `DROP`, `TRUNCATE`, `DELETE` or `UPDATE` without `WHERE`, `gh pr comment`, `gh pr review` and `gh api` writes to comment endpoints, and still allows a commit message that only mentions them |
| Guard blocks hand edits to generated files | An agent edits a `// Code generated ... DO NOT EDIT.` file | The edit is refused with the command that regenerates it; `sqlc diff` already catches the drift later |
| Value-object shape | A project has value objects that get mutated | A marker like `//agent:value-object` with value receivers and unexported fields, checked by an analyzer (the approach in ADR-0016) |
| Explicit nullable query parameters | A project has optional filters that misbehave on null | `writing-queries` and `db check` say whether a nullable parameter must be named, with a fixture |
| A "Confirmation" section in new ADRs | Decisions start to rot without an enforcer named | The ADR template and `adr check` ask for the tool or fixture that enforces the decision, or "review only" |
| Multi-tenancy | A project serves more than one tenant | An ADR first: row-level security with a per-transaction tenant, the tenant taken from the request principal, and a query rule that every tenant table filters on the tenant column |
| Query inside a loop, found statically | The runtime query budget in tests misses a case | An analyzer flags a repository call inside a `for` body; today only the budget test catches it |
