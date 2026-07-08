<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Design review — preserved learnings

These are un-curated review learnings preserved verbatim-in-substance from the
now-retired `design-reviewer` agent's memory (its `MEMORY.md` plus per-finding
feedback notes) — patterns of good and bad HoloMUSH specs discovered during
adversarial design review. They are kept for reference by GSD's
`gsd-plan-checker` and human reviewers. This file is **not** auto-loaded; read
it on demand when reviewing a spec or design. jj-specific mechanics from the
original notes have been converted to native-git equivalents or dropped.

## Common spec weaknesses in this codebase

- **Round-2 fixes can introduce orthogonal contradictions.** When a round-1
  finding asks for a single sweeping rule, the R2 author often applies it past
  the boundary where another section made a different considered choice.
  Specs accumulate considered decisions section-by-section; a localized R1 fix
  applied as a generalization can flatten over a different decision made
  elsewhere without the author noticing. When R2 introduces a generalization of
  the form "the same [mechanism] applies to [enumeration]", grep each item in
  the enumeration elsewhere in the spec for an explicit ordering / mechanism /
  contract claim. If found, raise as a contradiction blocking finding even when
  both R1 #N and R2 #N look correct in isolation. Seen 2026-05-07 in
  event-payload-crypto-phase5-totp-substrate r2: §"Verify mechanics"
  (lines 343-346) prescribes emit-after-COMMIT for `crypto.totp_locked`
  (considered choice — lockout is a defensive signal that should not be aborted
  by a NATS hiccup), but the §"Bootstrap closure mechanism" ghost-case rewrite
  (lines 558-559) generalizes "publish-before-COMMIT" to include
  `crypto.totp_locked`, contradicting the earlier paragraph.

- **Amendment rolls back a just-landed sibling spec.** In a multi-epic chain, a
  Phase-N+1 spec that amends a master-spec contract a Phase-N sibling just
  shipped (with code-level docs + meta-tests) is structurally larger than a
  one-row "REMOVE" entry in an amendments table. Before accepting an
  amendment-table row that says "REMOVE: <thing>" or "REPLACE shape A with
  shape B" in a multi-epic chain, run a five-point cross-check:
  1. `rg` the dropped string across all sibling specs (decomposition, prior
     sub-epic specs). Any hit is an undeclared amendment the table is missing.
  2. `rg` the dropped string against `*.go` files (especially policy/contract
     doc comments, e.g. `internal/access/grants.go`-style). Code comments that
     enshrine the contract must be amended in the same PR.
  3. `rg` for `TestSpec*Amendments*`-style meta-tests in the sibling's
     enforcement directory. They fail or silently mask regressions if the
     dropped substring is in the test's positive list.
  4. Read the parent decomposition spec's decisions for the dropped contract.
     If the decomposition stated it as a MUST, the sub-epic is overriding its
     parent — that needs threat-model justification, not tractability prose.
  5. If the rollback rationale is "tractability" ("needs a detour through table
     X to look up role Y"), separate "this is hard to implement" from "this is
     the wrong threat model". The first is not a valid reason to drop a security
     control; the second requires a threat-model amendment with a compensating
     control or explicit risk acceptance.

  Seen 2026-05-09 in event-payload-crypto-phase5-sub-epic-d finding 1: D's §10
  amendment row 1 ("REMOVE: §5.9 step 6 admin role check") presented as a 5-row
  scope but actually invalidates ~6 master-spec strings, the doc comment on
  `internal/access/grants.go:14-18`, the `TestSpecAmendmentsLanded` meta-test
  (which substring-asserts the conjunction), the parent decomposition spec
  line 177 (Decision 5), and the threat model's "RoleAdmin AND crypto.operator"
  defense layering.

- **Universal stamp-contract vs per-kind invariant.** When a spec asserts BOTH
  a per-kind invariant carving out an exception ("every Actor for kinds {A,B,C}
  carries a ULID") AND a universal stamp-site contract ("every stamp site
  stamps a valid ULID; parse failure is a hard error"), the two contradict at
  runtime: the excluded kind still flows through the universal failure-mode
  contract. When a spec carves out a kind in an invariant, grep all production
  stamp sites for the excluded kind and walk each through the failure-mode
  contracts. If any fires on a kind-D stamp site, the contract needs an explicit
  kind-D exception, OR the kind-D sites need migration, OR kind-D inclusion in
  the invariant. Seen 2026-05-04 in legacy-id-elimination r2: INV-W9ML-1 carved
  out `ActorSystem` from the ULID requirement, but `coreActorToEventbusActor`
  was specified to return `ACTOR_ID_NOT_ULID` on parse failure universally.
  Three production sites (`internal/world/event_store_adapter.go:34`,
  `internal/grpc/server.go:531`, `internal/command/types.go:619`) stamp
  `core.Actor{Kind: ActorSystem, ID: "system"|"world-service"}` — all would
  fail at cutover. (Sibling of "lint heuristic enumeration claim ≠ heuristic
  logic": both are universal-claim-vs-per-instance-behavior mismatches.)

- **oops code vs gRPC status code conflation.** HoloMUSH error codes like
  `STREAM_ACCESS_DENIED`, `SCENE_AUDIT_ACCESS_DENIED`,
  `AUDIT_PLUGIN_HISTORY_RPC_FAILED` are **oops codes** (constructed via
  `oops.Code(...).Errorf(...)`), asserted via `errutil.AssertErrorCode`, NOT
  gRPC `codes.Code` values. When a spec says "the wire-level gRPC code is
  `STREAM_ACCESS_DENIED`" it means "what survives in the error chain through an
  in-process gRPC test." Over a real network, gRPC marshals a non-status oops
  error as `codes.Unknown` and the oops chain is lost. This is a pre-existing
  convention (`internal/grpc/query_stream_history.go:170,178,203` +
  `query_stream_history_test.go:220`). Flag the wording as a **non-blocking**
  (medium) finding; do NOT block — the design works, the wording is the bug.
  Suggest concrete text: "the error chain carries oops code X" rather than "the
  wire-level gRPC code is X". Real gRPC status codes
  (`codes.PermissionDenied`, `codes.InvalidArgument`) at plugin boundaries are
  correct as written — only flag conflations affecting host-boundary error
  chains.

- **gRPC `FromError` message rewrite on wrapped statuses.** When a spec proposes
  round-tripping a gRPC status error via `status.FromError(err)` then
  `st.Err()`/`status.Errorf("%s", st.Message())`, verify whether the production
  call passes the status DIRECTLY or WRAPPED.
  `google.golang.org/grpc/status/status.go:96-127` has two paths: bare
  `status.Error` (implements `GRPCStatus()` directly) returns the status's own
  proto, message untouched; but a wrapped `GRPCStatus()`-bearing error (any
  `oops.Wrap`, `fmt.Errorf("%w", ...)`) returns a NEW status with
  `p.Message = err.Error()` — the OUTER error's full text replaces the inner
  clean message. `Status.Details()` survives both; `Status.Message()` does NOT.
  Flag any "verbatim" / "pass-through" / "wire-equivalent" claim about the
  message field when the production caller wraps
  (`oops.Code(...).Wrap(...)`, `oops.With(...).Wrap(...)`). For true verbatim
  pass-through the spec must `errors.As(err, &innerStatus)` to extract the inner
  status BEFORE `.Message()`/`.Err()`.

- **"Mirrors existing technique X" claims need verbatim verification.** When a
  spec says "use the same technique as `Y`", read both sides and diff them.
  Authors paraphrase or reconstruct from memory and produce structurally
  different (broken) variants. Seen 2026-04-25 in session-workspace-isolation:
  spec claimed to mirror a Taskfile helper's worktree-relative path resolution,
  but the provided snippet computed a different path that failed when invoked
  from a linked worktree.

- **Shell-snippet error handling in spec docs is rarely verified.** Patterns
  like `var=$(cmd | tail -n 1) || return` (bash) and
  `set var (cmd | tail -n 1); or return $status` (fish) do NOT detect failure
  of `cmd` — the pipeline status is the LAST command's status. Run the snippet
  before believing it. Seen 2026-04-25.

- **Spec "verifiable by …" criteria with overly-broad history queries.**
  `git log --all` returns full history; for experiment-level verification
  prefer a scoped commit range (`main..HEAD`, or a named base). Not blocking but
  degrades the diagnostic value of the criterion.

- **Stated rationale for shell idioms can be wrong even when the idiom works.**
  A spec may correctly include a `cd` or `cd && cmd` block but give an incorrect
  *reason* ("needed because X requires Y" where Y is false). Documentation-rot
  risk, not a correctness blocker. Verify rationale claims by running the inner
  command without the `cd`. Seen 2026-04-25 in session-workspace-isolation r3:
  spec claimed `cd "$MAIN_REPO"` was needed for a fetch, but `git fetch` works
  from any linked worktree because git resolves repo storage via the worktree's
  `.git` file.

- **Check existing RPCs before "new RPC" proposals.** When a spec proposes a new
  RPC for "is the user signed in", "check session", "fetch current player", or
  any read-only auth-state probe, grep `api/proto/holomush/{web,core}/v1/*.proto`
  and `internal/grpc/auth_handlers.go` + `internal/web/auth_handlers.go` before
  accepting it as net-new. The 2026-04-25 multi-tab spec proposed `WhoAmI`
  without mentioning `WebCheckSession` / `CheckPlayerSession`, which already
  serve the purpose — the plan would have produced parallel surfaces with no
  migration path. Run:
  - `rg -n "rpc " api/proto/holomush/web/v1/*.proto`
  - `rg -n "rpc " api/proto/holomush/core/v1/*.proto`
  - `rg -n "<proposed-method>|Check|WhoAmI|Validate" internal/grpc/auth_handlers.go internal/web/auth_handlers.go`

  If anything overlaps, the spec MUST (a) explicitly extend the existing RPC, or
  (b) explicitly deprecate it with a migration note. "Add a new RPC" without
  addressing the existing one is blocking.

- **Extending an existing RPC: audit callers for contract-shape dependencies.**
  When a spec extends an RPC's failure-path contract (e.g. error →
  `authenticated=false`), grep for both the RPC name and its TypeScript client
  method (`webCheckSession`, `webCreateGuest`, etc.) under `web/src/`. The web
  client is full of `try { await client.foo({}); } catch { redirect(...); }`
  patterns that assume the RPC throws on auth failure; changing the contract
  breaks the redirect chain silently. If any caller relies on a throw to trigger
  control flow (especially redirects in `+layout.ts`), flag it as a blocking BC
  issue. Caught in v2 of `holomush-9q8n`: the authed layout load depended on
  `webCheckSession` throwing to redirect to /login; the spec didn't acknowledge
  the break.

- **"ctx carries an actor at boundary X" claims need a full call-graph trace.**
  Specs proposing to authenticate an actor pulled from
  `core.ActorFromContext(ctx)` at a boundary MUST be checked against EVERY
  production caller of that boundary, not the obvious one. `core.WithActor` is
  often called *after* the boundary returns (for a downstream
  `EmitPluginEvent`), not before. Seen 2026-04-25 in plugin-actor-claim-auth:
  spec §3.3.4 assumed `Host.DeliverEvent`/`DeliverCommand` ctx carries an actor,
  but `subscriber.go:104-150` and `dispatcher.go:118-310` both stamp the actor
  only after the Deliver call — the "verbatim cascade preserved" branch was dead
  code at every production call site.

- **Migration scope: enumerate ALL in-tree plugins that hit the new gate, not
  the marquee one.** Specs adding a manifest gate name one plugin (the binary
  one with provenance) and miss Lua plugins that emit through a different code
  path. Check BOTH `events:` declarations in `plugin.yaml` (subscriber-driven
  emits) AND command capability `action: emit` (dispatcher-driven emits) across
  every `plugins/*/plugin.yaml`, then read the corresponding `main.lua`/`main.go`
  to confirm whether character-driven emits actually happen. Seen 2026-04-25 in
  plugin-actor-claim-auth: r1 named only `core-scenes` and missed
  `core-communication`; r2 named both but missed `echo-bot` (a Lua plugin
  subscribed to `events: [say]` that emits a follow-up `say` for non-plugin
  actors).

- **In-tree manifest filename is `plugin.yaml`, NOT `manifest.yaml`.** The
  `Manifest` Go type's docstring confirms (`internal/plugin/manifest.go:69,291`).
  Specs saying "update `<plugin>/manifest.yaml`" name a path that doesn't exist;
  the acceptance criterion is unverifiable. Grep `internal/plugin/manifest.go`
  for the canonical filename before approving any acceptance criterion that
  names the manifest path.

- **Lint heuristics in spec acceptance criteria need a verbatim 3-plugin
  walkthrough.** When a spec proposes "lint check that flags any plugin with
  property X", apply the predicate to each in-tree plugin yaml and check the
  result. Seen 2026-04-25 in plugin-actor-claim-auth r3: criterion 8's heuristic
  checked `action: emit` capabilities OR non-empty `events:`; this caught
  `core-communication` (capabilities) and `echo-bot` (events:) but silently
  passed `core-scenes` (top-level `emits: [scene]`, but command capabilities are
  `action: write` and no `events:`). Fix surface: evaluate the proposed heuristic
  against every plugin in the migration scope — if any scoped plugin is not
  flagged when it should be, the heuristic is wrong, not the scope.

- **Migration-section consistency: every section that lists migrating plugins
  must list ALL of them.** Specs fix a "missed plugin" finding in the design
  section but forget the same fix in §migration-path / §release-notes / §risks.
  Round 2 of plugin-actor-claim-auth surfaced echo-bot; round 3 fixed §3.2, §6,
  criterion 8 but left §9 step 1 still naming only `core-scenes`. Cross-grep the
  spec for the marquee name AFTER any migration-scope edit.

- **Lint heuristic enumeration claim ≠ heuristic logic.** When a spec writes
  "this heuristic catches X via path A and Y via path B and Z via path C", the
  failure mode is that the conjunction doesn't actually evaluate true on Z's
  manifest. Re-evaluate the predicate term-by-term against EACH plugin's actual
  on-disk yaml — do not trust the prose. Round 4 of plugin-actor-claim-auth:
  criterion 8 required `emits:` non-empty AND `(commands: OR events: non-empty)`
  AND missing-character-claim; the enumeration claimed it caught echo-bot "via
  events + emits in handler" but echo-bot's `plugin.yaml` has no top-level
  `emits:` field at all (emits happen at runtime in main.lua), so the
  conjunction short-circuits false. The heuristic operates on manifest static
  state, not runtime Lua handler behavior.

- **"Existing field" claims about proto must be verified against the actual
  `.proto` file, not comments or adjacent code.** Specs have called proto fields
  "existing" because a same-named DB column or NATS header exists — not the same
  wire-format surface. Always grep the proto. Seen 2026-04-25 in
  event-payload-crypto-design: spec marked `codec` "EXISTING" on the `Event`
  proto; only the audit table column and NATS header have that name, the proto
  Event has no codec field.

- **Specs that build on a substrate spec must state which substrate abstractions
  they supersede.** When a new design introduces types that overlap/replace
  abstractions shipped in a prior spec (e.g., `KeyProvider`/`KeySelector`
  replaced by `Provider`/`DEKManager`), the new spec must call out the
  supersession or the implementation carries two competing models in one
  package. Seen 2026-04-25 (event-payload-crypto-design vs
  jetstream-event-log-design).

- **"Before X returns" / "synchronously" / "atomically" claims for
  cross-replica operations need a specified protocol.** NATS core publish,
  JetStream publish-with-ack, and request-reply have different consistency
  guarantees. A spec saying "MUST invalidate caches in all replicas before the
  operation returns" without naming the ack mechanism asserts an unprovable
  invariant.

- **Global negatives ("MUST NOT be written to disk anywhere ever") tagged
  "Unit + Lint" are usually too vague to enforce.** Unit tests can't prove a
  global negative; lint rules need concrete forbidden-symbol/call lists. Force
  the spec to enumerate the actual checks.

- **Invariants whose AAD or signature includes proto-marshaled bytes must
  specify the canonicalization rule.** Proto serialization is not deterministic
  by default; if encrypt-side and decrypt-side don't use the same canonical
  encoder, a library upgrade silently breaks decryption. The spec must name the
  canonicalization function.

- **Schema additions referenced in spec body but not in the data-model
  section.** When a spec mentions new columns on existing tables in flow diagrams
  or invariants but its data-model section only adds a NEW table, the migration
  for the additions is undefined and plan-writers invent column types. Seen
  2026-04-25 in event-payload-crypto-design: `events_audit.dek_ref/dek_version`
  referenced but never declared in §4.

- **Lifecycle invariants asserted as MUST that the AuthGuard cannot satisfy.**
  When a spec promises "previous-tenure player retains decryption" but the
  access decision tree only matches the current binding, the invariant is
  unprovable. Walk every assertion against the access-control branches before
  believing it.

- **Fix-text in revisions can introduce NEW factual errors about repo
  symbols/files.** When a spec author addresses a "list the affected call sites"
  finding, the resolution paragraph may name additional symbols or wrong file
  locations. Re-verify the fix-text against `rg` even when the prior round's
  finding was about the same area. Seen 2026-04-30 in
  event-payload-crypto-phase2-substrate r2: a "Phase 3 scope addition"
  paragraph named `IdentityKeyProvider` (does not exist) and located
  `identityKeySelector` in `codec.go` (it lives in `publisher.go:392`).

- **"Compile-time enforced" claims about proto serialization need to match the
  actual ruleguard scope.** A spec may claim "bare `proto.Marshal` is
  compile-time prevented by ruleguard rule X" when X only fires for arguments of
  an unrelated type. Trace the ruleguard predicate. Seen 2026-04-30: claim that
  `dek_no_serialize` prevents `proto.Marshal(event.Actor)` — but that rule's
  `Type.Is(...)` filter is on `*dek.Material`/`dek.Material`, not `*Actor`, so it
  never fires. The real defense was the runtime byte-equality test.

- **Lint-rule scope migrations need to account for the OLD rule's de-facto
  exclusions.** A rule that runs through `gocritic` inherits gocritic's
  `_test.go` exclusion in `.golangci.yaml`. Migrating that rule to a top-level
  linter in `linters.enable` LOSES the exclusion unless re-added. Grep for
  callsites in `_test.go` to estimate the regression surface. Seen 2026-05-01 in
  go-analysis-migration: `ulidmakeforbidden` declared "Allowlist: none
  (repo-wide)" but the codebase has 1300+ `ulid.Make()` callsites in `_test.go`
  that the existing gocritic-hosted rule silently let pass.

- **"Cost"/callsite-count paragraphs need verbatim `rg` audit, including
  test-file impls and constructor seams.** Specs changing a substrate interface
  signature commonly miss (a) test stubs implementing the interface (`*_test.go`
  Codec/Provider impls also need the new signature) and (b) constructor seams
  producing instances of the modified type (adding `Version uint32` to
  `codec.Key` requires updating `dek.Material.AsCodecKey` since it produces
  `codec.Key{}` literals). Seen 2026-05-02 in
  event-payload-crypto-phase3a-grounding: doc said "five callsites" but reality
  was 3 impls + 4 prod + 4 test callsites + an `AsCodecKey` constructor. Run
  `rg "func \(.*\) (Encode|Decode)\("` AND `rg "\.(Encode|Decode)\(ctx"` AND
  find any constructor that returns the modified type before accepting a count.

- **Substrate signature changes touching both directions (Encode AND Decode)
  need decode-side wiring stated.** When a spec extends `Codec.Encode/Decode`
  with a new parameter (e.g., `aad []byte`), only the encode-side caller is
  usually described. Decode callers must also pass *something*. If the substrate
  has live Decode callers (subscriber, history hot tier, plugin audit consumer),
  the spec must state what value they pass in the interim (typically `nil` if
  `IdentityCodec` ignores). Seen 2026-05-02 in
  event-payload-crypto-phase3a-grounding: Decision 1 specified encode-side
  `aad.Build` but not what the 3 production Decode callsites pass during 3a.

- **"Pseudocode call shape" in a master spec doesn't validate against the actual
  interface.** When a master spec §X shows a call with rich arguments (e.g.,
  `abac_engine.Evaluate(subject, action, resource, attributes={...})`), grep the
  actual interface BEFORE accepting a spec that imports it as-is. Real
  `types.AccessRequest` had only `(Subject, Action, Resource string)` — no
  `attributes` field — so master spec §7.2 Branch 3's call shape was fictional.
  Every spec declaring `<interface>.<method>(<args>)` should verify `<args>`
  against the real signature. Seen 2026-05-02 in
  event-payload-crypto-phase3b-grounding finding 1.

- **Naming collisions between new package types and existing types in the same
  import set.** When a new package introduces a type whose name already exists in
  a package its consumers import (`authguard.Subject` struct vs `eventbus.Subject`
  string), every callsite importing both has two `Subject`s in scope. Grep the
  consumers' import set for namespace conflicts BEFORE the names are baked in.
  Seen 2026-05-02 in event-payload-crypto-phase3b-grounding finding 2:
  `authguard.Subject` proposed while `eventbus.Subject` is the JetStream filter
  type in the very same `OpenSession` signature.

- **Constructor-list cross-section consistency.** A grounding doc that
  enumerates `New<Kind>Subject(...)` constructors in Decision A must enumerate
  the SAME set in every later decision. Seen 2026-05-02 in
  event-payload-crypto-phase3b-grounding finding 3: Decision 1 declared four
  constructors + a 4-non-Unknown enum; Decision 2 named five including
  `NewSystemSubject` with no enum value, no §X branch, no boundary-table row.

- **"Embedded `<other-package>.Decision` for trace data" rationale needs to match
  the actual struct shape.** Specs write "the foreign engine's Decision is
  embedded only when consulted, for trace data" but the struct has only flat
  fields — trace data flow is undefined. Either the struct needs `ABACDecision
  <type>` (or pointer/embedded) or the rationale drops. Seen 2026-05-02
  (phase3b-grounding finding 4).

- **Cost-line for test-file edits frequently misdescribes the load-bearing
  assertion.** When a wire-shape change ripples into an integration test, the
  cost-line author often says "byte-equality assertion shifts from X to Y"
  without checking the invariant is preserved. Trace the assertion line-by-line
  under the new shape. Seen 2026-05-02 (phase3b-grounding finding 5): spec said
  INV-21 shifts from `msg.Data` to `envelope.Payload`, but
  `audit.projection.go:281` writes `msg.Data()` to the audit row — both stay
  byte-equal under the new shape; shifting to `envelope.Payload` REGRESSES
  INV-21.

- **TOCTOU defense paragraphs need an in-process plaintext-residency contract.**
  A permit→decrypt→enqueue-audit→stamp-metadata_only flow with a "stamp
  metadata_only=true on enqueue failure" defense does NOT specify what happens
  to the decrypted plaintext in memory at the stamp moment. INV-9 ("subject not
  in DEK MUST NOT receive plaintext") is wire-testable but the residency window
  is undocumented. The spec must state operation order: (a) AuthGuard first /
  decrypt only on permit, OR (b) decrypt first / overwrite-on-deny, with a
  unit-test invariant naming the chosen order. Seen 2026-05-02
  (phase3b-grounding finding 8).

- **Sibling type signature drift in the same package.** When a spec proposes two
  sibling types with conceptually identical methods (`dek.Cache`,
  `dek.ParticipantsCache`, both `InvalidateContext`), readers in different
  sections independently propose different signatures (one takes a struct, the
  other split string args). Grep the spec for the method name and reconcile
  EVERY occurrence, including pseudocode/protocol-flow callsites. Seen
  2026-05-02 (phase3c-grounding finding 1).

- **Single-replica self-pill failure mode.** Specs proposing strict-N + probe +
  pill protocols on a "self-counts-as-member" model say "single-replica
  degenerates to N=1; the contract is identical." The CONTRACT is identical but
  the FAILURE MODE is worse: in N≥2 a hung member gets pilled and the cluster
  proceeds with N-1; in N=1 the member self-pills and the cluster self-immolates.
  Require an explicit "Coordinator MUST NOT issue a pill targeted at its own
  MemberID" invariant + test. Seen 2026-05-02 (phase3c-grounding finding 4).

- **Performance-only substrate must say so explicitly.** When Phase X proposes
  substrate above Phase X-1 and the only correctness invariant satisfied is one
  X-1 already satisfied differently, the new substrate is performance work. The
  spec must either (a) state "Phase X is performance optimization; correctness
  unchanged from X-1" and re-justify cost on that basis, or (b) declare the new
  correctness-binding invariant. Otherwise ~14 implementation tasks chase a
  ghost. Seen 2026-05-02 (phase3c-grounding finding 5): "Full scope" caches
  participants but every new INV-53..58 covers protocol mechanics, none binds
  the load-bearing "after Add() returns, every replica's Participants(...)
  reflects the new participant within timeout" promise.

- **Authority-boundary ambiguity for primitives shared across consumers.** When
  a spec puts a primitive (`ProbeAndPill`) on a shared package
  (`cluster.Registry`) with "future consumers can reach for the same primitive"
  but defines a rate-limit on the *consumer* (`Coordinator`) not the primitive,
  the limit is bypassable: any other consumer calls Registry directly and floods
  the wire. Pin rate-limits on the primitive, OR document "rate-limit is
  per-caller; future callers MUST also rate-limit." Seen 2026-05-02
  (phase3c-grounding finding 2).

- **Payload schema field reuse across actions with mismatched semantics.** A
  single payload schema for a multi-action protocol
  (`{old_version, new_version, action}`) may not map to all fields. `Add()` on a
  vN active-version has no "old_version" — it has an "active_version that was
  just mutated." Plan-writers populate `old_version=0` → receive-side cache
  keys on `Version: 0` → silent no-op eviction → the load-bearing invariant
  fails. Either rename to an action-neutral field (`active_version`) or define
  per-action sub-schemas. Seen 2026-05-02 (phase3c-grounding finding 3).

- **"Documentary" invariants are usually lintable.** When a spec assigns
  test-class "Documentary" to an invariant with rationale "absence-of-thing
  property", push back: most negative-space properties are testable via
  `gorules` ruleguard, semgrep, or a `go/analysis` AST walker (e.g., "no
  subtraction of remote-sourced time field from local clock"). Force the spec to
  enumerate the actual lint check; if none exists, the invariant is too vague.
  Seen 2026-05-02 (phase3c-grounding finding 1, non-blocking): INV-58 "no
  cross-host wall-clock comparison" is lintable.

- **Decomposition specs that lock substrate decisions can quietly contradict the
  master spec they cite.** When a meta-spec says "master spec §X remains
  authoritative" but introduces a substrate decision depending on a property the
  master spec explicitly denied, the contradiction surfaces in the dependent
  sub-epic spec weeks later rather than at decomposition review. Grep the master
  spec for the *negation* of any defense-factor / capability / mechanism the
  decomposition relies on. Seen 2026-05-07 in
  event-payload-crypto-phase5-decomposition Finding 1: Decision 6 listed "Shell
  access to host" as a defense factor; master spec §5.9 lines 1276-1279 + §7.5
  lines 1698-1701 explicitly state SO_PEERCRED/host-access is NOT a defense
  factor, and the decomposition didn't amend those sentences. Procedure: for
  every "factor"/"gate"/"defense" claim in a decomposition spec, grep the master
  spec for the same noun and verify alignment.

- **Master-spec amendment tables miss schema additions for new audit-event
  shapes.** Decomposition specs introducing new audit event types (e.g.,
  `crypto.policy_set`) with new fields (`prev_hash`, `policy_hash`) amend §11.1
  phasing and §10 failure modes but forget §4.6 (audit-event-shapes) where the
  schema lives. Check whether the new event type has a defined wire shape in
  master spec §4 and require a row that adds it. Seen 2026-05-07 Finding 2:
  `crypto.policy_set`'s `prev_hash`/`policy_hash`/server-start ULID/full
  snapshot were none anchored in master spec §4.6 — two implementers would
  invent different shapes.

- **"Capability" naming collisions with existing host-side authorization
  types.** When a spec proposes a new player-attribute grant called "capability"
  (`crypto.operator`), check whether `internal/command/`, `internal/access/`, or
  other host packages already use "Capability" as a structured type. HoloMUSH has
  `command.Capability{Action, Resource, Scope}` — distinct from a player-grant
  string. Force a distinct name ("operator-grant", "crypto-operator allow-list")
  or an explicit collision call-out.

- **Generated-binding filename pattern claims need verification.** Specs
  mentioning "ConnectRPC bindings generated alongside `*_grpc.pb.go`" usually
  mis-describe the layout. HoloMUSH generates ConnectRPC into
  `<svc>v1connect/<svc>.connect.go` (a separate sub-package per service), not
  `<svc>_connect.go` next to `<svc>_grpc.pb.go`. Run
  `rg --files <proto-output-dir> | rg connect` before approving a layout claim.

- **"Pinned recompute composition" claims with typed-struct vs bytes
  asymmetry.** When a spec generalizes a hash-recompute function from
  `f(*TypedStruct) []byte` to `f([]byte) []byte` to make it primitive-shareable,
  walk the typed body for steps operating on TYPED STATE before marshal (e.g.,
  `if len(canon.PrevHash) == 0 { canon.PrevHash = nil }` empty-vs-nil
  normalization). Those steps have nowhere to live in a `[]byte → []byte`
  primitive: already-marshaled bytes cannot distinguish `[]byte{}` from `nil`
  once JSON has them as `""` vs `null`. The "preserved by construction" claim
  fails unless the per-chain Canonicalize absorbs the normalization (contradicting
  a "no per-chain divergence" invariant) or callers normalize first (demoting "by
  construction" to "by caller discipline"). Seen 2026-05-10 in
  event-payload-crypto-phase5-sub-epic-e R2: pinned `RecomputeSelfHash([]byte)`
  claimed to subsume D's `ComputePolicyHash(*PolicySetPayload)` whose PrevHash
  normalization (chain.go:50-52) is asserted by
  `TestComputePolicyHashNormalizesEmptyPrevHashToNil`.

- **Subject-prefix migration must trigger an ABAC-denial-coverage audit.** When
  a spec moves an audit-event subject family from `audit.*` to `events.<game>.*`
  (to land on JetStream's `events.>` SubjectFilter), the master-spec's ABAC
  denial line for plugin/character subscribers (enumerating `audit.*` literally)
  no longer covers the migrated family. Grep master spec for
  `INV-15|deny.*subscribe|audit\.>` and force the §amendments table to either
  (a) cite a default-deny source already covering the new prefix or (b) add an
  explicit ABAC-denial amendment row. Seen 2026-05-10 in sub-epic E: rekey moved
  to `events.<game>.system.rekey.*`, master INV-15 still enumerated `audit.*`,
  no §9 row updated it. (D made the same move for `crypto.policy_set` without an
  ABAC update — a latent gap, not a precedent to follow.)

- **Spec-illustrated example registrations must satisfy the spec's own MUST
  invariants.** When a spec adds new MUST invariants on a registry/struct (e.g.,
  INV-E27 "every Chain MUST populate ScopeFromPayload"), grep every literal
  example registration in the same spec to verify each MUST field is populated.
  Plan authors copy the example verbatim and ship non-compliant code; only the
  meta-test catches it. Seen 2026-05-10 in sub-epic E R2: §3.7's
  `var RekeyChain = auditchain.Chain{...}` literal omits both `ScopeFromPayload`
  (INV-E27) and `SelfHashFieldName` (INV-E28).

- **`workflow_run` cannot fire on a path-skipped upstream.** A spec proposing a
  `workflow_run`-triggered aggregator to restore branch-protection coverage for
  a `paths-ignore`-skipped upstream is broken by construction: a path-skipped
  workflow produces zero `workflow_run` events (no run was created; skipped
  checks stay `Pending`). Flag BLOCKING. Known-working patterns: (1) "same-name
  skip workflow" — a sibling with the OPPOSITE `paths`/`paths-ignore` filter and
  the SAME job names so GitHub treats them as one check; (2) a single always-on
  workflow using `dorny/paths-filter` to conditionally run expensive jobs. A
  `pull_request`-triggered aggregator that introspects the CI workflow status via
  API works but is a different design and must be specified as such. **Companion
  gotcha:** rollouts that add a `CI Required` aggregator ALONGSIDE the legacy
  required checks before deploying `paths-ignore` deadlock — any docs-only PR
  opened between "protection updated" and "legacy checks removed" stalls on the
  legacy checks the paths-ignore prevents from reporting. Order the
  protection-settings swap atomic with (or BEFORE) the `paths-ignore`
  deployment. Seen 2026-05-14 in pr-prep-docs-fast-lane-design.

### go-task / Taskfile shell semantics (verified empirically)

- **Taskfile shell idioms must be verified against mvdan/sh, not `/bin/sh`.**
  go-task's inline `cmd:` blocks execute under `mvdan.cc/sh/v3/interp` (a
  pure-Go shell), NOT `/bin/sh`. Flag for empirical verification any of:
  `exec N>`/`exec N<` for N≥3 (rejected — mvdan/sh only supports fds 0/1/2);
  `coproc` or process substitution `<(cmd)`; reliance on fd inheritance through
  `os/exec` boundaries (go-task's external path uses `os/exec.Cmd` with only
  Stdin/Stdout/Stderr — `ExtraFiles` is never set); `set -o pipefail` outside
  `set: [pipefail]`; background jobs (`&`, `wait`); signal handlers (`trap`).
  **Fix recipe:** wrap in `/bin/sh -c '…'`, or use the tool's native form — e.g.
  `flock LOCKFILE COMMAND` (opens the fd inside the `flock` binary) instead of
  `exec 9>; flock -n 9`. Seen 2026-04-26 in pr-prep-concurrency-safety: spec
  proposed `exec 9>"$LOCK"; flock -n 9; task pr-prep:run` — empirically fails on
  the first line.

- **go-task `internal: true` is CLI-blocked and hidden from `--list-all`.**
  Enforced by go-task itself, not the shell: any CLI invocation `task <name>` is
  short-circuited with stderr `task: Task "<name>" is internal` and exit 202
  BEFORE any cmd runs — including from inside a `sh -c '…'` subshell spawned by
  another Taskfile cmd (it spawns the `task` binary as a child). Internal tasks
  are reachable only via the YAML `task: <name>` keyword, and are hidden from
  BOTH `--list` and `--list-all` (verified against this repo's `license:run`,
  Taskfile.yaml:447). Layering an env-var "bypass guard" on top is dead code —
  go-task's gate fires first. Seen 2026-04-26 in pr-prep-concurrency-safety r2:
  a flock harness `exec task pr-prep:run` where `pr-prep:run` was
  `internal: true` — lock acquired, info file populated, exec returned 202, the
  CI body never ran; plus invariant I-10 asserted "MUST appear in `--list-all`",
  structurally false. Workaround: drop `internal: true`, keep only the env-var
  guard (which then becomes load-bearing and testable).

- **go-task wraps user exit codes by default; `--exit-code` required for
  passthrough.** Whenever a `cmd:` returns non-zero, go-task itself exits with
  **201** ("Command execution error") regardless of the user command's code
  (codes 200-255 are go-task-specific). To pass through the actual code the
  operator must invoke `task --exit-code <name>` (or `-x`). Verified against
  go-task 3.50.0: `task outer` (cmd `exit 42`) → 201; `task --exit-code outer` →
  42. This double-wraps when one Taskfile invokes `task` as a child via
  `exec`/`sh -c` — each layer wraps unless `--exit-code` is passed at every
  layer. Flag specs that assert literal exit-code passthrough ("harness MUST
  exit 42 when inner exits 42") or a literal `[ "$status" -eq 1 ]` test row
  against a harness that does `exit 1`. Fix: (a) weaken to "MUST exit non-zero",
  (b) add `--exit-code` at every layer + update operator instructions, or (c)
  use a sentinel go-task doesn't wrap. Note: flock's own non-zero exits (75
  EWOULDBLOCK via `-E 75`, 66 EACCES) are captured by the harness's `rc=$?`
  BEFORE go-task sees them, so the harness can branch on those correctly — only
  codes flowing up through `cmd:` non-zero return get wrapped. Seen 2026-04-26
  in pr-prep-concurrency-safety rev-3 (I-6 `propagates_exit_code`).

- **go-task `exec` inside one cmd does NOT short-circuit subsequent cmds.** When
  a task has multiple `- cmd:` entries, each runs in its own subshell. A
  successful `exec <other>` (or bare `exit 0`) in cmd1 terminates ONLY that
  subshell; go-task then proceeds to cmd2. `X=1 task default` with a
  conditional `exec sh -c 'echo replaced; exit 0'` in cmd1 and
  `echo "second cmd ran"` in cmd2 prints BOTH. Only a NON-ZERO exit from cmd1
  short-circuits cmd2 (and even then the parent exits with wrapper 201). Designs
  relying on "cmd1 detects-and-execs to skip cmd2" are structurally broken on
  the happy path. Correct shape: a single `- cmd:` containing both branches, or
  a dispatcher/runner task split. Seen 2026-05-14 in pr-prep-docs-fast-lane r2
  §4.3.1.

### golangci-lint

- **golangci-lint v1 vs v2 exclusion schema.** ALWAYS check the file's top-level
  `version:` field before approving exclusion YAML. v1 → v2 renames:
  `issues.exclude-rules` → `linters.exclusions.rules`; `issues.exclude-files` →
  `linters.exclusions.paths`; `linters-settings.<linter>.ignore-tests` →
  `linters.exclusions.rules` (path-scoped); `linters-settings` →
  `linters.settings`. A v1-shaped snippet in a v2 config is either silently
  ignored (exclusion doesn't take effect) or fails config-load. HoloMUSH is v2
  (confirm at `.golangci.yaml:1`). Seen 2026-05-01 in go-analysis-migration
  pass 2: a `_test.go` exclusion for `ulidmakeforbidden` used
  `issues.exclude-rules` against a `version: "2"` config, defeating the very fix
  it was meant to add.

- **golangci-lint module-plugin package name claim.** Module plugins do NOT
  require `package main`; the canonical upstream example
  (`golangci/example-plugin-module-linter`) uses `package linters`. They load
  via Go's module/import machinery, not `go build -buildmode=plugin`. Verify
  against `golangci/plugin-module-register/register.go` (defines `LinterPlugin`,
  `NewPlugin`, `LoadModeSyntax`, `LoadModeTypesInfo`, `Plugin(name, constructor)`
  with no package-name constraint). Seen 2026-05-01 in go-analysis-migration
  §4.3: spec claimed `package main // must be main per golangci-lint loader` —
  both halves fabricated. Related positive from the same spec: the
  `//go:build ruleguard` tag IS visible to `go mod tidy` (only `ignore` is
  hidden), so any spec carving a sub-module with `//go:build ruleguard` files
  must delete those files before/with the new `go.mod`.

### Registration & proto-shape verification

- **`internal/core/builtins.go` `VerbRegistration` literals must be checked
  field-by-field** against the actual type in `internal/core/registry.go:14-30`
  AND an existing precedent (e.g., the rekey entry at `builtins.go:93`). Common
  failure modes: `Category: AuditOnly` — `Category` is a plain string ("system",
  "movement"), NOT an enum; "AUDIT_ONLY" lives in the separate `DisplayTarget`
  field as `corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY`. `ProducedBy: "core"`
  — this field does not exist; the ownership field is `Source: "builtin"`.
  Missing `Format`/`DisplayTarget` — both required by `registerNoLock`
  validation (`registry.go:54-89`); empty values produce `INVALID_REGISTRATION`
  at boot. Seen 2026-05-12 in event-payload-crypto-phase5-sub-epic-f r4: §3.8
  declared `{Category: AuditOnly, ProducedBy: "core"}`; real shape per
  `builtins.go:93` is `{Category: "system", Format: "audit", DisplayTarget:
  corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"}`. INV-F13
  ("registered with `AuditOnly` category") was ungrounded because Category is a
  different field surface than DisplayTarget.

## Interfaces and boundaries that recur

- **`.git` dir-vs-file across worktrees** (the native-git analogue of the old
  jj worktree-detection note): in the main working tree `.git` is a directory;
  in any linked worktree `.git` is a *file* containing `gitdir: <path>` pointing
  back to the main repo's per-worktree gitdir. Anything that needs to know "am I
  in the main working tree" or "where is MAIN_REPO" can key off this dir-vs-file
  distinction.
- **SessionStart hook output**: plain stdout is concatenated as additional
  context. `bd prime` is the canonical example. JSON
  `hookSpecificOutput.additionalContext` is the alternative but no in-tree hook
  uses it.
- **`task` cannot mutate the caller's shell `pwd`/env**: any spec that wants
  "after this task, your shell is in directory X" must be a shell
  function/wrapper, not a task target. Treat as a hard constraint when reviewing
  automation specs.

## Phase 7 plugin SDK review patterns (2026-05-13)

- **Proto-path claims need verbatim filesystem verification.** `api/plugin/v1/
  plugin.proto` may be the logical path (`<package>/<file>`); on-disk the
  convention is `api/proto/holomush/<package>/<file>.proto` (reverse
  `pkg/proto/holomush/<package>/<file>.pb.go` is generated). Run
  `Glob api/proto/**/*.proto` AND check the `source:` line in any `*.pb.go` /
  `*_grpc.pb.go` in the same generated tree. Seen 2026-05-13 in
  event-payload-crypto-phase7-plugin-sdk v2: spec cited
  `api/plugin/v1/plugin.proto` throughout; actual path is
  `api/proto/holomush/plugin/v1/audit.proto`.

- **Clean-break proto reshape MUST enumerate every caller of dropped fields.**
  The "no prod-shape discipline for undeployed codebases" rule exempts a spec
  from `reserved` markers / compat shims / deprecated field IDs — it does NOT
  exempt it from naming every caller of every dropped field. For every field
  added OR removed, run `rg "Get<FieldName>" --type go` AND
  `rg "\.<FieldName>\s*[=:]"` across the repo and list every callsite in the
  spec's §migration; test files count. Seen 2026-05-13 in phase7 v2: §4.2
  swapped `AuditEventRequest.event + headers` → `AuditEventRequest.row AuditRow`
  and dropped the `headers` map silently; three callers consumed it
  (`plugins/core-scenes/audit.go:160-237`,
  `test/integration/eventbus_e2e/plugin_audit_isolation_test.go:175-220`,
  `internal/eventbus/audit/plugin_consumer_unit_test.go:82`) — the spec named
  one. Also: the new `AuditRow` carried `codec`/`dek_ref`/`dek_version` but NOT
  `schema_ver` even though `scene_log.schema_ver` is a NOT NULL column, with no
  statement of where `schema_ver` data comes from post-reshape.

- **"Existing callback/reload hook" claims need a `path:symbol` citation.** When
  a spec says "refreshed on manifest reload via existing hook" or "uses the
  existing watcher", the hook MUST be named `path:symbol` — otherwise the
  invariant it supports is a ghost. Generic callback infra (registrar /
  unregistrar / `OnChange`) is often misremembered as supporting reload when it
  is load-only. Grep `internal/<package>/manager*.go` / `*loader*.go` for
  `Reload`, `Refresh`, `Subscribe`, `OnChange`, `Watch`. If only initial-load
  callbacks exist, require the spec to (a) cite the symbol verbatim, (b)
  downscale the invariant to "atomic on initial load", or (c) spec the new
  callback. Seen 2026-05-13 in phase7 v2: §4.4 said "no new manifest-watch
  infrastructure"; probing `internal/plugin/` found
  `RegisterPluginProviderFunc`, `WithAttributeProviderRegistrar`,
  `WithAttributeProviderUnregistrar`, `alias_seeder.SeedManifestAliases`,
  `Manager.unregisterPluginProviders` — none a manifest-set-changed callback, so
  INV-P7-8 ("atomic refresh on manifest reload") was unprovable as drafted.
