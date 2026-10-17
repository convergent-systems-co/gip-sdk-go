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

A command may declare `timeout_seconds`, up to 7200, when one invocation runs longer than the host's default.

The full protocol specification is in the gip repository under `docs/spec/plugin-protocol.md`. A reference plugin is [gip-plugin-discovery](https://github.com/convergent-systems-co/gip-plugin-discovery).

## License

[PolyForm Noncommercial 1.0.0](LICENSE). Commercial use requires a separate license from Convergent Systems.
