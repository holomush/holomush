<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plan review — preserved learnings

These are un-curated review learnings preserved verbatim-in-substance from the
now-retired `plan-reviewer` agent's memory (its `MEMORY.md` plus per-finding
feedback notes) — patterns of good and bad HoloMUSH plans discovered during
adversarial plan review. They are kept for reference by GSD's `gsd-plan-checker`
and human reviewers. This file is **not** auto-loaded; read it on demand when
reviewing a plan. jj-specific mechanics from the original notes have been
converted to native-git equivalents or dropped.

## R2-fix regressions (INV-S5 plan, 2026-05-17 round 3)

- **Global find-replace double-qualification.** When R2 fixes a "bare
  `NewHostWithFunctions` won't resolve in `package lua_test`" finding by
  global-replacing `NewHostWithFunctions` → `pluginlua.NewHostWithFunctions`,
  the replacement also fires on sites already qualified, producing
  `pluginlua.pluginlua.NewHostWithFunctions`. Require: "scope the replace to
  bare (un-prefixed) occurrences only", and grep `<pkg>\.<pkg>\.` after every
  revision that qualifies symbols.
- **Package-internal qualifier inside a test file declared in that package.**
  When the parity test file is `package plugins` (the package under test),
  `WithLuaHost` is unqualified — but `plugins.TypeBinary` reads naturally to a
  planner thinking in cross-package sites. Grep the test file for
  `<own-package>\.` after writing/revising; any hit is a package-internal
  qualifier and a Go compile error.
- **`_test.go` symbols don't cross package boundaries.** A wrapper `NewMockHost`
  in `internal/plugin/goplugin/mock_export_test.go` is visible only when
  building tests of `package goplugin` — NOT when `go test ./internal/plugin/`
  imports `goplugin` as a regular library. Cross-package test helpers MUST live
  in a non-`_test.go` file (or the parity test moves into the helper's own
  package). "Expose via _test.go file" as if sufficient is wrong — demand a
  non-test file or an explicit acknowledgment the test must move.
- **`oops.Code("X").Is(err)` matches ANY `OopsError` regardless of code** (per
  `samber/oops@v1.21.0/error.go:87-93`). Test assertions like
  `oops.Code("X").Is(err) || strings.Contains(err.Error(), "X")` pass for any
  oops-wrapped error in the first disjunct. Repo convention is
  `errutil.AssertErrorCode(t, err, "CODE")` which DOES check code identity.
  Treat the `oops.Code(...).Is` pattern in test assertions as a non-blocking
  finding to flag.

## INV-S5 emit-type-validation reflexes (2026-05-16)

- **Lua plugins have NO init phase** (`internal/plugin/lua/host.go:111-163`).
  `Host.Load` runs `L.DoString(code)` in a THROWAWAY state from
  `factory.NewState(ctx)` with `defer L.Close()` and does NOT call
  `hostFuncs.Register(L, name, requires...)` — the `holomush` global table
  doesn't exist there. Every `DeliverEvent`/`DeliverCommand` (host.go:186,264)
  creates a fresh state and calls `Functions.Register` per delivery. Plans
  shaped "plugin init registers types, host reads after Init" against the Lua
  runtime target a phase that does not exist. To add one: extend `*luaPlugin`
  with a registry field, run script-once on Load in a state where hostFuncs ARE
  registered, capture the registry, store on the `*luaPlugin`.
- **Binary plugins are out-of-process** (`internal/plugin/goplugin/host.go:420`
  `exec.Command(realExec)`). Host's only handle is
  `pluginv1.PluginServiceClient` + `pluginv1.PluginHostServiceClient` over mTLS
  gRPC. A Go method like `func (p *scenePlugin) EmitRegistry() *pluginsdk.EmitRegistry`
  returning a pointer to in-memory plugin state is unreachable by host code. To
  expose plugin state to host: add a field to `pluginv1.InitResponse` (proto
  change + regen, populated in `pluginServerAdapter.Init`) OR add a new
  `PluginService.GetEmitTypes` RPC.
- **`internal/plugin/goplugin/host.go:528`** is where binary plugin Init RPC is
  invoked (`pluginClient.Init(ctx, initReq)`). InitResponse today carries only
  `provided_services` — extending it is the cheapest path to add post-Init
  plugin metadata to the host's view.
- **`internal/plugin/manager.go::loadPlugin`** starts at **line 849**, not 844.
  `host.Load(...)` is invoked at **line 989**, NOT inside semantic validation
  (lines 880-904, which run BEFORE host.Load). Plans wiring "post-Init
  validation" against manager.go:880 insert the call BEFORE any Init fires. For
  Lua, "post-Init" doesn't apply. For binary, the Init RPC lives in
  goplugin/host.go:528, not in manager.go::loadPlugin — the manager-level
  wrapper has no return-value handle on Init outputs.
- **`(*Manifest).Crypto.Emits []CryptoEmit`**
  (`internal/plugin/crypto_manifest.go:26-37`) is where to read declared event
  types. `m.Crypto == nil` guard required (per `LookupEmitSensitivity` precedent
  at line 60-69).
- **Plan-deferred substrate seams**: "the implementer settles based on the
  current Host interface shape" / "discover during task" / "examine the existing
  test harness" — each is a plan-failure finding when the seam is load-bearing
  (Task 1 wiring), acceptable when peripheral (Task 5 site nav).
- **Fail-closed flip without fixture audit**: plans that say "flip fail-closed,
  investigate test fixtures if they break" have unscoped blast radius. The audit
  (`rg "^crypto:" plugins/ test/`, classify each manifest) MUST precede the flip,
  not chase it via prose after `task test:int` breaks.

## Phase 7 plugin-SDK reflexes (2026-05-13)

- **Per-row fence refusal must NOT be stream-fatal.** A `Next() (ev, err)` where
  `err != nil` short-circuits the entire history stream — the caller stops
  iterating. Plans that wrap a HistoryStream with a fence returning
  `oops.Code("AUDIT_ROW_DOWNGRADE_DETECTED")` on the FIRST refused row DoS
  legitimate subsequent rows. Per-row refusals MUST surface as `(ev, nil)` with
  `ev.MetadataOnly=true` + a typed `NoPlaintextReason` enum value. Add the new
  reason to `internal/eventbus/types.go` (existing enum at `types.go:28`) — don't
  reuse a generic reason.
- **`internal/eventbus/audit` import from `pkg/plugin/`** is technically allowed
  (same root) but creates an SDK precedent plugin authors will copy. Force the
  plan to commit on package location: extract the helper to a shared neutral
  package (`pkg/eventbus/auditheader/`) OR explicitly accept the SDK→internal
  coupling with a justifying comment. "Decide at implementation time" is a
  blocking deferral.
- **`PluginConsumerManager` constructor signature** is
  `NewPluginConsumerManager(js jetstream.JetStream) *PluginConsumerManager`
  (`plugin_consumer.go:89`). Plans adding `WithKeySelector` options MUST also
  retrofit the constructor to variadic options OR add the field directly.
  Showing `audit.NewPluginConsumerManager(/* options */, WithKeySelector(...))`
  without changing the constructor is a compile error.
- **`pluginConsumer` (lowercase) has no manager backpointer**: per-consumer
  dispatch can't reach manager-level fields (`plugin_consumer.go:67-74`). Plans
  wiring "shared selector" on the manager need to (a) thread the selector through
  per-consumer config, (b) accept the selector is only consumed at boot-time
  validation, or (c) add a manager backpointer. (b) matches INV-P7-9
  substrate-symmetry framing but needs explicit acknowledgment.
- **Encrypted wire format is per-FIELD, not per-envelope**:
  `internal/eventbus/publisher.go:266-292` encrypts `event.Payload` only;
  envelope metadata stays cleartext. `hot_jetstream.go:441-444` proto-unmarshals
  `msg.Data()` directly into `eventbusv1.Event`. Plans asking the agent to
  "verify the wire-format invariant" before locking implementation reopen a
  settled question — cite the publisher + decoder lines instead of hedging.
- **Self-review tables can hide deferred decisions**: when a plan's self-review
  "all invariants covered" table maps INV-N to "C.3 (test name)" but C.3.4 says
  "agent decides between three approaches", the invariant coverage is
  theoretical. Cross-check self-review claims against the steps they cite.
- **`core-scenes/audit.go:177-191` has a header→Event.Type fallback chain**:
  existing AuditEvent reads `headers[auditHeaderEventType]` with fallback to
  `ev.GetType()`. Plans rewriting to `row.GetType()` only must explicitly say
  whether the fallback is dropped intentionally (because the new wire guarantees
  row.Type via dispatcher) and audit existing tests for the fallback path.
- **`MetadataOnly` field on `eventbus.Event` exists**
  (`internal/eventbus/types.go:172-179`) but `NoPlaintextReason` needs a NEW
  entry for fence-refusal cases. Don't reuse `NoPlaintextReasonAuthGuardDeny` or
  `NoPlaintextReasonStaleDEK` — those have specific operator-runbook semantics.

## Shell-script plan reflexes (2026-05-14)

- **`yq` (mikefarah v4, the one in `task setup`) emits literal "null" on missing
  keys with exit 0.** Plans guarding extraction with `[ -n "$VAR" ]` silently
  accept the string `null` and process it as data. Verified: `yq '.absent.key'
  file.yaml` prints `null`, exits 0. Correct guard: `yq -e '...'` (exits 1 on no
  match) AND a `[ "$VAR" = "null" ]` belt-and-braces check. The
  pr-prep-docs-fast-lane rev3 plan shipped both `docs-paths-regex.sh` and
  `lint-docs-paths-sync.sh` with the broken guard — under any future rename of
  `vars.DOCS_ONLY_PATHS`, every diff classifies as full-lane silently.
- **bats integration tests against production `task <X>` are recursion traps.**
  When the task under test transitively calls `task test:bats` (holomush
  `pr-prep:run` does at Taskfile.yaml:577-578) and the bats file under
  `scripts/tests/` is the one driving the test, the outer invocation spawns an
  inner bats invocation re-running the same file. The existing
  `scripts/tests/pr-prep-lock.bats` solves this via a `fixture_taskfile`
  indirection (`scripts/tests/Taskfile.test.yaml`) — extract the production cmd
  block into a fixture and let bats drive `task -t <fixture> <task>`. Plans that
  invoke `task pr-prep` directly under bats inherit lock contention
  (`/tmp/holomush-pr-prep/lock`) plus real-lint output racing the test's stdout
  assertion.
- **`timeout 5 task ...` is not a substitute for fixture-based test isolation.**
  It masks false positives (the `docs-only diff detected` marker prints BEFORE
  the `exec`, so a docs lane that crashes AFTER the marker still satisfies a
  contains-marker assertion) and produces non-deterministic runtimes.
- **Plan author's "regression-guard" stdout-marker selection.** Plans asserting
  `! [[ "$output" == *"pr-prep:run"* ]]` to prove the flock body didn't run pick
  the wrong proxy — `pr-prep:run` appears in multiple unrelated stdout paths
  (task --list, desc strings, error messages). The spec's recommended assertion
  (lock-file existence under a `BATS_TEST_TMPDIR`-scoped `TMPDIR`) is
  structurally sounder than the plan's "pinned decision" override.
- **Line-range citations in plan modify lists drift by ±1 from the separator
  blank line.** Plan said "Replace lines 7-12" but current `Taskfile.yaml:12` is
  the blank separator before `tasks:`. Literal subagent execution would consume
  the separator. Read the cited range to confirm whether the blank-line
  separator is inside or outside the replacement.
- **Intermediate-commit broken `task lint` from out-of-order wiring.** When the
  plan wires a new sub-lint into the `lint:` umbrella in Task N (before the
  YAML/code the sub-lint validates lands in Task N+1, N+2), every commit between
  N and N+2 leaves `task lint` broken. Breaks `git bisect` and traps concurrent
  agents running `task lint` during that window. Fix: reorder (data-first,
  lint-last) or introduce a temporary intermediate umbrella not called from the
  general `lint:` task.

## Sub-epic F reflexes (2026-05-12)

- **`eventbus.ActorKindHost` does NOT exist.** Real constants at
  `internal/eventbus/types.go:62-71` are
  `ActorKindUnknown / Character / Player / System / Plugin`. Host audit emit uses
  `ActorKindSystem` (precedent:
  `internal/eventbus/authguard/audit/emitter.go:259`). Plans saying
  `Actor.Kind: ActorKindHost` with "if the constant name differs, align" are
  placeholder-asks the implementer to guess.
- **`internal/core` verb registry uses `NewVerbRegistry` +
  `registerBuiltinTypes(r, hostVersion)`.** NOT `NewRegistry` /
  `registerBuiltins(r)`. The unexported func takes TWO args (second is
  `hostVersion string`); single-arg calls don't compile. Public symbol is
  `BootstrapVerbRegistry(hostVersion string)` (`internal/core/builtins.go:20`).
  Plans inventing `r.Get(name)` for lookups must verify the actual accessor
  against `registry.go`.
- **`approval.RequestID` is `[16]byte`, NOT a string**
  (`internal/admin/approval/types.go:13`). Go does NOT allow direct
  `string→[16]byte` named-type conversion. Fixtures saying
  `approval.RequestID("01H...")` or `Approval{RequestID: "01H"}` don't compile.
  Use `ulid.MustParse(...)` then convert.
- **Plan-rev drift after substrate amendment**: when an R1 review forces a NEW
  substrate (e.g., `HistoryQuery.SensitiveOnly`) into the spec, the spec rev
  bumps but the plan header citing "rev N" often lags. Diff plan-header rev vs
  spec-header rev at the start of R2.
- **`eventbus.Event.Sensitive` is publisher-set, NOT decoder-set on hot path**:
  per `types.go:127-134` comment, "cold-tier reads return Sensitive=false (the
  row's codec column is the source of truth on read)".
  `hot_jetstream.go:499-507` `decodeJetStreamMessage` builds the return Event
  WITHOUT setting `Sensitive`. Plans filtering on `!decoded.Sensitive` at hot
  tier are no-ops unless they also amend the decoder to stamp `Sensitive=true`
  for non-identity codec.
- **`eventbusEventToEventFrame` precedent**:
  `internal/grpc/query_stream_history.go:536` maps
  `eventbus.Event → *corev1.EventFrame`, NOT `corev1.Event`. Package-private,
  depends on `plugins.IdentityRegistry` + `legacyStreamName`. Plans saying
  "mirror the gRPC adapter" must extract a shared helper (new task) or write the
  full body inline — the precedent doesn't reuse cleanly when the target type is
  `corev1.Event` (different oneof field).
- **`SessionAuthGuard` / `SessionAuditEmitter` / `SessionCheckRequest` /
  `SessionDecision` / `PluginDecryptRecord`**: all defined in
  `internal/eventbus/subscriber_auth.go:14-94`. Plans wiring against these MUST
  cite this file, not authguard or audit packages (avoids import cycle).
- **Same-file type duplication regression**: when an R1 review forces a
  fakeAuth/fakeAuditEmitter rewrite, authors often APPEND the new definition
  without DELETING the old one. Same package + same file = compile error. Grep
  `rg -n "^type fake[A-Z]" <plan>.md | uniq -c` for duplicates after a
  fake-fixture rewrite.
- **Same-task Handler/Config struct redeclaration regression**: when a plan
  shows two code blocks targeting the same `handler.go` with different
  `Handler struct { ... }` shapes (one with a test hook, one without), the
  implementer can't disambiguate. R1 fixes saying "added X field for test hook"
  must DELETE the no-hook version in the same task.
- **`acquireApproval`/`emitTimeoutFinish` ResponseSender propagation**: when an
  R1 review introduces a narrow `ResponseSender` seam for `handleInternal`, the
  propagation MUST reach EVERY helper called from `handleInternal` that touches
  the stream. Plans fixing only `handleInternal`'s signature leave compile-broken
  downstream tasks. Grep `*connect.ServerStream[T]` in the plan after the seam
  introduction; only `Handle` should retain it.
- **`approval.ComputeOpArgsHash(proto.Message)`** is the real signature at
  `internal/admin/approval/oparghash.go:20` — takes `proto.Message`, returns
  `([]byte, error)`. Compatible with any proto request.
- **Contradictory plan prose**: when R1 forces real bodies into helpers
  (`buildStartedFrame`, `buildFinishedFrame`), the surrounding "Note: skeleton
  intentionally leaves these as TODO bodies" prose is a prior-draft artifact.
  Grep for "TODO bodies" / "implementer fills in" / "skeleton" AFTER reviewing
  R1-fix code blocks; remove the stale narrator.

## Sub-epic D reflexes (2026-05-09)

- **`socket.PeerCred` is NOT `{OSUser string}`.** Real shape is
  `{UID uint32, GID uint32, PID int32}`
  (`internal/admin/socket/peercred.go:15-21`). Plans inlining
  `req.PeerCred.OSUser` won't compile. The `OperatorIdentity.OSUser
  "uid=1001 (alice)"` audit string needs an explicit formatter step that exists
  in no task today.
- **`socket.WithPeerCred(ctx, cred)` does NOT exist.**
  `internal/admin/socket/peercred.go` exposes only `PeerCred` struct,
  `PeerCredFromContext(ctx) (PeerCred, bool)`, and `PeerCredMiddleware(next)`.
  Context insertion happens inside the middleware via the *unexported*
  `peerCredContextKey{}`. Plans testing capture via `socket.WithPeerCred(...)`
  invent a setter; either add an exported helper task or test-drive via
  `PeerCredMiddleware` + `httptest.NewRequest`.
- **R1-fix propagation: rename one type, propagate everywhere.** When R1 forces
  a struct shape change (`OperatorIdentity.OSUser` → `OperatorIdentity.PeerCred`),
  revisions update the definition + assignment site but leave stale references in
  (a) downstream test names and (b) inline implementer notes. Grep the entire
  plan for the old field name after a rename; a single missed reference breaks
  compile or misleads a subagent.
- **R1-fix propagation: rename a type → spec amendment required.** When a plan
  deviates from a spec-mandated field name, the §10/T23 amendments table MUST
  grow a row covering the rename + a NEGATE substring assertion in
  `TestSpecAmendmentsLandedSubEpicD`. Otherwise spec and code disagree on a
  security-relevant audit field.
- **INV-32/33/37 do NOT live in `BootstrapSubsystem.Start`.** They live in
  `kek.NewLocalAEADProvider` constructor
  (`internal/eventbus/crypto/kek/local_aead.go`) and run during EventBus
  subsystem setup. `internal/bootstrap/setup/subsystem.go::Start` does only
  policy/setting/admin seeding (5 steps). Plans/specs saying "alongside
  INV-32/33/37" pointing at BootstrapSubsystem are repo-fiction.
- **`productionSubsystems` signature is named-param, not variadic.**
  `cmd/holomush/core.go:870-884` takes 12 named `lifecycle.Subsystem` params.
  Adding a 13th means: (a) extend signature, (b) update
  `TestProductionSubsystemsIncludesCluster` (count==12 assertion), (c) update
  `TestProductionSubsystemsIncludesAdminSocket` (12 args), (d) update
  `TestSubsystemAdminSocketConstantExists` ID list, (e) add
  `TestProductionSubsystemsIncludesCryptoPolicy`. "Append to the slice"
  understates the test cascade.
- **Lifecycle SubsystemID iota gotcha.** New IDs go at END of the const block
  (after `SubsystemAdminSocket` per `internal/lifecycle/subsystem.go:29`), then
  run `task generate` to regenerate `subsystemid_string.go`. Inserting near a
  middle constant breaks the linecomment-driven stringer.
- **Type juggling between `ulid.ULID` and `string` for player IDs.** TOTP layer
  uses `ulid.ULID` value (`internal/totp/types.go:38-41`); access/store layer
  uses string ULID. Plans consuming both layers in one provider will inline
  `player.ID.String()` repeatedly — intentional, not a smell.
- **JCS canonicalizer import path is unusually deep.**
  `github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer`
  — verify before approving any plan that pins this lib.
- **TDD red phase that's only "compile error" is degenerate.** When T6's
  `EnrollmentChecker` interface holds both `IsEnrolled` AND `Verify`, production
  can wire to raw `totp.Service` directly — the AuditingService decorator
  dependency is at the HANDLER level (T15), not the provider level. Bead-chain
  edges that gate `T6 → T13` over-constrain parallel execution.
- **Repository differentiator-SELECT race.** When MarkApproved's atomic UPDATE
  returns 0 rows, a follow-up SELECT to determine WHICH predicate failed has a
  race window. Plans saying "differentiate by re-querying" need to accept the
  race or use `RETURNING` for deterministic differentiation. Also:
  `expires_at`-time-travel tests need explicit `Clock` injection AND test docs
  noting the direct-SQL `UPDATE expires_at` workaround.
- **`eventbus.Event` ≠ `eventbusv1.Event` — the publisher takes the in-memory
  type, not the proto type.** `internal/eventbus/types.go:91` defines in-memory
  `eventbus.Event` (typed `Subject Subject`, `Type Type`, `ID ulid.ULID`,
  `Payload []byte`); `pkg/proto/holomush/eventbus/v1/eventbus.pb.go:140` defines
  the wire `eventbusv1.Event` (`Id []byte`, `Subject string`, `Type string`,
  `Payload []byte`). `Publisher.Publish(ctx, eventbus.Event) error`
  (`rendering_publisher.go:58`, `bus_test.go:20`) takes ONLY the in-memory type;
  the chain marshals to the proto envelope internally on the way to NATS. Plans
  importing `eventbusv1` at any emit-site and passing `*eventbusv1.Event` to
  `Publisher.Publish` won't compile. Construct with `eventbus.NewSubject(s)`,
  `eventbus.NewType(t)`, `core.NewULID()`,
  `eventbus.Actor{Kind: ActorKindSystem, ID: ulid.ULID{}}`, `Payload: jsonBytes`.
- **R2→R3 placeholder-fill drift: filling a placeholder can introduce new
  compile bugs the placeholder hid.** When a finding says "T13 has a placeholder
  body — implement it" and the revision lands ~150 lines of inline Go, do NOT
  assume it compiles just because the placeholder is gone. Re-verify: (a) every
  imported package is what the call sites want (eventbusv1 vs eventbus is the
  canonical trap), (b) every method signature matches the repo interface, (c)
  every struct-literal type matches the receiving function's field types. Author
  hedges like "verify the actual package name" can mask "wrong type entirely".

## w9ml legacy_id-elimination reflexes (2026-05-04)

- **`core.ActorUnknown` does NOT exist.** Three constants in
  `internal/core/event.go:147-152` (`ActorCharacter`, `ActorSystem`,
  `ActorPlugin`). The wire-side equivalent is `eventbus.ActorKindUnknown`. Plans
  gating on `core.ActorUnknown` won't compile.
- **`Manifest.Runtime` does NOT exist.** Field is `Manifest.Type`
  (`internal/plugin/manifest.go:74`). Constants `plugins.TypeLua`,
  `plugins.TypeBinary`, `plugins.TypeSetting`. Binary path is
  `Manifest.BinaryPlugin.Executable` (NOT `Binary.Path`).
- **`Manager.UnloadPlugin` does NOT exist.** Unloads are inline
  `host.Unload(ctx, name)` calls inside `loadPlugin` rollback paths
  (`manager.go:777,793,822,913,924,948`). No exported Manager-level Unload —
  plans that "modify UnloadPlugin" need a prerequisite task to introduce one.
- **`coreActorToBusActor` is the wrong function name.** Real name is
  `coreToBusActor` at `cmd/holomush/sub_grpc.go:501`.
- **Test helpers `newTestPool` / `runMigrations` do NOT exist.** Each
  `internal/store/*_integration_test.go` composes its own testcontainer pool
  inline. Plans referencing these as "existing helper" need a Task X.0 building
  the harness, or must inline the testcontainer pattern from
  `migrate_integration_test.go`.
- **`internal/telnet/` does NOT render actor names.** Package contains gateway,
  limits, sanitize, refuse, metrics — no actor-name rendering.
  `rg -n 'Actor\b' internal/telnet/` returns ZERO matches. The actual display
  path today is `internal/grpc/server.go::actorIDString` (lines 596-606).
- **`task proto:gen` is the wrong task name.** Real task is `task proto`
  (`Taskfile.yaml:252-256`).
- **`cmd/holomush/*_test.go` is `package main`, NOT `package main_test`.** Verify
  with `rg -n '^package' cmd/holomush/sub_grpc_adapters_test.go`.
- **`actorIDString` zero-ULID guard is load-bearing.**
  `internal/grpc/server.go:600-606` returns `""` for `ulid.ULID{}` to avoid
  sending `"00000000000000000000000000"` to web clients. Simplifying to
  `return a.ID.String()` regresses ActorKindUnknown / system-zero-actor cases.
- **`eventbus.Actor.ID` is `ulid.ULID` value, not `[]byte` proto bytes.**
  `internal/eventbus/types.go:50-58`. The proto type uses `bytes`, the in-memory
  Go struct uses `ulid.ULID`. Plans saying `Actor.ID = parsed.Bytes()` for
  in-memory use are wrong.

## Common plan gaps in this codebase

- **Verify helper existence before approving "extend the existing test file"
  plans.** Plans frequently invent methods/helpers that don't exist on the
  target types. For every method call on an existing type (`store.X(...)`), grep
  `rg "func \(.*SceneStore\) X" plugins/`; zero hits = blocking. For every helper
  (`setupFoo`, `seedX`), grep the test dir; zero hits = invented. For every
  "extend the Ginkgo suite" instruction, verify the file actually has
  `var _ = Describe(...)` — plain `func TestX(t *testing.T)` files are NOT
  Ginkgo. Plans that hand-wave "if any are not yet defined, add small wrappers"
  for 6+ helpers are deferring design to execution. Seen on
  `2026-04-23-plugin-history-authz-plan.md`: plan called `store.CreateScene` /
  `store.JoinScene` (neither on `*SceneStore`; `JoinScene` is on
  `*SceneServiceImpl`), used `setupSceneStore` (real helper is `newTestStore`),
  and prescribed Ginkgo blocks for a plain `testing.T` file
  (`test/integration/eventbus_e2e/plugin_audit_isolation_test.go`).

- **No signature placeholders.** A plan may NOT leave interface method signatures
  or function parameter lists as `/* COPY VERBATIM FROM existing.go */` comments
  — even when the source is "right there". Search plans for `/* COPY`, `TBD`,
  `fill in`, `verify against`, `match the existing signature`; each occurrence in
  a code block is blocking. The author has the same `Read` tool — the signature
  should be inlined, not deferred to execution time. Seen on
  `2026-04-23-plugin-history-authz-plan.md`: two `/* COPY PARAMS FROM
  *SceneAuditStore.queryLog */` placeholders in Task 9 (one in the interface
  decl, one in the fake's body) — an interface whose signature is derived at
  execution time is a TODO, not an interface; if the guess is wrong every test
  in the task breaks at compile time.

- **Verify imports against code blocks.** When a plan ships a code block plus an
  "imports needed" directive, the directive must enumerate every missing/new
  import the block references (packages already imported in the target file don't
  need listing). Authors under-list imports because they write the code first
  and the import list second. For every `pkg.Symbol` qualifier, grep the target
  file's imports — confirm the package is imported or listed. Cross-reference
  both directions: imports listed but unused (dead) and uses without imports
  (compile error). Seen: a new `QueryHistory` body used `slog.InfoContext(...)`
  with no `import "log/slog"` in the directive (file had no slog import); and a
  `fakeAuditStore.queryLog` param list used `*timestamppb.Timestamp` with
  `timestamppb` absent from the test file's import block.

- **False-starts left in executable text.** Watch for "Wait — that command is
  wrong, use this instead:" patterns inside step bodies. Subagents skim and may
  run the first block they see. Flag any plan that emits two contradictory
  commands in the same step.

- **Long-running steps without a budget annotation.** `task pr-prep` runs 5–15
  min; a per-task subagent dispatch with default timeout will starve. Plans
  should annotate long-running verifications and recommend `run_in_background` +
  Monitor.

- **"Manual-ish" steps in subagent-driven plans.** Multi-terminal manual checks
  can't be executed by an automated agent. Either script them inline or label
  them `(MANUAL — pre-merge only)`.

- **Stale prose around revised code blocks.** When a previous review forces the
  author to simplify a code block, the surrounding "Notes"/explanation prose
  often still describes the OLD construct. On revision passes, diff the prose
  against the current code, not just the code against itself. (R2 example: Task 7
  step 2's source line was simplified from a two-fallback construct to one line,
  but the post-code Notes still described "two fallbacks because…".)

- **Cross-task instruction drift after partial-mirror fixes.** When ONE task
  adopts a per-task commit-cadence pattern and a later task deliberately does
  NOT (last-phase exception), prose like "Mirror this pattern at Tasks N, M, K"
  must enumerate exceptions explicitly. (R2 example: Task 6 step 4 said "Mirror
  at Tasks 9, 13, and 16" but Task 16 step 3 explicitly forbids the mirror.
  Per-task subagent execution hides this; an inline-execution agent might
  propagate the wrong pattern to Task 16.)

- **Running test-count drift after a per-task split.** When a revision splits one
  bats/test task into N tests, the author updates the FINAL tally and the
  per-task breakdown but forgets the intermediate "Expected: N tests" lines in
  subsequent tasks. Grep `rg -n "Expected: [0-9]+ tests"` and recompute the
  cumulative sum from per-task contributions; flag any mismatch. (pr-prep-lock
  R2: Task 8 split 1→3; Task 12 step 2 correctly updated to "12 tests" but Tasks
  9/10/11 step 2 still said 7/8/9 instead of 9/10/11.)

## Decomposition patterns that work here

- **Helper extraction for shell-script reuse**: when two consumers (Taskfile cmd
  + hook) need the same path-discovery logic, ship one `scripts/<helper>.sh`
  sourced by both. Spec called this out as an "Implementation note"; plan
  implemented it. Pattern works cleanly in the HoloMUSH layout.
- **Repo-reality verification of cited line numbers**: plans citing
  `file:line-line` ranges should be grep-verified before review approval. The
  2026-04-25 plan cited `Taskfile.yaml:515-551`, `CLAUDE.md` 552/570/849 — all
  matched current `main`. When citations are accurate, reviewers can move fast.

## EventType migration reflexes

- **Two parallel EventType constant sources**: `internal/core/event.go` AND
  `pkg/plugin/event.go` (`pluginsdk.EventType*`) both declare bare event-type
  strings (`"say"`, `"pose"`, `"arrive"`, …). Any migration plan MUST list both
  files in its File Structure table and grep both namespaces in its call-site
  survey. The SDK side has ~100 references across pkg/holo and plugin tests.
- **Lua emit syntax is table-literal, not function-call**: HoloMUSH Lua plugins
  emit via `{stream = ..., type = "say", payload = ...}` return tables, not
  `emit_event(...)` calls. Plans that grep `emit_event\(.*"(say|...)"` produce
  zero matches and silently no-op. The matching pattern is `type = "(say|...)"`.
- **`core-scenes` ops events ≠ stream event types**: the scene plugin uses
  `OpsEventKind` constants (`membership.invite`, `lifecycle.created`) for
  plugin-owned audit, and only emits `pluginsdk.EventTypeSystem` on streams.
  Plans declaring `scene_create`, `scene_ic`, etc. in `crypto.emits` fabricate
  events the plugin doesn't emit.
- **No `cmd/holomush/cmd_plugin.go`**: the holomush CLI has no `plugin` cobra
  subcommand group. Plans adding `plugin events` / `plugin validate` MUST include
  an explicit task to introduce the parent group + wire it into root.go.
- **`Taskfile.yaml` not `Taskfile.yml`**: a recurring plan-author error. Both
  work via auto-detect, but file-mention claims must match disk.
- **Manager has no `loadManifest` extraction point**:
  `internal/plugin/manager.go` inlines `ParseManifest` inside `Discover`
  (line 349). Plans pretending a `Manager.loadManifest(raw []byte)` exists need a
  refactor task before the new validator hook can be inserted.

## Phase 3a crypto reflexes

- **`internal/eventbus/publisher.go` exposes `PublishOption`, NOT
  `PublisherOption`.** New `With*` options must use the existing type name
  (`publisher.go:67`).
- **No `EventsAuditRow` struct exists.** The audit write path is inline raw SQL
  in `internal/eventbus/audit/projection.go:240-260`. Plans claiming to modify
  `internal/store/events_audit_repo.go` fabricate that file.
- **`Manifest.Crypto` is `*CryptoSection` (pointer).** Fixtures using
  `Crypto: plugins.CryptoSection{...}` value form won't compile. Helpers
  iterating `manifest.Crypto.Emits` must guard `manifest.Crypto == nil`
  (`manifest.go:107`).
- **`ActorKindPlugin` lives in `internal/eventbus/types.go:46` (package
  `eventbus`), NOT in `internal/plugin/` (package `plugins`).** The `plugins`
  package has no `Actor`/`ActorKind`/`ActorKindPlugin`. `ActorResolver` returns
  `core.Actor` (`internal/plugin/event_emitter.go:26`); `core.Actor.ID` is a
  `string`, not a `ulid.ULID`.
- **`Manifest.ActorKindsClaimable` is `[]string`, not `[]ActorKind`**
  (`manifest.go:84`). Use canonical strings (`"plugin"`, etc.) per
  `validateActorKindsClaimable` normalization.
- **Audit projection test file is `//go:build integration`-only.** Plans
  appending unit-style tests to
  `internal/eventbus/audit/projection_test.go` silently disable themselves under
  `task test`. The sibling `projection_unit_test.go` is the unit-test target.

## Pass-revision drift reflexes

- **Modify-without-Create artifacts after a structural rewrite.** When a pass-1
  finding forces deleting/rewriting an early task (e.g., remove a smoke-test file
  from the bootstrap task), downstream tasks' Modify lists often still reference
  the deleted artifact. Grep `rg -n 'plugin_test\.go|<deleted-symbol>' <plan>`
  and confirm every Modify line targets a real file or one created by an earlier
  task. (Pass-2 example: 2026-05-01 plan rewrote Task 5 to drop `plugin_test.go`
  but Tasks 10/11/13/19 still listed `gorules/plugin_test.go` as Modify.)
- **Partial application of an NB-fix across siblings.** When pass-1 returns a
  multi-target fix-up (NB#2: "fix line numbers AND verbatim Before blocks in
  three doc-comment edits"), the author often applies it to the first one or two
  and stops. Verify each instance independently. (Pass-2 example: NB#2 cited
  `material.go` and `api_test.go`; both `material.go` Before blocks were
  corrected but the `api_test.go` Before block was NOT — its first line only
  exists as the END of a longer line in the actual file, so the Edit tool's
  exact-match contract fails.)

## golangci-lint module-plugin reflexes

- **One `register.Plugin(name, …)` call = ONE enableable linter ID.**
  golangci-lint v2 wraps all analyzers from a single plugin registration into
  one `goanalysis.Linter` whose `Name()` is the registered plugin name. To
  expose N analyzers as N independent `linters.enable` entries, the module MUST
  contain N `register.Plugin(...)` calls in `init()` — one per analyzer (see
  `github.com/albertocavalcante/go-analyzers-gcl/plugin.go`). Plans using
  `register.Plugin("X", New)` returning N analyzers AND `linters.enable:
  [- a, - b, - c]` with N names fail with "unknown linter" at the first lint
  run. The fix touches the plugin scaffolding, the `linters.settings.custom` map
  (one entry per registered plugin name), and `linters.enable` (one entry per
  registered plugin name). govet is the only stdlib linter supporting
  per-analyzer enable/disable inside one linter ID, via
  `linters.settings.govet.{enable,disable}` — special-cased, not a general
  module-plugin pattern.
- **`linters.exclusions.rules` scopes are by linter ID, not analyzer name.** With
  a single-plugin shape, an exclusion for `_test.go` against
  `linters: [holomushrules]` disables ALL bundled analyzers in test files, not
  just one. Per-analyzer test-file scoping requires either per-analyzer plugins
  OR the filename check inside the analyzer's `run` via
  `pass.Fset.Position(file.Pos()).Filename`.

## Review reflexes

- For every `path:line` citation in the plan, run a quick `Read` or `rg` to
  verify the line range still covers what the plan claims. Drift across PRs is
  real — the spec under review used `:96-102` for a block, the plan used
  `:95-102` for the same block. Both can be off after the next merge.
- Plans that put a `task pr-prep` gate at the END but never run `task lint`
  per-task accumulate lint debt across N commits. Per project rule (MUST run
  `task lint` before committing), each commit checkpoint should be lint-clean.
  Flag as non-blocking but real.
- For Ginkgo vs `testing.T`: never assume a `_test.go` file is BDD-style. Verify
  with `rg "var _ = Describe" path/to/file`. Plain `func TestX(t *testing.T)` is
  the default in this repo's `eventbus_e2e` integration tree.
- For `task test:int`: integration tests live in mixed locations. Plugin store
  integration tests are at `./plugins/<name>/` with `//go:build integration`,
  NOT at `./test/integration/plugin/...`. Cross-reference `Taskfile.yaml:test:int`
  for the canonical package list before approving a path.
- `//nolint:unparam` does NOT suppress revive's `unused-parameter` rule. Both
  `unparam` (`.golangci.yaml:31`) and revive rule `unused-parameter`
  (`.golangci.yaml:130`) flag unused params; nolint suppresses by linter name,
  not by individual revive rule. Plans introducing a temporary unused-parameter
  state for staged refactors MUST suppress both: `//nolint:unparam,revive // …`.
- `task test:int` does NOT accept `--` package args — its package list is
  hard-coded in `Taskfile.yaml:93-111`. Plans saying `task test:int --
  ./test/integration/foo/...` are wrong; `task test:int:focus` is the only
  narrowed variant and it's pinned to `./test/integration/plugin`.
- **Mocking style varies by package**: `internal/web/` uses hand-rolled struct
  mocks (`mockCoreClient` in `internal/web/handler_test.go:36`); `internal/grpc/`
  uses mockery `EXPECT()` for repos like
  `authmocks.NewMockPlayerSessionRepository(t)`. Verify with
  `rg -n 'EXPECT\(\)|type mock.*struct' <package>/` before accepting test
  snippets.
- **Playwright E2E lives at `web/e2e/`**, not `test/e2e/playwright/`.
  `task test:e2e` runs `npx playwright test` from the docker compose
  `playwright` service reading config from `web/`. Existing specs:
  `landing.spec.ts`, `auth.spec.ts`, `terminal.spec.ts`,
  `session-security.spec.ts`, `character-switcher.spec.ts`, `scenes.spec.ts`,
  `admin.spec.ts`.
- **`web/package.json` does NOT include `@testing-library/svelte`** — only
  Vitest + Playwright. Plans importing `@testing-library/svelte` in
  component-test code introduce an undocumented dependency.
- **`oops.Code()` returns `any`**: comparisons like `code == "FOO"` work but the
  canonical repo pattern is
  `code, ok := oopsErr.Code().(string); if !ok { return false }` (see
  `test/integration/access/evaluation_test.go:92`). Bare `==` against an `any`
  should cite the existing pattern or be flagged.
- **Tautological TDD red phase via parallel test-fixture types.** When a plan's
  "red" test asserts behavior X by calling `testFooBar.method()` (a fake of type
  `testFooBar`) instead of the real production type `*FooBar`, AND the fake's
  method implements X, the red phase produces a false green before any production
  change. Verify the test calls into the production type. The
  `2026-04-25-plugin-actor-claim-authentication.md` Task 1 had
  `testSubscriber.deliverAsync` (fake) instead of constructing `*plugins.Subscriber`
  via `NewSubscriber(host, emitter)` and calling its package-private
  `deliverAsync` — both legal from `package plugins`; the plan chose wrong.
- **Constructor return-type mismatch.** Some "constructors" return factory
  functions, not struct pointers.
  `internal/plugin/goplugin/host_service.go:28`'s `newPluginHostServiceServer`
  returns `func([]grpc.ServerOption) *grpc.Server`, not
  `*pluginHostServiceServer`. Read the constructor signature before approving any
  plan that invokes it.
- **`yq` is NOT installed in HoloMUSH CI.** `rg -n 'yq' .github/workflows/`
  returns zero hits, no `setup-yq` action, no `apt-get install yq`. Plans
  shipping `yq`-based lint scripts silently fail in `task pr-prep` on CI. Prefer
  a small Go program in `cmd/lint-<name>/` using the project's existing yaml
  parser.
- **Type and field names: verify before approving.** Descriptive type names like
  `CommandEntry` (not `Entry`) and unexported struct fields exposed via methods
  (`(e *CommandEntry).PluginName()` returns `e.pluginName`, NOT a public
  `PluginNameStr`). Plans fabricating exported fields like
  `command.Entry{PluginNameStr: "..."}` won't compile. `rg -n 'type Entry struct|
  type CommandEntry struct' internal/command/types.go` first.
- **Existing test-helper inventory in `internal/command/dispatcher_test.go`:**
  `newTestDispatcherWithPlugin(t, deliverer)` and `newTestCommandExecution(t)`
  already exist (lines 2063, 2104). Plans inventing
  `newTestDispatcher`/`newTestExecutor` are wrong; redirect to these.
- **Existing event_emitter test pattern is `eventbustest.Embedded` +
  `newEmitter(t, bus, lookup, resolve)` + `pluginActorResolver`** (per
  `internal/plugin/event_emitter_test.go:35,41`). Plans introducing
  `mocks.NewMockPublisher(t)` need to (a) add a mockery config or (b) be
  redirected to the existing eventbustest pattern. `internal/plugin/mocks/` has
  `MockEventEmitter`, `MockHost`, `MockManagerOption` — but NO `MockPublisher`.
- **Missing integration harness ≠ "adapt the existing eventbus_e2e patterns".**
  "If no harness exists, build a minimal one based on…" is a deferred design
  problem, not a plan. Either require a "Task X.0 — build the harness" with
  concrete file paths and method bodies, OR use an existing test seam (e.g.,
  `internal/plugin/integration_test.go` already runs real subprocess plugins).
- **Plausible-looking but nonexistent enum constants in test fixtures.** The
  `2026-04-25-plugin-actor-claim-authentication.md` Task 2 used
  `pluginsdk.StatusOK`, but the real constant is `pluginsdk.CommandOK`
  (`pkg/plugin/command.go:13`). Same trap for any `Status*`/`Code*`/`OK*` family
  — verify by grepping `pkg/plugin/*.go` before approving. A TDD red that fails at
  compile-time is a defect, not a TDD red.
- **go-task per-`cmd:` `vars:` is silently ignored.** `vars:` is supported at
  task-level (sibling of `cmds:`) and under `task: <subtask>`, but NOT as a
  sibling of `cmd:` inside a list item — the block is silently dropped and the
  variable resolves to empty or a built-in shadow (`{{.TASKFILE}}` is a go-task
  built-in = abs path of the loaded Taskfile). Verify by reading; run a 5-line
  minimal Taskfile to confirm (version drift is real).
- **Skip-stub `@test` declarations defeat invariant enforcement.** When a plan
  consolidates multiple invariants into one test body and adds skip-only
  `@test "<name>"` aliases just to satisfy a name-grepping meta-test, the
  meta-test loses drift-detection power. Skip-aliases ARE detectable (a `bats`
  body containing only `skip "..."`). If a spec assigns invariant I-N to test X
  and the plan implements X as a skip-stub, that's a blocking gap — rename in
  spec (jointly enforced) or split tests to give each invariant real assertions.
- **`task fmt -- <file>` does NOT scope to the file** in HoloMUSH's Taskfile
  (sub-tasks `fmt:go`/`fmt:yaml`/`fmt:markdown`/`fmt:dprint` don't consume
  `CLI_ARGS`). Plans citing this as targeted formatting are wrong; it's a no-op
  arg and `fmt` formats everything. Non-blocking but flag.
- **`task proto:gen` / `task proto:lint` do NOT exist.** Real names: `task proto`
  (Taskfile.yaml:257) and `task lint:proto` (Taskfile.yaml:385). Same pattern:
  `task test:cover -- ./pkg/...` silently ignores the `--` arg because
  `test:cover` (line 87) does not interpolate `{{.CLI_ARGS}}` — it always runs
  whole-repo coverage.
- **`http.Server.ConnContext` + `PeerCredMiddleware` integration is NOT covered
  by direct `readPeerCred` tests alone.** AC text saying "PeerCredFromContext
  returns populated cred" is met by manually putting a value into context, but
  the wiring (`ConnContext: storeUnixConn` → middleware reads UnixConn →
  `readPeerCred` → context store) needs an end-to-end UDS-dial test or a
  regression goes uncaught. Flag NB when a plan tests the syscall layer and the
  context helper in isolation but never together.
- **CI does not run `task pr-prep` in HoloMUSH.** `.github/workflows/ci.yaml`
  invokes the underlying tasks directly (`task lint`, `task test:cover`,
  `task test:int`, `task test:e2e:cover`). Plans claiming "CI runs task pr-prep"
  are wrong about CI's invocation pattern.
- **`NewManager` requires `WithVerbRegistry` (INV-GW-10).**
  `internal/plugin/manager.go:181-201` returns `ErrMissingVerbRegistry` if
  `m.verbRegistry == nil`. Plans introducing a `newManagerForXxxTest(t, opts...)`
  helper passing only `WithPluginRepo`/`WithLuaHost` WILL fail at the
  `require.NoError(t, err)` line. Require `WithVerbRegistry(core.NewVerbRegistry())`
  (or a test stub). Verify with
  `rg -n "ErrMissingVerbRegistry|verbRegistry == nil" internal/plugin/manager.go`.
- **`Manager.TestLoadPlugin` silently no-ops without a registered host.**
  `manager.go:982-1001` checks `m.hosts[manifest.Type]` and falls back to
  `m.luaHost` for `TypeLua`. If neither is configured, it inserts into `m.loaded`
  but NOT `m.pluginHosts`. Plans testing cache mutation after `TestLoadPlugin`
  must register a host (via `WithLuaHost(noOpHost)`) or accept
  `pluginHosts[name]` is empty post-call.
- **Cache-mutation idempotency separate from host lifecycle.** When introducing a
  new `Manager.UnloadPlugin`, do NOT gate cache cleanup behind a host-loaded
  check. Move `delete(m.activeByName, name)` BEFORE any `if !loaded { return nil }`
  early-return so cleanup is idempotent regardless of host state.
- **Bare `error` return + `defer { if err != nil ... }` doesn't work.**
  Functions like `loadPlugin` returning bare `error` (not named `(err error)`)
  do NOT propagate the final return value to deferred closures. Rollback-defer
  plans MUST either (a) refactor to a named return, or (b) use an explicit
  `var rollback bool; ...; defer func() { if rollback { ... } }()`. "Verify the
  signature; if not, use a different defer pattern" defers design — block.
- **Deferred E2E test-harness design.** Plans introducing `setupTestEnv()`,
  `registerPluginInRegistry()`, `emitFromBinaryPlugin()` for an E2E task then
  saying "Copy the env scaffolding… names verified at impl time" are the
  missing-harness antipattern in disguise. If a real harness exists (e.g.,
  `test/integration/eventbus_e2e/`), name it and reference its types; if not, add
  a Task X.0 with concrete struct definitions and method bodies.
- **Repo-grounding tables at the top of revised plans are highly effective.**
  When Revision 2 starts with "Repo grounding (verified before this plan was
  written)" enumerating real-vs-fabricated symbols with citations, R2 review can
  move quickly through surface checks and focus on second-order issues.
  Recommend this pattern for any plan that survived a Revision 1 NOT READY with
  fabricated-symbol findings.

## ADR-capture / shell-hook reflexes (2026-05-14)

- **`bd create` stdout format**: `✓ Created issue: holomush-XXXX — <title>`
  (unicode bullet + em-dash; verified live 2026-05-14). Regex
  `r"Created issue: (\S+)"` works; ID is the first non-space sequence. Plans
  should self-test the regex at module import.
- **`bd dep add <from> <to> --type <reltype>`** is the correct positional form
  (verified `bd dep add --help`). `--type supersedes` works. `bd close -r/--reason
  "..."` and `bd delete -f` are both valid.
- **Bash `$(...)` strips trailing newlines.** A hook computing SHA via
  `stripped="$(awk ... <file>)"; printf '%s' "$stripped" | sha256sum` hashes
  content WITHOUT the final `\n`. If a paired writer computes SHA over
  `normalized_content` ending with `\n`, the two values cannot agree. Plans
  claiming "the hook and the writer compute the same SHA" must nail down whether
  input bytes include the trailing newline, AND the test harness must exercise
  the writer SHA via a different code path than the hook SHA — a `spec_sha()`
  helper reusing the hook's bash pipeline produces fake agreement that breaks in
  production. (Reproduced: hook-side `683376e290829b48` vs file-content-side
  `2751a3a2f303ad21`.)
- **Existing-repo `**Status:** Superseded by …` format heterogeneity.**
  HoloMUSH's `0007-command-security-model.md` uses `**Status:** Superseded by
  [ADR 0014](0014-...md)` (SPACE between `ADR` and digits, MARKDOWN-LINK form);
  the README index uses `Superseded by 0014` (no `ADR` prefix). A regex
  `r"Superseded by ADR-?(\d+)"` matches NEITHER. Use a liberal alternation like
  `r"Superseded by\s+(?:\[)?ADR[\s-]?0*(\d+)"`, and always verify supersession
  regexes against the real repo files first.
- **Taskfile `lint:` task uses `cmds: [- task: lint:xxx, ...]`, NOT `deps:`.**
  Plans saying "add `lint:adr` to `lint`'s `deps:` list" cannot be applied — the
  field doesn't exist on this task. Correct edit: append `- task: lint:adr` to
  the `cmds:` list. Grep the current Taskfile and show the BEFORE/AFTER block.
- **Dogfood-step file-count conflict with strict-equality invariants.** When a
  plan ends with "Task N: run the skill on the spec; captured ADRs MAY be 0–5"
  AND a doctor asserts "exactly 17 real ADR files + exactly 17 stubs + exactly 1
  README = exactly 35 total", accepting any candidate breaks the doctor. Plans
  MUST either loosen the invariant to `>= 17` / `>= 35`, or instruct the dogfood
  step to skip every candidate (weakening the proof). Watch for this when a
  strict-equality count is tagged `doctor` AND a dogfood step is in the same plan.
- **`bd dolt commit` before `verify_post_migration` is a recovery hole.** When a
  migration script commits to dolt BEFORE running its inline asserts, an assert
  failure followed by discarding the working-tree changes (`git reset --hard` /
  `git restore`) rolls back the working copy but the bd records are already in
  dolt. Plans claiming "single change; recovery is discard + rerun" must defer
  ALL state-changing operations (including `bd dolt commit`) until AFTER the
  verify, or the recovery is incomplete and leaves orphaned bd records.
