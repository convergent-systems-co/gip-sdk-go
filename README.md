# gip-sdk-go

The Go SDK for writing [Governed Intent Programming](https://github.com/convergent-systems-co) plugins: the plugin protocol (v1), a `Serve` helper, and a conformance harness.

It depends only on the Go standard library. Plugins import this module and nothing from the gip implementation.

```sh
go get github.com/convergent-systems-co/gip-sdk-go@latest
```

| Package | Purpose |
|---|---|
| `protocol` | The `gip-plugin.json` manifest (schema `gip.plugin-manifest/v1`) and its validation; JSON-RPC 2.0 messages over NDJSON on stdin/stdout; the typed argument parser the gip host also uses |
| `gipplugin` | `Serve(manifest, handlers)` for implementing a plugin; telemetry, host intent/state calls and structured errors |
| `conformance` | `conformance.Run(ctx, dir)`, protocol checks CNF-001 to CNF-008 for a built plugin directory |

Rules a plugin must follow:

- Discovery reads only your manifest. Your code runs only after the user has installed, trusted (pinned version and digest) and enabled the plugin.
- Arguments are validated by the host. Outputs are validated against your declared `output_schema`.
- Proposals (`goal`, `work`, `invariant`, `interpretation`, `ambiguity`) need the matching declared capability. They only ever become candidates; a plugin cannot admit anything.
- Stdout belongs to the protocol. Use stderr or the telemetry calls for anything else.
- Report cost only with a basis (`measured`, `reported` or `estimated`).

### Executor plugins (v0.2.0)

An executor carries work units through the GIP process: it reads the frontier and contracts, and acts on work. It needs three host permissions, each granted explicitly at trust time:

| Permission | Host methods (`Request` helpers) |
|---|---|
| `work.read` | `host/frontier` (`Frontier`), `host/contract` (`Contract`, which also returns the project root) |
| `work.execute` | `host/act` (`Act`: start, submit, qualify, integrate, or rework to resolve a merge conflict), `host/check` (`Check`: the host runs a predicate's defined check and records what it observed) |
| `config.read` | `host/config` (`Config`): the plugin's own `<config>/<short name>.yaml`, parsed by the host |

An executor never acts as itself. `Act` and `Check` name an agent principal, which must be one the human listed with `gip plugin trust --act-as`, bound in the project and holding the grants for the transition. A refusal is a disposition, not an error. Reviews and acceptance stay with humans.

The same interface serves a local CLI executor (Claude, Codex, Copilot) and a remote one such as a Conduit adapter with pools:

- **Several units at once.** `Act` and `Check` take a `workspace`: the project root, or a linked git worktree of the project. The host runs checks, observes code and records the qualified commit there.
- **Resumable.** A stable `request_id` makes each act idempotent: re-sending it replays the recorded result.
- **Claims.** `start` is the claim. A unit already in progress cannot be started by another executor.

### Leases and progress (v0.3.0)

A `start` grants the executor a time-limited **lease** on the unit (ADR-0015 in the gip repository). The act result carries it (`HostActResult.Lease`: id, holder, time to live, expiry).

| Act or method | Purpose |
|---|---|
| `Act` kind `renew` | Renew the lease, naming it in `Lease` (`RotateLease` replaces it with a new id, for an executor resuming a unit). Renew well within the time to live (for example every third of it). A lease that was reclaimed or replaced is refused (`LEASE_RECLAIMED`, `LEASE_SUPERSEDED`): stop working on the unit. |
| `Act` kind `reclaim` | Return a unit whose lease expired to the frontier, recording the holder that lost it. Needs the `work.reclaim` grant; a live lease is refused (`LEASE_ACTIVE`). |
| `Act` kind `submit` with `Lease` | A stale holder's submission is refused, even as the same principal. |
| `host/progress` (`Progress`, `work.execute`) | Report a phase, a short message, an optional percent and the cost so far for a unit the executor holds. Progress is telemetry, never a governed transition. A report under a lost lease is not recorded and says why. |
| `host/execution` (`Execution`, `work.read`) | Read the lease and latest progress of one unit, or of every unit in progress. |

### Plan-once frontier (v0.4.0)

gip plans the work once, up front, as a tree of stable addresses (A, B, B2, B2.1.1): the root, goals, sub-goals and, as every leaf, work units (WUs). Dependency edges that may cross branches, and keeps a **plan document** (`gip.plan-document/v1`) up to date incrementally (ADR-0021 in the gip repository). The plan never authorizes anything: every claim still goes through `Act`.

| Method (`Request` helper) | Permission | Purpose |
|---|---|---|
| `host/plan` (`Plan`) | `work.read` | Read the plan document. Pass the digest you hold: an unchanged plan returns `Unchanged` without the document. `Ready` lists ready work leaves in the election order fixed at planning time. |
| `host/finding` (`Finding`) | `work.execute` | Record discovered work against a node, as a trusted agent principal. A finding waits for its branch's milestone; a `blocking` one (only the unit's holder, or a principal granted `plan.find`) stops the unit and has gip plan from that node's goal down. |
| `host/plan/pass` (`PlanPass`) | `plan.write` (new) | Submit one planning pass as a trusted agent principal (for example `agent:planner`): new nodes (binding goals and work, or creating and admitting them), edges, cancellations, finding resolutions. Its trigger and scope must be due (`Triggers`), except `background` and `idle` passes, which schedule no findings. gip assigns addresses. The pass is decided under the principal's own grants (`plan.pass` by name, `goal.admit`, `work.admit`, ...). |

A command may declare `timeout_seconds`, up to 7200, when one invocation runs longer than the host's default.

The full protocol specification is in the gip repository under `docs/spec/plugin-protocol.md`. A reference plugin is [gip-plugin-discovery](https://github.com/convergent-systems-co/gip-plugin-discovery).

## License

[PolyForm Noncommercial 1.0.0](LICENSE). Commercial use requires a separate license from Convergent Systems.
