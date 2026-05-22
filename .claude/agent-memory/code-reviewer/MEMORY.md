# code-reviewer agent memory

This file accumulates HoloMUSH-specific anti-patterns, subtle invariants, and
recurring blind spots discovered during adversarial code review. Entries are
added by the agent itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

## Anti-patterns

- **`AGENTS.md` and `CLAUDE.md` are paired SSoTs**: at main they are
  byte-identical. `Taskfile.yaml`
  `lint:docs-symmetry` enforces only the `<!-- BEGIN: plugin-runtime-symmetry -->`
  anchored subsection byte-identical, but the surrounding convention is
  whole-file twinning (initial setup, two later "chore: fix agents md" syncs).
  Any edit to one MUST be mirrored in the other (or AGENTS.md replaced with a
  one-line stub pointing at CLAUDE.md and `lint:docs-symmetry` updated). When
  reviewing CLAUDE.md changes, ALWAYS `diff CLAUDE.md AGENTS.md` and grep both
  for the same anchor names; treat divergence beyond the symmetry-block as
  a blocking finding.

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
  enumerates packages that contain `//go:build integration` files; `cmd/holomush/` is
  intentionally absent (compilation failures). Integration tests written in
  `cmd/holomush/*_integration_test.go` are never run by `task test:int`.
  When a task adds integration tests to that package, adding `./cmd/holomush/` to the
  list is a required companion change. Encountered: T19 review (holomush-w9ml.17, 2026-05-04).

- **Ginkgo regression-guard assertions may be vacuously true.** When a test asserts
  "all returned events satisfy invariant X" over a result slice that should be empty
  under correct behavior, the assertion trivially passes whether or not the query
  mechanism itself is functional. This is the intended design for regression guards
  (if the gate breaks, events surface and the assertion fails), but it means the test
  does NOT prove the query path runs. Always verify: (a) the test actually runs — `rg`
  for the Ginkgo `Describe` label in source, not just in plan docs; (b) the Ginkgo
  filter is `-ginkgo.focus=`, not `-run` (which matches Go test functions, not Ginkgo
  descriptions); (c) a seed event is planted before the floor so a breakage is
  detectable. Encountered: iwzt.9 (2026-05-21, `privacy_test.go:150-156`) and iwzt.10
  (2026-05-21, `privacy_test.go:331-349` — second `It` in move-floor test plants a post-move
  event via `EmitDirectEvent` but omits `Expect(events).NotTo(BeEmpty())`. `EmitDirectEvent`
  returns post-JetStream-ack so the race is slim but the vacuity gap persists).
