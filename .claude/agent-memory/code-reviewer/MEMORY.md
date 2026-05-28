# code-reviewer agent memory

This file accumulates HoloMUSH-specific anti-patterns, subtle invariants, and
recurring blind spots discovered during adversarial code review. Entries are
added by the agent itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

## Anti-patterns

- **ast-grep (`sg` 0.42.3) CANNOT match package-qualified Go CALL patterns.**
  `sg -p 'slog.Info($$$ARGS)' -l go` (and `slog.InfoContext($CTX, $$$ARGS)`,
  `fmt.Println(...)`, any `pkg.Fn(...)`) parses at statement top-level as a
  `type_conversion_expression` with an `ERROR` node, NOT a `call_expression`, so
  it matches ZERO real call sites — confirm with `--debug-query=ast`. Known open
  upstream limitation (ast-grep/ast-grep #646, discussion #2220). COMPOSITE
  LITERALS are fine: `sg -p 'core.Event{$$$FIELDS}' -l go` parses to
  `composite_literal` and matches correctly. Trap: a doc/audit that uses a broken
  call pattern and reads "zero matches confirms the invariant holds" is FALSE
  reassurance — it returns zero even in a codebase full of the violation. Flag any
  ast-grep qualified-call example as non-functional. Encountered: search-tooling
  review of `.claude/rules/search-tools.md:34-44` (2026-05-25).

- **sloglint `context:scope` (v0.11.1, golangci-lint v2.11.4) does NOT flag bare
  `slog.X` inside a closure** even when the enclosing FuncDecl has a `ctx`
  first param. Standalone `sloglint@v0.11.1 -context-only=scope` DOES flag that
  shape (`isContextInScope` walks FuncDecl only, finds enclosing ctx → reports),
  but the golangci-lint-integrated path returns 0 issues — an integration quirk.
  Consequence for sloglint Tier-C migration beads (holomush-ow4ix.*): bare calls
  inside closures are NOT in the "findings" set the orchestrator migrates, so
  sibling calls in the same function can legitimately diverge (one bare, others
  `*Context`). Live example: `internal/world/events.go:79` (`slog.Debug` in the
  `retry.Do` closure) stays bare while lines 93/102 in the same `emitWithRetry`
  became `*Context`. Not a regression (predates the change); flag as non-blocking
  consistency note, not a blocker. Encountered: holomush-ow4ix.12 (2026-05-24).

- **G706 (log-injection) is excluded BY CONFIG for most internal pkgs, NOT all.**
  `.golangci.yaml:117-123` excludes gosec G706 for a path regex covering
  `internal/(access|command|core|grpc|logging|observability|plugin|store|telnet|tls|web|world|xdg)|cmd|pkg`.
  `internal/bootstrap` and `internal/lifecycle` are NOT in that list, so those
  pkgs need inline `//nolint:gosec // G706` directives. gosec slog G706 sinks are
  `Info/Warn/Error/Debug` with `CheckArgs:[0]` (message only — attribute KV pairs
  are auto-escaped, never flagged; per securego/gosec PR #1623). When a bare
  `slog.Info(...) //nolint:gosec // G706` migrates to `slog.InfoContext(ctx, ...)`,
  dropping the directive is correct IF the message arg is a static literal (it
  always is under `static-msg:true`) — and nolintlint `require-explanation`+
  `require-specific` would FAIL on the now-unused directive if kept. Verify by
  running full-config `custom-gcl run ./<pkg>/...` → 0 issues. Encountered:
  holomush-ow4ix.12 admin.go:111 (2026-05-24).

- **`AGENTS.md` is now a relative symlink → `CLAUDE.md`** (verified 2026-05-24,
  sloglint worktree: `readlink AGENTS.md` = `CLAUDE.md`). `task lint:docs-symmetry`
  enforces the symlink integrity (holomush-f7t2). Any CLAUDE.md edit propagates to
  AGENTS.md automatically — NO manual mirror needed, and no symmetry-divergence
  finding is possible. (HISTORICAL: an earlier era kept them as byte-identical
  twin files; that convention is superseded by the symlink. Do NOT flag a missing
  AGENTS.md edit when CLAUDE.md changes — confirm the symlink first with
  `readlink AGENTS.md`.)

- **`task test:int -- -run X` does NOT filter to test X.** The Taskfile's `--`
  pass-through composes args into the gotestsum command but does not isolate the
  package or limit the run to matching tests. A run that "passes" against a
  named test may simply be the entire integration suite running with no test
  matching that name (and -run defaulting to "match nothing" silently). Symptom:
  `DONE 783 tests, 4 skipped` for a command that should hit only one test file.
  When verifying a named test exists and passes:
  - First confirm presence: `rg "TestNameHere" --type go` → must return ≥1 hit.
  - Then run `task test:int` and grep gotestsum's per-test output for the name,
    OR use a per-package `-count` discrepancy check.
  - Do NOT accept suite-wide green as proof a specific test exists.
  Encountered: T7 review (2026-05-03), where the prompt asserted a spec-required
  test "already passed" but the test did not exist in the codebase — the green
  came from the rest of the suite running.

- **Incomplete pattern-fix scope**: when a fix replaces an anti-pattern at
  N "identified" sites, always re-grep the WHOLE pattern (not just the
  spelling of the call the implementer happened to find) across the repo
  before approving. For testcontainers postgres: the canonical search is
  `rg "wait\\.ForLog|testcontainers\\.WithWaitStrategy" --type go`. Only
  `test/testutil/postgres.go` should remain on `wait.ForLog` (correctly
  paired with `ForListeningPort`); every other site should use
  `postgres.BasicWaitStrategies()`. holomush-bmcq fix touched 3 of 5
  affected sites; `internal/eventbus/crypto/dek/manager_integration_test.go`
  and `internal/eventbus/crypto/kek/none_integration_test.go` were missed.

- **Stale-base diff illusion**: When reviewing a stack pre-push, always check
  `jj log -r 'main@origin'` head against the branch's fork point. A bare
  `jj diff main@origin..@` will conflate the branch's actual changes with
  upstream-only changes that landed since the branch was forked, producing
  a noisy diff that may suggest the PR touches unrelated files. Use
  `jj diff -r 'fork_point(main@origin | @)..@'` to see the true PR scope.
  Always confirm the branch is current before claiming a "small PR" — the
  rebase gap itself is often a blocking finding ("rebase before push").

- **Wrapped-status `FromError` rewrites Status.Message**: grpc-go's
  `status.FromError` walks the error chain via `errors.As`. When err is
  `oops.Wrap(status.Error(...))`, FromError reaches the inner status BUT
  rewrites its Message to `err.Error()` (the outer chain's stringification).
  This means `st.Message()` and `st.Err().Error()` are NOT necessarily the
  plugin's original message; both can include outer wrapper text. If
  client-visible message purity matters, the inner status must be unwrapped
  explicitly (e.g., via `oops.Unwrap` chain walk) before re-emitting.
  Documented as out-of-scope at `internal/grpc/query_stream_history.go:295`.

- **Comment-only proto reservations.** `// field N is reserved (was X)` without
  an actual `reserved N;` declaration does NOT prevent reuse — proto3 permits
  re-use of any field number that isn't in a real `reserved` block. Search
  pattern: `rg "field [0-9]+ is reserved|reserved [0-9]+;" api/proto/`.
  Project convention uses real `reserved N;` (see
  `api/proto/holomush/core/v1/core.proto:109`,
  `api/proto/holomush/web/v1/web.proto:94`). Pre-existing wart introduced by
  PR #179 (cookie cutover, commit f5473248e) in
  `WebAuthenticatePlayerResponse` and `WebCreatePlayerResponse`.

- **Stale TS regen across stacked proto commits.** When a stack of commits
  edits proto files, `task proto` may not be run by every commit's author,
  leaving the per-language generated bindings out of sync with each other.
  When reviewing a stacked-PR proto change, check whether earlier commits in
  the stack regenerated all generated artifacts (Go, TS, etc.) or only some.

- **`errors.Is` against `oops.Code(...).Errorf(...)` sentinels is tautological.**
  `samber/oops@v1.21.0` `OopsError.Is(err)` returns `true` for ANY `OopsError`
  target (`oops@v1.21.0/error.go:87-93`), so `errors.Is(anyOopsErr, anySentinel)`
  passes regardless of code. Tests asserting "function returned ErrFoo" with
  `errors.Is` against an oops sentinel pass even if the function returns a
  different oops error. Use `errutil.AssertErrorCode(t, err, "EXPECTED_CODE")`
  or compare `oops.AsOops(err).Code()` directly. As of 2026-05-03 the only
  package-level oops sentinels in the codebase are `internal/cluster/probe_pill.go:23-36`,
  `internal/auth/hasher.go:39`, `internal/plugin/manager.go:61` — first uses
  with `errors.Is` are in `internal/cluster/probe_pill_test.go`.

- **Lua plugin "full plumbing chain" failure mode.** When adding a new
  field to `pluginsdk.EmitEvent` (e.g., `Sensitive`) and exposing it via
  Lua, four sites must be updated, not three: (1) the Go-side
  `holo.Emitter` setter — easy; (2) the Lua hostfunc that writes to the
  emitter (`stdlib.go:emitLocation/Character/Global`) — easy; (3)
  `stdlib.go:emitFlush` which marshals the buffered events to a Lua
  table — **easy to miss** because all unit tests of (1)+(2) read
  `emitter.Flush()` directly on the Go side and never round-trip
  through Lua; (4) `internal/plugin/lua/host.go:parseEmitEvents` which
  parses the Lua-returned table into `pluginsdk.EmitEvent` — same blind
  spot. The canonical Lua flow is `holo.emit.X(); return holo.emit.flush()`,
  which routes through (3) → Lua return → (4). A field set in (1) and
  exercised in unit tests via direct `emitter.Flush()` will silently
  drop on the canonical path. Always require an end-to-end test that
  drives a Lua snippet through `Host.DeliverEvent` and asserts the
  field survives. Encountered: Phase 3d Task 9 review (2026-05-04).

- **Default-flip without was-set guard silently overrides operator config.**
  When changing a koanf-backed bool default from `false` to `true` in a
  `Defaults()` method, an unconditional `c.X = true` clobbers explicit
  `false` from the operator's YAML — Go's zero-value can't distinguish
  "unset" from "false." Other fields in the same `Defaults()` typically
  use `if c.X == zero { c.X = default }` and are safe by construction.
  The fix is `*bool` + nil-check, koanf "Was Set" tracking, or moving
  the default to the construction site of the embedded struct (zero-value
  becomes the default). Always require a regression test: explicit
  `false` survives `Defaults()`. Encountered: `internal/eventbus/config.go:85`
  in Phase 3d (`Crypto.Enabled` flip).

- **Shared-helper TDD coverage gap.** When an autofix swaps multiple call
  sites to a new shared helper, check all `testdata/` dirs for per-analyzer
  bypass test cases — not just the one the implementer touched. Acceptable
  when the helper is end-to-end covered elsewhere; flag as non-blocking gap.

- **Flat CLI flag can't reach a NESTED koanf struct.** `config.Load`
  (`internal/config/config.go:254-258`) maps every pflag to
  `section + "." + ReplaceAll(f.Name, "-", "_")` — hyphens→underscores, NO
  dots. So flag `--log-stderr` becomes key `<section>.log_stderr`, which can
  ONLY bind a flat field. A nested struct like `LoggingConfig{Stderr
  LoggingSink koanf:"stderr"{Enabled koanf:"enabled"}}` needs key
  `logging.stderr.enabled` and is UNREACHABLE from any flag name (you can't
  emit a dot). All existing working flags (`log-format`→`core.log_format`) are
  flat fields — that's why the pattern looked fine. YAML binding to the nested
  struct works (koanf parses YAML nesting natively), so the bug is invisible if
  you only test YAML. Detection: any new CLI flag targeting a nested koanf
  struct is dead unless bound explicitly (`cmd.Flags().GetX` + assign in RunE)
  or via a custom name→key table in the posflag callback. A `--help`-text test
  (`TestCoreCommand_LogSinkFlags`) does NOT catch this — require a parse-then-
  assert-config test. Encountered: otel-logs-zgtqo (2026-05-23), all 6 `--log-*`
  flags inert.

- **Per-sink-floor gate hardwired to global level drops downward overrides.**
  When a fanout/bridge handler is wrapped in a coarse `levelGate(globalLevel,
  bridge)` and the real per-sink filtering happens DOWNSTREAM (inside a
  LoggerProvider's per-sink `Processor`), any per-sink level set BELOW the
  global level is unreachable — the coarse gate drops the record before the
  fine filter can pass it. The gate floor must be `min(enabled sink levels)`,
  not the global level. Symptom: per-sink "raise the floor" works but "lower
  the floor below global" silently does nothing. Encountered: otel-logs-zgtqo
  bridge `bridgeLevel=level` at `cmd/holomush/core.go:215` vs spec diagram
  `floor: min(enabled OTel sinks)` (INV-L4 downward).

- **`packages.Load` p.Imports is DIRECT-only.** An import-guard test iterating
  `for imp := range p.Imports` (even with `NeedDeps`) sees only the package's
  DIRECT imports, not transitive ones — `NeedDeps` populates each child
  `*Package`'s own `.Imports` but the test must recurse to walk them. A guard
  claiming to catch "transitive" imports of a forbidden package while only
  ranging `p.Imports` is direct-only. Fine for catching direct SDK use (the
  usual threat); flag the comment if it overclaims. Encountered: INV-L1 guard
  in `internal/logging/import_guard_test.go` (2026-05-23).

- **Command-style Lua plugins MUST be driven via `DeliverCommand`, not `DeliverEvent`.**
  `on_command` returns plain string (→ `CommandOK`) or `{status, output, events}`
  table; `DeliverCommand` (`internal/plugin/lua/host.go:310`) routes through
  `parseCommandResponse` (host.go:656-695) which parses that shape. `DeliverEvent`
  feeds the wrapper to `parseEmitEvents` (wrong path) → nil emits. A test driving a
  command plugin via `DeliverEvent` and asserting `[]EmitEvent` is testing the wrong
  entry point. SDK status: `CommandOK=0, CommandError=1, CommandFailure=2`
  (`pkg/plugin/command.go:13-19`). Lua `{status=2}` → fail-closed (e.g. core-help
  logs the engine error + returns "temporarily unavailable", main.lua:24-28/110-114
  — genuine fail-closed, not error-swallow). Encountered: holomush-gria3 (2026-05-25).

- **Subscriber emit path is NOT the capability gate; `dispatch` filters on qualified
  event type.** `internal/plugin/subscriber.go:88-102` dispatch → deliverAsync →
  the *test's own* mock emitter, never `event_emitter.go::Emit`. So an echo-bot
  "emit-count==0" failure is a SUBSCRIBE-FILTER mismatch, not capability gating:
  `dispatch` matches `string(event.Type)` against `sub.eventTypes`, and
  `corecomm.EventTypeSay == "core-communication:say"` (qualified,
  `plugins/core-communication/events.go:24`) — subscribing with `Manifest.Events`
  short-form `"say"` never matches. Don't trust a bead's "gated on capability"
  hypothesis without tracing the actual path. Encountered: holomush-gria3.

- **`location:<id>` stream reads bypass `engine.Evaluate` — gated by the location
  hard-gate, not ABAC Layer 3.** `QueryStreamHistory`
  (`internal/grpc/query_stream_history.go:196-211`) routes `isLocationStream`
  streams to a hard-gate: `if !staffOverride(...) { if info.LocationID != extractLocationID(stream) { DENY } }`.
  `staffOverride` (`scope_floor.go:88-103`) is the ONLY ABAC consult for location
  streams — it evaluates `read_unrestricted_history` on `stream:*`. A **co-located**
  location read (`info.LocationID == requested`) is permitted by the hard-gate
  WITHOUT ever consulting ABAC for the `stream:` resource, so
  `seed:player-location-stream-read` is effectively dead for this handler. Consequence
  for harness-real-ABAC test review: a "colocated permit succeeds only because the
  stream provider is registered" sentinel does NOT work through `QueryStreamHistory`
  (the hard-gate permits regardless of provider registration). The valid g776
  sentinel is the ADMIN/non-colocated path: admin role → `"admin" in
  principal.character.roles` (CharacterProvider + RoleResolver, needs RoleStore wired)
  → `seed:admin-full-access`/`seed:staff-read-unrestricted-history` permits
  `read_unrestricted_history` → staffOverride true → bypass hard-gate. An unregistered
  roles provider flips admin to denied. Only ABAC Layer 3 (`global`, `system`, …
  non-location public streams) hits `engine.Evaluate` directly. Encountered:
  holomush-f5t07 (2026-05-26).

## Invariants worth remembering

- **Top-level oops Code() is the wire-visible code**: client-side error
  classification reads only the OUTERMOST oops node's code via
  `oops.AsOops(err).Code()`. `errutil.AssertErrorCode` walks the chain and
  passes if the code appears anywhere — DO NOT use it for opacity-invariant
  pin tests. Use `oops.AsOops(err).Code()` directly to assert the
  client-visible code (see
  `internal/grpc/query_stream_history_test.go:944` for the canonical pattern).

- **Plugin-status preservation chain**: for `mapHistoryError` to translate
  plugin gRPC codes correctly, every layer between the plugin and the
  handler MUST preserve the gRPC status. The chain is:
  `PluginAuditService.QueryHistory` (plugin) → `pluginHistoryStream.Next`
  (`internal/eventbus/audit/plugin_router.go:158-176`) → `HistoryReader`
  → handler. Each preservation site uses the pattern:
  `if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown { return err }`
  with `//nolint:wrapcheck` justifying the deliberate non-wrap. Adding an
  `oops.Wrap` anywhere in this chain would shadow the code from
  `mapHistoryError`'s `status.FromError` lookup.
- **Proto field-number lifecycle**: deletion → MUST add `reserved N;` AND
  `reserved "field_name";` in the same commit. Comment-only reservation is
  not enforced by `protoc` and not enforced by the project's lint chain.

- **Generated artifacts inventory** for proto changes:
  - `pkg/proto/holomush/<svc>/v1/<svc>.pb.go` (Go)
  - `web/src/lib/connect/holomush/<svc>/v1/<svc>_pb.ts` (TS bindings)
  - Run `task proto` (or `task web:generate`) to regenerate.

- **Diff-scope verification**: for proto-only tasks, `jj diff -r @ --name-only`
  should show only `.proto`, `.pb.go`, and `_pb.ts` files. Anything else is
  scope creep.

- **Plugin emit-gate symmetry checkpoint**: any host-side trust check on
  the plugin emit path MUST land in `internal/plugin/event_emitter.go::Emit`
  (the single gate site reached by both Lua and binary runtimes). Lua
  flows through `Manager.EmitPluginEvent` → `emitter.Emit`; binary flows
  through `pluginHostServiceServer.EmitEvent` → `emitter.Emit`. A check
  added only on the binary path (e.g., in `host_service.go::EmitEvent`)
  silently bypasses Lua plugins, violating the project's runtime-symmetry
  invariant (CLAUDE.md "Plugin Runtime Symmetry"). Runtime-specific
  authentication (e.g., the gRPC token mechanism) IS OK in
  runtime-specific code, but the policy/manifest gate MUST be at the
  shared site.

- **Focus per-connection delta delivery is asymmetric BY DESIGN — not a
  symmetry violation.** Two distinct focus delivery seams: (1) session-level
  `focus.StreamSender` (`coordinator.go:22`, `JoinFocus → StreamSender.Send →
  SessionStreamRegistry.Send → r.channels[sessionID]`, `join.go:51`) wired on
  the COORDINATOR in prod (`sub_grpc.go:447`) — both runtimes use it; (2)
  per-connection `focus.ConnectionSender` (binary host field, `host_service.go:255,315`
  AutoFocusOnJoin/SetConnectionFocus → `SendToConnection → r.connections`).
  ConnectionSender is **nil in production** (`core.go:405-422` PluginSubsystemConfig
  omits it; `sub_grpc.go` never sets it) — per-connection deltas are SKIPPED in
  prod today. Lua DELIBERATELY drops per-connection deltas: `focus_ops_adapter.go:46-49`
  ("Lua plugins react to focus events via JetStream, not via the RPC return
  value"). So neither runtime drives per-connection deltas in prod; both deliver
  session-level via the same coordinator StreamSender. A binary-only
  `ConnectionSender` field/wiring is NOT a privilege gradient — it's
  runtime-specific delta delivery, not a trust/policy/manifest gate (those stay
  at `event_emitter.go::Emit`). Both Subscribe registrations (`server.go:822`
  session-wide Register + `:872` RegisterConnection) write the SAME ctrlCh, so a
  single-connection joiner gets delivery via session-level StreamSender
  regardless of ConnectionSender. Trap: a "binary AutoFocusOnJoin delivery never
  wired in prod" claim is FACTUALLY true (ConnectionSender nil) but does NOT
  imply a missing capability — session-level delivery covers it. Encountered:
  holomush-y5inx.9 harness focus wiring (2026-05-28).

- **Plugin emit token store: pluginName binding is the cross-plugin
  defense**: `emitTokenStore.Lookup(pluginName, token)` rejects when the
  stored entry's pluginName != caller pluginName. The caller pluginName
  is `pluginHostServiceServer.pluginName`, set at server construction and
  mTLS-bound. Adding any code path that lets a plugin influence the
  pluginName argument (e.g., reading it from request metadata) re-opens
  the cross-plugin actor-escalation surface. The unit test guarding this
  is `TestEmitEventCrossPluginTokenLeakFails`
  (`internal/plugin/goplugin/host_service_test.go:744-776`).

- **Sentinel-name vs sentinel-ULID gap in PluginRepo bootstrap.** T5
  (holomush-w9ml.6) guards against sentinel-ULID collision via
  `core.IsSentinelULID(row.ID)` but does NOT guard against a plugin row
  whose `Name` is "system" or "world-service". `manifest.go:namePattern`
  matches both ("system" and "world-service" both satisfy
  `^[a-z](-?[a-z0-9])*$`). A plugin named "system" would write
  `activeByName["system"] = <plugin ULID>`, breaking `IDByName("system")`
  returning false and causing attribution ambiguity in NameByID. The fix
  is a reserved-name set in `Manifest.Validate()` or a bootstrap guard.
  Applies identically to T6 (Upsert / loadPlugin). Filed as non-blocking
  follow-up (2026-05-04).

- **Late-bound host field read outside RLock**: `goplugin.Host` fields set via
  `SetX()` (which takes write lock) must be snapshot under RLock before use.
  `eventEmitter`, `focusCoordinator`, `historyReader` use accessor methods that
  lock properly. `identityRegistry` (added T10) is read bare at
  `host.go:604,679` after the RLock is released — latent race, not triggered in
  practice because registry is set before LoadAll. Fix: add an accessor method
  or snapshot inside RLock. Check this pattern whenever a new late-bound field
  is added to Host.

- **FK cascade fix over-reach contradicts crypto soft-delete spec**: a bug fix
  that adds `ON DELETE CASCADE` to fix one FK (e.g. guest reaper needs only
  `player_character_bindings.player_id` cascade) must NOT also cascade
  `character_id`. The crypto design spec
  (`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md:732-734`)
  mandates character deletion SOFT-deletes bindings (`ended_at`/`ended_reason`)
  and explicitly "do NOT cascade-delete them" — historical `binding_id` is
  forensic-retention substrate in `crypto_keys.participants[]`. `Service.DeleteCharacter`
  (`internal/world/service.go:602` → `character_repo.go:77 DELETE FROM characters`)
  is a live method (no prod caller yet, Phase-4-deferred). Always scope a cascade
  fix to the exact FK the bug needs and check the touched table against any
  soft-delete lifecycle spec before widening. Encountered 2026-05-23 (21bd9).

- **Plan-file markdown breaks lint gate**: `task lint` includes `rumdl` markdown
  lint over `docs/`. A stray bare ` ``` ` fence will fail `lint:markdown` and
  block CI. Run `task lint` after any plan file edit.

- **Discarded-value "production adapter resolves it" smell.** When a handler
  validates a field then blank-assigns with `_ = val // consumed by X adapter`,
  verify the adapter signature. The claim is unfalsifiable without cross-checking
  the adapter's actual type (`rg` the adapter type + `func.*Run.*<RequestType>`).
  Search heuristic: `rg "_ = [a-zA-Z]+ // (consumed|used|resolved) by"`.
  Encountered: Phase 5 sub-epic E review (2026-05-11), `RekeyResume` handler drops
  `request_id` — `socket.RekeyRunRequest` has no `RequestID` field.

- **`task test:int` explicit package list excludes `cmd/holomush/`**: `Taskfile.yaml:145`
  enumerates packages with `//go:build integration`; `cmd/holomush/` absent (compilation
  failures). Tests in `cmd/holomush/*_integration_test.go` never run. Adding integration
  tests there requires adding `./cmd/holomush/` to the list. Encountered: T19 review.

- **Ginkgo regression-guard vacuity + colon-style scene stream trap.**
  *Documented recurring pattern — both arms were caught by code-reviewer
  before push during iwzt.9-11 and fixed in the same PRs (e.g., iwzt.11
  PR #4164 dot-style fix at `test/integration/privacy/privacy_test.go:404`
  via `Server.GameID()` + `"events.<gid>.scene.<id>.ic"` construction).
  Re-surface this on any future iwzt/scene-stream test review.*
  (a) Assertions "all returned events satisfy X" over an empty result
  slice are vacuously true; require a seed event so a breakage is
  detectable. (b) `"scene:<ulid>:ic"` (colon-style) is NOT a private
  stream — `isSceneStream` requires dot-style
  `events.<gid>.scene.<id>.ic`. Using colon-style in a "not_member" test
  entry silently exercises the ABAC path instead of the I-17 membership
  gate. Always verify stream format matches the classifier the test claims
  to exercise. `stream_access_test.go:39` explicitly documents the
  rejection: `{"returns false for old colon-style scene stream",
  "scene:01ABC:ic", false}`. Encountered: iwzt.9-11 (2026-05-21).
  (c) **Harness scene-helper REAL-vs-synthetic distinction**: `Session.CreateScene`
  (`integrationtest/session.go:465`) drives the REAL `SceneServiceClient().CreateScene`
  RPC → core-scenes mints a BARE ULID (`service.go:1113`, holomush-y5inx) and persists
  a backing row; `Server.NewSceneWithoutMember`/`NewScene` (`harness.go:569`) return a
  SYNTHETIC `idgen.New()` ULID with NO backing row. A "real CreateScene bare-ULID"
  regression claim is only honored by the `CreateScene` helper. Confirmed: y5inx.4
  (2026-05-28) — its INV-Y5INX-2 spec correctly uses `CreateScene`.

- **Contributor-guide example YAML/JSON MUST match the validator's regex/schema,
  not just "look plausible."** When a how-to doc shows a copy-paste registry/config
  example that a meta-test validates, run the doc's literal example through the
  validator's extraction regex before trusting it. Real miss: quarantine.md
  Step-2 example used `- id: holomush-xxxx` (bead under `id:`) but the bijection
  meta-test's `registryBeadRE = ^\s*bead:\s*(holomush-...)`
  (`test/meta/quarantine_registry_test.go:24`) and `test/quarantine.yaml:10`
  schema `{ id, kind, bead, since, reason }` require a `bead:` key — the example
  registers ZERO beads, so a contributor copying it breaks INV-2. The doc lint
  (`task lint:markdown`) passes because the YAML is well-formed; only schema/regex
  cross-check catches it. Verify: `printf '<example>' | rg '<validator-regex>'`.
  Encountered: holomush-b4myw.10 (2026-05-25).

- **`jq '.[0].status // "unknown"'` fallback fires on `[]`, NOT on empty stdin.**
  In quarantine-audit.sh (holomush-b4myw.5), `bd show <missing-bead> --json` emits
  NOTHING; jq over empty input outputs an empty string, so the `// "unknown"`
  default never fires (verified: `echo "" | jq -r '.[0].status // "unknown"'` →
  empty; `echo "[]" | jq ...` → `unknown`). Net effect: a registry row citing a
  renamed/deleted bead fails OPEN (status="" ≠ "closed" → audit passes). Acceptable
  for INV-3 (contract is "is the cited bead closed", and INV-2 bijection meta-test
  is the existence guard) — flag non-blocking. Also: `grep -oE 'bead:[[:space:]]*holomush-...'
  | awk '{print $2}'` returns empty if a row uses no-space `bead:holomush-x` (whole
  match is one field) — latent, today's registry + bijection RE both use the space
  form. Encountered: holomush-b4myw.5 (2026-05-25).

- **Tier-split docs intentionally LEAD not-yet-landed tooling — verify the bead
  graph before flagging "command doesn't exist."** In the tier-split-quality-gates
  epic (holomush-b4myw), Task 10 docs reference `task quarantine:audit` (Task 5,
  OPEN) and nightly `HOLOMUSH_RUN_QUARANTINED=1` wiring in nightly-soak.yml (Task 6,
  OPEN — currently only runs `task soak:eventbus`). The plan explicitly instructs
  the doc to mention them (plan lines 1085/1089), same lead-the-promotion pattern
  as the Task 11 ruleset flip. So a doc citing a not-yet-existing `task X` is
  Medium/non-blocking IF a sibling bead owns it; check `bd list` for the owning
  task before calling it a blocker. (Distinct from finding #1 above, which is a
  doc that breaks an ALREADY-LANDED validator.) Encountered: holomush-b4myw.10.
