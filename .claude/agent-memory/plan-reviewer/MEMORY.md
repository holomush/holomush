# plan-reviewer agent memory

This file accumulates HoloMUSH-specific patterns of good and bad plans
discovered during adversarial plan review. Entries are added by the agent
itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

## Sub-epic D plan-review reflexes (HoloMUSH 2026-05-09)

- **`socket.PeerCred` is NOT `{OSUser string}`.** Real shape is `{UID uint32, GID uint32, PID int32}` (`internal/admin/socket/peercred.go:15-21`). Plans that inline `req.PeerCred.OSUser` will not compile. The `OperatorIdentity.OSUser "uid=1001 (alice)"` audit string needs an explicit formatter step that does not exist in any task today.
- **`socket.WithPeerCred(ctx, cred)` does NOT exist.** `internal/admin/socket/peercred.go` exposes only `PeerCred` struct, `PeerCredFromContext(ctx) (PeerCred, bool)`, and `PeerCredMiddleware(next)`. Context insertion happens inside the middleware via the *unexported* `peerCredContextKey{}`. Plans that test PeerCred capture by calling `socket.WithPeerCred(...)` are inventing a setter; either add an exported helper task or test-drive via `PeerCredMiddleware` + `httptest.NewRequest`.
- **R1-fix propagation: rename one type, propagate everywhere.** When R1 forces a struct shape change (e.g., `OperatorIdentity.OSUser` → `OperatorIdentity.PeerCred`), the revision often updates the type definition + the assignment site but leaves stale references in (a) downstream test names and (b) inline implementer notes. Always grep the entire plan for the old field name after a rename revision; a single missed reference breaks compile or misleads a subagent.
- **R1-fix propagation: rename a type → spec amendment required.** When a plan deviates from a spec-mandated field name (`OperatorIdentity.OSUser` → `PeerCred`), the §10/T23 amendments table MUST grow a row covering the rename + a NEGATE substring assertion in `TestSpecAmendmentsLandedSubEpicD`. Otherwise spec and code disagree on a security-relevant audit field.
- **INV-32/33/37 do NOT live in `BootstrapSubsystem.Start`.** They live in `kek.NewLocalAEADProvider` constructor (`internal/eventbus/crypto/kek/local_aead.go`) and run during EventBus subsystem setup. `internal/bootstrap/setup/subsystem.go::Start` does only policy/setting/admin seeding (5 steps). Plans (and specs) that say "alongside INV-32/33/37" pointing at BootstrapSubsystem are repo-fiction.
- **`productionSubsystems` signature is named-param, not variadic.** `cmd/holomush/core.go:870-884` takes 12 named `lifecycle.Subsystem` params. Adding a 13th means: (a) extend signature, (b) update `TestProductionSubsystemsIncludesCluster` (count==12 assertion), (c) update `TestProductionSubsystemsIncludesAdminSocket` (12 args), (d) update `TestSubsystemAdminSocketConstantExists` ID list, (e) add `TestProductionSubsystemsIncludesCryptoPolicy`. Plans that say "append to the slice" understate the test cascade.
- **Lifecycle SubsystemID iota gotcha.** New IDs go at END of const block (after `SubsystemAdminSocket` per `internal/lifecycle/subsystem.go:29`), then run `task generate` to regenerate `subsystemid_string.go`. Inserting "alphabetical-ish" near a middle constant breaks the linecomment-driven stringer.
- **Type juggling between `ulid.ULID` and `string` for player IDs.** TOTP layer uses `ulid.ULID` value (`internal/totp/types.go:38-41`). Access/store layer uses string ULID. Plans that consume both layers in one provider will inline `player.ID.String()` repeatedly — that's intentional, not a smell.
- **JCS canonicalizer import path is unusually deep.** `github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer` — verify before approving any plan that pins this lib. Use `mcp__deepwiki__ask_question` or `go list` to confirm.
- **TDD red phase that's only "compile error" is degenerate.** When T6's `EnrollmentChecker` interface holds both `IsEnrolled` AND `Verify`, the production code can wire to raw `totp.Service` directly — the AuditingService decorator dependency is at the HANDLER level (T15), not the provider level. Bead-chain edges that gate `T6 → T13` over-constrain parallel execution.
- **Repository differentiator-SELECT race.** When MarkApproved's atomic UPDATE returns 0 rows, a follow-up SELECT to determine WHICH predicate failed has a race window. Plans that say "differentiate the failure cases by re-querying" need to either accept the race or use `RETURNING` for deterministic differentiation. Also: `expires_at`-time-travel tests need explicit `Clock` injection AND test docs noting the direct-SQL `UPDATE expires_at` workaround.
- **`eventbus.Event` ≠ `eventbusv1.Event` — the publisher takes the in-memory type, not the proto type.** `internal/eventbus/types.go:91` defines the in-memory `eventbus.Event` (typed `Subject Subject`, `Type Type`, `ID ulid.ULID`, `Payload []byte`). `pkg/proto/holomush/eventbus/v1/eventbus.pb.go:140` defines the wire `eventbusv1.Event` (`Id []byte`, `Subject string`, `Type string`, `Payload []byte`). `Publisher.Publish(ctx, eventbus.Event) error` (`internal/eventbus/rendering_publisher.go:58`, `internal/eventbus/bus_test.go:20`) takes ONLY the in-memory type. The publisher chain marshals to the proto envelope internally on the way to NATS; callers MUST NOT construct `eventbusv1.Event` directly. Plans that import `eventbusv1` from any emit-site (T13 AuditingService, T10 chain emitter) and pass `*eventbusv1.Event` to `Publisher.Publish` will not compile. Construct with `eventbus.NewSubject(s)`, `eventbus.NewType(t)`, `core.NewULID()`, `eventbus.Actor{Kind: ActorKindSystem, ID: ulid.ULID{}}`, `Payload: jsonBytes` and pass that.
- **R2→R3 placeholder-fill drift: filling a placeholder with code can introduce new compile bugs that the placeholder hid.** When a prior review finding says "T13 has a placeholder body — implement it" and the revision lands ~150 lines of inline Go, do NOT assume the new code compiles just because the placeholder is gone. Re-verify: (a) every imported package is what the call sites actually want (eventbusv1 vs eventbus is the canonical trap), (b) every method signature matches the interface in the repo, (c) every struct-literal type matches the field-types of the receiving function. Plan author hedges like "verify the actual package name" can mask the real issue ("wrong type entirely") and should not be accepted as a substitute for inline-correct code.

## w9ml legacy_id-elimination plan-review reflexes (HoloMUSH 2026-05-04)

- **`core.ActorUnknown` does NOT exist.** Three constants in `internal/core/event.go:147-152` (`ActorCharacter`, `ActorSystem`, `ActorPlugin`). The wire-side equivalent is `eventbus.ActorKindUnknown`. Plans that gate on `core.ActorUnknown` will not compile.
- **`Manifest.Runtime` does NOT exist.** Field is `Manifest.Type` (`internal/plugin/manifest.go:74`). Constants `plugins.TypeLua`, `plugins.TypeBinary`, `plugins.TypeSetting`. Binary path is `Manifest.BinaryPlugin.Executable` (NOT `Binary.Path`).
- **`Manager.UnloadPlugin` does NOT exist.** Unloads are inline `host.Unload(ctx, name)` calls inside `loadPlugin` rollback paths (`manager.go:777,793,822,913,924,948`). No exported Manager-level Unload entry point — plans that "modify UnloadPlugin" need a prerequisite task to introduce one.
- **`coreActorToBusActor` is the wrong function name.** Real name is `coreToBusActor` at `cmd/holomush/sub_grpc.go:501`. Spec/plan drift toward the longer name; verify with grep.
- **Test helpers `newTestPool` / `runMigrations` do NOT exist.** Each `internal/store/*_integration_test.go` composes its own testcontainer pool inline. Plans that reference these as "existing helper" need to either add a Task X.0 building the harness, or inline the testcontainer pattern from `migrate_integration_test.go`.
- **`internal/telnet/` does NOT render actor names.** Package contains gateway, limits, sanitize, refuse, metrics — no actor-name rendering. `rg -n 'Actor\b' internal/telnet/` returns ZERO matches. Plans that "modify the existing telnet renderer" are inventing a renderer that doesn't exist; the actual display path today is `internal/grpc/server.go::actorIDString` (lines 596-606).
- **`task proto:gen` is the wrong task name.** Real task is `task proto` (`Taskfile.yaml:252-256`). Common plan author error.
- **`cmd/holomush/*_test.go` is `package main`, NOT `package main_test`.** Verify with `rg -n '^package' cmd/holomush/sub_grpc_adapters_test.go`.
- **`actorIDString` zero-ULID guard is load-bearing.** `internal/grpc/server.go:600-606` returns `""` for `ulid.ULID{}` to avoid sending `"00000000000000000000000000"` to web clients. Any plan that simplifies this to `return a.ID.String()` introduces a regression for ActorKindUnknown / system-zero-actor cases.
- **`eventbus.Actor.ID` is `ulid.ULID` value, not `[]byte` proto bytes.** `internal/eventbus/types.go:50-58`. The proto type uses `bytes`, the in-memory Go struct uses `ulid.ULID`. Plans that say `Actor.ID = parsed.Bytes()` for in-memory use are wrong.

## Common plan gaps in this codebase

- [Verify helper existence](feedback_verify_helper_existence.md) — plans frequently invent methods/helpers on existing types ("extend test file X" with calls that don't exist on X)
- [No signature placeholders](feedback_no_signature_placeholders.md) — interface signatures and parameter lists must be inlined, never deferred via `/* COPY FROM existing.go */` comments
- [Verify imports against code blocks](feedback_verify_imports_in_code_blocks.md) — every package qualifier (`slog.`, `timestamppb.`, `status.`) used in a plan's example code must appear in the file's existing imports OR in the plan's "imports needed" directive
- **Missing `jj new` between phase-commit boundaries.** When a plan decomposes a PR into N
  per-phase commits, every phase-end "commit task" MUST conclude with `jj new -m "phase
  N+1 (in progress)"` so the next phase's edits land in a fresh `@`. Plans that end the
  phase task with bare `jj describe` cause subsequent file edits to fold into the named
  phase commit; a later "describe" then clobbers the earlier description. Always grep
  the plan for `jj describe` not followed by `jj new` at task boundaries.
- **`@-` vs `@` bookmark targets.** When a plan creates a push bookmark, verify which
  commit `@-` actually points to by tracing the commit/new sequence forward from the
  last `jj new`. Plans that say "@- is the Phase N commit, since @ is currently empty
  after the commit-then-new pattern" must actually contain that `jj new` somewhere —
  authors sometimes assume the pattern without writing the step.
- **False-starts left in executable text.** Watch for "Wait — that command is wrong, use
  this instead:" patterns inside step bodies. Subagents skim and may run the first
  block they see. Flag any plan that emits two contradictory commands in the same step.
- **Long-running steps without a budget annotation.** `task pr-prep` runs 5–15 min; a
  per-task subagent dispatch with default timeout will starve. Plans should annotate
  long-running verifications and recommend `run_in_background` + Monitor.
- **"Manual-ish" steps in subagent-driven plans.** Multi-terminal manual checks cannot
  be executed by an automated agent. Either script them inline or label them
  `(MANUAL — pre-merge only)`.
- **Stale prose around revised code blocks.** When a previous review forces the author to
  simplify a code block, the surrounding "Notes" / explanation prose often gets left
  describing the OLD construct. On revision passes, diff the prose against the current
  code, not just the code-against-itself. (Round-2 example: Task 7 step 2's source line
  was simplified from a two-fallback construct to one line, but the post-code Notes
  block still described "two fallbacks because...".)
- **Cross-task instruction drift after partial-mirror fixes.** When ONE task adopts a
  pattern (e.g., `describe + jj new`) and a later task deliberately does NOT adopt it
  (last-phase exception), prose like "Mirror this pattern at Tasks N, M, K" must
  enumerate exceptions explicitly. Round-2 example: Task 6 step 4 said "Mirror at Tasks
  9, 13, and 16" but Task 16 step 3 explicitly forbids the mirror. Subagent execution
  per-task hides this contradiction; an inline-execution agent might propagate the
  wrong pattern to Task 16.
- **Running test-count drift after a per-task split.** When a revision splits one
  bats/test task into N tests, the author often updates the FINAL tally and the
  per-task breakdown but forgets to propagate through the intermediate
  "Expected: N tests" lines in subsequent tasks. Always grep
  `rg -n "Expected: [0-9]+ tests"` against the plan and recompute the cumulative sum
  from the per-task contributions; flag any line that doesn't match. Pr-prep-lock
  round-2 example: Task 8 split 1→3; Task 12 step 2 correctly updated to "12 tests"
  but Tasks 9/10/11 step 2 still said 7/8/9 instead of 9/10/11.

## Decomposition patterns that work here

- **Per-phase commit pattern that DOES work**: Task N edits files; Task N+1 step 1 runs
  `jj describe -m "phase N: ..."` then step 2 runs `jj new -m "phase N+1 (in progress)"`.
  Plan 2026-04-25-session-workspace-isolation.md gets this RIGHT for Phase 3→4 (Task 13)
  but WRONG for Phase 1→2 (Task 6 missing the `jj new`).
- **Helper extraction for shell-script reuse**: when two consumers (Taskfile cmd + hook)
  need the same path-discovery logic, ship one `scripts/<helper>.sh` sourced by both.
  Spec called this out as an "Implementation note"; plan implemented it. Pattern works
  cleanly in the HoloMUSH layout.
- **Repo-reality verification of cited line numbers**: plans that cite `file:line-line`
  ranges should be grep-verified before review approval. The 2026-04-25 plan cited
  `Taskfile.yaml:515-551`, `CLAUDE.md` 552/570/849 — all four matched current
  main@origin (`642c93e39baf`). When citations are accurate, reviewers can move fast.

## EventType migration plan reflexes (HoloMUSH)

- **Two parallel EventType constant sources**: `internal/core/event.go`
  AND `pkg/plugin/event.go` (`pluginsdk.EventType*`) both declare bare
  event-type strings (`"say"`, `"pose"`, `"arrive"`, ...). Any plan to
  migrate these constants MUST list both files in its File Structure
  table and grep both namespaces in its call-site survey. The SDK
  side has ~100 references across pkg/holo and plugin tests.
- **Lua emit syntax is table-literal, not function-call**: HoloMUSH Lua
  plugins emit via `{stream = ..., type = "say", payload = ...}` return
  tables, not `emit_event(...)` calls. Plans that grep
  `emit_event\(.*"(say|...)"` produce zero matches and silently no-op.
  The matching grep pattern is `type = "(say|...)"`.
- **`core-scenes` ops events ≠ stream event types**: scene plugin uses
  `OpsEventKind` constants (`membership.invite`, `lifecycle.created`)
  for plugin-owned audit, and only emits `pluginsdk.EventTypeSystem`
  on streams. Plans that declare `scene_create`, `scene_ic`, etc. in
  `crypto.emits` are fabricating events the plugin doesn't emit today.
- **No `cmd/holomush/cmd_plugin.go`**: the holomush CLI does not have a
  `plugin` cobra subcommand group as of `main@origin` (f8bd6543b).
  Plans adding `plugin events`, `plugin validate` subcommands MUST
  include an explicit task to introduce the parent group + wire it
  into root.go.
- **`Taskfile.yaml` not `Taskfile.yml`**: a recurring plan-author error.
  Both work via the Task CLI auto-detect, but file-mention claims must
  match disk.
- **Manager has no `loadManifest` extraction point**: `internal/plugin/manager.go`
  inlines `ParseManifest` inside `Discover` (line 349). Plans that
  pretend a `Manager.loadManifest(raw []byte)` already exists need a
  refactor task before the new validator hook can be inserted.

## HoloMUSH Phase 3a crypto plan reflexes

- **`internal/eventbus/publisher.go` exposes `PublishOption`, NOT
  `PublisherOption`.** Plans introducing new `With*` options must use the
  existing type name. `internal/eventbus/publisher.go:67`.
- **No `EventsAuditRow` struct exists.** The audit write path is inline raw
  SQL in `internal/eventbus/audit/projection.go:240-260`
  (`p.pool.Exec(..., INSERT INTO events_audit ...)`). Plans that claim to
  modify `internal/store/events_audit_repo.go` are fabricating that file.
- **`Manifest.Crypto` is `*CryptoSection` (pointer).** Test fixtures that
  use `Crypto: plugins.CryptoSection{...}` value form will not compile.
  Helper functions iterating `manifest.Crypto.Emits` must guard for
  `manifest.Crypto == nil`. `internal/plugin/manifest.go:107`.
- **`ActorKindPlugin` lives in `internal/eventbus/types.go:46` (package
  `eventbus`), NOT in `internal/plugin/` (package `plugins`).** The
  `plugins` package has no `Actor`, no `ActorKind`, no `ActorKindPlugin`.
  `ActorResolver` returns `core.Actor`
  (`internal/plugin/event_emitter.go:26`). `core.Actor.ID` is a `string`,
  not a `ulid.ULID`.
- **`Manifest.ActorKindsClaimable` is `[]string`, not `[]ActorKind`.**
  `internal/plugin/manifest.go:84`. Use canonical strings (`"plugin"`,
  etc.) per `validateActorKindsClaimable` normalization.
- **Audit projection test file is `//go:build integration`-only.** Plans
  appending unit-style tests to `internal/eventbus/audit/projection_test.go`
  silently disable themselves under `task test`. The sibling
  `projection_unit_test.go` is the unit-test target.

## Pass-revision drift reflexes

- **Modify-without-Create artifacts after a structural rewrite.** When a pass-1
  finding forces an author to delete or rewrite an early task (e.g., remove a
  smoke-test file from the bootstrap task), the downstream tasks' Modify lists
  often still reference the deleted artifact. Always grep
  `rg -n 'plugin_test\.go|<deleted-symbol>' <plan>` and confirm every Modify
  line either targets a real file or a file created by an earlier task. Pass-2
  example: 2026-05-01 plan rewrote Task 5 to drop `plugin_test.go` (Option A
  per-analyzer plugins don't need a smoke test), but Tasks 10, 11, 13, 19
  still listed `gorules/plugin_test.go` as Modify.
- **Partial application of an NB-fix across siblings.** When pass-1 returns a
  multi-target fix-up (e.g., NB#2: "fix line numbers AND verbatim Before
  blocks in three doc-comment edits"), the author often applies the fix to
  the first one or two and stops. Verify each instance independently. Pass-2
  example: NB#2 cited `material.go` and `api_test.go`. Both `material.go`
  Before blocks were corrected (lines 7-9 and 39-42 match exactly); the
  `api_test.go` Before block was NOT corrected — its first line
  ("// This is the ground-truth defense for") only exists as the END of a
  longer line in the actual file, so the Edit tool's exact-match contract
  will fail.

## golangci-lint module-plugin reflexes

- **One `register.Plugin(name, …)` call = ONE enableable linter ID.** golangci-lint v2 wraps all analyzers from a single plugin registration into one `goanalysis.Linter` whose `Name()` is the registered plugin name. To expose N analyzers as N independent `linters.enable` entries, the plugin module MUST contain N `register.Plugin(...)` calls in `init()` — one per analyzer (see `github.com/albertocavalcante/go-analyzers-gcl/plugin.go` for the canonical pattern). The upstream example `golangci/example-plugin-module-linter` registers one plugin returning ONE analyzer for a reason. Plans that use `register.Plugin("X", New)` returning N analyzers AND `linters.enable: [- a, - b, - c]` with N analyzer names will fail with "unknown linter" at the first lint run. The fix touches the plugin scaffolding, the `linters.settings.custom` map (must have one entry per registered plugin name), and `linters.enable` (one entry per registered plugin name). govet is the only stdlib linter that supports per-analyzer enable/disable inside one linter ID, via dedicated `linters.settings.govet.{enable,disable}` configuration — this is special-cased in golangci-lint, not a general module-plugin pattern.
- **`linters.exclusions.rules` scopes are by linter ID, not analyzer name.** With a single-plugin shape, an exclusion for `_test.go` against `linters: [holomushrules]` disables ALL bundled analyzers in test files, not just one. Per-analyzer test-file scoping requires either per-analyzer plugins OR doing the filename check inside the analyzer's `run` function via `pass.Fset.Position(file.Pos()).Filename`.

## Review reflexes

- For every `path:line` citation in the plan, run a quick `Read` or `rg` to verify the line range still covers what the plan claims. Drift across PRs is real — the spec under review used `:96-102` for a block, the plan used `:95-102` for the same block. Both can be off after the next merge.
- Plans that put a `task pr-prep` gate at the END but never run `task lint` per-task accumulate lint debt across N commits. Per project rule (`MUST run task lint before committing`), each commit checkpoint should be lint-clean. Flag this as non-blocking but real.
- For Ginkgo vs `testing.T`: never assume a `_test.go` file is BDD-style. Verify with `rg "var _ = Describe" path/to/file`. Plain `func TestX(t *testing.T)` is the default in this repo's `eventbus_e2e` integration tree.
- For the `task test:int` invocation: integration tests in HoloMUSH live in mixed locations. Plugin store integration tests are at `./plugins/<name>/` with `//go:build integration`, NOT at `./test/integration/plugin/...`. Always cross-reference `Taskfile.yaml:test:int` for the canonical package list before approving a path in a plan.
- `//nolint:unparam` does NOT suppress revive's `unused-parameter` rule. Both `unparam` (linter, `.golangci.yaml:31`) and `revive` rule `unused-parameter` (`.golangci.yaml:130`) flag unused function parameters. golangci-lint nolint directives suppress by linter name (`unparam`, `revive`), not by individual revive rule. Plans that introduce a temporary unused-parameter state for staged refactors MUST suppress both: `//nolint:unparam,revive // ...`.
- For jj-colocated repos with @ on a non-empty docs commit, the safe cadence is "new-first, then edit, then describe" — never "edit, then describe, then new", which silently merges code into the docs commit AND clobbers its message. Always verify with `jj log -r 'main@origin..@'` BEFORE approving any plan whose Task 1 starts with file edits.
- `task test:int` does NOT accept `--` package args — its package list is hard-coded in `Taskfile.yaml:93-111`. Plans saying `task test:int -- ./test/integration/foo/...` are wrong; `task test:int:focus` is the only narrowed variant and it's pinned to `./test/integration/plugin`.
- **Mocking style varies by package**: `internal/web/` uses hand-rolled struct mocks (`mockCoreClient` in `internal/web/handler_test.go:36`); `internal/grpc/` uses mockery `EXPECT()` for repos like `authmocks.NewMockPlayerSessionRepository(t)`. Plans that pick the wrong style for a package will not compile. Always verify with `rg -n 'EXPECT\(\)|type mock.*struct' <package>/` before accepting test snippets.
- **Playwright E2E lives at `web/e2e/`**, not `test/e2e/playwright/`. The Taskfile `task test:e2e` runs `npx playwright test` from the docker compose `playwright` service which reads config from `web/`. Existing specs: `landing.spec.ts`, `auth.spec.ts`, `terminal.spec.ts`, `session-security.spec.ts`, `character-switcher.spec.ts`, `scenes.spec.ts`, `admin.spec.ts`.
- **`web/package.json` does NOT include `@testing-library/svelte`** — only Vitest + Playwright. Plans that import `@testing-library/svelte` in component-test code are introducing an undocumented dependency. Verify the testing infrastructure before accepting Svelte component-render snippets.
- **`oops.Code()` returns `any`**: comparisons like `code == "FOO"` work but the canonical pattern in this repo is `code, ok := oopsErr.Code().(string); if !ok { return false }` (see `test/integration/access/evaluation_test.go:92`). Plans using bare `==` against an `any` should cite the existing pattern or be flagged.
- **Tautological TDD red phase via parallel test-fixture types.** When a plan's "red" test asserts behavior X by calling `testFooBar.method()` (a fake of type `testFooBar`) instead of the real production type `*FooBar`, AND the fake's method itself implements X, the red phase produces a false green before any production change. Always verify the test calls into the production type. The `2026-04-25-plugin-actor-claim-authentication.md` Task 1 had `testSubscriber.deliverAsync` (fake) instead of constructing `*plugins.Subscriber` via `NewSubscriber(host, emitter)` and calling its (package-private) `deliverAsync` directly. Both are legal from `package plugins`; the plan chose the wrong one.
- **Constructor return-type mismatch.** Some "constructors" in this codebase return factory functions, not struct pointers. `internal/plugin/goplugin/host_service.go:28`'s `newPluginHostServiceServer` returns `func([]grpc.ServerOption) *grpc.Server`, not `*pluginHostServiceServer`. Plans that read the name as if it returned the struct will write `s := newFoo(...); s.Method(...)` and not compile. Always read the constructor signature before approving any plan that invokes it.
- **`yq` is NOT installed in HoloMUSH CI.** `rg -n 'yq' .github/workflows/` returns zero hits, and there's no `setup-yq` action or `apt-get install yq` step. Plans that ship `yq`-based lint scripts will silently fail in `task pr-prep` on CI. Prefer a small Go program in `cmd/lint-<name>/` that uses the project's existing yaml parser; it composes with `go mod tidy` and matches the codebase style.
- **Type and field names: verify before approving.** This codebase uses descriptive type names like `CommandEntry` (not `Entry`) and unexported struct fields exposed via methods (`(e *CommandEntry).PluginName()` returns `e.pluginName`, NOT a public `PluginNameStr` field). Plans that fabricate exported fields like `command.Entry{PluginNameStr: "..."}` will not compile. Always `rg -n 'type Entry struct|type CommandEntry struct' internal/command/types.go` before approving struct-literal usage.
- **Existing test-helper inventory in `internal/command/dispatcher_test.go`:** `newTestDispatcherWithPlugin(t, deliverer)` and `newTestCommandExecution(t)` already exist (lines 2063, 2104). Plans that invent `newTestDispatcher`/`newTestExecutor` are wrong; redirect to the existing helpers.
- **Existing event_emitter test pattern is `eventbustest.Embedded` + `newEmitter(t, bus, lookup, resolve)` + `pluginActorResolver`** (per `internal/plugin/event_emitter_test.go:35,41`). Plans that introduce `mocks.NewMockPublisher(t)` need to (a) add a mockery config or (b) be redirected to the existing eventbustest pattern. The `internal/plugin/mocks/` package has `MockEventEmitter`, `MockHost`, `MockManagerOption` — but NO `MockPublisher`.
- **Missing integration harness ≠ "adapt the existing eventbus_e2e patterns".** When a plan says "if no harness exists, build a minimal one based on…", that's a deferred design problem, not a plan. Either require a "Task X.0 — build the harness" prerequisite with concrete file paths and method bodies, OR replace with a smaller-scope test that uses an existing test seam (e.g., `internal/plugin/integration_test.go` already runs real subprocess plugins).
- **Plausible-looking but nonexistent enum constants in test fixtures.** Plans
  often invent "natural" status names that don't match the real codebase
  conventions. The `2026-04-25-plugin-actor-claim-authentication.md` Task 2
  used `pluginsdk.StatusOK` in test mock returns, but the real codebase
  defines `pluginsdk.CommandOK` (per `pkg/plugin/command.go:13`). The same
  mistake is easy to make for any `Status*` / `Code*` / `OK*` family. Always
  verify enum constants by grepping `pkg/plugin/*.go` (or the relevant
  package) before approving any plan that names them — TDD red phase that
  fails at compile-time is a defect, not a TDD red.
- **go-task per-`cmd:` `vars:` is silently ignored.** `vars:` is supported at task-level (sibling of `cmds:`) and under `task: <subtask>` invocations, but NOT as a sibling of `cmd:` inside a list item. Plans that put `vars:` directly on a `cmd:` will see the block silently dropped and the variable will resolve to either empty or a built-in shadow (`{{.TASKFILE}}` in particular is a go-task built-in = abs path of loaded Taskfile). Verify by reading: any `cmd: |...` followed by `vars:` at the same list-item indent. Run a 5-line minimal Taskfile to confirm before flagging — go-task version drift is real.
- **Skip-stub `@test` declarations defeat invariant enforcement.** When a plan consolidates multiple invariants into one test body and adds skip-only `@test "<name>"` aliases just to satisfy a meta-test that greps for names, the meta-test loses its drift-detection power. The skip-aliases ARE detectable: `bats` body contains only `skip "..."` and nothing else. If spec assigns invariant I-N to test name X and plan implements X as a skip-stub, that's a blocking gap. Either rename in spec (I-N enforced jointly with I-M, test = `<combined-name>`) or split tests in plan to give each invariant its own real assertions.
- **`task fmt -- <file>` does NOT scope to the file** in HoloMUSH's Taskfile (sub-tasks `fmt:go`/`fmt:yaml`/`fmt:markdown`/`fmt:dprint` don't consume `CLI_ARGS`). Plans citing this invocation as targeted formatting are wrong; it's a no-op arg and `fmt` formats everything. Non-blocking but flag — common copy-paste error from other go-task projects that DO route CLI_ARGS.
- **`task proto:gen` / `task proto:lint` do NOT exist** in HoloMUSH's Taskfile. Real names: `task proto` (Taskfile.yaml:257) and `task lint:proto` (Taskfile.yaml:385). Plans for new proto modules consistently get this wrong via copy-paste from other projects. Same pattern: `task test:cover -- ./pkg/...` silently ignores the `--` arg because `test:cover` (line 87) does not interpolate `{{.CLI_ARGS}}` — it always runs whole-repo coverage.
- **`http.Server.ConnContext` + `PeerCredMiddleware` integration is NOT covered by direct `readPeerCred` tests** alone. AC text saying "PeerCredFromContext returns populated cred" is technically met by manually putting a value into context, but the wiring (`ConnContext: storeUnixConn` → middleware reads UnixConn → `readPeerCred` → context store) needs an end-to-end UDS-dial test or a regression in the wiring goes uncaught. Flag as NB when a plan tests the syscall layer in isolation and the context helper in isolation but never together.
- **CI does not run `task pr-prep` in HoloMUSH.** `.github/workflows/ci.yaml` invokes the underlying tasks directly (`task lint`, `task test:cover`, `task test:int`, `task test:e2e:cover`). Only one comment in `.github/workflows/nightly-soak.yml` mentions pr-prep. Plans claiming "CI runs task pr-prep" are wrong about CI's actual invocation pattern; the conclusions about CI behavior may still be correct but for the wrong reasons.
- **`NewManager` requires `WithVerbRegistry` (INV-GW-10).** `internal/plugin/manager.go:181-201` returns `ErrMissingVerbRegistry` if `m.verbRegistry == nil`. Plans that introduce a `newManagerForXxxTest(t, opts...)` helper passing only `WithPluginRepo` / `WithLuaHost` etc. WILL fail at the `require.NoError(t, err)` line. Always require a `WithVerbRegistry(core.NewVerbRegistry())` (or equivalent test stub). Verify by `rg -n "ErrMissingVerbRegistry|verbRegistry == nil" internal/plugin/manager.go`.
- **`Manager.TestLoadPlugin` silently no-ops without a registered host.** `manager.go:982-1001` checks `m.hosts[manifest.Type]` and falls back to `m.luaHost` for `TypeLua`. If neither is configured, the function inserts into `m.loaded` but NOT into `m.pluginHosts`. Plans that test cache mutation following `TestLoadPlugin` must either register a host (via `WithLuaHost(noOpHost)`) or accept that `pluginHosts[name]` is empty post-call. This silently breaks any logic that gates on `m.pluginHosts[name]` (e.g., `UnloadPlugin`'s early-return).
- **Cache-mutation idempotency separate from host lifecycle.** When introducing a new `Manager.UnloadPlugin` or similar lifecycle method, do NOT gate cache cleanup behind a host-loaded check. Move the `delete(m.activeByName, name)` BEFORE any `if !loaded { return nil }` early-return so cleanup is idempotent regardless of host state. Otherwise tests that pre-populate cache and call Unload (without first invoking host.Load) silently leak cache entries.
- **Bare `error` return + `defer { if err != nil ... }` doesn't work.** Functions like `loadPlugin` that return bare `error` (not named `(err error)`) do NOT propagate the final return value to deferred closures. Plans with rollback-defer patterns MUST either (a) refactor signature to named return, or (b) use an explicit `var rollback bool; ...; defer func() { if rollback { ... } }()` pattern. Plans that say "verify the signature; if not, use a different defer pattern" are deferring design — block.
- **Deferred E2E test-harness design.** Plans that introduce `setupTestEnv()`, `registerPluginInRegistry()`, `emitFromBinaryPlugin()` etc. for an end-to-end task and then say "Copy the env scaffolding... names verified at impl time" are the missing-harness antipattern in disguise. If a real harness exists somewhere (e.g., `test/integration/eventbus_e2e/`), name it and reference its types. If one doesn't, add a Task X.0 with concrete struct definitions and method bodies. Either way, the helpers MUST be defined in the plan, not deferred.
- **Repo-grounding tables at top of revised plans are highly effective.** When Revision 2 of a plan starts with "Repo grounding (verified before this plan was written)" enumerating real-vs-fabricated symbols with citations, Revision-2 review can move quickly through the surface checks and focus on second-order issues. Recommend this pattern for any plan that survived a Revision 1 NOT READY with fabricated-symbol findings.
