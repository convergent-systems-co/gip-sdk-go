# gip-sdk-go

The Go SDK for writing [Governed Intent Programming](https://github.com/convergent-systems-co) plugins: the plugin protocol (v1), a `Serve` helper, and a conformance harness.

It depends only on the Go standard library. Plugins import this module and nothing from the gip implementation.

```sh
go get github.com/convergent-systems-co/gip-sdk-go@latest
```

| Package | Purpose |
|---|---|
| `protocol` | The `gip-plugin.json` manifest (schema `gip.plugin-manifest/v1`) and its validation; JSON-RPC 2.0 messages over NDJSON on stdin/stdout; the typed argument parser the gip host also uses |
| `gipplugin` | `Serve(manifest, handlers)` for implementing a plugin; telemetry, host calls (intent, state, executor, plan, delivery, plugin data), structured errors, and `Command` for starting child processes without the protocol channel |
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

An executor never acts as itself. `Act` and `Check` name an agent principal, which must be one listed with `gip plugin trust --act-as`, bound in the project and holding the grants for the transition. A refusal is a disposition, not an error. Reviews are decided by an independent agent principal through the review act (v0.6.0, below).

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

gip plans the work once, up front, as a tree of stable addresses (A, B, B2, B2.1.1): the root, goals, sub-goals and, as every leaf, work units (WUs), with dependency edges that may cross branches. It keeps a **plan document** (`gip.plan-document/v1`) up to date incrementally (ADR-0021 in the gip repository). The plan never authorizes anything: every claim still goes through `Act`.

| Method (`Request` helper) | Permission | Purpose |
|---|---|---|
| `host/plan` (`Plan`) | `work.read` | Read the plan document. Pass the digest you hold: an unchanged plan returns `Unchanged` without the document. `Ready` lists ready work leaves in the election order fixed at planning time. An executor reports slots it could not fill with `PlanReport` (`idle_slots`; 0 withdraws the report), and gip then lists `idle` planning as due on branches with nothing in flight. |
| `host/finding` (`Finding`) | `work.execute` | Record discovered work against a node, as a trusted agent principal. A finding waits for its branch's milestone; a `blocking` one (only the unit's holder, or a principal granted `plan.find`) stops the unit and has gip plan from that node's goal down. A `decision` finding (v0.5.0) is an open owner decision, not work: gip never plans, counts or dismisses it in a pass; it waits for a human (`gip plan decide`), and then has status `decided`. |
| `host/plan/pass` (`PlanPass`) | `plan.write` (new) | Submit one planning pass as a trusted agent principal (for example `agent:planner`): new nodes (binding goals and work, or creating and admitting them), edges, cancellations, finding resolutions. Its trigger and scope must be due (`Triggers`), except `background` and `idle` passes, which schedule no findings. gip assigns addresses. The pass is decided under the principal's own grants (`plan.pass` by name, `goal.admit`, `work.admit`, ...). |

### Agent review (v0.6.0)

In dark factory mode an independent agent reviewer decides each unit's review (ADR-0012 in the gip repository). A `submit` of work that needs review opens a review ceremony (`HostActResult.Ceremony`); a reviewer plugin decides it with the review act:

| Act | Permission | Purpose |
|---|---|---|
| `Act` kind `review` (`ActReview`) | `work.review` (new) | Decide the open review ceremony `Ceremony` of work unit `Work` as a trusted agent principal: `Outcome` is `approve` or `reject` (`ReviewApprove`, `ReviewReject`), `Rationale` is required, and `Evidence` optionally names evidence records the decision relies on. A rejection returns the unit to its executor for rework. |

- `work.review` is its own permission, granted at trust time; `work.execute` does not include it, and `work.review` grants no other act (since v0.9.0 it also records `rescope`/`split` findings; the goal and delivery acts need `work.deliver`). A plugin granted `work.review` may be trusted `--act-as` agent principals.
- The acting principal needs a `work.review` grant in the project. The unit's executor can never review its own work (`CONFLICT_OF_INTEREST`), and a plugin can never claim the solo self-review waiver.
- A ceremony that is not a review of `Work`, or does not exist, is refused before anything is submitted; one already decided is refused by the gateway (`ILLEGAL_TRANSITION`).
- (v0.9.1, gip ADR-0024 §8) A reviewer that cannot review a submission at all rejects it with `Unreviewable` set to a reason from a closed set (`UnreviewableReasons`): `commit-unknown`, `workspace-changed`, `submitted-elsewhere`. gip does not verify the reason. The unit returns to its builder to resubmit; the rejection is not a defect and never counts toward the contract's `review.rejections`, so it never reaches the re-scope cap or spends the lineage's re-scope. After `MaxUnreviewable` (2) such rejections since the unit's last approval, a further one raises a blocking class decision question (`unreviewable-exhausted`) at the unit's plan node for a person to answer. `Unreviewable` is valid only with `Outcome` `reject` (`ValidateUnreviewable`; `Act` refuses to send otherwise). Acts sent to an older gip ignore the field; an older gip refuses ledgers containing `decision.unreviewable`.
- The decision is recorded as the agent's own, with no human attestation.
- (v0.9.3) gip refuses a carriage return in a rationale; `Act` normalises `Rationale` (CRLF, then lone CR, to LF) before sending, and `protocol.NormalizeNewlines` exposes the same rewrite.

### Front-loaded planning (v0.7.0)

Most of the time goes into planning, so development is quick (ADR-0021 amendment in the gip repository). A plan carries a versioned **design** document: markdown describing the shared architecture, the modules and the exact interface contracts (routes, request and response JSON shapes) consistent with the goals' outcome checks. Builders and reviewers build to it.

| Surface | Field | Purpose |
|---|---|---|
| `host/plan/pass` (`PlanPass`) | `Design` | Set or replace the design (UTF-8 markdown, at most `MaxDesignBytes`, 64 KiB). Only `initial`, `milestone`, `early-milestone` and `final` passes may; empty leaves it unchanged. gip records the revision with the pass number and the pass's planners, and keeps every superseded revision. |
| `host/plan` (`PlanDocument`) | `Design` | The current design (`PlanDesign`: `revision`, `pass`, `digest`, `content`, `planners`); absent until a pass sets one. |
| `host/plan` (`PlanDocument`) | `Delivery` | Present once the final discovery ran for the current intent revision (`PlanDelivery`: its `pass` and the open `findings` left for the human at delivery). The final discovery fires once per intent revision: completing the work it scheduled does not make it due again. |
| `host/contract` (`HostContractResult`) | `Design()` | The design the work contract carries (`design`), or nil. Since **v0.8.0** it is a `ContractDesign` (the return type changed from `*PlanDesign`: field reads compile as before, code naming the old type must adapt): the `PlanDesign` fields plus `pinned` (the served revision is the one the unit was started on), `current_revision` and `current_digest` (the plan's current design). `Superseded()` compares digests, not revision numbers (after a ledger merge two texts can share a number); `Pin()` returns the served revision as a `*DesignPin`; both are nil-safe. A gip that predates pinning sends none of the three. |
| `host/plan` (`PlanWork`) | `Design` | (v0.7.1) The design revision an in-flight unit was started on (`DesignPin`: `revision`, `digest`); the unit and its reviewer build to it while the plan's design moves on. Absent for a unit not started. |

All of it is additive: a gip that predates it never sends the new fields, and refuses a pass that names `design` (`INVALID_PAYLOAD`).

### Dark factory completion (v0.9.0)

A person is asked exactly three things: changing or extending the intent, adding a goal the intent does not contain, and a decision that has no clear answer (ADR-0024 in the gip repository). Each is a **question**, answered by a human revision of the intent. A repeatedly rejected unit is re-scoped once before anyone is asked, and completion is one human decision per **delivery**. All of it applies on rules 2 projects (`RulesDarkFactory`; new projects start there, existing ones move with `gip project settings --rules 2`); on rules 1 gip refuses it (`RULES_REQUIRED`).

| Surface | Field or method | Purpose |
|---|---|---|
| `host/plan/pass` (`PlanPass`) | `Questions` | Questions the pass raises (`PlanQuestion`: `ref`, `text`, `class` `intent`/`goal`/`decision`, `options`, `recommended`, `default`, `node`, `blocking`, `refs`). A scope dismissal (`FindingResolution.Grounds` `scope`) must name one by `Question` (its ref), so the dismissal and its question are one event (`QUESTION_REQUIRED`). The result lists the `omitted` refs (goals the intent does not contain, their subtrees and dependants; each becomes a class `goal` question) and maps each question ref to its id (`questions`). |
| `host/plan/pass` (`PlanNodeFields`) | `Replaces` | May also name, by ref, a node an earlier entry of the same pass bound (same-pass supersession, gip PLAN-025): bind coarse work, then replace it with refined `work` nodes. In a `rescope` pass it may name the one started unit the finding names. |
| `host/finding` (`HostFindingParams`) | `Kind` | `work` (default), `decision`, `rescope` (the unit's rejections reached the cap) or `split` (too large to review). `rescope`/`split` make a `rescope` pass due at once; one per unit lineage, then gip records a blocking question instead (`HostFindingResult.Question`). Under rules 2 a `decision` is recorded as a question. A `rescope` or `split` finding may be recorded under `work.review` as well as `work.execute`, so a reviewer plugin needs no `work.execute`. |
| `host/plan` (`PlanDocument`) | `Autonomous`, `Intents`, `Questions`, `HumanDue` | The project's `autonomous` setting; the current intent revisions with content (the planner recalls them in every prompt); the open questions (a closed one carries `closed_by`: `{kind: revision, ref: "main@4"}` or `{kind: delivery, ref: "delivery-1"}`); and `human_due` (`due`, `reason`, `questions`, `proposal`). Executors stop electing while `HumanDue.Due` and report it. New triggers: `rescope`, `refine` (an open question has no pending proposal: answer it with `IntentPropose`). |
| `host/plan` (`PlanDelivery`) | `Record` | The delivery record (`DeliveryRecord`: intents, final pass, design pin, commit, goals with qualification and acceptance refs, check evidence, held questions with defaults, basis digest, `status` `prepared`/`accepted`/`superseded`, provenance). |
| `host/plan` (`PlanFinding`, `PlanWork`, `PlanNode`) | `Kind`, `RecordedSeq`/`RecordedPass`; `CompletedSeq`/`CompletedPass`; `Rescopes` | Finding kind (`EffectiveKind()` also reads the old `decision` flag); when a finding was recorded and when a unit completed (gip PLAN-036: a finding recorded after its unit was done is not stale work it was asked to do); a lineage's re-scopes. |
| `host/state` (`HostStateResult`) | typed view | Decode `Request.State` into it. Work units carry `completed_seq`/`completed_pass`; open ambiguities (`Ambiguity`) carry the question shape and an agent resolution's `resolution_basis`/`resolution_span` (`IsQuestion()`, `Blocks()`). |
| `host/intent/propose` (`IntentPropose`) | `plan.write` | Record a proposed intent revision (`IntentProposal`: `intent`, `base_revision`, full `content`, the `questions` it covers). Needs no project grant, changes nothing until a person adopts it; a newer proposal from the same principal for the same base supersedes the older. |
| `host/delivery/prepare` (`DeliveryPrepare`) | `work.deliver` (new) | As the reviewer: gip starts the design's runtime in a temporary worktree of the integration tip, runs the root and first-level goals' outcome checks against it, qualifies and accepts the goals, and records the delivery. A person accepts it with `gip delivery accept` at a terminal. Needs the `delivery.prepare` grant (no wildcard covers it). (v0.9.4) When the runtime did not start (exited before ready, never ready, ready address in use, start command not startable) it returns `BLOCKED` with `CodeRuntimeNotStarted` (`RUNTIME_NOT_STARTED`) and records nothing. |
| `host/act` (`HostActParams`) | kinds `goal.qualify`, `goal.accept`; `Goal` | The reviewer qualifies and accepts goal `Goal` (`work.deliver`; the `goal.qualify`/`goal.accept` grants). `Work` is now omitted when empty: name exactly one of `Work` and `Goal` (`ValidateTarget`; `Act` refuses to send otherwise). A principal that built or planned anything in the goal's subtree is refused (`CONFLICT_OF_INTEREST`). A qualified, unaccepted root goal is ready with reason `ACCEPTANCE_DUE`. |
| `host/check` (`HostCheckParams`) | `Subject` | `goal:<id>` (`GoalSubject`) or `work:<id>` instead of `Work`, to observe a goal's outcome checks; a goal subject needs `work.deliver`. Name exactly one of `Work` (now omitted when empty) and `Subject` (`ValidateTarget`; `Check` refuses to send otherwise). Evidence a reviewer records (a `work.review` grant covering it, not its builder) satisfies `review` predicates. |
| `host/contract` (`HostContractResult`) | `Review()` | The contract's `review` block (`ContractReview`: `rejections` computed from the ledger, `rejection_cap`, `rescopes`, `last_unreviewable`); `AtCap()`. Executors read it instead of counting. |

`work.deliver` (new, v0.9.0) is the permission for the reviewer's completion acts: goal qualification and acceptance, goal-subject checks and delivery preparation. Neither `work.execute` nor `work.review` includes it; `work.review` stays "decide a review" (and record `rescope`/`split` findings).

Types without a host method, for planners and tools: `RuntimeBlock` (the design's required fenced `gip-runtime` block under rules 2: `start`, `ready` with a loopback `http(s)` `url` or a `check` argv, `timeout_seconds` 1 to 600, `env` whose keys are identifiers not starting `GIP_`/`DYLD_`/`LD_` in any case, or `{"none":true}`. `ParseRuntimeBlock` takes exactly one block and refuses unclosed fences, closing fences indented more than three spaces, duplicate or wrong-case keys and trailing data; `Validate`; `Fence`), `IntentResolution` with `CheckResolutionSpan` (an agent resolves an ambiguity only with basis `intent` and a verbatim span of at least 24 bytes and 4 words, never a question), and `ProjectSettings` (`rules`, `autonomous`, `review_rejection_cap`; human-only, `Effective()` fills the defaults: rules 1, autonomous true, cap 3).

Everything is additive: a gip built against an earlier SDK never sends the new fields. It decodes host requests into its own types, so it drops fields it does not know (a finding's `kind` is lost and recorded as work), refuses act kinds it does not know, answers a check without `work` with `host.invalid_params`, and answers the new methods with method-not-found.

### Plugin data, agent authority and presence (v0.10.0)

gip ADR-0025 (batch C) gives a plugin a private key-value store and moves grant, identity and conflict administration to agents under rules 3. The SDK carries the plugin data methods and the record shapes a plugin reads; gip decides and verifies everything.

**Plugin data** (`host/plugin/data`, spec PLG-028, #61). With the `plugin-data` permission (`PermPluginData`, granted at trust time) a plugin keeps state the host holds in the project's `.gip/state.db`, which no sandbox declaration makes readable or writable, so neither the plugin's sandbox nor its model processes can read or forge it. The namespace is the plugin's short name and the project. The data is not governed: it is outside the ledger, not committed, and losing it means starting over.

| Method | `Request` helper | Purpose |
|---|---|---|
| `host/plugin/data/get {key}` | `DataGet`, `DataGetJSON` | The value and its `version`; a missing key is `Found: false`, not an error. |
| `host/plugin/data/put {key, value, if_version?}` | `DataPut`, `DataPutJSON` | Write and return the new `version` (every write raises it). `if_version` is a compare-and-swap: absent writes unconditionally, `protocol.IfVersion(0)` only creates, `protocol.IfVersion(n)` replaces only version n. Versions are never reused: they are monotonic per (plugin, project, key) across delete and re-create (the host keeps a high-water mark), so a stale `if_version` never matches a re-created key. |
| `host/plugin/data/delete {key, if_version?}` | `DataDelete` | Delete; reports whether the key existed. `if_version` as for put. |
| `host/plugin/data/list {prefix?, after?, limit?}` | `DataList` | One page of keys in byte order (`key`, `version`, `size`, no values); pass `next` as the next page's `after`. |

| Limit (PLG-028) | Constant |
|---|---|
| Key: 1 to 200 bytes of printable ASCII (0x20 to 0x7E) | `MaxPluginDataKeyBytes` |
| Value: at most 1 MiB (raw bytes; base64 on the wire) | `MaxPluginDataValueBytes` |
| Per plugin per project: 4,096 keys and 64 MiB (host-enforced) | `MaxPluginDataKeys`, `MaxPluginDataBytes` |
| List page: default 256, at most 1024 (contract core H implements) | `DefaultPluginDataListLimit`, `MaxPluginDataListLimit` |

The helpers refuse locally, sending nothing, when `plugin-data` was not granted (`host.permission_denied`) or a key, value, prefix, cursor, limit or `if_version` breaks the limits (`DATA_KEY_INVALID`, `DATA_VALUE_TOO_LARGE`, `DATA_LIMIT_INVALID`, all `CodeInvalidParams`); each `Validate` method is the same check, for hosts and parity tests. The host refuses with code `CodeDataRefused` (-31004) and the stable ids `DATA_VERSION_MISMATCH` (compare-and-swap failed, nothing written), `DATA_QUOTA_EXCEEDED` and `NO_PROJECT`. Match them with `protocol.IsVersionMismatch`, `protocol.IsNoProject` (which also accepts the older `host.unavailable`) and `protocol.ErrorID`. A gip that predates plugin data answers the methods with method-not-found.

Beyond the PLG-028 text, this SDK fixes the contract core H implements on the host side: the ids `NO_PROJECT` and `DATA_QUOTA_EXCEEDED` with code `CodeDataRefused` (-31004), the invalid-request ids above, the list page sizes, `if_version` 0 as create-only (matches a key that was never written or was deleted), and versions that are never reused for a key.

**Children never get the channel.** The JSON-RPC channel (stdin and stdout) is the only way to the host, so a process the plugin starts must not inherit it. `gipplugin.Command` returns an `exec.Cmd` whose stdin and stdout are the null device (stderr is the plugin's); `gipplugin.CheckChild` returns `ErrChildInheritsChannel` for a command that would pass `os.Stdin` or `os.Stdout` as a stream or extra file. Use them for every process you start, model processes above all.

**Record shapes** (read-only for plugins):

| Surface | Field or constant | Purpose |
|---|---|---|
| Questions (`PlanQuestion`, `Ambiguity`) | `closed_by.kind` `ledger.resolve` (`ClosedByLedgerResolve`, `QuestionClosure.IsClosedByLedgerResolve`) | A merge conflict no independent agent may resolve becomes a blocking class `decision` question tagged `merge-conflict` (`QuestionTagMergeConflict`) naming its ceremony in `refs`. Only the person's `ledger.resolve` closes it (`answered`, `ref` the resolve event): an answering intent revision carries it over, `delivery.accept` never defaults it, no agent closes it. `ReservedQuestionTags` lists the tags only the gateway sets. |
| Merge-conflict ceremony (`MergePending`) | `authors`, `class` | Principals conflicted over the object (refused as resolvers, `CONFLICT_OF_INTEREST`) and `human`/`agent` (`ConflictClassHuman`: only a person, `HUMAN_CLASS_CONFLICT`). A conflict recorded before rules 3 has no authors (`AUTHORS_UNKNOWN`). `IsAuthor` is for display only: gip also follows grant and binding chains. |
| `Attestation` | `presence {key, signed_at, sig}` (`Presence`) | The Secure Enclave touch a person enrolled in presence adds to every human-only act, human decision and change to a human's identity or grants. `PresenceContext`, `PresenceDomainLedger`/`PresenceDomainInstallation`, `PresenceFreshness` (5 minutes); refusals `PRESENCE_REQUIRED`, `PRESENCE_INVALID`. |
| Rules epochs | `RulesAgentAuthority` (3), `HumanOnlyActions(rules)` | Under rules 3 only `intent.revise`, `delivery.accept` and `project.settings` stay human; grants, identities, gateways, leases and conflicts go to agents (`agent:*` only). Not selectable until gip's switch-on release. |

Everything is additive: a gip built against an earlier SDK never sends the new fields, and plugins built against an earlier SDK keep working without `host/plugin/data`.

A command may declare `timeout_seconds`, up to 7200, when one invocation runs longer than the host's default.

The full protocol specification is in the gip repository under `docs/spec/plugin-protocol.md`. A reference plugin is [gip-plugin-discovery](https://github.com/convergent-systems-co/gip-plugin-discovery).

## License

[PolyForm Noncommercial 1.0.0](LICENSE). Commercial use requires a separate license from Convergent Systems.
