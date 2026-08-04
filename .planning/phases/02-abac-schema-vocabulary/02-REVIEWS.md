---
phase: 2
reviewers: [codex]
reviewed_at: 2026-08-04T03:03:23Z
plans_reviewed: [02-01-PLAN.md, 02-02-PLAN.md, 02-03-PLAN.md, 02-04-PLAN.md, 02-05-PLAN.md, 02-06-PLAN.md, 02-07-PLAN.md, 02-08-PLAN.md, 02-09-PLAN.md, 02-10-PLAN.md, 02-11-PLAN.md, 02-12-PLAN.md]
---

# Cross-AI Plan Review — Phase 2

**Verdict: NOT READY — REVISE.** One external reviewer (Codex, `gpt-5.6-sol`,
`model_reasoning_effort=high`) reviewed all 12 plans against the source tree at
`4b3a820`. It raised **17 distinct HIGH-severity blockers** and ~21 actionable
MEDIUM/LOW items. Every HIGH was independently re-verified by the orchestrator
against the repo before being recorded here; all 17 hold.

## How this review was run

The 12 plans total ~365 KB, which does not fit one prompt at a depth that would
produce grounded findings, so Codex was invoked **twice** through the standard
`codex exec` lane — plans 02-01…02-06, then 02-07…02-12. Both passes received the
full `PROJECT.md` excerpt, the Phase 2 roadmap section, `REQUIREMENTS.md`, all 26
locked decisions from `02-CONTEXT.md`, and read/write access to the repository so
they could check every `path:line` claim. `02-RESEARCH.md`, `02-PATTERNS.md` and
`02-VALIDATION.md` were left on disk and referenced by path rather than inlined.

Two deviations from the default lane are worth recording:

- **Reasoning effort was raised from `low` to `high`.** The `adaptive` model
  profile's fast-mode resolves the Codex lane to `model_reasoning_effort=low`. A
  low-effort pass over 12 dense plans would produce the impressionistic review the
  prompt explicitly warns has no value — and, worse, a false green. The argv is
  otherwise exactly the lane descriptor's.
- **Only one reviewer ran** (`--codex`), so the "Consensus" section below is a
  single-reviewer verdict plus the orchestrator's independent verification. It is
  not cross-model agreement, and should not be read as such.

---

## Codex Review

### Part 1 — plans 02-01 through 02-06

#### Executive summary

The plans contain several unusually strong design choices: the opaque `charname.Admitted` token, AST-derived admission census, compile-then-swap block-list cache, RED-first gates, and `SET NOT NULL` before the unique index. Migration numbers `000054`–`000056` are sequential and do not collide with the current highest migration, `000053`.

The set is nevertheless **not ready to execute**. I found six blockers:

1. Plan 02-01 assigns database persistence and gate wiring to `CharacterService`, but that service has neither a pool nor a writer; it delegates to `CharacterGenesisService`.
2. Plan 02-02 permanently forbids the imports Plan 02-06 explicitly introduces.
3. Plan 02-03’s player-role lookup cannot be wired through the current `ABACConfig.RoleStore` interface as specified.
4. Plan 02-04 would bind an invariant whose “only deletion path” claim is already false.
5. Plan 02-05 can silently interpret malformed block-list JSON as an absent/empty list and wires a long-running poller into a one-shot subsystem.
6. Plan 02-06 switches existence checks to a nullable column several plans before the backfill and constraint land.

Overall risk: **HIGH**.

---

#### 02-01-PLAN.md

##### Summary

The Unicode pipeline and schema design are sound, but the production tracer is attached to the wrong layer. As written, Task 2 cannot persist the three derived identity columns or construct a production `Gate` from `CharacterService`.

##### Strengths

- Replacing title-casing is correct. The existing normalizer lowercases and title-cases each word at `internal/world/validation.go:107`, so the proposed display/key split fixes a real lossy behavior.
- The existing validator already supports Unicode letters and enforces the strict letters-and-spaces shape at `internal/world/validation.go:59`; keeping that validation separate from normalization is appropriate.
- `000054` is the correct next migration number: the corpus currently ends at `000053_sessions_location_index.sql`. Goose embedding and provider setup are already live at `internal/store/migrate.go:27` and `internal/store/migrate.go:119`.
- The plan correctly keeps `normalized_name` nullable until the later backfill and constraint migration at `02-01-PLAN.md:199`.

##### Concerns

- **HIGH — 02-01-PLAN.md:** The plan says `createWithMaxAndBind` will “persist” the display, key, skeleton, and Unicode version at `02-01-PLAN.md:220`, but `CharacterService` has only read repositories and a genesis delegate at `internal/auth/character_service.go:22` and `internal/auth/character_service.go:56`. The real insert is in `internal/world/postgres/character_repo.go:52`, reached through `internal/auth/character_genesis.go:162`. None of those writer files is owned by Plan 02-01. This tracer cannot meet its persistence acceptance criteria as scoped.
- **HIGH — 02-01-PLAN.md:** “Wire a `SkeletonLookup` over the same pool” at `02-01-PLAN.md:226` omits both production composition roots. Runtime construction occurs at `cmd/holomush/sub_grpc.go:410`, while bootstrap creates a second service at `internal/bootstrap/setup/subsystem.go:171`. Neither is in the plan’s file list.
- **MEDIUM — 02-01-PLAN.md:** Routine regeneration downloads security data directly from a versioned URL but specifies no content digest at `02-01-PLAN.md:161`. Version-addressing prevents accidental upgrades, but not upstream replacement or network-dependent `task generate` failures.

##### Suggestions

- Choose one coherent ownership model:

  - Move the production writer-boundary portion of Plan 02-06 ahead of this tracer, then have Plan 02-01 use `Gate.Admit` and pass the token through genesis; or
  - Reduce Plan 02-01 to the pure package, migration, and DB-backed gate integration test, explicitly deferring production create-path persistence to 02-06.

- If production wiring remains here, add at least:

  - `internal/auth/character_genesis.go`
  - `internal/world/postgres/character_repo.go`
  - `internal/bootstrap/setup/adapters.go`
  - `internal/bootstrap/setup/subsystem.go`
  - `cmd/holomush/sub_grpc.go`
  - constructor tests and integration harness wiring

- Commit the pinned `confusables.txt` input or pin its SHA-256 and have the generator verify it before emitting code.

##### Risk assessment

**HIGH.** The pure Unicode work is viable, but the stated end-to-end tracer cannot compile and persist through the current architecture.

---

#### 02-02-PLAN.md

##### Summary

The mixed-script table and username regression coverage are well targeted. The package-wide import-separation guard, however, directly contradicts the later structural admission design.

##### Strengths

- The plan preserves the existing character validation contract instead of silently widening it. That contract is explicit at `internal/world/validation.go:59`.
- IDENT-08 correctly pins rather than rewrites the existing ASCII username policy, which is exactly `^[a-zA-Z][a-zA-Z0-9_]*$` at `internal/auth/player.go:28`.
- Whole-script non-Latin positive controls and `Common`/`Inherited` cases are good safeguards against accidentally converting the security rule into an English-only policy.
- Short-circuiting mixed-script rejection before the skeleton database lookup is a useful performance and verification property.

##### Concerns

- **HIGH — 02-02-PLAN.md:** Task 3 requires that no file under `internal/auth` import `internal/charname` at `02-02-PLAN.md:232` and makes that a lasting success criterion at `02-02-PLAN.md:331`. Plan 02-06 explicitly requires both `internal/auth/character_service.go` and `internal/auth/guest_service.go` to call `Gate.Admit` at `02-06-PLAN.md:378` and `02-06-PLAN.md:384`. The Plan 02-02 test will therefore fail by design in wave 4.

##### Suggestions

Replace the package-wide import ban with narrower, durable assertions:

- `internal/charname` MUST NOT import `internal/auth`.
- `internal/auth/player.go` and its username-validation path MUST NOT call or import `charname`.
- Character creation services MAY import `charname`, because character-name admission is their intended responsibility.

An AST check over `player.go` or the `ValidateUsername` call graph would protect the two-policy distinction without prohibiting legitimate character admission.

##### Risk assessment

**HIGH.** The feature implementation is plausible, but the permanent guard makes the full plan set internally inconsistent.

---

#### 02-03-PLAN.md

##### Summary

The prefix and provider design follows existing ABAC conventions closely. Player-role wiring and one verification claim need redesign before execution.

##### Strengths

- Registering `viewer:`, `profile:`, and `admin_section:` in `knownPrefixes` is necessary because `ParseEntityRef` actually validates against that list at `internal/access/prefix.go:45` and `internal/access/prefix.go:190`.
- The anonymous `player_id` omission follows the established fail-closed provider pattern. `PropertyProvider` explains and implements omission rather than empty-string sentinels at `internal/access/policy/attribute/property.go:88`.
- The player-scoped role semantics match the existing `PlayerHasRole` join over all characters owned by the player at `internal/store/role_store.go:83`.
- Adding a direct guard that `PropertyProvider` still emits `name` is worthwhile; it currently does so at `internal/access/policy/attribute/property.go:79`.

##### Concerns

- **HIGH — 02-03-PLAN.md:** The plan adds `PlayerRoles` only to concrete `PostgresRoleStore`, then says `setup.go` will wire it at `02-03-PLAN.md:264` and `02-03-PLAN.md:279`. `BuildABACStack`, however, receives `RoleStore store.RoleStore` at `internal/access/setup/setup.go:89`, and that interface lacks `PlayerRoles` at `internal/store/role_store.go:14`. The specified wiring cannot compile without either broadening the interface—and updating every fake—or adding a separate lookup seam.
- **MEDIUM — 02-03-PLAN.md:** The proposed “no viewer WARN” RED demonstration is vacuous in this wave. The plan explicitly says viewer policies are not seeded until 02-07 at `02-03-PLAN.md:53`, while the validator only scans `policy.SeedPolicies()` at `internal/access/setup/seed_coverage.go:28`. Removing `ViewerTierProvider` in Plan 02-03 will not produce a viewer warning because no current seed references it.

##### Suggestions

- Add `PlayerRoleLookup attribute.PlayerRoleLookup` to `ABACConfig`, then populate it from the concrete store in `internal/access/setup/subsystem.go:95`. Add `subsystem.go` and its tests to this plan’s ownership.
- Alternatively, deliberately extend `store.RoleStore` and enumerate every fake/mock that must implement the new method.
- Keep the direct real-resolver test in this plan, but move the viewer seed-coverage RED/no-WARN assertion to Plan 02-07, after viewer-referencing seeds exist. A synthetic seed may be used here if the earlier RED proof is important.

##### Risk assessment

**HIGH.** ABAC semantics are sound, but the role lookup is not expressible through the stated configuration surface.

---

#### 02-04-PLAN.md

##### Summary

The lifecycle vocabulary and direct-idle test are strong. The plan must not bind `INV-WORLD-6` in its current form because the registry’s “only delete path” assertion contradicts shipped code.

##### Strengths

- `SelectCharacter` currently checks ownership but has no lifecycle eligibility check at `internal/grpc/auth_handlers.go:265`, so routing it through a fail-closed `Selectable` function closes a real gap.
- Directly inserting `idle` is the correct non-vacuous way to prove the unreachable third state.
- Pairing idle/retired denial with an active control, and retire-preserves-name with delete-releases-name, follows the requested verification-integrity discipline.
- The Docker fallback correctly refuses to mark invariants bound when their integration proof was not executed.

##### Concerns

- **HIGH — 02-04-PLAN.md:** `INV-WORLD-6` currently says `world.Service.DeleteCharacter` is the **only** path that releases a name at `docs/architecture/invariants.yaml:5074`. That is already false: `CharacterReapingService` is documented as a second sanctioned deletion writer at `internal/auth/character_reaping.go:90` and deletes character rows at `internal/auth/character_reaping.go:239`. The proposed retire-vs-`world.Service.DeleteCharacter` test at `02-04-PLAN.md:192` cannot genuinely assert exclusivity. Binding it would create a false registry guarantee.
- **MEDIUM — 02-04-PLAN.md:** The claim that `character_repo.go` has five full character `SELECT` lists is false at `02-04-PLAN.md:40`. There are three full-row reads—Get, GetByLocation, ListByPlayer—at `character_repo.go:30`, `character_repo.go:155`, and `character_repo.go:175`. `GetNamesByIDs` and `ListAll` are intentionally partial projections at `character_repo.go:366` and `character_repo.go:401`. “Every repository read populates status” is therefore also false unless those APIs are deliberately changed.
- **MEDIUM — 02-04-PLAN.md:** The existing world integration environment exposes repositories only at `test/integration/world/world_suite_test.go:50`. The plan requires the real authorized `world.Service.DeleteCharacter` path but does not specify wiring its access engine, mutator, transactor, property repository, and outbox.

##### Suggestions

- Amend the invariant before binding it. A truthful form would be: retirement never releases the name; only sanctioned tombstone-emitting hard-delete paths do. Enumerate both `world.Service.DeleteCharacter` and `CharacterReapingService`, or scope the invariant explicitly to non-guest deletion.
- Add a guest-reaping release proof if exclusivity across all deletion paths remains part of the guarantee.
- Replace “five SELECTs/every read” with an explicit inventory:

  - Full entity reads MUST populate lifecycle fields.
  - ID/name-only projections MAY omit them and MUST NOT make lifecycle decisions.

- Extend the world integration fixture with the real service dependencies or relocate the proof to a harness that already constructs the production world stack.
- Use the existing `task invariants:render` task rather than raw `go run ./cmd/inv-render`.

##### Risk assessment

**HIGH.** Binding a known-false invariant is a hard blocker.

---

#### 02-05-PLAN.md

##### Summary

The two-signal polling correction is excellent, but the configuration loader and lifecycle wiring have fail-open and liveness holes.

##### Strengths

- `(updated_at, hash(value))` is the correct correction for direct SQL edits. The supported setter updates `updated_at` at `internal/store/postgres.go:84`, while a direct SQL `UPDATE value=…` does not.
- The design closely follows a proven in-tree cache: immutable snapshots at `internal/access/policy/cache.go:24` and compile-before-swap retention of last-good state at `internal/access/policy/cache.go:126`.
- Immediate first poll, retry after failed initial reload, and a 10-second default match the established poller at `internal/access/policy/poller.go:82` and `internal/access/policy/poller.go:101`.
- The direct-SQL integration test is specifically capable of catching the inadequate timestamp-only implementation.

##### Concerns

- **HIGH — 02-05-PLAN.md:** The plan tells `Cache.Reload` to read the settings value at `02-05-PLAN.md:209` after explicitly citing `StringSliceN`. That API returns `(nil,false)` for malformed JSON at `internal/settings/game.go:120`, and the interface contract treats scalar/non-array values the same way at `internal/settings/settings.go:40`. If `false` is treated as the permitted “absent row means empty list,” a malformed direct-SQL edit silently disables the block list instead of failing startup/reload.
- **HIGH — 02-05-PLAN.md:** The plan wires the poller into `BootstrapSubsystem` at `02-05-PLAN.md:187`, but that subsystem is explicitly one-shot and has no-op `Activate` and `Stop` methods at `internal/bootstrap/setup/subsystem.go:238`. A long-running poll loop placed there either never runs or has no lifecycle/shutdown owner.
- **HIGH — 02-05-PLAN.md:** Production character and guest services are independently constructed in `cmd/holomush/sub_grpc.go:410` and `cmd/holomush/sub_grpc.go:465`. Modifying only the bootstrap subsystem cannot make the live runtime paths consume the cache.
- **MEDIUM — 02-05-PLAN.md:** The gate consumes a `Match` interface, but Task 2 says to hand it the cache’s “snapshot accessor” at `02-05-PLAN.md:231`. Capturing one startup snapshot means later reloads never affect the gate; a function accessor does not implement the stated `Match` interface.

##### Suggestions

- Implement a strict loader over raw `GetSystemInfo`:

  - missing row → empty list;
  - valid JSON array → compile;
  - malformed JSON, scalar, or wrong element type → error and retain last-good snapshot.

- Give the block-list poller its own lifecycle subsystem, or attach it to an existing long-running auth/core subsystem with `Activate(ctx)` and cancellation-aware `Stop`.
- Add all runtime composition roots and lifecycle tests to the file list.
- Make `Cache` itself implement `Match` by reading the current snapshot for every evaluation, or provide a `LiveMatcher` adapter. Add a test that reloads the cache and proves an already-constructed `Gate` sees the new list.

##### Risk assessment

**HIGH.** The current plan can either fail to update production behavior or fail open on malformed configuration.

---

#### 02-06-PLAN.md

##### Summary

This is architecturally the strongest plan in the set. The opaque token, writer-boundary restriction, and AST census are excellent. Cross-plan contradictions, premature query cutover, and an incomplete signature-change census make it unsafe to execute as written.

##### Strengths

- Removing name writes from generic `Update` closes a real hole: the method currently writes `name` without any admission token at `internal/world/postgres/character_repo.go:77`.
- The opaque `Admitted` token is substantially stronger than a call-site convention. Coupling it with a single-constructor census gives both compile-time prevention and a regression backstop.
- The new census correctly complements, rather than weakens, the existing raw-SQL fence. That fence parses Go string literals at `test/meta/world_sql_fence_test.go:80` and permits mutations only within `internal/world/postgres` at `test/meta/world_sql_fence_test.go:132`.
- The plan catches a genuine vacuous-test regression. The current stale-CAS test witnesses only `name` at `character_repo_test.go:729`; once `Update` stops writing `name`, that assertion would stay green even if the CAS failed. Re-witnessing on description and version is exactly right.

##### Concerns

- **HIGH — 02-06-PLAN.md:** This plan intentionally adds `internal/auth → internal/charname` calls at `02-06-PLAN.md:378`, while Plan 02-02 installs a test forbidding that import at `02-02-PLAN.md:243`. The wave-4 test suite cannot be green.
- **HIGH — 02-06-PLAN.md:** Task 3 changes all existence checks to `normalized_name = $1` at `02-06-PLAN.md:391`, but Plan 02-01 deliberately leaves the column nullable at `02-01-PLAN.md:206`, and the backfill does not land until Plan 02-12’s `000055` at `02-12-PLAN.md:146`. Existing characters therefore become invisible to the friendly existence check during the intermediate state. The old query currently sees them through `LOWER(name)` at `internal/bootstrap/setup/adapters.go:38`.
- **HIGH — 02-06-PLAN.md:** The `Create` signature change has more callers than the file list acknowledges. Examples outside the owned set include `test/integration/auth/guest_reaper_race_test.go:84` and multiple genesis tests. Production constructor changes also require `cmd/holomush/sub_grpc.go:436`, which is absent from the declared ownership at `02-06-PLAN.md:7`.
- **MEDIUM — 02-06-PLAN.md:** The guest action says `acquireUniqueName` obtains an `Admitted` value, but the current method returns only `string` at `internal/auth/guest_service.go:218`, and `CreateGuest` later reconstructs the display name at `internal/auth/guest_service.go:129`. The plan never explicitly changes the return signature, so the token needed by genesis has no defined path out of the retry loop.
- **LOW — 02-06-PLAN.md:** The action instructs the executor to run raw `go build ./...` at `02-06-PLAN.md:324`, contrary to the repository’s task-only build convention. It also does not compile integration-tagged callers.

##### Suggestions

- Narrow the Plan 02-02 guard as described above.
- Move the `ExistsByNormalizedName` query cutover into Plan 02-12, in the same atomic execution unit as the backfill and constraint. Plan 02-06 may introduce the method name and token boundary, but it must not make legacy NULL rows invisible.
- Run a complete call-site census before fixing signatures and add every compile target to `files_modified`, including production roots, integration reaper tests, and genesis tests.
- Change guest acquisition explicitly to something like:

  ```go
  acquireUniqueName(ctx) (rawName string, admitted charname.Admitted, err error)
  ```

  Thread the returned token unchanged into `genesis.Create`.
- Replace `go build ./...` with `task build`, followed by full `task test` and `task test:int`.
- Use `task mocks:generate`, which already exists at `Taskfile.yaml:649`.

##### Risk assessment

**HIGH.** The core design is excellent, but the current wave ordering creates an invalid intermediate data state and the signature migration is incomplete.

---

#### Plan-set assessment

##### Summary

The plans demonstrate strong security thinking and verification discipline, but their cross-plan interface model has drifted. Individual plans are internally detailed while the shared composition, lifecycle, and migration cutover paths are not treated as one atomic dependency graph.

##### Strengths

- Migration numbering is clean: existing SQL migrations stop at `000053`, and the proposed `000054`–`000056` sequence is collision-free.
- The goose model is correctly understood. SQL files are embedded at `internal/store/migrate.go:27`, while Go migrations require explicit registration as documented at `internal/store/migrations/doc.go:23`.
- Plan 02-12’s `SET NOT NULL` before unique-index ordering is correct and safely makes an omitted/failed backfill visible.
- Optional ABAC attributes consistently follow omit-don’t-sentinel.
- The AST census, symmetric-difference checks, paired controls, RED demonstrations, and vacuity analysis are materially better than ordinary plan-level testing.
- The scoped `task test -- …` and `task test:int -- …` commands are valid: both tasks consume `CLI_ARGS` at `Taskfile.yaml:185` and `Taskfile.yaml:265`.

##### Concerns

- **HIGH — plan set:** Production composition is not modeled as a shared dependency. Character services are built independently for bootstrap and runtime, and guest services add a third constructor seam. Plans 02-01, 02-05, and 02-06 do not own all of those roots.
- **HIGH — plan set:** Plan 02-02’s import guard and Plan 02-06’s admission implementation are mutually exclusive.
- **HIGH — plan set:** The registry would be updated to claim a deletion-path invariant that current guest-reaping code disproves.
- **HIGH — plan set:** Name lookup moves to `normalized_name` before existing rows are backfilled. This is precisely the kind of intermediate broken state that makes plan-by-plan commits unsafe to bisect or execute independently.
- **HIGH — plan set:** Malformed block-list JSON can collapse into the same result as absent configuration unless a strict loader is specified.
- **MEDIUM — plan set:** Even after moving the lookup cutover, the 000055/000056 deployment should declare whether old writers are quiesced. `SET NOT NULL` and the unique index fail safely if old binaries insert NULLs or duplicates, but the rollout can repeatedly fail until every old writer is stopped.

##### Suggestions

Before execution, revise the dependency graph around three atomic seams:

1. **Admission seam**

   - Pure normalization/generator.
   - `Admitted` token and writer boundary.
   - All constructor/composition roots.
   - Only then production create/guest tracer tests.

2. **Legacy-data cutover seam**

   - Backfill and collision report.
   - Operator resolution.
   - `SET NOT NULL`.
   - Unique index.
   - Existence-query cutover.
   - Explicit rollout/quiescence note for old writers.

3. **Block-list lifecycle seam**

   - Strict raw settings loader.
   - Live cache implementing `Match`.
   - Dedicated lifecycle owner for polling.
   - Injection into bootstrap, runtime character creation, guest creation, and integration harnesses.

Also amend `INV-WORLD-6` before any binding edit, and move viewer seed-coverage verification to the wave where viewer policies actually exist.

##### Overall risk assessment

**HIGH — not ready for execution.**

The issues are fixable without abandoning the architecture. Most are plan-boundary and dependency corrections, not design failures. Once the six blockers above are resolved, the resulting plan set should be substantially safer than the current implementation because its core structural mechanisms—opaque admission, writer fencing, exhaustive lifecycle decisions, and fail-closed compiled snapshots—are strong.
---

### Part 2 — plans 02-07 through 02-12

#### Executive summary

Outer-gate verdict: **REVISE**.

The plans are unusually strong on threat modeling, non-vacuous tests, RED-first demonstrations, and default-deny reasoning. However, several source-grounded blockers remain:

- The viewer-flavored property policies compare player identity against character-keyed property data and have no viewer-role attribute for the admin case.
- The admin registry validator is not actually wired into startup.
- The exposure audit can be marked complete when the database is unavailable.
- The name migration has a live-write window between backfill and constraint creation.
- The uniqueness RED test targets a schema that cannot run the post-02-06 create path.
- The operator CLI is never added to the root Cobra command.

Overall risk: **HIGH** until those issues are repaired in the plans.

---

#### 02-07 — Seed policy family

##### Summary

The policy-family decomposition is sound, especially the separate actions for the tier-floor and row-level terms. The viewer-twin identity model is not implementable as specified, however, and the empty player-floor policy conflicts with the current DSL grammar.

##### Strengths

- Separating term A and term B by action is correct. The engine filters applicable policies by action and resource type before evaluation (`engine.go:368`), then combines satisfied permits disjunctively (`engine.go:591`). The proposed `read_profile_attribute` versus `read` split prevents accidental OR semantics.
- Keeping the public-read widening restricted to `property` and `character` resources is safer than a bare-resource policy. Resource-type targeting is an actual engine mechanism (`engine.go:374`).
- The no-write-twin prohibition correctly preserves default-deny around mutations; the existing owner-write seed is distinct from the five read-side policies (`seed.go:129`).

##### Concerns

- **HIGH — 02-07-PLAN.md:239-255:** The viewer twins cannot preserve the existing row semantics using `principal.viewer.player_id`. Private ownership is currently compared to `principal.character.id` (`seed.go:117`); `visible_to` and `excluded_from` are explicitly character-ID lists (`03-property-model.md:43`). By contrast, `ViewerTierProvider` exposes only `tier`, `player_id`, and `has_player_id` (`01-SPEC.md:1365`). Comparing a player ID against character IDs will false-deny private and restricted fields.

  The admin twin is worse: the viewer namespace has no roles attribute at all. A `principal.viewer.roles` condition either fails schema compilation or can never resolve. The player-role provider cannot repair this because it resolves `player:` subjects, while term B uses a `viewer:` subject.

- **MEDIUM — 02-07-PLAN.md:150-174:** The plan requires exactly three floor policies but permits omitting the player policy when its member list is empty. The current seeded table indeed has no player-floor property (`01-SPEC.md:1548`), and the DSL list grammar requires at least one literal (`ast.go:232`). Execution will therefore violate either the three-policy truth or the grammar.
- **LOW — 02-07-PLAN.md:** The objective describes eleven new entries, but the artifact list contains twelve: three floors, reachability, five twins, two widenings, and admin access.

##### Suggestions

1. Amend plan 02-03/02-07 to define real viewer authorization attributes. At minimum, settle:

   - how a player viewer proves ownership of a character-keyed property;
   - how character-ID `visible_to`/`excluded_from` lists apply to a player with multiple characters;
   - how admin role membership is exposed under a `viewer:` subject.

   Do not substitute `player_id` for character IDs.

2. Resolve the empty player floor explicitly. Either add a DSL-supported always-false condition such as `when { false }`, or amend D-03 and the must-have to allow no seed entry when a rung has no members.

3. Correct the artifact count.

##### Risk assessment

**HIGH** — the proposed viewer policies cannot represent private, restricted, or admin semantics with the attributes being shipped.

---

#### 02-08 — Profile visibility conjunction helper

##### Summary

The two-call conjunction and its RED-first tests are well conceived, but the plan depends on inaccessible test helpers and inherits 02-07’s broken identity mapping.

##### Strengths

- Testing all four A/B permit combinations is the right proof that conjunction occurs in the caller rather than the engine.
- Reachability-first short-circuiting and propagating evaluation failures distinguish “withheld” from “not evaluated,” matching the engine’s error-bearing API (`types.go:473`).
- The fourth-rung and term-B-removed demonstrations are high-value mutation-style RED gates, not merely happy-path tests.

##### Concerns

- **HIGH — 02-08-PLAN.md:163-178:** The tests are directed to use `createSeedEngine` and `viewerProvider` from `internal/access/policy/seed_smoke_test.go`, but plan 08 owns only `internal/access/profilevis/*` files. `createSeedEngine` is unexported, declared in a `_test.go` file, and belongs to package `policy` (`seed_smoke_test.go:4`, `seed_smoke_test.go:21`). A separate `profilevis` package cannot use it.
- **HIGH — 02-08-PLAN.md:232-263:** Real-corpus tests can be made falsely green if fixtures put player IDs into `owner`, `visible_to`, or `excluded_from`. Production rows use character IDs for those semantics. The test plan must exercise the settled player-to-character mapping, not redefine row contents to match the faulty policy.
- **MEDIUM — 02-08-PLAN.md:120-122:** The documented call form `Evaluate(viewerSubject, action, resource)` does not exist. The engine signature is `Evaluate(ctx, types.AccessRequest)` (`engine.go:80`), with request construction defined at `types.go:139`. The production helper and recording double need to use that exact interface.

##### Suggestions

1. Add a reusable real-engine test builder in a non-`_test.go` test-support package, or specify local builders/providers in plan 08’s owned test files.
2. Amend behavior fixtures to use real character-keyed ownership/restriction data after the viewer mapping is settled.
3. Replace every positional `Evaluate` example with exact `types.NewAccessRequest` plus `Evaluate(ctx, req)` pseudocode.

##### Risk assessment

**HIGH** — as written, the tests cannot access the named helper and may validate invented identity semantics.

---

#### 02-09 — Admin section registry and gate

##### Summary

Gate-before-lookup is the correct oracle-resistant ordering, and the proposed invariant belongs in the existing privacy scope. The plan does not actually connect validation to startup, and several APIs are internally inconsistent.

##### Strengths

- `INV-PRIVACY-11` is the correct next identifier: the current registry ends at `INV-PRIVACY-10` (`invariants.yaml:2156`, `invariants.yaml:2163`).
- Evaluating ABAC before registry lookup structurally closes the registered-versus-unregistered oracle.
- Iterating `All()` for the 7-permit/21-denial suite and pairing each denial with an admin permit makes the authorization tests non-vacuous.

##### Concerns

- **HIGH — 02-09-PLAN.md:232-250:** `section.Validate()` is not wired into production startup. The files list contains only the new package and invariant docs. Actual startup work is explicitly sequenced in `BootstrapSubsystem.Prepare` (`subsystem.go:127`), and production subsystems are explicitly registered in `core.go` (`core.go:834`). Merely creating `internal/admin/section/boot.go` causes nothing to execute.
- **HIGH — 02-09-PLAN.md:183-188:** The proposed positional `engine.Evaluate(subject, action, resource)` call does not match the real interface, which accepts `context.Context` and one `types.AccessRequest` (`types.go:473`).
- **MEDIUM — 02-09-PLAN.md:103-119:** The plan alternates between `Registry.Validate()` and package-level `Validate()`, but defines no `Registry` type and no validation function accepting a substitute slice. Tests requiring malformed registries therefore have no injection surface.
- **MEDIUM — 02-09-PLAN.md:183-190:** `AssertSectionAccess` evaluates the caller-supplied action before lookup; the returned registry descriptor is never used for authorization. The mandatory descriptor is validated but operationally inert.
- **MEDIUM — 02-09-PLAN.md:34,189-190:** The must-have says planned sections return `NOT_IMPLEMENTED`, but the implementation only returns a `Section` and leaves status handling to a future caller. No Phase-2 artifact can prove this truth.
- **LOW — 02-09-PLAN.md:283:** Use `task invariants:render` rather than raw `go run`; the task already exists (`Taskfile.yaml:227`).

##### Suggestions

1. Add `internal/bootstrap/setup/subsystem.go` to `files_modified` and call `section.Validate()` early in `Prepare`, or register a real lifecycle subsystem.
2. Define `type Registry` with `Validate()` and `Lookup()`, or add `validateEntries([]Section)` so malformed-registry and boot tests are implementable.
3. Make the descriptor participate in authorization after the opaque type-level gate, or narrow the descriptor claim.
4. Either add a post-gate resolver that produces `NOT_IMPLEMENTED`, or defer that must-have to Phase 6.
5. Use exact `AccessRequest` construction.

##### Risk assessment

**HIGH** — boot enforcement and the documented engine call will not work as planned.

---

#### 02-10 — Public-read exposure audit

##### Summary

The paired behavioral control and five-part audit query are strong. The audit gate is nevertheless fail-open: an unavailable database is accepted, and the prescribed remediation cannot be applied to descriptions.

##### Strengths

- The query correctly distinguishes public character properties from unenumerated names and descriptions.
- Calling out the absent `entity_properties → characters` foreign key is accurate: the schema declares no such FK (`000001_baseline.sql:354`).
- Comparing the full corpus against a corpus excluding exactly the two widening policies is an excellent causal control.

##### Concerns

- **HIGH — 02-10-PLAN.md:159-175:** An unavailable database is treated as a recorded result, and acceptance allows an explanation for missing result sets. That permits the phase to merge without examining any live data, contradicting the plan’s own “ships only after audit” security gate.
- **HIGH — 02-10-PLAN.md:151-167:** The human check says a sensitive description should be remediated by changing its row’s `visibility`. `characters.description` is an intrinsic column with no visibility field (`000001_baseline.sql:72`); visibility exists only on `entity_properties` (`000001_baseline.sql:354`). The mandated remedy is impossible for descriptions.
- **MEDIUM — 02-10-PLAN.md:25,190-230:** The must-have says a zero-property character “still publishes name,” but the planned integration behavior only evaluates profile reachability. No projection or character-name response is exercised, so that truth is not proven.
- **LOW — 02-10-PLAN.md:121-122:** `rg -c` returns exit 1 on zero matches. As an automated command, the read-only check can report failure when the desired count is zero.

##### Suggestions

1. Make database unavailability a blocking checkpoint. Only a successful query against the named target, including a genuine zero-row result, should discharge the audit.
2. Define a separate description verdict: accept exposure, redact/clear content, or raise the configured description floor. Do not refer to nonexistent row visibility.
3. Move the “publishes name” assertion to Phase 4 or add a real projection/facade test.
4. Wrap expected-zero searches with an explicit assertion form rather than a bare `rg -c`.

##### Risk assessment

**HIGH** — the privacy widening can currently pass its audit gate without an audit.

---

#### 02-11 — Amendments, validation, and phase gate

##### Summary

The amendment discipline and mandatory ABAC review are good closeout mechanisms. The validation-map scope and RED-first accounting contradict themselves, and the integration command conflicts with repository execution rules.

##### Strengths

- Applying amendments both in place and in §14, with single-line superseded-text searches, directly addresses stale normative text.
- Routing the completed authorization diff to `abac-reviewer` with three focused questions is appropriate.
- Setting `nyquist_compliant` from observed results rather than aspiration is a sound audit discipline.

##### Concerns

- **MEDIUM — 02-11-PLAN.md:160-166:** The plan reads the 02-12 summary but fills the per-task map only for plans 02-01 through 02-10. That excludes all three 02-12 tasks while claiming “every task in this phase.”
- **MEDIUM — 02-11-PLAN.md:174-201:** The action enumerates four RED demonstrations, while acceptance requires three rows. It also omits plan 02-08’s separate wildcard/totality RED demonstration (`02-08-PLAN.md:245`).
- **MEDIUM — 02-11-PLAN.md:241-255:** The plan runs `task test:int` inline. Repository rules require `task test`, `lint`, `build`, and `test:int` to be dispatched through `local-check`; only the final `task pr-prep` stays inline (`AGENTS.md:227`).
- **LOW — 02-11-PLAN.md:201:** “Three rows” also conflicts with the preceding prose’s four named demonstrations, independently of the missing wildcard case.

##### Suggestions

1. Define the validation scope as plans 02-01 through 02-10 plus 02-12, excluding only plan 02-11’s self-referential closeout tasks.
2. Enumerate all required RED rows explicitly—currently at least five—and use the same number in action, acceptance, and verification-integrity sections.
3. Dispatch `task test:int` through `local-check`; retain only final `task pr-prep` inline.

##### Risk assessment

**MEDIUM** — closeout can overstate coverage and may hit a hook-enforced execution failure.

---

#### 02-12 — Backfill, unique index, and resolution CLI

##### Summary

The A→B→C migration rationale is thoughtful, and the world-SQL fence integration is particularly strong. Three blockers remain: the CLI is unreachable, the RED schema is incompatible with the new create path, and live writers can invalidate the backfill before the constraint lands.

##### Strengths

- The Go-migration registration hazard is real and correctly handled: registration is filename-derived and requires `init()` with `AddMigrationContext` (`migrations/doc.go:12`, `migrations/doc.go:23`).
- `SET NOT NULL` before the unique index is the correct defense against PostgreSQL’s distinct-NULL behavior.
- Keeping mutation SQL inside the world writer boundary and having the CLI use the gated repository is well aligned with `INV-WORLD-4`.
- The planned fullwidth-versus-ASCII collision test is a meaningful proof that the migration catches cases the old `LOWER(name)` query could not.

##### Concerns

- **HIGH — 02-12-PLAN.md:7-29,235-238:** The new command is never added to the root Cobra command. `NewRootCmd` explicitly registers every top-level command via `AddCommand` (`root.go:36`), but `root.go` is absent from `files_modified`. `holomush character …` will not exist.
- **HIGH — 02-12-PLAN.md:299-310:** The RED test stages schema version 000053 but runs the post-02-06 real create path. Plan 02-06 changes `CharacterRepository.Create` to insert `normalized_name`, skeleton, and Unicode-version columns (`02-06-PLAN.md:269`); those columns are introduced by 000054. Against 000053, both creates fail because the columns do not exist. That is not evidence that the uniqueness gate detects the missing index.
- **HIGH — 02-12-PLAN.md:146-179:** There is an unclosed write window between 000055 and 000056. Goose’s advisory lock serializes migrators only (`migrate.go:85`); each replica migrates before starting its own database subsystem (`core.go:273`). Existing replicas are not stopped or locked. An old writer can insert a NULL `normalized_name` after the backfill commits but before `SET NOT NULL`, causing deployment failure; a new writer can insert a duplicate before the index.
- **MEDIUM — 02-12-PLAN.md:151-153:** `goose.TransactionEnabled` and `RunTx` belong to the programmatically constructed `goose.NewGoMigration` fixture (`migrate_gointerleave_integration_test.go:153`). A registered `AddMigrationContext` migration uses exact functions of shape `func(context.Context, *sql.Tx) error` (`migrations/doc.go:37`). The current wording mixes the two APIs.
- **LOW — 02-12-PLAN.md:10:** `internal/store/migrations_register.go` already blank-imports the whole migrations package (`migrations_register.go:6`); it should be verified, not modified.

##### Suggestions

1. Add `cmd/holomush/root.go` and a root-command test proving `character name set` and `duplicates` are discoverable.
2. Run the RED test against schema 000055 with only 000056 excluded. The post-02-06 writer then has its columns, while no unique index exists. Assert both concurrent claims succeed in the RED variant before asserting exactly one succeeds under the full chain.
3. Add a deployment/write-exclusion mechanism:

   - drain all character writers before 000055;
   - or hold an application-wide advisory lock also acquired by character writers;
   - or combine backfill and constraint creation into one transactional Go migration if lock duration is acceptable.

   Add an integration test that attempts an insert between B and C.
4. Specify exact `AddMigrationContext(up, down)` function signatures.
5. Remove `migrations_register.go` from modified files unless an actual change is required.

##### Risk assessment

**HIGH** — the current sequence can fail during rolling deployment, its RED proof is non-diagnostic, and the recovery CLI is unreachable.

---

#### Plan-set assessment

##### Strengths

- The set consistently uses real policy engines and real PostgreSQL for load-bearing proofs instead of canned allow/deny doubles.
- Denials are generally paired with positive controls, including the policy-exclusion control in plan 10 and the one-winner/one-loser uniqueness shape in plan 12.
- Migration numbering is sequential after the current `000053` tip, and the planned SQL directionality matches the goose one-file convention.
- The task definitions do consume package arguments: `task test` uses `CLI_ARGS` (`Taskfile.yaml:185`), as does `task test:int` (`Taskfile.yaml:265`).
- Threat registers identify meaningful failure mechanisms rather than generic categories.

##### Cross-plan concerns

- **HIGH:** Plans 07–08 share an unresolved identity-model defect. Fixing tests without first fixing viewer-to-character/admin semantics risks producing convincing but invalid green results.
- **HIGH:** Plans 07 and 10 permit the public-read widening to exist before an audit whose unavailable state is accepted as completion.
- **HIGH:** Plans 06 and 12 are not aligned on the RED schema: the writer introduced by 06 requires migration 000054, while 12 deliberately stages 000053.
- **HIGH:** Plan 12’s migration correctness assumes no concurrent application writes, but neither the plan nor current migrator enforces that assumption.
- **MEDIUM:** Several plans describe exact APIs that differ from source—especially positional `Evaluate` calls and mixed goose migration APIs.
- **MEDIUM:** Closeout plan 11 can mark validation complete while omitting plan 12’s tasks and at least one RED demonstration.

##### Recommended revision order

1. Settle viewer ownership/restricted/admin semantics and amend 02-03, 02-07, and 02-08.
2. Make the 02-10 audit unavailable state blocking and define description-specific remediation.
3. Add real startup wiring and exact engine APIs to 02-09.
4. Redesign 02-12’s migration write exclusion and RED schema.
5. Wire the CLI into `root.go`.
6. Reconcile 02-11’s validation scope and RED counts.

##### Overall risk assessment

**HIGH.** The plan set has excellent verification intent, but the remaining defects affect authorization semantics, privacy widening, production startup enforcement, migration safety, and recovery operability. These are outer-gate blockers, not polish items.
---

## Orchestrator verification pass

Codex's findings were independently re-checked against the tree at `4b3a820`
before being recorded here. This section states what was confirmed, so a reader
does not have to re-derive it. Every Part A blocker was verified as real.

### Codex HIGH findings — verification verdicts

| # | Finding | Verdict | Evidence checked |
| - | ------- | ------- | ---------------- |
| 1 | 02-01's tracer cannot persist the identity columns from `CharacterService` | **CONFIRMED** | `internal/auth/character_service.go:56-60` — `CharacterService{charRepo, locRepo, genesis}` has no pool and no writer; persistence delegates to `CharacterGenesis.Create(ctx, *world.Character, string)` (`character_service.go:49-51`). `world.Character` (`internal/world/character.go:15-30`) carries no `NormalizedName`/`Skeleton`/`UnicodeVersion` field, and the real INSERT is `internal/world/postgres/character_repo.go:55`. 02-01 owns none of `character.go`, `character_genesis.go`, `character_repo.go`. |
| 2 | 02-02's import guard contradicts 02-06 | **CONFIRMED AND SHARPER THAN REPORTED** | 02-02 asserts "`internal/auth` imports no `internal/charname` symbol" (`02-02-PLAN.md:232`) and makes it a standing success criterion (`:331`). But the banned import is introduced in **wave 1**, not wave 4: `02-01-PLAN.md:50` states `createWithMaxAndBind` calls `charname.Gate.Check`, and `02-01-PLAN.md:19` owns `internal/auth/character_service.go`. The guard is therefore RED the moment it lands in wave 2 — one wave earlier than Codex diagnosed. 02-06 then compounds it (`02-06-PLAN.md:378-388`). |
| 3 | 02-03's role lookup is not expressible through `ABACConfig` | **CONFIRMED** | `internal/store/role_store.go:15-23` — the `RoleStore` interface has `GetRoles`/`AddRole`/`RemoveRole`/`PlayerHasRole` and no `PlayerRoles`. `internal/access/setup/setup.go:89` types the field as `store.RoleStore`. The population site holding the concrete `*PostgresRoleStore` is `internal/access/setup/subsystem.go:95,104`, which 02-03 does **not** own (its `files_modified` stops at `setup.go`). Extending the interface instead has an unenumerated implementor blast radius. |
| 4 | Binding `INV-WORLD-6` would create a false registry guarantee | **CONFIRMED** | `docs/architecture/invariants.yaml:5074-5080` claims `world.Service.DeleteCharacter` is the **ONLY** path releasing a name. `internal/auth/character_reaping.go:92` documents `CharacterReapingService` as "the SECOND sanctioned out-of-world writer", and `reapCharacter` deletes the row via `s.deleter.Delete(txCtx, char.ID, char.Version)` (`character_reaping.go:263`). This is precisely the fabricated-binding failure `.claude/rules/invariants.md` forbids. |
| 5 | Malformed block-list JSON collapses into "absent" | **CONFIRMED** | `internal/settings/game.go:120-128` — `StringSliceN` returns `(nil, false)` both when the key is absent and when `json.Unmarshal` fails. A loader that reads `false` as "absent ⇒ empty list" silently disables the block list on a malformed direct-SQL edit — the exact edit path D-16 says is the only one v0.13 has. |
| 5b | Block-list poller wired into a one-shot subsystem | **CONFIRMED** | `internal/bootstrap/setup/subsystem.go:238-240` — `Activate` is an explicit no-op ("bootstrap's work is a one-shot Prepare-time seed"). A poll loop placed there has no lifecycle or shutdown owner. |
| 5c | Runtime paths would not consume the cache | **CONFIRMED** | Two independent production composition roots build `CharacterService`: `internal/bootstrap/setup/subsystem.go:194` and `cmd/holomush/sub_grpc.go:436`; `GuestService` adds a third at `cmd/holomush/sub_grpc.go:469`. Neither 02-01 nor 02-06 lists either file. |
| 6 | 02-06 cuts existence checks to `normalized_name` before the backfill | **CONFIRMED** | 02-06 (wave 4) rewrites all five existence checks to `SELECT EXISTS(... WHERE normalized_name = $1)` (`02-06-PLAN.md:391-394`). 02-06 only *writes* `BackfillCharacterIdentity`; nothing calls it until migration `000055` in 02-12 (**wave 5**), and the `UNIQUE` index lands in `000056` (wave 5). Between waves 4 and 5, pre-existing rows have `normalized_name IS NULL`, so the check cannot see them **and** no constraint backstops it — the `LOWER(name)` safety net at `internal/bootstrap/setup/adapters.go:43` has already been removed. IDENT-09's guarantee is absent in a deployable commit. |

### Codex MEDIUM findings spot-checked

- **02-04's "five SELECT column lists" is factually wrong — CONFIRMED.** The exact
  full-row list `SELECT id, player_id, name, description, location_id, created_at, version`
  occurs **three** times in `internal/world/postgres/character_repo.go` (lines 33, 162, 183).
  `GetNamesByIDs` and `ListAll` are deliberate partial projections. The plan repeats
  "five" at `02-04-PLAN.md:40`, `:93`, `:132` and `:154`, including in a `read_first`
  citation — an executor told to move five lists in lockstep will either hunt for two
  that do not exist or wrongly widen the projections.
- **`task invariants:render` and `task mocks:generate` exist** (`Taskfile.yaml:227`, `:649`),
  so Codex's suggestion to prefer them over raw `go run ./cmd/inv-render` / bare `mockery`
  is actionable. Weight is low, though: `.claude/rules/invariants.md` itself prescribes
  `go run ./cmd/inv-render`, so the plans are following a repo rule rather than inventing
  one. The clear-cut case is `go build ./...` at `02-06-PLAN.md:328`, which does contradict
  CLAUDE.md's task-only rule.

### Independently verified as sound (no action needed)

These were checked because they are the usual failure sites in this repo, and each held:

- **Migration numbering** — highest on disk is `000053`; `000054`–`000056` are collision-free.
- **The D-20 goose gate is satisfied** — `internal/store/migrate.go:22` imports `goose/v3`,
  migrations are single-file with both directions, and zero `.up.sql`/`.down.sql` pairs remain.
- **Wave graph** — every `depends_on` sits in a strictly lower wave, and there are **zero
  same-wave file-write collisions** across all 12 plans (checked programmatically).
- **Cross-plan artifact promises** — all 11 "written by 02-NN" claims match that plan's
  `files_modified`.
- **Requirement coverage** — IDENT-06/07/08/09, PROFILE-11 and EXT-07 each land on ≥2 plans.
- **Task targets** — every `task` target the plans invoke exists, and both `test` and
  `test:int` consume `CLI_ARGS` (`Taskfile.yaml:189`, `:289`), so the scoped `-- ./pkg/`
  forms are valid. (The older "test:int rejects `--` args" lore is stale.)
- **Invariant ids** — `INV-PRIVACY-11` is the next free id in scope; `INV-WORLD-5`/`-6` are
  `binding: pending` as the plans assume.
- **The §8.5.1 conjunction is genuinely conjunctive.** The engine combines permits
  disjunctively (`internal/access/policy/engine.go:591-611`), and 02-08 ANDs two separate
  `Evaluate` calls in Go, separated by **action** — `read_profile_attribute` (term A) vs
  `read` (term B) — with 02-07 asserting no other seed policy uses that token
  (`02-07-PLAN.md:176`). This is the trap §8.5.1.1 exists to prevent, and it is closed.
- **D-01 is honored** — 02-07 carries an explicit negative criterion that
  `seed:viewer-property-owner-write` must **not** appear (`02-07-PLAN.md:295`).
- **The public-read widening resolves** — it is guarded on
  `resource.property.parent_type == "character"`, and `parent_type` is emitted
  unconditionally at `internal/access/policy/attribute/property.go:82`, so the condition
  is not silently unresolved.
- **The widening is additive, not an edit** — new permits alongside an untouched
  `seed:player-character-colocation`, which is correct given disjunctive combining and
  avoids a `SeedVersion` bump.
- **omit-don't-sentinel** is followed for the new viewer provider (`02-03-PLAN.md:191-197`).
- **The naive `updated_at`-only poller gap was already found and closed by the plans
  themselves** — 02-05 uses `(updated_at, md5(value))` with a RED-first demonstration and a
  threat-register row, precisely because `SetSystemInfo` is the only writer that maintains
  `updated_at` (`internal/store/postgres.go:88`) and v0.13's only edit path is direct SQL.
- **The confusable-rejection oracle is closed** — the message must name no colliding
  character, asserted over the caller-received message, with threat row `T-02-03`.
- **`AdminSectionResource`'s panic follows existing precedent** (`internal/access/prefix.go:67,77,91,104`)
  and 02-09 guards the request path ahead of it (`T-02-58`).
- **The Unicode skew is real and correctly characterised** — stdlib `unicode.Version` is
  `15.0.0` under Go 1.26.5 while the confusables table targets 17, which is what Amendment D
  records; `golang.org/x/text v0.40.0` is already an indirect dependency, so 02-01's
  promotion to direct needs no version change.

### Environment note

Codex ran under `sandbox: workspace-write`. The worktree was verified clean at `4b3a820`
before and after both passes — the reviewer modified nothing.

---

## Consensus Summary

Only one reviewer ran, so this is not multi-model consensus. It is Codex's verdict
plus the orchestrator's independent verification pass, which confirmed every HIGH.

### Agreed Strengths

Both the reviewer and the verification pass independently rated these as genuinely
above-average plan work:

- **Verification integrity is the standout.** RED-first gates that are demonstrated
  against a real prior state, denials paired with positive controls, set-equality
  with symmetric-difference diffs, and explicit vacuity analysis (02-06 catches a
  stale-CAS test that would stay green after `Update` stops writing `name`). This is
  materially better than ordinary plan-level testing.
- **`charname.Admitted` is the right shape.** A single-constructor opaque token makes
  "a name write that skipped the gate" *unexpressible in Go* rather than merely
  caught by a test, and 02-06 explicitly forbids adding an escape hatch.
- **Migration sequencing is correct where it matters most.** `SET NOT NULL` before
  `CREATE UNIQUE INDEX` is the right defense against Postgres treating NULLs as
  distinct — without it the index would deploy green and enforce nothing.
- **The §8.5.1 conjunction is genuinely conjunctive.** Two `Evaluate` calls ANDed in
  Go, separated by action (`read_profile_attribute` vs `read`), correctly avoiding
  the engine's disjunctive permit combining. This is the trap §8.5.1.1 exists to
  prevent and the *structure* is right — though see HIGH #10 for the identity defect
  inside it.
- **Threat registers name real mechanisms**, not generic STRIDE categories.

### Agreed Concerns

The blockers cluster into five themes. Individually the plans are meticulous; what
has drifted is the **seams between them**.

1. **Production composition is not modeled as a shared dependency** (HIGH #1, #2,
   #12). `CharacterService` is built at two independent roots and `GuestService` at
   a third; none of `cmd/holomush/sub_grpc.go`, `internal/bootstrap/setup/subsystem.go`
   or `cmd/holomush/root.go` is owned by any plan that needs them. Several plans
   therefore ship code that nothing constructs, validates, or reaches at runtime.
2. **Cross-plan contradiction** (HIGH #3). 02-02 installs a permanent guard against
   an import that 02-01 introduces one wave earlier and 02-06 depends on.
3. **Intermediate broken states** (HIGH #9, #16, #17). The existence-check cutover
   lands a wave before the backfill; the RED proof stages a schema the new writer
   cannot run against; the backfill→constraint window assumes a write exclusion
   nothing enforces.
4. **An identity-model defect inside the ABAC surface** (HIGH #10, #11). The viewer
   twins compare a *player* ID against *character*-keyed row data, and the admin twin
   references a `roles` attribute the viewer namespace does not have. Because the DSL
   treats a missing key as false, this fails **silently and closed** — private,
   restricted, and admin profile fields become permanently invisible with no error.
   That is the "profile looks bare" symptom §8.5.1.1 warns provokes the forbidden
   repair of dropping term B.
5. **Gates that can pass without gating** (HIGH #5, #13, #14). `INV-WORLD-6` would be
   bound to a guarantee shipped code already violates; the PROFILE-11 exposure audit
   accepts an unavailable database as a recorded result; and its mandated remedy is
   impossible for `characters.description`, which has no visibility column.

### Divergent Views

No second reviewer, so no model-vs-model divergence. Two places where the
verification pass **disagreed with or corrected Codex**:

- **Codex's 02-07 MEDIUM on the empty player floor overstates the conflict.** It
  claims execution must violate either the three-policy rule or the DSL grammar. The
  grammar claim is correct (`ListExpr` requires ≥1 literal, `dsl/ast.go:232-236`),
  but 02-07 already carries an escape hatch permitting 2 policies with the omission
  recorded in the SUMMARY (`02-07-PLAN.md:150-153,176`). The residual issue is
  narrower: that escape **silently deviates from locked decision D-03**, which
  mandates three. It needs flagging as a deviation, not redesign.
- **Codex over-claims in an 02-03 *strength* bullet.** It says registering the new
  prefixes is necessary "because `ParseEntityRef` actually validates against that
  list". `ParseEntityRef` has **zero production callers** — every engine path uses
  `SplitN(ref, ":", 2)`, exactly as `02-CONTEXT.md` records. Registration is correct
  hygiene, not a functional requirement. No finding changes; noted for accuracy.
- **HIGH #17's blast radius is smaller than stated for today's topology.**
  `compose.prod.yaml` runs a single `core` service with no `replicas:` key, and
  migrations complete before `orch.StartAll` (`cmd/holomush/core.go:857`), so the
  local process never serves during its own migration. The concern is real for
  `compose.cluster.yaml` and for the multi-replica posture D-16 itself asserts — the
  fix is to *state and enforce* the assumption, not necessarily to redesign the
  chain.

### What this does NOT mean

The architecture is not in question. Every blocker is a plan-boundary, ownership, or
sequencing correction — file lists that need to grow, a guard that needs narrowing, a
RED that needs restaging, an identity mapping that needs settling. The reviewer and
the verification pass agree that once these are repaired, the underlying mechanisms
(opaque admission, writer fencing, exhaustive lifecycle reads, fail-closed compiled
snapshots, gate-before-lookup) are stronger than what they replace.

### Recommended revision order

1. Settle the viewer identity model (ownership, `visible_to`/`excluded_from`, admin
   role exposure) and amend 02-03, 02-07, 02-08 together — everything downstream of
   term B depends on it.
2. Narrow 02-02's import guard to the two-policy separation it actually protects.
3. Add the real composition roots to 02-01, 02-05, 02-06, 02-09 and 02-12.
4. Move the existence-check cutover into the same atomic unit as the backfill and
   constraint; restage 02-12's RED against `000055`.
5. Make the 02-10 audit's unavailable state blocking, and define a description-
   specific remedy.
6. Amend `INV-WORLD-6` before binding it.
7. Reconcile 02-11's validation scope and RED counts last.

Feed this back with `/gsd-plan-phase 2 --reviews`.
