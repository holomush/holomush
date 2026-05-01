# plan-reviewer agent memory

This file accumulates HoloMUSH-specific patterns of good and bad plans
discovered during adversarial plan review. Entries are added by the agent
itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

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
- **CI does not run `task pr-prep` in HoloMUSH.** `.github/workflows/ci.yaml` invokes the underlying tasks directly (`task lint`, `task test:cover`, `task test:int`, `task test:e2e:cover`). Only one comment in `.github/workflows/nightly-soak.yml` mentions pr-prep. Plans claiming "CI runs task pr-prep" are wrong about CI's actual invocation pattern; the conclusions about CI behavior may still be correct but for the wrong reasons.
