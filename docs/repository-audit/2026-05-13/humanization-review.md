# HoloMUSH Codebase Humanization Audit

**Date:** 2026-05-13
**Reviewer model:** Claude Opus 4.7 (1M context)
**Scope:** Read-only sample-based audit of the HoloMUSH repository for AI-slop signatures.
**Follow-up tracking:** `holomush-89o9` — epic for triaging findings into child task beads (`bd show holomush-89o9`)

## Executive Summary

The codebase is in noticeably better shape than typical AI-assisted projects. There
is no marketing-style preamble in package docs, no `It is important to note that…`
filler, and the AI-vocabulary signal is concentrated almost entirely in plans and
specs (where it has the lowest blast radius) rather than in Go source. The
production code reads like a careful engineer wrote it, even where machines
clearly did.

That said, slop is concentrated in three places:

1. **Duplicated CLAUDE.md / AGENTS.md** at the repo root — two 21,377-byte files,
   byte-for-byte identical, both checked in.
2. **`internal/world/service.go` + its 7,643-line test** — a 12-method CRUD
   surface with five near-identical Create/Get/Update/Delete blocks per entity
   type and 226 copy-pasted `engine := policytest.NewGrantEngine()` test setups.
3. **`internal/access/prefix.go`** — eight single-line resource-prefix helpers,
   each with a 4-line docstring and a defensive `panic` on empty string.

Secondary concentrations: stale plan docs in `docs/plans/` that say "Ready for
implementation" for work shipped in February; em-dash overload in
`docs/superpowers/plans/` (265 dashes in a single file); and a `NewX` /
`NewXWithLogger` constructor split in `internal/auth/` that copy-pastes 30 lines
of `if dep == nil` boilerplate twice.

**Top-3 cleanup targets**, ranked by deletable lines:

1. Replace `AGENTS.md` with a one-line symlink-or-shim to `CLAUDE.md` (saves
   21 KB of drift surface).
2. Collapse `world.Service` CRUD into a generic helper or accept that the
   repetition is intrinsic and document it once (kills ~600 LOC of test setup).
3. Merge each `NewXWithLogger` pair in `internal/auth/` into a single
   functional-options constructor (kills ~200 LOC across three services).

Beyond that, the cleanup is a long tail of small wins — tautological
godoc, stale `Ready for implementation` headers, and a handful of "rule of
three" lists in design docs.

## 1. Comment slop

### 1.1 Tautological getter/setter docstrings

`internal/session/memstore.go:34-69` is the clearest example:

```go
// Get retrieves a session by ID.
func (m *MemStore) Get(_ context.Context, id string) (*Info, error) {
// ...
// Set creates or updates a session.
func (m *MemStore) Set(_ context.Context, id string, info *Info) error {
// ...
// Delete removes a session and its associated connections.
func (m *MemStore) Delete(_ context.Context, id string) error {
```

A repo-wide ripgrep for the pattern `^// (Get|Set|Delete|List|Find|Update|Create|New|Is|Has)X (retrieves|returns|sets|creates|...)` returns **124 hits** in non-test Go files. Roughly half are load-bearing (they explain the failure mode); the other half restate the function name.

**Replace with:** delete the docstring or convert to a single sentence that says *why*, not *what*. The "// Delete removes a session and its associated connections." comment is justified — it tells you about the side-effect on `m.connections`. The "// Get retrieves a session by ID." comment is not.

### 1.2 Defensive godoc for typed helpers

`internal/access/prefix.go:60-179` defines eight helpers (`PluginSubject`, `CharacterSubject`, `LocationResource`, `ExitResource`, `ObjectResource`, `SceneResource`, `CommandResource`, `PropertyResource`, `StreamResource`, `KVResource`), each three to five lines of code wrapped in a four-line docstring. Sample:

```go
// LocationResource returns a properly formatted location resource identifier.
// It panics if locationID is empty.
func LocationResource(locationID string) string {
    if locationID == "" {
        panic("access.LocationResource: empty locationID would create invalid resource reference")
    }
    return ResourceLocation + locationID
}
```

The docstring would be useful once, on the first helper, with a `// See LocationResource for the rationale.` on the rest. Eight near-identical docblocks add line count without information.

**Replace with:** one package-level doc paragraph explaining the panic contract; reduce each helper's doc to a single line.

### 1.3 Multi-line preamble on file headers

`internal/admin/readstream/handler.go:4-21` opens with a 17-line ADR walkthrough as a file-level comment. This is good content but it's prose that belongs in `docs/adr/0017-*.md`, not duplicated above the package declaration. The same content is already in the ADR.

**Replace with:** trim to a 2-line pointer (`// AdminReadStream entry point. See ADR-0017 for the full flow.`) and let the ADR be the source of truth.

### 1.4 AI-vocabulary words in design docs

Repo-wide grep for `robust|comprehensive|elegantly|seamlessly|leverage|delve|underpin` returns **73 hits** across Go + Markdown. Distribution: 6 in tests/code (mostly fine), 67 in `docs/`. Notable offenders:

- `docs/plans/2026-01-20-error-handling-modernization.md:170` — "Acceptable risk given zero releases and comprehensive test coverage."
- `docs/plans/2026-01-23-world-model-design-improvements.md:7` — "Add comprehensive input validation to domain types."
- `docs/specs/decisions/epic7/phase-7.6/065-git-revert-migration-rollback.md:26` — "Comprehensive test coverage…"
- `docs/superpowers/plans/2026-04-26-pr-prep-concurrency-safety.md:471` — "Robust against go-task version drift."

**Replace with:** for "comprehensive," say what's covered. For "robust," name the failure mode it survives. For "leverages," use "uses." None of these words add information — they only add a tone.

### 1.5 Em-dash overuse in plans

```text
docs/superpowers/plans/2026-05-10-phase5-sub-epic-e.md:    265
docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md:    264
docs/superpowers/plans/2026-05-09-phase5-sub-epic-d.md:    242
docs/superpowers/plans/2026-05-04-legacy-id-elimination.md: 198
docs/superpowers/plans/2026-05-07-event-payload-crypto-phase5-totp-substrate.md: 135
```

These plans are 5,000–9,000 lines each. 265 em-dashes in one file is a tell.

**Replace with:** in a one-time pass, swap 80–90 % of em-dashes for periods or commas. Not all — em-dashes are fine when they actually mean a strong pause — but the average plan currently uses one every 30 lines.

### 1.6 Stale TODOs

Repo-wide grep for `^// TODO|FIXME` returns **54 hits** in Go. A few examples that look genuinely stranded:

- `internal/session/memstore.go:89` — `// TODO: filter by playerID when player-character relationship table exists.` That table now exists (see `internal/auth/player_session_store.go`).
- `internal/session/session.go:241` — duplicate of the above.
- `internal/auth/reset_service.go:213` — `// TODO: Consider making session invalidation mandatory (return error on failure).` — no bead, no link, no decision.

**Replace with:** either file a bead and reference its ID, or delete. `TODO(holomush-XXXX)` is the existing convention (used in `internal/grpc/query_stream_history_test.go:33` and the JS-storage e2e skeletons) — apply it uniformly.

## 2. Defensive overengineering

### 2.1 Double constructors (`NewX` + `NewXWithLogger`)

`internal/auth/auth_service.go:51-99` and `internal/auth/reset_service.go:27-85` each define two constructors with a 30-line `if dep == nil` block per copy:

```go
func NewAuthService(...) (*Service, error) {
    if players == nil { return nil, oops.Errorf(...) }
    if playerSessions == nil { return nil, oops.Errorf(...) }
    if hasher == nil { return nil, oops.Errorf(...) }
    // ... build svc with discard logger ...
}

func NewAuthServiceWithLogger(..., logger *slog.Logger, opts ...ServiceOption) (*Service, error) {
    // ...exact same nil checks, plus one for logger...
}
```

Same shape in `internal/auth/reset_service.go:27` (`NewPasswordResetService` / `NewPasswordResetServiceWithLogger`) and `pkg/holo/emit.go:50` (`NewEmitter` / `NewEmitterWithLogger`).

**Replace with:** single constructor that takes functional options. `WithLogger(*slog.Logger)` is already the pattern used elsewhere (`ServiceOption` in the same file). The split exists only to avoid a 5th parameter.

### 2.2 Pointless `Validate()` after a nil check

`internal/world/service.go:191-211` (`CreateLocation`) and seven sibling methods follow the shape:

```go
if loc == nil { return oops.Code("LOCATION_INVALID").Errorf("location is nil") }
if loc.ID.IsZero() { loc.ID = idgen.New() }
if err := loc.Validate(); err != nil { ... }
```

`Validate()` already rejects nil receivers and zero IDs in `internal/world/location.go:85`. The outer nil check and the auto-ID assignment are redundant gates around the inner one. Net: 4 lines of pre-validation per entity type, all entity types, repeated.

**Replace with:** push the auto-ID + nil check into `Validate()` (or a `prepareForCreate` helper), or accept it once. The current shape has the same code in 6 methods.

### 2.3 "Defensive: all call sites should use…"

`internal/world/service.go:127-138`:

```go
req, reqErr := types.NewAccessRequest(subject, action, resource, nil)
if reqErr != nil {
    // Defensive: all call sites should use typed helpers
    // (access.CharacterSubject, access.LocationResource, etc.) that panic on
    // empty input, and action strings are hardcoded literals. Kept as defense
    // in depth against future call sites that might bypass the typed helpers.
    ...
}
```

This is honest about being dead code. The comment is good, the code path is not. Per `CLAUDE.md` ("validate at boundaries only"), an internal helper consuming output of typed builders that already panic on empty input has no defensible reason to revalidate.

**Replace with:** if `NewAccessRequest` cannot return error for inputs from the typed helpers, make it not return error. If it can, fix the helpers to return error too. The current state is "this can't fail but I logged it anyway."

### 2.4 `wrapFanOutFocusError` vs `wrapFocusError`

`pkg/plugin/focus_client.go:288-330` defines two near-identical error wrappers, one with `sessionID` in scope and one without. The comment explains "Keeping a separate wrapper avoids log entries with `session_id=""`, which otherwise misleads log aggregators." Reasonable, but a single function with an optional `sessionID string` (empty means omit) is two lines shorter and removes the maintenance hazard of two parallel error-code maps.

**Replace with:** one `wrapFocusError(err, op string, sessionID string, target FocusKey)` that conditionally adds `With("session_id", …)` only when non-empty.

### 2.5 Compile-time interface assertions on internal types

`internal/session/memstore.go:32`: `var _ Store = (*MemStore)(nil)`.

This is fine where the interface is in another package (catches build breakage at the call site of a mock). Where the interface and impl are in the same package, the compile-time check is paid every build for an assertion the type-checker would already make at use site. The repo has roughly two dozen of these; about half are same-package.

**Replace with:** keep cross-package assertions; remove same-package ones.

## 3. Verbose-by-default Go

### 3.1 Single-implementation interfaces

`internal/world/service.go:30-40` defines `ServiceConfig` carrying eight repository interfaces (`LocationRepository`, `ExitRepository`, `ObjectRepository`, `SceneRepository`, `CharacterRepository`, `PropertyRepository`, plus `types.AccessPolicyEngine`, `EventEmitter`, `Transactor`). All are satisfied by exactly one production implementation. The interface boundary is there to support mocking, which is legitimate. But two of them — `Transactor` and `EventEmitter` — are passed through unchanged to one method each and could be function-typed (`type EventEmit func(ctx, evt) error`) without losing test substitutability.

`pkg/plugin/event_sink.go`, `pkg/plugin/audit.go`, `pkg/plugin/validate.go`, `pkg/plugin/sdk.go`, `pkg/plugin/focus_client.go` — each declares an interface adapter over a single concrete connect-rpc client. Necessary for the plugin SDK boundary, but several intermediate "narrow seam" interfaces (e.g., `ColdRowReader` at `internal/admin/readstream/handler.go:72`) exist only to support a unit test that is also written in the same package. Move the test back to `_internal_test.go` and drop the interface.

### 3.2 CRUD repetition in `world.Service`

`internal/world/service.go` is 1,048 lines, almost entirely:

```go
func (s *Service) GetX(ctx, sub, id) ...     { checkAccess; repo.Get;    map errors }
func (s *Service) CreateX(ctx, sub, x) ...   { checkAccess; validate;    repo.Create; map errors }
func (s *Service) UpdateX(ctx, sub, x) ...   { checkAccess; validate;    repo.Update; map errors }
func (s *Service) DeleteX(ctx, sub, id) ...  { checkAccess; tx(deps;     repo.Delete; map errors) }
```

… for X ∈ {Location, Exit, Object, Scene, Character, Property}. Six entities × four methods = 24 nearly-identical functions. The error code suffix (`LOCATION_GET_FAILED`, `EXIT_GET_FAILED`, …) and the entity prefix (`prefixLocation`, …) are the only varying parts.

Hand-rolled CRUD is not slop *per se* — generics here are a real engineering tradeoff. But the test file (`service_test.go`, 7,643 lines, 103 `TestXxx` functions) reflects the same explosion. There is one test pattern repeated 24 times.

**Replace with:** generic helper `func crud[E entityWithValidate](...)` is overkill; the simpler win is a single shared test fixture (`runCRUDSuite(t, ent worldEntityKit)`) that encapsulates the "engine + mockRepo + svc" setup the test file does 226 times.

### 3.3 Tests as duplicated setup

```bash
$ rg -c 'engine := policytest.NewGrantEngine\(\)' internal/world/service_test.go
226
```

Every subtest does:

```go
engine := policytest.NewGrantEngine()
mockRepo := worldtest.NewMockLocationRepository(t)
svc := world.NewService(world.ServiceConfig{LocationRepo: mockRepo, Engine: engine})
```

…then sets one grant and one expectation. A `newTestSvc(t, entityKind)` helper would erase 600+ lines from this one file.

**Replace with:** one `worldtest.NewServiceFixture(t)` that returns `(svc, engine, repos)` and wires the mocks. Adopt it incrementally per entity type — start with Location, prove the pattern, then convert the rest in one PR.

### 3.4 Long parameter lists that resist struct refactoring

`internal/auth/auth_service.go:51` takes four positional dependencies; the `…WithLogger` variant takes five. `internal/auth/reset_service.go:27` takes four; the `…WithLogger` variant takes five. None use a `Config` struct, despite `world.ServiceConfig` setting precedent in the same codebase.

**Replace with:** `auth.ServiceConfig{Players, PlayerSessions, Hasher, Logger}` matches the existing world-service pattern and removes the constructor split.

### 3.5 Trivial `(int) string` mappers

`internal/access/access.go:19-31` `ParseSubject` is a 13-line method that does what `strings.Cut` (Go 1.18+) does in one line:

```go
func ParseSubject(subject string) (prefix, id string) {
    if subject == "" || subject == "system" { return /* per-case */ }
    prefix, id, _ = strings.Cut(subject, ":")
    if prefix == "" { return "", subject }
    return prefix, id
}
```

Not exactly slop, but the original phrasing with `SplitN` + explicit length check reads as "I wrote this before realising `Cut` exists."

## 4. Markdown / docs slop

### 4.1 `AGENTS.md` is a byte-perfect copy of `CLAUDE.md`

```bash
$ cmp CLAUDE.md AGENTS.md && echo IDENTICAL
IDENTICAL
$ ls -l AGENTS.md CLAUDE.md
-rw-r--r-- 21377 AGENTS.md
-rw-r--r-- 21377 CLAUDE.md
```

Both files are tracked in git, not symlinks. Any edit to one risks drift from the other.

**Replace with:** `AGENTS.md` → symlink to `CLAUDE.md`, or a 5-line stub `# AGENTS.md / This file is identical to CLAUDE.md — see that file.` Pre-commit hook to fail if they diverge.

### 4.2 Stale `Ready for implementation` plan headers

`docs/plans/` has seven files marked `Ready for implementation` or `Draft` whose feature is shipped (per recent commit history and `bd` state). Examples:

- `docs/plans/2026-01-24-claude-md-refresh-design.md:3` — "**Status:** Ready for implementation" — refresh shipped Jan 24-25.
- `docs/plans/2026-01-23-world-model-bdd-tests-design.md` — BDD tests are in `test/integration/world/` and have been for months.
- `docs/plans/2026-01-18-holomush-roadmap-design.md` — 658-line "roadmap" plan that the codebase has long outgrown (references "Phase 1.6 Extism/WASM spike").

**Replace with:** move shipped plans to `docs/plans/archive/` (the convention already exists; ten files are there already), or add an explicit `> **Status: Implemented in PR #NNN**` header like `docs/plans/2026-01-24-database-migrations-implementation.md:3` does.

### 4.3 Stale WASM/Extism references outside the roadmap

`docs/plans/archive/2026-01-17-phase1-implementation.md`, `docs/plans/2026-01-23-zensical-docs-design.md:56`, `docs/plans/2026-01-24-website-landing-page-implementation.md:48-114` reference WASM as if it might still be relevant. The archive entries are fine; the live `docs/plans/` entries are doc rot.

**Replace with:** the website-landing-page plan is a shipped checklist (`- [x] No WASM references remain in site/docs`) — archive it. The zensical-docs plan has `WASM plugins` in an `mkdocs.yml` snippet that no longer exists in the live site (`mkdocs.yml` was replaced by zensical config).

### 4.4 Section title clichés

`Overview` / `Summary` / `Introduction` / `Conclusion` appear as section headers in 12+ docs (`docs/superpowers/specs/*-design.md` and `site/docs/contributing/event-store.md:11`). These are AI-template defaults. Most of these `## Overview` sections re-state the abstract; most `## Summary` sections re-state the introduction.

`site/docs/operating/telnet-security.md:122-129` is the type case: the "Summary" section is one short paragraph repeating what the page already said.

**Replace with:** delete the section header; let the doc flow without ceremony. Or rename to something content-bearing ("What this means in practice" / "The short answer").

### 4.5 Trivial-content tables

`site/docs/extending/index.md:13-15`:

```markdown
| Type   | Language | Best For                        | Compilation |
| ------ | -------- | ------------------------------- | ----------- |
| Lua    | Lua 5.1  | Simple scripts, rapid iteration | None        |
| Binary | Go       | Complex logic, external APIs    | Required    |
```

Two rows. Same content as a sentence: "There are two plugin types: Lua (scripts, no compile) and Binary (Go, compiled)." The table is more lines than the prose.

Same pattern in `docs/CLAUDE.md:9-13` ("file naming" 3-row table), various `docs/specs/2026-*` design docs.

**Replace with:** if a table has ≤2 rows or each row is a single short phrase, inline as a sentence.

### 4.6 `## Choose Your Path` / `## Community` / "Coming Soon"

`site/docs/index.md:62-105` ends with a "Choose Your Path" 4-card section, a "Community" header for a single GitHub link, and a "Coming Soon" admonition listing three unreleased features. The first is reasonable navigation; the latter two are filler.

**Replace with:** drop the "Coming Soon" block (or move to a roadmap page); inline the "Community" link into the footer.

## 5. Test slop

### 5.1 Verbose t.Run prose

`internal/world/service_test.go:48-99` per-subtest naming:

```go
t.Run("returns location when authorized", ...)
t.Run("returns permission denied when not authorized", ...)
t.Run("returns permission denied on explicit policy deny", ...)
t.Run("preserves decision context on permission denied", ...)
t.Run("returns ErrAccessEvaluationFailed when engine errors", ...)
t.Run("returns ErrAccessEvaluationFailed on infrastructure failure", ...)
```

The names are descriptive (good), but the subtests are 20-line copy-paste setups with one assertion difference each. The same 6 cases recur per entity × per operation = 24 × 6 = 144 near-clone subtests with hand-written sentences.

**Replace with:** table-driven test where the `name` field is the sentence and the `setup` / `assertErr` fields encode the difference. The pattern is already in use at `internal/totp/service_enroll_test.go:26-50` — adopt it consistently.

### 5.2 Mocks of trivial types

`internal/session/mocks/mock_Store.go` (1,390 LOC), `internal/totp/mocks/mock_Repository.go` (771 LOC), `internal/grpc/mocks/mock_*` (4 files). Mockery-generated, so the line count is automatic — but several of the mocked interfaces have only one or two methods that take primitives and return `(T, error)`. A 50-line hand-written fake would beat the 700-line mock for readability.

Repo has 32 `mock_*.go` files totalling ~12 KLOC of generated code.

**Replace with:** triage; keep mocks for interfaces with ≥4 methods or complex args. Replace tiny ones (e.g., the `GuestNamer` mock, the `GuestTransactor` mock) with hand-written fakes in `*_test.go`.

### 5.3 `// Use table-driven tests for comprehensive coverage."` (the quote that quotes the guideline)

`internal/totp/service_enroll_test.go:23-26`:

```go
// TestEnrollScenarios consolidates the Enroll cases (already-enrolled,
// success, insert-error) per repo guideline "Use table-driven tests for
// comprehensive coverage."
```

This is a comment that quotes `CLAUDE.md`. It's not wrong — the comment is documenting *why* the test exists in this shape — but the quoted line carries no information the table itself doesn't. Same pattern recurs in `cmd/holomush/gateway_test.go:318`.

**Replace with:** delete the meta-comment; the table is its own justification.

### 5.4 Placeholder ULIDs that look like real IDs

`cmd/holomush/cmd_admin_totp_reset_test.go:85` `"01JXXXXXXXXXXXXXXXXXXX0001"` is repeated four times with `…0001…0004` suffixes. `internal/plugin/event_emitter_crypto_test.go:73,93,112,131,149` repeats `"scene:01HXXXTESTSCENE000000000"` five times. Innocuous, but it shows up in the TODO grep and reads like AI placeholders that were never replaced.

**Replace with:** if these are intentional sentinels, fine. If they're "fix later" placeholders, lift them into package-level test constants so the intent is clear.

### 5.5 Half-skipped E2E tests

`test/integration/eventbus_e2e/multi_protocol_fanout_test.go:36`, `js_storage_corruption_test.go:38`, `backfill_rebuild_test.go:28`, `audit_drift_detector_test.go:36` — four integration tests that are 40-100 LOC each and start with `t.Skip("TODO(holomush-XXX): not yet implemented — skeleton retained for the follow-up bead")`.

The skeletons are legitimate (they document the wire-up the implementer will need) but they currently contribute zero coverage and add to `task test:int` output noise.

**Replace with:** either leave a single 5-line skeleton with `t.Skip` and a comment pointing at the bead, or move to `test/integration/eventbus_e2e/skeletons/` so they don't appear in normal runs.

## 6. Documentation rot

### 6.1 Stale plans tagged "Ready"

Already covered in 4.2. Seven plans in `docs/plans/` claim `Ready for implementation` for work landed in Q1 2026.

### 6.2 ADR drift

`docs/specs/decisions/epic7/` carries 95+ phase-by-phase decisions for the ABAC migration. Many of these are now historical context. The directory is well-organized (phase-7.1 through phase-7.7 subdirs) but `docs/specs/decisions/README.md` is the only navigation index and it predates several phases.

**Replace with:** add an ADR retirement field. Decisions whose constraint is now self-enforcing in code (e.g., #65 git-revert-rollback) should be marked `Status: superseded by code` so newcomers don't reread them as live policy.

### 6.3 Three CLAUDE.md files + AGENTS.md duplicate

`./CLAUDE.md` (21,377 B), `./AGENTS.md` (identical), `./docs/CLAUDE.md` (155 lines), `./site/CLAUDE.md` (72 lines), `./web/CLAUDE.md` (168 lines). The scoped CLAUDE.md files are fine (they apply per-subtree). The root duplicate with AGENTS.md is pure overhead.

### 6.4 `docs/plans/archive/` has fossil docs that reference removed systems

`docs/plans/archive/2026-01-18-extism-plugin-framework-design.md` — describes the Extism/WASM plugin model that was abandoned for gopher-lua + go-plugin. Archived correctly, but the file is still 2,000+ lines and shows up in repo greps. It is doing nothing for anyone.

**Replace with:** prune the archive after 6 months. Git history is forever; the working tree isn't.

### 6.5 `docs/superpowers/plans/` has 91 plans, mostly large

```text
$ ls docs/superpowers/plans/ | wc -l
91
```

These are AI-generated implementation plans (per `CLAUDE.md`). Several appear to be intermediate versions of the same epic (`2026-05-02-event-payload-crypto-phase3a*`, `…phase3b*`, `…phase3c*`, `…phase3d*`, plus separate `-grounding`, `-design`, and `-decomposition` files). They are heavy on em-dashes (top file: 265).

**Replace with:** after an epic ships, collapse the phase plans into one retrospective. Or move shipped plans to `docs/superpowers/plans/archive/` mirroring the convention in `docs/plans/`.

## Quick Wins (top 20, ranked by LOC impact)

| Rank | Target                                                                              | Est. LOC reduced |
| ---- | ----------------------------------------------------------------------------------- | ---------------- |
| 1    | `AGENTS.md` → symlink/stub to `CLAUDE.md`                                           | ~700             |
| 2    | Collapse `world.service_test.go` setup into `newTestSvc(t)` fixture                 | ~600             |
| 3    | Archive seven stale `docs/plans/*.md` `Ready for implementation` files              | ~3,000 (moved)   |
| 4    | Merge `NewAuthService` + `NewAuthServiceWithLogger` via `ServiceConfig`             | ~80              |
| 5    | Merge `NewPasswordResetService` + `…WithLogger`                                     | ~70              |
| 6    | Merge `NewEmitter` + `NewEmitterWithLogger`                                         | ~30              |
| 7    | Trim `internal/access/prefix.go` repeated 4-line docstrings to 1 line               | ~50              |
| 8    | Merge `wrapFocusError` + `wrapFanOutFocusError`                                     | ~25              |
| 9    | Delete dead "Defensive:" comment + branch in `world.Service.checkAccess`            | ~12              |
| 10   | Strip tautological godoc on `MemStore.Get/Set/Delete/...`                           | ~40 (across repo) |
| 11   | Remove `Validate()` redundancy after nil + auto-ID checks in `world.Service`        | ~30              |
| 12   | Remove same-package `var _ Interface = (*Impl)(nil)` lines                          | ~25              |
| 13   | Inline `## Summary` / `## Overview` sections that restate prior content             | ~150             |
| 14   | Drop trivial 2-row tables in `site/docs/extending/index.md` and similar             | ~80              |
| 15   | Replace `internal/access/ParseSubject` body with `strings.Cut`                      | ~10              |
| 16   | Remove "Coming Soon" + redundant "Community" section in `site/docs/index.md`        | ~15              |
| 17   | Replace "// per repo guideline …" meta-comments in tests                            | ~10              |
| 18   | Trim ADR-style file-header narratives that duplicate the ADR (`readstream/handler`) | ~30              |
| 19   | Delete `internal/session/memstore.go:89` stale TODO (table exists)                  | ~3               |
| 20   | One-pass em-dash sweep in `docs/superpowers/plans/` (mechanical)                    | n/a              |

## Bigger Cleanups (needs refactor, not delete)

1. **`world.Service` CRUD compression.** Either accept the explicit 24 methods as
   load-bearing for readability (and document the choice in a comment at the top
   of the file) or introduce a shared internal helper. The test file is the real
   tax — the production methods are mostly fine.

2. **`internal/access/prefix.go` panic discipline.** The eight `Resource()` /
   `Subject()` helpers all panic on empty. That's defensible at the public API
   boundary but it's also the reason `world.Service.checkAccess` has the
   "Defensive:" dead branch. Pick one: either the helpers can fail (return
   `(string, error)`) and the engine validates inputs, or they cannot fail and
   the engine trusts inputs. Today the codebase has both.

3. **Mockery audit.** 32 mock files, ~12 KLOC generated. Several of the mocked
   interfaces have one or two methods and could be hand-written fakes inside
   the relevant `_test.go`. Triage and prune; the win is comprehensibility
   more than line count.

4. **Plan archive lifecycle.** Define a rule: a plan stays in `docs/plans/`
   while work is open; on close (or 30 days post-merge), it moves to
   `docs/plans/archive/`. Same for `docs/superpowers/plans/`. The archive then
   gets pruned annually. This is a process change, not a code change, but it
   prevents the doc rot from re-accumulating.

5. **AGENTS.md drift prevention.** Replace the file with a symlink or pre-commit
   hook that fails on divergence. Either is one line; the current state is two
   21 KB files that will silently drift.

## Closing Recommendation

**This is rolling-cleanup territory, not a mega-PR.**

The codebase is too healthy to justify a 60-file de-slop PR. The patterns are
distributed, not concentrated, and most are 3–30 lines each. A single PR that
touches `internal/world/`, `internal/auth/`, `internal/access/`, `pkg/plugin/`,
plus 10+ docs would be impossible to review and would conflict with every
in-flight epic.

The right shape is:

1. **One small PR** to fix `AGENTS.md` (symlink or stub + hook). 5 minutes,
   blocks future drift. Do this first.
2. **One epic** ("repo de-slop") with one bead per category:
   - `de-slop-1`: `world.Service` test fixture (#2 in quick wins).
   - `de-slop-2`: Auth/Reset/Emitter constructor mergers (#4–6).
   - `de-slop-3`: `access/prefix.go` doc trim + panic-discipline decision (#7,
     plus bigger-cleanup item 2).
   - `de-slop-4`: Stale plan archive sweep (#3, plus bigger-cleanup item 4).
   - `de-slop-5`: Tautological godoc + filler section pass (#10, 13, 17, 18).
   - `de-slop-6`: Em-dash + AI-vocabulary sweep in `docs/superpowers/plans/` —
     this is the biggest content delta but the lowest review risk (no code).
3. **No follow-up.** Once the epic closes, document the patterns in
   `CLAUDE.md` ("Don't write `NewXWithLogger`; use functional options") so the
   slop doesn't reaccumulate.

The repo is in good shape. The cleanup is real but optional — none of it is a
correctness bug, none of it is a security risk. Treat it as gardening: do a
little, regularly, and the codebase stays readable. Don't treat it as a
rescue mission; it doesn't need one.
