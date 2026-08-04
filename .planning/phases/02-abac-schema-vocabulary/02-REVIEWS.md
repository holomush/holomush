---
phase: 2
reviewers: [codex, pi]
reviewed_at: 2026-08-04T10:53:32Z
cycles: 3
cycle_1_reviewed_at: 2026-08-04T03:03:23Z
cycle_2_reviewed_at: 2026-08-04T04:28:29Z
cycle_3_reviewed_at: 2026-08-04T10:53:32Z
cycle_3_reviewers: [codex, pi]
cycle_3_models: {codex: "gpt-5.6-sol@xhigh", pi: "openrouter/moonshotai/kimi-k3@xhigh"}
cycle_3_tree: 87b563913
plans_reviewed: [02-01-PLAN.md, 02-02-PLAN.md, 02-03-PLAN.md, 02-04-PLAN.md, 02-05-PLAN.md, 02-06-PLAN.md, 02-07-PLAN.md, 02-08-PLAN.md, 02-09-PLAN.md, 02-10-PLAN.md, 02-11-PLAN.md, 02-12-PLAN.md, 02-13-PLAN.md]
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

---
---

# Cycle 2 — re-review after the fix pass

**Reviewed:** 2026-08-04T04:28:29Z · **Reviewer:** Codex (`gpt-5.6-sol`,
`model_reasoning_effort=xhigh`, `--sandbox read-only`), two source-grounded passes
· **Plans:** 13 (`02-01`…`02-13`) · **Tree:** `926dcf0e2`

**Verdict: NOT READY — a further fix pass is required.**

## How this cycle was run

Cycle 1's lesson was that a `low`-effort pass over this plan set is impressionistic
and worthless, and that splitting beats one skimmed pass. So this cycle ran Codex at
`xhigh` in **two semantic clusters**, each ~275 KB of inlined plan text plus live
repo read access:

| Pass | Cluster | Plans |
| ---- | ------- | ----- |
| A | identity / charname / migration | 02-01, 02-02, 02-04, 02-05, 02-06, 02-12 |
| B | ABAC / viewer / policy / closeout | 02-03, **02-13 (new)**, 02-07, 02-08, 02-09, 02-10, 02-11 |

Each pass was told which cycle-1 findings landed in its cluster and was required to
mark each INCORPORATED / PARTIAL / NOT INCORPORATED **with a citing plan line** —
because `/gsd-execute-phase` reads `PLAN.md` and nothing else, so a fix living only
in a planner's summary or a plan's narrative prose is not incorporated.

The orchestrator ran an independent verification pass over every finding below
before recording it. **Two reported findings were rejected as false positives and
are NOT recorded:** a claim that `task test:int` ignores `--` package args
(`Taskfile.yaml:289` does interpolate `{{.CLI_ARGS | default "./..."}}` — cycle 1
was right), and a set of cross-plan artifact-promise hits that were regex artifacts
of bare-filename-vs-full-path matching.

## Cycle-1 disposition summary

**The fix pass did substantial, genuine work.** The dependency graph is clean, and
most cycle-1 HIGHs are properly closed with executable plan text rather than prose.

| Cycle-1 finding | Disposition | Evidence |
| --------------- | ----------- | -------- |
| #1 tracer cannot persist identity columns | **INCORPORATED** | `02-01-PLAN.md:73-99` reduces the tracer and names the real writer; `02-06-PLAN.md:290-305` assigns persistence |
| #2 import guard contradicts 02-01/02-06 | **INCORPORATED** | Narrowed to file scope at `02-02-PLAN.md:30-32,248-270`, with a synthetic control at `:272-277` proving it does NOT fire for other `internal/auth` files |
| #3 role lookup not expressible via `ABACConfig` | **INCORPORATED** | `02-13` uses a `PlayerRoleLookup` **func field**, not an interface widening (`02-13-PLAN.md:30,47,143-177`), and owns the population site `internal/access/setup/subsystem.go` (`:17`) |
| #4/#13 false `INV-WORLD-6` binding | **INCORPORATED** | Amend-before-bind is a task action at `02-04-PLAN.md:292-317`; ordering enforced by `:339` (`rg … 'ONLY'` → 0); spec side at `02-11-PLAN.md:147-157` |
| #5 malformed block-list JSON → "absent" | **INCORPORATED** | Strict raw loader at `02-05-PLAN.md:274-299`; `StringSliceN` banned by `:365` |
| #5b poller in a no-op subsystem | **INCORPORATED** | Dedicated `blocklist.Subsystem` with real `Activate`/`Stop` at `02-05-PLAN.md:331-355` |
| #5c/#12 production composition roots | **PARTIAL** | All three roots named (`02-06-PLAN.md:490-507`) — but see HIGH C2-2/C2-3: nothing specifies how the cache *reaches* them |
| #6/#9 existence cutover before backfill | **INCORPORATED** | Dual predicate at `02-06-PLAN.md:49,452-475`, retired atomically with the constraint at `02-12-PLAN.md:265-276,307` |
| #10 viewer twins compare player id vs character-keyed data | **INCORPORATED** | `02-13` derives the three player-keyed peers (`:31-34,207-274`); `02-07-PLAN.md:314-316` transcribes the mapping; coverage test at `:386` |
| #11 admin twin references unsupplied `roles` | **INCORPORATED** | `ViewerTierProvider` emits `roles`/`has_roles` incl. `Schema()` (`02-03-PLAN.md:26-27,287`); `02-13` supplies the lookup |
| #14 audit accepts unavailable DB | **INCORPORATED** | Blocking checkpoint + escalate at `02-10-PLAN.md:192-201`, acceptance `:208-214` |
| #15 description remedy impossible | **INCORPORATED** | Description-specific verdict vocabulary at `02-10-PLAN.md:29,177-188` |
| #16 RED stages a schema the writer can't run | **INCORPORATED IN TEXT / MECHANICALLY BROKEN** | Staged at `000055` (`02-12-PLAN.md:49,415-434`) — but the staging recipe cannot produce it; see HIGH C2-6 |
| #17 concurrent writers in backfill→constraint window | **PARTIAL** | Declared and failure-tested (`02-12-PLAN.md:50,254-263,441-450`); not enforced for the cluster posture |

**Actionable findings:** 18 of 21 verified incorporated — including 02-04's
"five"→**three** SELECT lists (correct against `character_repo.go:33,162,183`), the
raw `go build ./...` removal, 02-09's startup wiring, 02-12's CLI into `root.go`,
the D-03 deviation record, and the struct-based `Evaluate(ctx, types.AccessRequest)`
call shape. 02-11's RED-count derivation is PARTIAL (executor discipline, not an
automated command).

### Deferral and rejections — judged on merit

- **The `02-10` "still publishes `name`" deferral to Phase 4 is SOUND.** Verified:
  no plan in the phase touches a `.proto`, so Phase 2 adds no RPC; `02-04`'s
  `internal/grpc/auth_handlers.go` edit is `SelectCharacter` lifecycle gating, not a
  profile projection. Critically, the deferral is **tracked** — `02-11-PLAN.md:303`
  carries it as a Phase-4 row in the validation map *with its rationale*, as an
  acceptance criterion. Not a dropped requirement. **Do not re-raise.**
- **Both `task invariants:render` rejections are SOUND.** Verified:
  `Taskfile.yaml:227-230` defines `invariants:render` whose only command is
  literally `go run ./cmd/inv-render`, and `.claude/rules/invariants.md:28,76`
  prescribes the raw form. Recorded in plan text at `02-04-PLAN.md:322-328` and
  `02-09-PLAN.md:355-365`. **Do not re-raise.**

## Structural checks — all clean

Re-derived programmatically across all 13 plans after the wave shifts:

- **Wave graph:** every `depends_on` target sits in a strictly lower wave. Zero
  violations. The shifts (`02-07` 3→4; `02-08`/`02-09`/`02-10` 4→5) and the new
  `02-13` (wave 3, depends on `02-03` wave 2) are graph-correct.
- **Same-wave file-write collisions:** zero, across all 13 `files_modified` sets.
- **Cross-wave multi-owner files:** 13 files have >1 owner; every one is strictly
  sequential and deliberate (e.g. `02-06` w4 lands the transitional predicate,
  `02-12` w5 retires it).
- **Requirement coverage:** IDENT-06/07/08/09, PROFILE-11, EXT-07 each land on ≥2 plans.
- **Invariant ids:** `INV-PRIVACY-11` is still the next free id in scope.

> **Caveat carried from Pass A:** the collision audit is only as good as the
> manifests, and several `files_modified` sets are now known-incomplete (C2-4,
> C2-5, C2-13, plus the MEDIUMs). Re-run it after those are corrected.

## Cycle-2 HIGH concerns (21) — all orchestrator-verified against source

The fix pass closed cycle 1's findings but introduced a new crop. These cluster into
six themes. Every one below was independently re-checked before recording.

### Theme A — the admission token proves less than the code requires (2)

**C2-1 — `charname.Admitted` does not prove the full character-name validity
contract, and the CLI rename path bypasses the rest.** `Gate.Check` covers
normalization, skeleton, mixed-script and block list (`02-01-PLAN.md:233-239`), and
`02-01-PLAN.md:283-287` deliberately leaves `world.ValidateCharacterName` separate.
That validator carries the real letters/length rules — verified at
`internal/world/validation.go:59-104`: min length, max length, "must contain letters
and spaces only", leading/trailing spaces, consecutive spaces. `02-12-PLAN.md:354-356`
sends `Gate.Admit` straight into `Rename`. So digits, punctuation and overlong names
receive an `Admitted` token and are written. This **falsifies `02-06`'s claim that
the token structurally discharges rename admission** — the writer requires the wrong
proof. Note this does *not* contradict cycle 1's "Admitted is the right shape"
verdict: the token's single-constructor/zero-value design is still sound; what is
wrong is the *completeness of what it attests*.

**C2-2 — no specified transport carries the block-list cache into production
composition.** Verified: `02-06` does **not** own `cmd/holomush/core.go`, where
`02-05` constructs the block-list subsystem (`02-05-PLAN.md:346-355`).
`02-06-PLAN.md:490-494` says to use "the block-list `*Cache` plan `02-05` exposes" —
a reference, not an implementation instruction. Neither plan defines the
provider/config seam that moves the `*Cache` from `core.go` into
`internal/bootstrap/setup`'s config or `sub_grpc.go`'s config. This is why cycle-1
#5c is PARTIAL rather than closed: naming the roots is not wiring them.

### Theme B — lifecycle and composition ordering (3)

**C2-3 — bootstrap can `Prepare` before the block list exists.** Verified against
source: `BootstrapSubsystem.DependsOn()` (`internal/bootstrap/setup/subsystem.go:104-112`)
returns Database, ABAC, World, Auth, Plugins, Sessions — no block-list entry. And
`02-05-PLAN.md:355-356` explicitly gives the new subsystem `DependsOn` =
`{SubsystemDatabase}` only ("the poller needs the pool and nothing else"). Bootstrap
creates a `CharacterService` and may create the initial admin during `Prepare`, so
the admission path can observe an unprepared cache. Nothing declares the edge.

**C2-4 — `02-05` does not own the files carrying the real `productionSubsystems`
assertions.** Verified: those assertions live in `cmd/holomush/core_subsystems_test.go`
and `cmd/holomush/core_topo_order_test.go`. `02-05`'s `files_modified` lists only
`cmd/holomush/core.go` and `cmd/holomush/core_test.go` — and `core_test.go` is not
one of the files that references `productionSubsystems`. The plan instructs the
executor to enumerate and update them (`02-05-PLAN.md:350-354`) but does not own them.

**C2-5 — `02-06`'s shared-interface blast radius omits three real
implementers/callers.** Verified by grep — none is in `02-06`'s `files_modified`:
`internal/auth/character_genesis_test.go:30` (`fakeCharWriter.Create`, a manual
implementation) and `:166` (a caller);
`internal/access/policy/attribute/character_test.go:45`
(`mockCharacterRepository.Create`, another manual implementation);
`internal/auth/character_genesis_integration_test.go:103,126,143,163,185` (five
callers). `02-06` changes exactly these signatures. Compounding it, the plan
verifies with `task build` (main binary only — `Taskfile.yaml:476-485` says
`task build:all` is the whole-tree gate) and its scoped test command omits
`internal/access/...`, so the breakage would not be caught by its own verification.

### Theme C — migration and CLI mechanics (3)

**C2-6 — the corrected RED staging cannot actually produce migration `000055`.**
This is the most consequential finding: HIGH #16's fix is textually right and
mechanically broken. `02-12` stages the RED at `000055` and cites the in-tree
technique as "temp dir plus `goose.WithDisableGlobalRegistry(true)`"
(`02-12-PLAN.md:102,407,421,506`). But `000055` is a **registered Go migration** —
disabling the global registry is exactly what removes it. Verified: the cited
precedent uses **both** options together —
`internal/store/migrate_gointerleave_integration_test.go:210` passes
`goose.WithGoMigrations(backfill)` alongside `:214`'s
`WithDisableGlobalRegistry(true)`, and the comment at `:121` says so explicitly.
`02-12` never mentions `WithGoMigrations`. As written the RED run stages schema 54
and cannot truthfully assert 55.

**C2-7 — the duplicate-resolution integration test has no executable CLI seam.**
`02-12-PLAN.md:452-459` places the test under `test/integration/charname` and has it
resolve through `holomush character name set`. `cmd/holomush` is `package main` and
is not importable, and `task test:int` does not build, install or invoke the binary.
The plan supplies neither a subprocess strategy nor an importable
command/service abstraction.

**C2-8 — multi-replica writer exclusion is documented, not enforced.** (PARTIAL on
cycle-1 #17.) The precondition block and the loud-failure test are real
(`02-12-PLAN.md:254-263,441-450`), and the single-process posture is genuinely safe
(migrations precede `orch.StartAll`). But `compose.cluster.yaml` runs a second
writer, and goose only serializes *migrators* (`internal/store/migrate.go:85-86`). A
header comment plus a test proving DDL fails loudly does not prevent a live old
replica from writing during the window.

### Theme D — self-defeating and self-contradictory gates (4)

**C2-9 — four `<automated>` verification gates fail on their own success path, and
one plan instructs the executor to propagate the broken form.** *(Newly introduced
by this fix pass: verified `0` occurrences at `4b3a82028`, `8` at `926dcf0e2`.)* The
plans correctly diagnosed that a bare `rg -c` exits 1 on the zero matches that mean
success — then fixed it with `|| true`, which yields an **empty string**, not `"0"`.
Empirically confirmed: `test "$(rg -c PAT f || true)" = "0"` is **false** when there
are no matches. Affected gates:
`02-10-PLAN.md:143` (the read-only-SQL safety gate on a script destined for a
production database), `02-10-PLAN.md:204` (the "policy was narrowed" privacy gate),
`02-11-PLAN.md:193`, `02-11-PLAN.md:296` (both phase-closeout gates). Worse,
`02-10-PLAN.md:147` propagates it: "apply the same form to every expected-zero
search in this plan". An executor who does the work correctly sees these fail
permanently, and the cheapest apparent repair is to delete the gate. Correct form:
`|| echo 0` (or, per `.claude/rules/grepping.md`, count with `-o | wc -l`).

**C2-10 — `02-07` Task 1 simultaneously orders two and three tier-floor policies.**
Verified verbatim: the action at `02-07-PLAN.md:149-156` says "**Three tier-floor
policies, one per rung**", naming `seed:profile-tier-floor-player` explicitly. The
acceptance at `:230-231` requires `rg -c 'seed:profile-tier-floor-'` to be **2** and
`seed:profile-tier-floor-player` to return **no match**. An executor following the
action fails the acceptance. The plan cannot be executed consistently. (The *intent*
— two policies, D-03 deviation recorded — is correct and stated at `:23,191-210`;
the Task 1 body was simply not updated to match.)

**C2-11 — `02-08`'s fourth-rung suite requires the player-floor policy that
`02-07` deliberately does not ship.** `02-08-PLAN.md:195-198` correctly says only
two policies exist, then `:200-204` requires player-floor decisions, `:220-228`
requires a RED observation of `spectator` clearing that floor, and `:238` commits a
player-floor assertion. Against the shipped corpus there is no such policy to
evaluate. (Fix: use the **guest** floor as the ordinal discriminator — `spectator >=
"guest"` would wrongly permit while literal membership denies — which tests the same
property against a policy that exists.)

**C2-12 — `02-07` and `02-08` disagree about the `abactest.PropertyProvider`
double.** `02-08-PLAN.md:273-282` requires the double to derive player peers from
character-keyed fixtures via a character→player map. `02-07`, which owns
`internal/testsupport/abactest/abactest.go`, promises only provider doubles and
schema parity (`02-07-PLAN.md:433-448`). The consumer's contract does not exist in
the producer.

### Theme E — the new `02-13` and its seams (2)

`02-13` got its first-ever review this cycle. **It is materially sound**: no
fabricated symbols across 14 checked (`PropertyProvider`, `BuildABACStack`,
`PlayerKindLookup`, `evalInExpr`, the `ParentLocationResolver` interface and the rest
all exist as cited); it honours the omit-don't-sentinel rule with `has_X` witnesses
on every path; and it handles the guest/orphan `player_id IS NULL` case explicitly
rather than collapsing it into "ownerless". Two seam defects:

**C2-13 — `02-13` changes `NewPropertyProvider` to three arguments but omits a live
call site.** Verified: `test/integration/access/access_suite_test.go:182` calls
`attribute.NewPropertyProvider(propRepo, parentLocResolver)`. That file is not
mentioned anywhere in `02-13`, while `:238-242` orders every call site updated and
`:296` demands every call have three arguments. Its scoped verification
(`:285,354`) does not compile that suite, so the break surfaces later, in another
plan's wave.

**C2-14 — `02-13` promises a `BuildABACStack` production-stack proof with no owned
test artifact.** Behavior and acceptance require resolving both
`principal.viewer.tier` and `resource.property.owner_player_id` through
`BuildABACStack` (`02-13-PLAN.md:319,362`), but Task 3 owns only `setup.go` and
`subsystem.go` (`:304-305`), and no setup integration test file appears in
frontmatter.

### Theme F — gates that admit what they should refuse (6)

**C2-15 — an invalid or zero admin-section `Status` fails open as available.**
`02-09` defines string statuses (`:143-150`) but `validateEntries` checks only
descriptor fields and resource drift (`:162-175,186-193`), and the gate rejects only
`StatusPlanned` (`:251-255`). `Status("")` or `"typo"` therefore passes boot
validation and is served as **available**. Verified the SPEC vocabulary is closed at
exactly two values — `01-SPEC.md:2107-2117` lists seven ids, one `available` and six
`planned`. Needs an exhaustive switch with a denying default.

**C2-16 — `Descriptor.Action` is operationally inert.** The mandatory descriptor
carries `{Action, Resource}` (`02-09-PLAN.md:143-146`), but request-time logic
evaluates the *caller-supplied* action (`:224-235`) and checks only
`Descriptor.Resource` (`:243-249`). Changing a descriptor's action changes no
authorization outcome — while the registry rows carry `read` and §10.4 requires both
read and write (`01-SPEC.md:2190-2200`).

**C2-17 — `02-10`'s audit query cannot produce the per-description verdicts it
mandates.** Result set 4 is aggregate counts and maximum length only
(`02-10-PLAN.md:121-124`), yet `:177-188` and `:214` require one recorded verdict for
**every** non-empty description. Individual descriptions cannot be identified from
that output.

**C2-18 — making the audit detailed enough would commit user content to the repo.**
Task 2 requires all result sets pasted verbatim into a committed artifact
(`02-10-PLAN.md:169-177`). Expanding the query to expose description text so a human
can adjudicate it puts player-authored prose in git history; leaving it aggregate
makes C2-17 impossible. The plan needs a split: an operator-only detailed report for
review, and a committed sanitized ledger of stable ids/hashes, lengths and verdicts.

**C2-19 — a "read-only" audit task directs live-data mutation with no structural
checkpoint.** The human check at `02-10-PLAN.md:205` tells the executor to change
each sensitive property row's visibility, while `:216` states the task only runs a
read-only query and writes a record. `autonomous: false` is not an
approval/backup/rollback protocol for production data.

**C2-20 — `02-11` claims a `02-CONTEXT.md` D-03 amendment it neither owns nor
edits.** This is the clearest instance of the failure mode this cycle was chartered
to hunt. The must-have at `02-11-PLAN.md:27` states that "**CONTEXT D-03**" will
record two tier-floor policies, "written down where D-03 is read, not left in a plan
SUMMARY". Verified: `02-11`'s `files_modified` lists **only** `01-SPEC.md` and
`02-VALIDATION.md`; Task 1 owns only the SPEC (`:88-89`); Amendment G edits only
§8.6 (`:175-185`). And `02-CONTEXT.md:53-59` still reads "**three policies, one per
floor rung**". The claimed fix has no executable path.

**C2-21 — `02-03`'s tier-provenance must-have is not implementable by that plan.**
The truth at `02-03-PLAN.md:29` asserts the viewer tier is derived from server-side
session state and never from a request field. The only task action backing it is a
doc comment (`:160-164`), and no gateway or session-construction root is in
`files_modified`. `ViewerSubject(tier string, …)` can validate the token's shape but
cannot distinguish a server-derived `"player"` from an attacker-supplied one. Either
scope it as a Phase-4 call-site obligation or own a construction root and prove it.

## Cycle-2 actionable MEDIUM / LOW concerns (31)

Each is invisible to `/gsd-execute-phase` unless it lands in plan text.

### Incomplete or contradictory `files_modified` / task ownership

1. **02-01** — Task 3 modifies `internal/charname/version_test.go` (`:318-320`); it is absent from frontmatter (`:7-23`).
2. **02-02** — Task 2 owns `internal/charname/pipeline.go` and `pipeline_test.go` (`:176-178,196-204`); neither is in frontmatter (`:7-14`).
3. **02-12** — Task 1's `<files>` (`:175`) omits all five existence-check files its own action (`:266-276`) orders it to edit and its acceptance (`:307-308`) gates on. They are in frontmatter but in no task scope.
4. **02-12** — contradictory `migrations_register.go` ownership: listed in Task 1 `<files>` (`:173-175`), while the action (`:231-235`) and acceptance (`:305`) require it untouched. Remove it from `<files>`.
5. **02-07** — `:444-448,484` require an `abactest` schema-parity **package test**, but task files list only `abactest.go` (`:396`). A Go test cannot live in a non-`_test.go` file. Add `internal/testsupport/abactest/abactest_test.go`.
6. **02-11** — Task 2 omits `02-13-SUMMARY.md` from `read_first` (`:217` stops at 02-10 plus 02-12) while requiring observed 02-13 results (`:220-278`).
7. **02-11** — `depends_on` (`:6`) omits 02-01, 02-03 and 02-07, all of which carry rule-4 RED demonstrations this plan must tabulate. Harmless for ordering (wave 6), but the list no longer matches the plan's stated inputs.

### Impossible, vacuous, or self-defeating criteria

8. **02-04:129** — requires both partial methods to return a `Character` with zero `Status`. Verified: `GetNamesByIDs` returns `map[ulid.ULID]string` (`internal/world/postgres/character_repo.go:368`). Restrict the zero-`Status` assertion to `ListAll`.
9. **02-04:240-258 vs :269-278,313-315** — offers relocating the INV-WORLD-6 proof to another harness suite while hard-coding `asserted_by` to `test/integration/world/character_lifecycle_test.go`. Option (b) cannot satisfy the plan. Choose one placement now.
10. **02-05:298-299 vs :365** — the action requires `Load`'s doc comment to name `StringSliceN`; acceptance requires `rg -c 'StringSliceN' internal/charname/blocklist/` to return zero. Mutually exclusive. Strip comments or assert over the AST.
11. **02-06:393** — requires `rg -c 'go build \./\.\.\.' 02-06-PLAN.md` to return 0, but the plan's own explanatory prose at `:366` contains that exact string. Verified: the file currently has 1 occurrence. The criterion necessarily fails.
12. **02-13:184** — `rg -n 'PlayerRoles' role_store.go | rg -c 'RoleStore interface'` is vacuous: a line matching `PlayerRoles` can never also contain `RoleStore interface`, so it can never go RED. Mitigated only by the sibling method-set test.
13. **~34 prose criteria across all 13 plans** spell expected-zero checks as "`rg -c '…' <file>` returns 0". `rg -c` prints **nothing** and exits 1 on zero matches. Per `.claude/rules/grepping.md`, count with `-o | wc -l`, not `-c`. (The four `<automated>` instances are HIGH C2-9; these are the ambiguous-but-survivable remainder.)

### Factually wrong or stale plan text

14. **02-06:452-454,477-481,538 and 02-12:265-276,307-308** — both describe "five sites with identical SQL bodies". Two of the five are **interfaces** (`internal/auth/character_service.go:15-24`, `internal/auth/guest_service.go:35-40`) and contain no SQL. Recast as five declarations / three concrete query implementations; compare only the three adapters.
15. **02-07:42, :291, :399** — attribute registration, player roles and the property-`name` assertion to plan **02-03**; all three are now **02-13**'s (`02-13-PLAN.md:276-282`, `files_modified:13`).
16. **02-03:74-76** — narrates that this plan touches `internal/store/role_store.go`; its frontmatter and tasks do not. That file is 02-13's.
17. **02-08:113** — stale corpus count: says twelve entries and three floors; 02-07 ships eleven and two.
18. **02-11:88, :99, :208** — task name, action opening and `<done>` say "four amendments"; the task specifies and accepts **seven** (`:147-203`).
19. **02-13** — mis-cited line numbers: `seed.go:120,136,142` are actually `119,137,143`; `dsl/evaluator.go:216` is the closing brace of `compareBools` (the missing-attr-false sites are `evalHas:217` and `evalInExpr:343`).

### Under-specified contracts

20. **02-03:24 vs :124,154-158** — contradictory empty-identifier contract: the must-have says every `ViewerSubject` call panics on an empty identifier, the behavior requires `ViewerSubject(ViewerTierAnonymous, "")` to succeed. Restate as "guest/player viewers and all resource constructors reject empty identifiers; anonymous has no identifier".
21. **02-08:128-130** — the engine interface is literally `<the access engine interface>`. The repo's `types.AccessPolicyEngine` also requires `CanPerformAction` (`internal/access/policy/types/types.go:473-484`) while the acceptance describes a double implementing only `Evaluate`. Declare a consumer-local minimal interface.
22. **02-08:123-124** — the "not-found-equivalent signal" has no defined error, sentinel, result type or code.
23. **02-09:290 vs :240-241** — the request-time descriptor-mismatch test needs a substitute registry entry, but `AssertSectionAccess` hardcodes package `Lookup`. No injection seam is defined.
24. **02-09:143-150** — `All()` is not required to return a defensive copy; returning the backing slice lets callers mutate entries after boot validation, defeating the boot guarantee.
25. **02-13:223** — `ResolveOwners` returns `[]string` for all three fields; the plan never says how the one-element owner result maps to the scalar `owner_player_id`.
26. **02-10** — the recorded result has no privacy-safe stable row identity design: it needs enough identity to prove every row was adjudicated without committing values or identifying text.

### Design decisions that should be recorded, not implied

27. **02-13:33** — the player-union rule ("any character's membership applies to the whole player") is a **new authorization decision**, not an implementation detail. Existing policies are character-specific (`internal/access/policy/seed.go:117-143`), and `02-CONTEXT.md:33-41` says to preserve the same row semantics without deciding this cross-character widening. `excluded_from` is conservative, but `owner_player_id` and `visible_to_players` broaden access across a player's alternate characters. Record it as an explicit decision in CONTEXT and the SPEC amendment.
28. **02-13:251** — owner resolution is one `ResolveOwners` call per field, i.e. **three extra queries per property row**. The per-context LRU (`internal/access/policy/attribute/cache.go:95`) is keyed per resource, so it dedupes repeat lookups of the same property, not distinct rows. No plan text budgets the N-row cost.
29. **02-06** — verification uses `task build`, which `Taskfile.yaml:476-485` documents as the main-binary-only compile; `task build:all` is the whole-tree interface blast-radius gate. This plan changes central interfaces and should use it (plus full `task test` and `task test:int`).
30. **02-12:237-263** — the rollout needs a real enforceable guard for the cluster posture (maintenance-mode writer rejection, a database advisory lock checked by write paths, or a drain-then-migrate command), not only a header comment.
31. **02-11** — the RED-count derivation (`:30,240-247,302`) is executor discipline; the only `<automated>` on that task (`:296`) checks placeholder removal. Consider a command that derives the count from the plans.

## Recommended revision order

1. **Fix the two self-contradictions first** — `02-07`'s two-vs-three tier floors (C2-10) and `02-08`'s dependence on the policy it does not ship (C2-11). Everything in the profile-visibility chain reads from them.
2. **Settle the admission contract** (C2-1): decide whether `Gate.Check` subsumes `world.ValidateCharacterName` or whether `Rename` requires both proofs, and make the CLI path consistent.
3. **Close the block-list composition seam** (C2-2, C2-3): define a provider API on `blocklist.Subsystem`, thread it through both configs in `core.go`, and add the `Bootstrap → BlockList` dependency edge. Decide which plan owns `core.go` for that edit.
4. **Repair the `000055` staging recipe** (C2-6) — add `goose.WithGoMigrations`, and assert the provider source list and DB version are 55 before the RED case runs.
5. **Complete the ownership manifests** (C2-4, C2-5, C2-13, C2-14, MEDIUMs 1-7), then **re-run the same-wave collision audit**, which is only valid over complete manifests.
6. **Fix the four `<automated>` gates and delete the propagation instruction** (C2-9) — a one-token change (`|| true` → `|| echo 0`) with outsized value, since these are closeout and safety gates.
7. **Redesign the `02-10` audit output** (C2-17, C2-18, C2-19) into an operator-only detailed report plus a committed sanitized ledger, and make remediation a separate approved checkpoint.
8. **Make `02-09` default-deny on unknown status and resolve the descriptor-action model** (C2-15, C2-16).
9. **Give `02-11` ownership of `02-CONTEXT.md`** or drop the D-03 claim from its must-have (C2-20); scope or prove `02-03`'s tier provenance (C2-21).
10. Sweep the stale attributions and counts (MEDIUMs 14-19) last.

## What this does NOT mean

The architecture is still not in question, and the fix pass was real work — every
cycle-1 HIGH was engaged with, most are properly closed with executable plan text,
the wave graph survived a non-trivial reshuffle intact, and the new `02-13` is a
well-grounded plan with no fabricated symbols. The remaining defects are again
plan-boundary, ownership and internal-consistency corrections rather than design
failures. But the count went **up**, not down (17 → 21 HIGH), and the pattern is
instructive: several cycle-1 fixes were applied to a plan's *must-have* and
*narrative* without being propagated into the *task body*, the *`<files>` list*, or
the *acceptance criteria* — which is precisely the surface `/gsd-execute-phase`
actually reads. The next pass should treat "did this fix reach the task body and the
file list?" as the primary checklist, not "is the fix described somewhere".

## Environment note

Codex ran `--sandbox read-only` in the `v013-milestone` worktree at `926dcf0e2`.
Two passes: A completed in 17m (21 KB), B in 18m (22 KB), both exit 0. The worktree
was verified clean before and after — the reviewer modified nothing.


# Cycle 3 — two-reviewer re-review after the targeted fix pass

**Reviewed:** 2026-08-04 · **Tree:** `87b563913` ("docs(02): targeted fix pass for
ship-breaking and unexecutable plan findings") · **Plans:** 13 (`02-01`…`02-13`)
· **Reviewers:** Codex (`gpt-5.6-sol`, `model_reasoning_effort=xhigh`) and pi
(OpenRouter), four source-grounded passes each — 8 passes total.

**Verdict: NOT READY — a further fix pass is required.**

Cycles 1 and 2 were the same model (Codex). Cycle 3 added a second, different
model family specifically to break that correlation, and **it paid for itself
immediately**: the highest-severity plan-internal defect in this cycle
(`NormalizeCharacterName` has no valid execution path) and the sharpest
cross-artifact defect (the gate has no self-exclusion channel) were both found
by *both* reviewers independently, and the second of those had survived two
prior Codex cycles undetected.

## How this cycle was run

The 13 plans total ~600 KB — more than doubled since cycle 1 — so a single pass
cannot ground findings in source. Four semantic clusters, each ~210–250 KB of
inlined plan text plus live repo read access:

| Pass | Cluster | Plans |
| ---- | ------- | ----- |
| A | identity / charname / lifecycle / block list | 02-01, 02-02, 02-04, 02-05 |
| B | ABAC substrate: prefixes, attributes, seeds | 02-03, 02-13, 02-07 |
| C | ABAC consumers: profile visibility, admin sections, audit | 02-08, 02-09, 02-10 |
| D | admission, backfill + unique index, closeout | 02-06, 02-12, 02-11 |

Each pass received `PROJECT.md`, the milestone and Phase-2 roadmap sections,
`REQUIREMENTS.md`, all 26 locked `02-CONTEXT.md` decisions, a briefing on this
codebase's twelve recurring failure shapes, and read access to the tree. Both
reviewers ran read-only; the worktree was verified unchanged after.

**Every finding recorded below was independently re-verified by the orchestrator
against source before being written down.** Findings that did not survive that
check are excluded, and two of the orchestrator's *own* earlier assessments were
corrected by reviewer pressure (see "Corrections", below).

Two honest caveats about this cycle's mechanics:

- **A host restart destroyed the first run's artifacts mid-cycle.** Five lane
  invocations across two providers returned exit=1 / 0 bytes inside a 90-second
  window, and the qwen fallbacks failed identically — a network/host event, not
  provider quota. The review was re-run from scratch, gap-first (passes C and D
  before A and B, so the never-reviewed clusters landed first). All eight
  artifacts in this cycle come from the second, complete run.
- **pi's model changed mid-cycle.** The first run used
  `openrouter/anthropic/claude-opus-4.7`; that account exhausted its OpenRouter
  credit after one pass (HTTP 402 — the request reserves `prompt_tokens +
  max_tokens` up front). At the maintainer's direction the re-run used
  `openrouter/moonshotai/kimi-k3` for all four passes. The pi findings recorded
  here are all from kimi-k3.

## Cycle-2 disposition

**The fix pass did substantial, genuine work.** Five cycle-2 findings are
properly closed with executable plan text, and the structural graph survived a
large manifest expansion intact.

| Cycle-2 finding | Disposition | Evidence |
| --- | --- | --- |
| C2-6 RED staging cannot produce `000055` | **INCORPORATED** | `02-12:430,463,476` names the `WithGoMigrations` + `WithDisableGlobalRegistry` **pairing** and why either alone builds a different provider; cites `migrate_gointerleave_integration_test.go:210,214` and the `:121` comment |
| C2-9 (in `<automated>` gates) | **INCORPORATED** | All `<automated>` blocks now use `rg -o … \| wc -l` with `-eq`. The 4 residual `\|\| true` strings are the fix's own prose warning *against* it (`02-10:217,342`, `02-11:244,355`) |
| C2-10 `02-07` orders 2 and 3 tier floors | **INCORPORATED** | Task 1 heading `:143` and body `:158-160` say two; acceptance `:245` asserts `-eq 2`; `:246` asserts zero `player` entries |
| C2-15 invalid `Status` fails open | **INCORPORATED — exemplary** | Boot rejection naming entry+value `:140`; exhaustive `sectionAvailability` with denying `default:` returning `ADMIN_SECTION_STATUS_UNKNOWN` `:141,181-183`; and a **structural gate at `:227` banning the `== StatusPlanned` / `!= StatusAvailable` single-constant comparison that caused the fail-open** — a regression guard, not just a test |
| C2-20 CONTEXT D-03 unowned | **INCORPORATED** | `02-CONTEXT.md:59-77` carries the AMENDED note, grounded in the DSL list grammar at `internal/access/policy/dsl/ast.go:232-236` (empty `in []` does not parse) |
| C2-5 shared-interface blast radius | **PARTIAL** | 2 of 3 files now reachable by broadened verify (loud failure), though still undeclared in `files_modified`. `internal/access/policy/attribute/character_test.go:45` remains neither owned nor compiled — see B-10 |
| C2-9 (prose acceptance criteria) | **STILL OPEN, 33 instances** | See B-11 |
| C2-7 CLI test has no executable seam | **STILL OPEN** | See B-17 |
| C2-11 / C2-12 `abactest` producer/consumer | **STILL OPEN** | See B-6 |
| C2-13 `NewPropertyProvider` call site | **STILL OPEN** | See B-9 |
| C2-14 `BuildABACStack` proof unowned | **STILL OPEN** | Behavior/acceptance require it (`02-13:417,458`); frontmatter owns only `setup.go` + `subsystem.go` |
| C2-16 `Descriptor.Action` inert | **STILL OPEN** | See Corrections |
| C2-21 tier provenance unimplementable | **STILL OPEN / cosmetic** | Must-have still claims server-side derivation (`02-03:29`); only action is a doc comment (`:165`); no gateway/session root owned; yet the threat register calls it closed (`:364`) |

## Structural re-audit — clean

Cycle 2 deferred this pending manifest corrections ("re-run it after those are
corrected"). The fix pass grew `02-06` from 9→25 files and `02-12` to 29.
Re-derived programmatically across all 13 plans:

```
wave-graph violations:      0    (every depends_on target sits in a strictly lower wave)
same-wave file collisions:  0    (no two same-wave plans write the same file)
multi-owner files:         14    (all strictly sequential — distinct waves)
requirement coverage:      IDENT-06/07/08/09, PROFILE-11, EXT-07 each on >=2 plans
```

This is a real improvement and should not be re-litigated. **The caveat carried
from cycle 2 still applies**: the audit is only as good as the manifests, and
this cycle found five more files that plans instruct the executor to edit
without declaring (B-4, B-5, B-9, B-10, B-19).

## Cycle-3 blockers — 22, all orchestrator-verified against source

Ordered by consequence, not by plan number.

### Theme A — the phase does not compile (3)

**B-1 — Import cycle `world → charname → world`.**
`02-01:250` has `Gate.Check` call `world.ValidateCharacterName` from
`internal/charname/gate.go` (⇒ `charname → world`). `02-06` Task 2 puts
`charname.Admitted` on `world.CharacterRepository.Create`, with
`internal/world/repository.go` in Task 2's `<files>` (⇒ `world → charname`).
Verified clean today: `go list -deps ./internal/world` contains no `charname`;
`internal/world/repository.go:197,201` confirms the interface and current
signature. Go rejects the build.

Compounding: **both directions are `<automated>` gates** — `02-01`'s
`go list -deps … -eq 0` and `02-06:401` — so the phase cannot go green in either
direction, and no plan says which edge dies. Worse, `02-06:401` cannot even
*execute* under the cycle: `go list` fails, prints nothing, and its
C2-9-shaped `rg -c … returns 0` criterion is unobservable. This will present as
a cryptic wave-4 gate failure, not as "your two plans disagree".

*Fix:* extract the syntactic rules into a dependency-free leaf (e.g.
`internal/charname/syntax`) that both `charname.Gate` and a thin
`world.ValidateCharacterName` wrapper call; then `world → charname.Admitted`
compiles.

**B-3 — `NormalizeCharacterName` has no valid execution path.** *(Both reviewers,
independently.)* `02-01:313-316` forbids touching
`internal/auth/character_service.go` — enforced by a `git diff --name-only`
acceptance criterion — while `02-01:317-321` orders the body deleted "and if
nothing else calls it, delete it and fix the callers". The **only** production
caller is `internal/auth/character_service.go:105`. Emptying the body reddens
`internal/world/character_test.go:539-593` (14 subtests: `alaric`→`Alaric`,
`ÉLISE`→`Élise`, `groß`→`Groß`) and `internal/auth/character_service_test.go`
(13 `"Alaric"` assertions, incl. `// normalized to Initial Caps` at `:96`).
02-01 owns neither test file. ⇒ `task test` red across waves 1–4.

*Fix:* leave the body intact behind a `// Deprecated:` marker; move removal and
the two test-file updates into `02-06`, which already owns the caller. Delete
02-01's "no production call site still performs title-casing" criterion — it
belongs to 02-06.

**B-4 — `[17]stubSubsystem` is a fixed-size array type.**
`cmd/holomush/core_subsystems_test.go:55` declares `func allStubs() [17]stubSubsystem`
(also `:79`, and `len(subs) != 17` at `:116`); `cmd/holomush/core_topo_order_test.go:159`
does `require.Len(t, graph, 17, …)`. Neither file appears in **any** of the 13
plans' `files_modified`. Adding the block-list subsystem is therefore a
**compile** error, so 02-05's own `task test -- ./cmd/holomush/` gate cannot pass.

### Theme B — shipped behavior is wrong (4)

**B-2 — The character-read permit is unconditional and exposes far more than
`description`.** `02-07:376` adds
`permit(principal is character, action in ["read"], resource is character);`
with **no `when` clause**. `internal/world/service.go:785` gates `GetCharacter`
on exactly that action/resource, and `internal/world/grpc_server.go:127-137`
(`characterToProto`) returns `Id`, **`PlayerId`**, `Name`, `Description`,
**`LocationId`**. The plan justifies it as carrying `characters.description`
(PROFILE-10a); it carries the whole `CharacterInfo`.

Three compounding elements, all verified:
1. **Guests are included** — the principal test is `principal is character`, and
   guests have characters. Every ephemeral guest can enumerate alt-to-player
   ownership and live locations for the entire roster.
2. **The phase's own privacy gate never adjudicates it** — 02-10's audit scope is
   descriptions plus public `entity_properties` rows, not the
   `PlayerId`/`LocationId` projection.
3. **D-11's mandated remedy does not exist here.** The plan's block comment
   repeats D-11 — "the fix for any such row is to change the row's `visibility`".
   Verified: `characters` (`000001_baseline.sql:72-79`) is
   `id, player_id, name, description, location_id, created_at` — **no
   `visibility` column**, and no plan adds one. `entity_properties` has one.
   D-10/D-11 accepted the widening on the strength of an escape hatch that
   exists for property rows and does not exist for characters.

Scope precision: this is character→character, not anonymous-web — an anonymous
viewer has no character. It is still alt-linking plus live position across the
whole roster.

*Fix:* do not install the broad permit before the projection is narrowed. Either
move it to Phase 4 with the projection change, or introduce field-specific
actions (`read_profile_description`) evaluated by a narrow facade returning only
those columns. If `PlayerId`/`LocationId` exposure is intended, that is a
decision that must be stated and audited, not inherited by omission.

**B-7 — Confusable detection is permanently unverifiable for pre-existing rows.**
`02-01:289` creates `idx_characters_name_skeleton` **non-unique** (restated
`:500`; `:342` asserts zero `CREATE UNIQUE INDEX` in `000054`). `02-12`'s
collision detection references only `normalized_name` (`:503,:532,:616`) — zero
skeleton-collision references — and `000056`'s unique index is on
`normalized_name` alone. NFKC deliberately does not collapse cross-script
confusables; that is *why* the skeleton mechanism exists separately. ⇒ Two
pre-existing rows that are confusables of each other are never detected by
anything in the phase. Additionally, in the wave-4 window a new confusable of an
existing row is **admitted**, because the skeleton lookup hits a NULL column.
Defeats ROADMAP success criterion 1 / IDENT-06 for the pre-existing corpus.
*(Filed MEDIUM by pi; promoted to HIGH on verification.)*

**B-20 — Skeleton admission is a check-before-insert race.** The non-unique index
(rationale — the confusables table shifts between Unicode versions — is
defensible on its own terms) means nothing serializes the check: `Gate.Check`
reads `SkeletonExists`, the write happens later. Two concurrent creates with the
same skeleton but *different* normalized names both read "not exists" and both
insert; `000056`'s unique index cannot stop them, because differing normalized
names is precisely what makes a pair confusable. `rg 'UNIQUE.*skeleton'` across
all 13 plans: zero.

Taken with B-7, **the confusable guarantee has no storage-layer enforcement at
all** — it is an advisory read before a write. ROADMAP criterion 1 ("rejected
server-side") holds only for the sequential single-writer case. This is as much
a framing defect as a code one: the plans assert an enforced guarantee where the
mechanism is best-effort.

**B-18 — The gate has no self-exclusion channel, so the SPEC-decided
case-variant rename is structurally impossible.** *(Both reviewers,
independently; missed by both prior Codex cycles.)* `01-SPEC.md:702-706` is
settled: "A request whose normalized **uniqueness key** matches the current one
but whose **display form differs** — a case or spacing variant — is a **real
rename** … It does not collide with itself in the uniqueness index, because the
row it would collide with is its own." But `02-01:241` defines
`SkeletonExists(ctx, skeleton string) (bool, error)` with no character-id
parameter, and `Gate.Check(ctx, submitted)` carries no self identity.
`rg -in 'self-exclu|own row|excluding the character'` across all 13 plans:
**zero hits**. Renaming "Alaric" → "ALARIC" computes the identical skeleton,
matches the character's own row, and returns `NAME_CONFUSABLE`. Since
`charname.Admitted`'s only constructor is the gate (`02-06:61,66`), Phase 3's
`RenameCharacter` cannot route around it. Retrofit cost is amplified by 02-06's
census rule C, which pins the constructor set. The normalized-name half of the
same problem belongs to 02-06.

### Theme C — gates that cannot catch what they claim (6)

**B-5 — Migration version/count constants are unowned; `task test:int` breaks
from wave 1.** `internal/store/migrate_integration_test.go:31` declares
`const latestMigrationVersion = 53`, asserted at `:181` and `:204`; `:45`
declares `expectedAppliedMigrationRows = 44`. The file's own comment (`:41-42`)
says "Bump these together with latestMigrationVersion when a migration is
added." The phase adds `000054` (02-01), `000055` and `000056` (02-12) ⇒ 56 / 47.
**No plan owns the file.** Breaks the moment 02-01 lands and stays broken every
subsequent wave.

**B-6 — `abactest.NewSeedEngine` is unconstructible as specified.** `02-07:476`
tells `abactest` to mirror `createSeedEngine`'s construction. That construction
(`internal/access/policy/engine_test.go:921-927`) does
`cache := NewCache(nil, nil); cache.mu.Lock(); cache.snapshot = &Snapshot{…}` —
assignment to the **unexported** `snapshot` (`cache.go:62`) under unexported
`mu`. `NewEngine(resolver, cache *Cache, …)` (`engine.go:71`) takes a
**concrete** `*Cache`, so no fake substitutes; the only exported population path
is `NewCache(store.PolicyStore, …)` + `Reload`. A non-test package at
`internal/testsupport/abactest` cannot do this — Go forbids the cross-package
access. 02-08 and 02-09 verify with plain unit tests (no DB), so the
store-backed path is not silently available either.

Cycle 1 correctly diagnosed "unexported and in a `_test.go`"; the fix moved the
**name** across the package boundary while the **mechanism** stayed behind it.
Three plans' RED-first gates depend on this helper.

Related and unresolved (C2-12): 02-08 requires the double to derive player peers
from character-keyed fixtures via a char→player map (`02-08:291`); 02-07, which
owns the file, promises only schema parity (`02-07:457`). Schema parity proves a
key is declared, not how its value was derived — so the downstream privacy suite
can pass against a fabricated identity model.

**B-15 — 02-08's forbid-side ANY subtest is overdetermined (a test that cannot
fail).** D-27 (`02-CONTEXT.md:89-93`) makes permit-side peers ALL-direction.
With fixture `visible_to=[C2]` and player P holding {C1,C2}, ALL is unsatisfied
⇒ `visible_to_players` is empty ⇒ the permit twin never matches ⇒ default-deny
fires **whether or not the forbid-side derivation exists at all**. Delete the
forbid derivation and the test stays green.
`internal/access/policy/engine.go:591-597` confirms deny-overrides (returns on
the first satisfied deny). *Discriminating fixture:* `visible_to=[C1,C2]` **and**
`excluded_from=[C1]` — the permit fires, so only the forbid stands between viewer
and row.

**B-19 — INV-WORLD-5's binding test cannot reach the path the invariant names.**
*(Both reviewers.)* `invariants.yaml:5065-5073` claims exclusion "from character
**selection**"; the only selection path is `CoreServer.SelectCharacter`
(`internal/grpc/auth_handlers.go:243`, which finds any owned character without
consulting status at `:265`). The fix pass mandates the world suite, which builds
`world.Service` + `CharacterReapingService` — not a `CoreServer` with
player-session plumbing. Task 2's acceptance (`02-04:271-285`) pins only that the
spec contains a direct `INSERT INTO characters` carrying `'idle'`. The natural
execution re-implements the predicate in the test and passes with the
`SelectCharacter` wiring entirely absent; its pre-Task-1 "RED" is then only a
compile error. Task 3 then flips the registry entry to `bound`. ⇒ a `bound`
invariant while production violates it — the false-green the registry rules
explicitly forbid. The cheap real pin is unowned:
`internal/grpc/auth_handlers_test.go:210` (`TestSelectCharacter`, fed by
`ListByPlayer` at `:225-237`).

**B-16 — 02-11's D-03 check is a self-healing false-green.** `02-11:221` says to
count seeded tier policies and, **if the count differs, update D-03**. Given code
that emits an extra player policy or omits the guest policy, Task 1 rewrites the
locked artifact until equality is green — laundering authorization drift into the
specification rather than detecting it. A verification step must fail on
disagreement, not reconcile it.

**B-11 — 33 acceptance criteria still fail on their own success path.** The fix
pass repaired C2-9 only in `<automated>` blocks; 33 **prose** criteria across all
13 plans still read ``rg -c PAT F`` returns 0``. Empirically confirmed: `rg -c`
at zero matches prints **nothing** and exits **1**, so "returns 0" is
unachievable under either reading. Correct form is `rg -o PAT | wc -l` compared
with numeric `-eq` (it emits leading whitespace, so string `= "0"` would also
fail).

Distribution: 02-01(3) 02-02(1) 02-03(2) 02-04(3) 02-05(2) 02-06(2) 02-07(2)
02-08(2) 02-09(3) 02-10(1) 02-11(1) 02-12(7) 02-13(3).

Severity is driven by *which* guards these are:

| Criterion | Guards against |
| --- | --- |
| `02-13:384-385`, `02-03:291-292` | ABAC empty-string / empty-list **sentinel** — the fail-open shape in `.claude/rules/abac-providers.md` |
| `02-04:278` | `Skip(`-only placeholder ⇒ **false-green invariant binding** |
| `02-04:283` | `AllowAllEngine`/`DenyAllEngine` ⇒ **test that cannot fail** |
| `02-12:309-315` | Migration transactionality + dual-predicate retirement |
| `02-06:401` | The import-cycle guard — which B-1 shows cannot execute at all |

A guard permanently red on its own success path is a guard an executor deletes.

### Theme D — ownership and unexecutable instructions (5)

**B-9 — C2-13 still open.** `test/integration/access/access_suite_test.go:182`
still calls `attribute.NewPropertyProvider(propRepo, parentLocResolver)` with two
arguments. 02-13 changes the signature to three and owns `property.go`,
`property_test.go`, `setup.go` — but **no plan owns `access_suite_test.go`**.
Integration-tagged, so `task test` cannot catch it; it breaks in a later wave.

**B-10 — C2-5 narrowed but genuinely open for one file.**
`internal/access/policy/attribute/character_test.go:45` holds
`mockCharacterRepository.Create`, a **manual** implementation of
`world.CharacterRepository` whose `Create` 02-06 changes (`02-06:693-694`). It is
untagged, in package `attribute`. `02-06:674` scopes verification to
`./internal/charname/ ./internal/world/... ./internal/auth/
./internal/bootstrap/... ./test/meta/` — **no `./internal/access/`**. Neither
owned nor compiled by any 02-06 gate; surfaces in 02-13's wave, forcing a later
plan to repair a break it did not make.

**B-8 — 02-06's NULL-visibility test is destroyed in wave 5 with no owner.**
`02-06:574` mandates "A test asserts a row with `normalized_name IS NULL` and a
matching `LOWER(name)` IS reported as existing". `02-12:54,73` land `SET NOT
NULL` in `000056` ⇒ the fixture becomes un-insertable, and `:265-276` deletes the
NULL branch. Searching 02-12 for any removal of that test: **zero hits**; 02-06
never names the file. A wave-4 gate destroyed in wave 5 with no owner.

**B-17 — CLI duplicate-resolution test is unexecutable (C2-7 stands).** *(Both
reviewers.)* `test/integration/charname` (package `charname_test`) cannot import
`cmd/holomush` (`package main`), and no subprocess recipe is specified. The
likely executor "repair" — calling `Gate.Admit` + `Rename` directly — would pass
while `holomush character name set` is broken.

**B-13 — 02-10's approved remediation is never executed.** Task 4 is solely a
`checkpoint:decision` with no `<action>`, files, command, or verification. Given a
`remediate` verdict, the plan completes while the sensitive row remains public.
The privacy gate fails open on its own remedy path.

### Theme E — the privacy audit does not cover what it must (2)

**B-14 — 02-10's audit never reads the population it must adjudicate.** *(Both
reviewers.)* Enumerated public rows (`profile.pronouns`, `profile.biography`, …)
appear only as **aggregate counts**; no query selects their text for
adjudication. Those are precisely the rows the widening publishes, since term A
(tier floor) matches only enumerated names. ROADMAP success criterion 4 requires
establishing "exactly **which** existing rows" the policy exposes — a count is
not which rows.

**B-12 — INV-WORLD-4 violation: the CLI rename bypasses the outbox.**
`docs/architecture/invariants.yaml:5048-5056` states there are "exactly **TWO**
sanctioned out-of-world writers, each emitting its envelope atomically" —
character-genesis and character-reaping. `02-12:388` has the CLI open a pool and
call `CharacterRepository.Rename` directly ⇒ a **third** out-of-world writer,
emitting no envelope/outbox event.

### Also recorded (2)

**B-21 — `Selectable`'s zero-status denial breaks test-backed stacks.** 02-04
routes `SelectCharacter` through `world.Selectable` and makes unknown/zero status
deny. Existing materializers that do not set `status` produce a zero value, so
active characters are denied in test-backed stacks — breaking the plan's own
scoped gates.

**B-22 — 02-01's blocking checkpoint offers an unexecutable branch.** Task 1's
checkpoint permits an `eskriett-dependency` option explicitly described as "No
generator" (`02-01:152`), but downstream tasks require the generated artifacts
(`confusables_table_gen.go`, the `UnicodeVersion` const). Selecting that branch
leaves the rest of the plan unexecutable, and the checkpoint does not say so.

## Where the two reviewers converged

Convergence across different model families is the strongest signal in this
cycle. Found independently by both:

| Finding | Codex | pi (kimi-k3) |
| --- | --- | --- |
| B-3 `NormalizeCharacterName` unexecutable | pass A | pass A |
| B-18 gate has no self-exclusion channel | pass A | pass A |
| B-19 INV-WORLD-5 false binding | pass A | pass A |
| B-6 `abactest` helper unusable | pass B, C | pass B, C |
| B-14 audit misses the population | pass C | pass C |
| B-17 CLI test unexecutable | pass D | pass D |
| B-10 shared-interface break unreached by gates | pass D | pass D |
| C2-16 `Descriptor.Action` still inert | pass C (HIGH) | pass C (MEDIUM) |

Reviewer-unique but verified: B-5, B-12, B-13, B-16, B-21, B-22 (Codex); B-7,
B-15, B-2's three extensions (pi).

## Corrections to earlier orchestrator assessments

Recorded because both were caught by reviewer pressure, and both are the same
mistake:

- **C2-16 (`Descriptor.Action` inert) is NOT fixed.** It was initially marked
  fixed on the strength of `02-09:88-90` *stating* "Task 2 makes it
  participate". Verified since:
  `rg 'Descriptor\.Action|desc\.Action|\.Action\b'` over all of `02-09-PLAN.md`
  returns **zero matches**, and the participation clause reads "…if the entry's
  **`Descriptor.Resource`** does not equal the resource string it evaluated".
  Resource participates; Action remains decorative. Reading the plan's *claim*
  instead of tracing the *mechanism* is exactly the failure this cycle was
  chartered to hunt.
- **B-7 was filed MEDIUM and is HIGH.** The permanent-non-detection consequence
  only appears when the non-unique skeleton index and `000055`'s
  `normalized_name`-only collision scan are read together.

## Recommended fix order

1. **Decide which import edge dies** (B-1) — nothing else in the identity track
   can be validated until the phase compiles. Extract a dependency-free syntax
   leaf.
2. **Settle the character-read permit** (B-2) — it is the only finding that
   changes what production exposes. Narrow it, or state and audit the
   `PlayerId`/`LocationId` exposure as a decision.
3. **Decide what the confusable guarantee actually is** (B-7 + B-20) — enforced
   or advisory. This is a spec-level answer, not a plan edit, and ROADMAP
   criterion 1's wording depends on it.
4. **Add the self-exclusion channel** (B-18) before the constructor set is
   pinned by 02-06's census.
5. **Own the five unowned files** (B-4, B-5, B-9, B-10, B-19) and re-run the
   collision audit afterward.
6. **Replace the 33 broken criteria** (B-11) mechanically — `rg -o … | wc -l`
   with `-eq`.
7. **Repair the false-green gates** (B-15, B-16, B-19) — each needs a
   discriminating fixture or a real production seam, not a reworded assertion.
8. The remainder (B-8, B-12, B-13, B-14, B-17, B-21, B-22) are ownership and
   executability corrections of the shape cycle 2 already characterized.

## What this does NOT mean

The architecture remains sound and the fix pass was real work: five cycle-2
findings are properly closed, `02-09`'s status handling is now exemplary
(a structural gate banning the *comparison shape* that caused the fail-open, not
merely a test), the D-03 amendment is correctly grounded in the DSL grammar, and
the wave graph survived a large manifest expansion with zero collisions.

But the count did not fall (21 → 22), and the *character* of the findings
changed. Cycles 1 and 2 were dominated by plan-boundary and ownership defects.
Cycle 3 surfaced four defects in the **substrate design itself** — an import
cycle, a missing self-exclusion channel, a confusable guarantee with no
storage-layer enforcement, and a permit that grants more than its plan describes.
Those are not fixable by propagating text into task bodies; three of them need a
decision before any plan edit is meaningful.

The single most useful process change this cycle was **adding a second model
family**. Two of the four substrate defects were found by both reviewers
independently on the first pass, after surviving two same-model cycles.

## Environment note

Both reviewers ran read-only in the `v013-milestone` worktree at `87b563913`;
`git status` was clean before and after. Codex: 4 passes, `gpt-5.6-sol` at
`model_reasoning_effort=xhigh`, 16–24 min each, all exit 0 (20.2 / 19.8 / 23.3 /
24.7 KB). pi: 4 passes, `openrouter/moonshotai/kimi-k3` at `--thinking xhigh`,
with `edit` and `write` tools excluded (`-xt edit,write`), all exit 0 (24.0 /
24.9 / 20.6 / 27.5 KB).

A first run of this cycle was destroyed by a host restart; see "How this cycle
was run". The pi lane's first pass used `claude-opus-4.7` before credit
exhaustion forced the switch to kimi-k3 — the findings recorded above are from
the completed kimi-k3 run.

---

# Cycle 3 — verbatim reviewer output

The eight passes below are the reviewers' unedited output. The synthesis above
records only findings the orchestrator independently re-verified against source;
anything here that is not in the synthesis did not survive that check, or was
judged already-settled. Cross-plan claims in one pass may reference plans the
reviewer did not own — treat those as leads, not verified findings.

## Codex Review

### Codex — pass A (02-01, 02-02, 02-04, 02-05)

#### Cycle 3 review — pass A

Overall verdict: **NOT READY**. Of the nine Cycle-2 findings in this range, five are genuinely fixed, three remain open, and C2-1 was moved into an import cycle. I found six additional HIGH blockers: self-colliding renames, a concurrent skeleton-admission race, an inconsistent Unicode decision branch, lifecycle test-fixture fallout, a false-green invariant binding, and an impossible RED requirement.

I did not re-report the six settled Cycle-3 findings except where needed to classify a Cycle-2 fix.

##### Plan 02-01 — Unicode normalization and confusable skeleton

### Summary

The normalization and generated-table design is detailed, but the plan is not executable. The C2-1 repair creates the already-confirmed import cycle, and the proposed skeleton lookup is unsuitable for both rename and concurrency. Those are architectural defects in the Gate API, not missing tests.

### Cycle-2 fix verification

- **C2-1 — cosmetically fixed / moved.** The Gate now calls the real `world.ValidateCharacterName` at [02-01-PLAN.md:250](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:250), whose production checks are at [validation.go:59](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/validation.go:59). That genuinely makes the Gate’s local verdict complete. But it creates `charname → world`, while 02-06 makes the root `world.CharacterRepository` interface depend on `charname.Admitted` at [02-06-PLAN.md:290](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:290). This is the settled `world → charname → world` cycle, so the mechanism remains unexecutable.
- **Cycle-2 MEDIUM 1 — still open.** Frontmatter ends at [02-01-PLAN.md:23](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:23), but Task 3 modifies `internal/charname/version_test.go` at [02-01-PLAN.md:362](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:362).

### Concerns

- **HIGH — every real case/spacing rename collides with itself.** `SkeletonLookup` accepts only a skeleton and returns a boolean; it has no current-character exclusion ([02-01-PLAN.md:240](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:240)). The prescribed query is literally `WHERE name_skeleton = $1` ([02-01-PLAN.md:299](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:299)). Given a rename from `Alaric` to a case or spacing variant, the candidate skeleton matches the same row, so Gate returns `NAME_CONFUSABLE`. That contradicts the normative requirement that same-key/different-display is a real rename and must not collide with itself ([01-SPEC.md:694](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/01-portal-spec/01-SPEC.md:694)).

- **HIGH — skeleton admission has a check-before-insert race.** The plan deliberately creates only a non-unique skeleton index ([02-01-PLAN.md:278](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:278)), and 02-06 obtains the admission token before calling genesis ([02-06-PLAN.md:440](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:440)). The real transaction starts later, inside `CharacterGenesisService.Create` ([character_genesis.go:162](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_genesis.go:162)). Given two concurrent, differently normalized names with the same skeleton, both `SELECT EXISTS` calls can return false and both inserts succeed; the normalized-name unique index does not help because the keys differ. That ships the impersonation IDENT-06 is supposed to prevent.

- **HIGH — the blocking checkpoint offers a branch the remaining plan cannot execute.** Task 1 permits `eskriett-dependency`, explicitly described as “No generator” ([02-01-PLAN.md:152](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:152)). Tasks 2 and 3 then unconditionally require the generator, generated file, digest test, and `go:generate` wiring ([02-01-PLAN.md:167](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:167), [02-01-PLAN.md:361](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:361)). Selecting an explicitly allowed answer makes the rest of the plan contradictory.

- **MEDIUM — the generator’s digest test has no owned or executed artifact.** Acceptance requires a byte-modified `-input` test at [02-01-PLAN.md:354](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:354), but Task 2 owns only `main.go`, not `main_test.go`, and verification runs `./internal/charname` plus integration tests rather than `./cmd/internal/gen-confusables` ([02-01-PLAN.md:167](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:167), [02-01-PLAN.md:332](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:332)).

### Suggestions

- Move the syntactic rule into a dependency-free `internal/charname/syntax` leaf used by both Gate and the compatibility `world.ValidateCharacterName` wrapper. That removes `charname → world`.
- Split create and rename lookup contracts, for example `SkeletonExistsExcept(ctx, skeleton, characterID)`.
- Make the skeleton recheck part of the write transaction and serialize equal skeletons, such as with a transaction-scoped advisory lock keyed from the skeleton. Preserve the required non-unique index.
- Remove the dependency checkpoint alternative unless the remainder of the plan has a complete alternative branch.
- Add `internal/charname/version_test.go` and `cmd/internal/gen-confusables/main_test.go` to frontmatter and run both packages.

### Risk Assessment

**HIGH.** The current shape cannot compile across plans, rejects valid renames, and permits concurrent confusable claims.

##### Plan 02-02 — mixed scripts and username regression

### Summary

The behavioral design is mostly sound. It correctly preserves the existing ASCII username policy—the production regex and validator are exactly where the plan says they are ([player.go:28](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/player.go:28), [player.go:153](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/player.go:153)). Its only material range-local defect is the still-incomplete ownership manifest.

### Cycle-2 fix verification

- **Cycle-2 MEDIUM 2 — still open.** Frontmatter lists neither `internal/charname/pipeline.go` nor `pipeline_test.go` ([02-02-PLAN.md:7](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-02-PLAN.md:7)), while Task 2 explicitly modifies both ([02-02-PLAN.md:176](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-02-PLAN.md:176)). Commit `87b563913` did not touch this plan.

### Concerns

- **MEDIUM — ownership remains internally contradictory.** Given execution tooling that uses frontmatter for overlap and ownership analysis, Task 2 changes files the plan says it does not modify. The tests can be correct while the plan scheduler still produces a false clean collision audit.

I found no additional source-grounded behavioral blocker in 02-02 itself. Its dependency on broken 02-01 remains an inherited phase blocker.

### Suggestions

Add `internal/charname/pipeline.go` and `internal/charname/pipeline_test.go` to `files_modified`. Keep the file-scoped username separation guard; the source confirms `ValidateUsername` is self-contained around `usernameRegex`.

### Risk Assessment

**MEDIUM standalone; HIGH through dependency.** The mixed-script work is plausible, but it cannot execute after the current 02-01.

##### Plan 02-04 — lifecycle vocabulary and invariant bindings

### Summary

The Cycle-2 corrections to projection semantics and test placement are real. The new pass nevertheless missed the status field’s existing test-adapter blast radius, demands an impossible RED result for behavior that already works, and would falsely bind an “every read” invariant with one behavioral path.

### Cycle-2 fix verification

- **Cycle-2 MEDIUM 8 — genuinely fixed.** The plan now restricts the zero-`Status` assertion to `ListAll` and explicitly recognizes that `GetNamesByIDs` returns a map ([02-04-PLAN.md:121](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-04-PLAN.md:121)). Source confirms the signatures and projections ([character_repo.go:366](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/postgres/character_repo.go:366), [character_repo.go:401](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/postgres/character_repo.go:401)).
- **Cycle-2 MEDIUM 9 — genuinely fixed.** The plan commits to `test/integration/world/character_lifecycle_test.go`, constructs both real services there, and keeps `asserted_by` aligned with that file ([02-04-PLAN.md:242](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-04-PLAN.md:242), [02-04-PLAN.md:318](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-04-PLAN.md:318)).
- The correction to INV-WORLD-6’s text is also substantive: source really does have a second tombstone-emitting deleter ([character_reaping.go:90](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_reaping.go:90), [character_reaping.go:263](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_reaping.go:263)).

### Concerns

- **HIGH — adding `Selectable` breaks the plan’s own unit and integration gates because existing materializers produce zero status.** The plan routes `SelectCharacter` through `world.Selectable` and makes unknown/zero status deny ([02-04-PLAN.md:124](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-04-PLAN.md:124), [02-04-PLAN.md:176](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-04-PLAN.md:176)). Existing successful handler tests return `world.Character` literals with no status, such as [auth_handlers_test.go:223](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/grpc/auth_handlers_test.go:223); that file is absent from frontmatter. Both integration adapters likewise scan no `status` or `last_active_at`, leaving zero values ([harness.go:1573](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/testsupport/integrationtest/harness.go:1573), [auth_suite_test.go:321](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/test/integration/auth/auth_suite_test.go:321)). Given an active character, those paths now deny selection, and the prescribed `task test -- ./internal/grpc/` cannot pass.

- **HIGH — INV-WORLD-5 would be falsely bound.** The registry guarantee is “every read of `characters.status` is an exhaustive switch” ([invariants.yaml:5065](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/docs/architecture/invariants.yaml:5065)). The proving test only drives idle/retired/active through the one selection path ([02-04-PLAN.md:212](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-04-PLAN.md:212)), while the plan explicitly declines a census ([02-04-PLAN.md:359](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-04-PLAN.md:359)). That test would remain green if another reader later used `status != retired`, so it does not prove the registry summary.

- **HIGH — the demanded INV-WORLD-6 RED observation is impossible against the baseline.** The plan requires both invariant specs to produce pre-fix non-zero exits and specifically names the delete-releases-name half ([02-04-PLAN.md:378](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-04-PLAN.md:378)). Today, creation already rejects a name while any same-name row remains ([character_service.go:112](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_service.go:112), [adapters.go:38](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/bootstrap/setup/adapters.go:38)), and `world.Service.DeleteCharacter` removes the row ([service.go:742](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/service.go:742)). Therefore retire/direct-status-update preserves the reservation and delete releases it before 02-04 changes anything. The intended regression test is green on the baseline.

### Suggestions

- Add `internal/grpc/auth_handlers_test.go`, `internal/testsupport/integrationtest/harness.go`, and `test/integration/auth/auth_suite_test.go` to this plan; populate `StatusActive` and scan both lifecycle columns.
- Add a meta/AST census that inventories lifecycle-decision reads and requires them to route through `world.Selectable`; bind INV-WORLD-5 only after that structural proof passes.
- Record INV-WORLD-6 as an existing-behavior regression pin. Do not claim or require a pre-fix RED result that cannot exist.

### Risk Assessment

**HIGH.** As written, the scoped test command fails, full integration behavior denies active characters in test-backed stacks, and one invariant binding is false-green.

##### Plan 02-05 — configurable block list

### Summary

The core Cycle-2 block-list fixes are substantive: the cache now has a named transport, bootstrap has the required dependency, and the malformed-value fail-open is addressed. The plan still carries the already-confirmed production subsystem compile break and leaves two important implementation contracts underspecified.

### Cycle-2 fix verification

- **C2-2 — genuinely fixed.** `Matcher()` returns the live cache, both config structs carry the subsystem, and `core.go` populates both from one instance ([02-05-PLAN.md:393](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-05-PLAN.md:393)). Those are the real construction seams visible in [core.go:441](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/cmd/holomush/core.go:441) and [core.go:675](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/cmd/holomush/core.go:675).
- **C2-3 — genuinely fixed.** The plan explicitly adds `Bootstrap → BlockList` ([02-05-PLAN.md:381](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-05-PLAN.md:381)); source confirms bootstrap currently constructs the character service during `Prepare` ([subsystem.go:171](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/bootstrap/setup/subsystem.go:171)).
- **C2-4 — still open.** Frontmatter still omits `core_subsystems_test.go` and `core_topo_order_test.go`, despite source having the fixed `[17]stubSubsystem` array and seventeen-member real graph ([core_subsystems_test.go:55](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/cmd/holomush/core_subsystems_test.go:55), [core_topo_order_test.go:112](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/cmd/holomush/core_topo_order_test.go:112)). This is the settled Cycle-3 compile failure.
- **Cycle-2 MEDIUM 10 — genuinely fixed.** The `StringSliceN` check now strips comments and uses `rg -o | wc -l` ([02-05-PLAN.md:433](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-05-PLAN.md:433)).

### Concerns

- **HIGH — the production subsystem test ownership remains missing.** Given the new lifecycle ID and `productionSubsystemSet` member, the fixed-size array cannot accept the new entry and the real graph still asserts exactly seventeen members. The plan’s instruction to “enumerate” tests does not authorize edits to either file. This ships a compile failure in `cmd/holomush`.

- **MEDIUM — the raw loader’s not-found contract is unspecified.** The plan defines `RawGetter.GetSystemInfo` and says “no such row” means an empty list ([02-05-PLAN.md:307](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-05-PLAN.md:307)). The raw production method returns a wrapped `store.ErrSystemInfoNotFound` ([postgres.go:69](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/postgres.go:69)); the existing settings adapter explicitly translates that into `settings.ErrNotFound` ([game.go:28](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/settings/game.go:28)). The plan names neither sentinel nor an adapter contract. Given an absent row, a straightforward implementation can classify the valid empty state as a database failure and abort startup.

- **MEDIUM — the lifecycle subsystem does not mechanically require idempotence.** The plan requires working `Prepare`, `Activate`, and `Stop`, but has no behavior or acceptance test for repeated calls ([02-05-PLAN.md:354](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-05-PLAN.md:354)). The actual subsystem contract requires all three to be idempotent and requires `Stop` to handle partial preparation and retries ([lifecycle/subsystem.go:95](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/lifecycle/subsystem.go:95)). Given an Activate failure elsewhere followed by rollback and retry, an unguarded implementation can start two pollers or retain a cancelled poller.

### Suggestions

- Add both production subsystem tests to frontmatter and update the array, `productionSubsystemSet`, real graph, and expected count atomically.
- Define a concrete storage seam, including the not-found sentinel and the `(updated_at, hash)` query. Prefer a blocklist-local adapter returning `(value, found, error)` so `Load` cannot guess which package’s sentinel it received.
- Add repeated `Prepare`, repeated `Activate`, and repeated/partial-state `Stop` tests. Reset cancellation/wait state so a retry starts exactly one poll loop.
- Extract a small config-assembly helper in `core.go` if pointer identity between the bootstrap and gRPC configs is meant to be tested; the current local variables are otherwise not observable by `core_test.go`.

### Risk Assessment

**HIGH.** The cache design is improved, but the current plan still cannot compile and leaves boot behavior dependent on guessed error and lifecycle contracts.

#### Overall Assessment

**Risk: HIGH. Do not execute this plan set yet.**

Cycle-2 disposition for this range:

- **Genuinely fixed:** 02-04 MEDIUM 8 and 9; C2-2; C2-3; 02-05 MEDIUM 10.
- **Cosmetic/moved:** C2-1.
- **Still open:** 02-01 MEDIUM 1; 02-02 MEDIUM 2; C2-4.

Recommended revision order:

1. Remove the `world ↔ charname` cycle and redesign Gate for rename self-exclusion and transactionally serialized skeleton admission.
2. Repair 02-04’s status propagation blast radius and invariant proof before binding anything.
3. Remove the impossible INV-WORLD-6 RED requirement.
4. Complete 02-05’s production subsystem ownership and storage/lifecycle contracts.
5. Finish the remaining frontmatter and test-artifact ownership corrections.

This was a read-only, source-grounded review; no repository files were modified and no implementation tests were run.

---

### Codex — pass B (02-03, 02-13, 02-07)

#### Cycle 3 review — pass B

##### Plan 02-03 — viewer/resource vocabulary

### Summary

The prefix and omit-don’t-sentinel design is sound, and the cycle-2 empty-identifier contradiction is fixed. The role lookup was also correctly moved behind a function seam because `RoleStore` does not expose `PlayerRoles` ([role_store.go:15](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/role_store.go:15)). However, the plan still claims to secure the most important trust boundary—server-derived viewer tiers—using only token validation and a comment. It cannot prove or enforce provenance.

### Cycle-2 fix verification

| Finding | Verdict |
|---|---|
| **C2-21: tier provenance** | **Still open.** The must-have still promises server-derived tier provenance ([02-03:29](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:29)), but the only implementation is a doc comment ([02-03:165](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:165)). No facade/session construction root is owned. |
| **MEDIUM #20: anonymous empty identifier contradiction** | **Genuinely fixed.** The must-have now distinguishes anonymous from guest/player ([02-03:24](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:24)), and the behavior exercises both the anonymous success and identified-rung failures ([02-03:124](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:124)). |
| **Vacuous seed-coverage check** | **Genuinely fixed.** It moved to 02-07 after viewer-referencing seeds exist ([02-03:332](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:332)); the production sweep really scans `SeedPolicies()` ([seed_coverage.go:43](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/setup/seed_coverage.go:43)). |
| **MEDIUM #16: stale ownership text** | **Still open.** The dependency note still says 02-03 touches `internal/store/role_store.go` ([02-03:74](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:74)); only 02-13 owns it. |

### Concerns

- **HIGH — viewer-tier provenance remains documentation-only.** `ViewerSubject(tier string, …)` can reject `"spectator"`, but it cannot distinguish a trusted `"player"` from the exact same string supplied by an attacker. The plan nevertheless claims the unrecognized-token test “stops a client-supplied string” ([02-03:340](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:340)). The resolver validates only the structural `type:id` shape ([resolver.go:333](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/attribute/resolver.go:333)). Given a future facade reading `tier=player` from a request field, every proposed constructor/provider test passes while the attacker receives player clearance.

- **MEDIUM — the provider constructor signature is internally inconsistent.** Task 2 explicitly specifies `NewViewerTierProvider()` and an empty struct ([02-03:220](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:220)), then requires `WithViewerRoleLookup` as a functional option ([02-03:257](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-03-PLAN.md:257)); 02-13 later constructs the provider with that option. The existing player precedent uses a variadic option constructor ([player.go:59](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/attribute/player.go:59)). Following the literal 02-03 signature makes 02-13 fail to compile.

### Suggestions

- Move the provenance must-have to the Phase-4 facade that constructs `ViewerSubject`, or add that construction root and its tests to this plan. Test anonymous, guest, and authenticated-player session states against the real server-derived session object; do not accept a tier parameter from the request.
- Specify `ViewerTierProviderOption` and `NewViewerTierProvider(opts ...ViewerTierProviderOption)` explicitly.
- Remove the stale `role_store.go` ownership statement.

### Risk Assessment

**HIGH.** The attribute mechanics are good, but the plan still declares a security property it cannot implement or test.

---

##### Plan 02-13 — player roles, property identity peers, production wiring

### Summary

The targeted fix materially improved this plan. D-27 is now explicit, the scalar owner mapping is defined, and the concrete-store function seam matches the existing composition root. But two cycle-2 ownership defects remain, including the already-confirmed missed `NewPropertyProvider` call site. The production-stack proof is still an acceptance sentence with no test artifact, and the proposed provider introduces substantial per-field database amplification.

### Cycle-2 fix verification

| Finding | Verdict |
|---|---|
| **C2-13: missed `NewPropertyProvider` call site** | **Still open; settled finding.** The manifest still omits `test/integration/access/access_suite_test.go` ([02-13:7](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:7)), while the live two-argument call remains at [access_suite_test.go:182](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/test/integration/access/access_suite_test.go:182). |
| **C2-14: production-stack proof has no test artifact** | **Still open; cosmetic fix only.** The behavior now describes the fixture precisely ([02-13:417](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:417)), but Task 3 still owns only `setup.go` and `subsystem.go` ([02-13:401](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:401)). |
| **MEDIUM #25: scalar owner mapping** | **Genuinely fixed.** `owner_player_id` is now explicitly limited to a player whose complete character set is represented by the scalar owner field ([02-13:329](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:329)). |
| **MEDIUM #27: player-union decision was implicit** | **Genuinely fixed.** D-27 is recorded and mechanically separated with the one-fixture ALL/ANY test ([02-13:389](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:389)). |
| **MEDIUM #28: three queries per property** | **Still open.** The plan still says one resolver call “per field” ([02-13:323](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:323)), meaning up to three owner-scope queries per property. |
| **MEDIUM #12: vacuous interface grep** | **Still open but partially mitigated.** The impossible pipeline remains at [02-13:243](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:243); the sibling reflection/method-set test is the only meaningful guard. |
| **MEDIUM #19: stale citations** | **Still open.** The plan cites `seed.go:120,136,142` and `evaluator.go:216` ([02-13:64](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:64)); the actual policy comparisons are at [seed.go:119](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/seed.go:119), [seed.go:137](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/seed.go:137), and [seed.go:143](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/seed.go:143), while unresolved `in` operands fail at [evaluator.go:343](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/dsl/evaluator.go:343). |

### Concerns

- **HIGH — the production-wiring proof still cannot be delivered from the owned files.** Acceptance requires resolving `viewer.tier` and `property.owner_player_id` through the returned `BuildABACStack` ([02-13:461](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:461)). The existing setup integration test only checks registered namespace coverage ([buildabacstack_seed_coverage_integ_test.go:61](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/setup/buildabacstack_seed_coverage_integ_test.go:61)); it never resolves either attribute. A miswired lookup can therefore ship with every plan-owned test green.

- **MEDIUM — the design produces roughly ten database operations per property visibility decision.** `PropertyProvider` already fetches the property at [property.go:70](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/attribute/property.go:70), and 02-13 adds up to three scope queries. The Phase-2 consumer performs two `Evaluate` calls per property ([02-08:132](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:132)); each evaluation eagerly resolves attributes ([engine.go:211](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/engine.go:211)) and creates a fresh per-resolution cache ([resolver.go:167](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/attribute/resolver.go:167)). Thus each field can execute two role lookups, two property reads, and six owner-scope queries. Twenty fields can approach 200 queries for one profile.

### Suggestions

- Add `test/integration/access/access_suite_test.go` to ownership and update its constructor call.
- Add a setup integration test file to 02-13 and drive `stack.Resolver.Resolve` through the real `BuildABACStack`.
- Resolve `owner`, `visible_to`, and `excluded_from` from one union of character IDs in one query, then apply ALL/ANY separately in memory.
- Add a query-count test around a multi-field profile and arrange for the two term evaluations to share pre-resolved property/viewer attributes.
- Delete the vacuous grep and rely on the exact method-set test.

### Risk Assessment

**HIGH.** The authorization model is substantially better, but the plan still breaks a live integration call site and does not own the test it claims proves production wiring.

---

##### Plan 02-07 — seed policies and shared ABAC test support

### Summary

The two-versus-three floor contradiction is genuinely repaired, the distinct action token correctly prevents disjunctive permit collapse, and the player-keyed twin mappings now match the real character-keyed policies. However, the test-support rewrite introduces a Go import cycle, the producer still does not implement the behavior required by its downstream consumer, and the seed family omits any mechanism for the column-backed `name` and `description` tier floors.

### Cycle-2 fix verification

| Finding | Verdict |
|---|---|
| **C2-10: two versus three floor policies** | **Genuinely fixed.** Task 1 now consistently specifies exactly the anonymous and guest policies ([02-07:158](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:158)), with corrected zero-count syntax at [02-07:245](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:245). |
| **C2-12: `abactest.PropertyProvider` producer/consumer disagreement** | **Still open.** 02-07 promises only schema-shaped doubles ([02-07:462](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:462)); 02-08 requires the double to derive peers from character-keyed fixtures through a character→player map ([02-08:291](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:291)). |
| **MEDIUM #5: missing `abactest_test.go`** | **Still open.** Task 4 owns only `abactest.go` ([02-07:420](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:420)), but acceptance requires a discoverable package test ([02-07:508](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:508)). |
| **MEDIUM #15: stale ownership attributions** | **Still open.** The key-links still attribute provider registration and player roles to 02-03 ([02-07:43](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:43)); those are 02-13 tasks. |
| **Viewer no-WARN relocation** | **Genuinely fixed.** It now executes after viewer-referencing seeds exist ([02-07:490](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:490)). |

### Concerns

- **HIGH — the shared test helper creates an import cycle.** The current smoke test is an internal `package policy` test ([seed_smoke_test.go:4](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/seed_smoke_test.go:4)). The plan makes it import `abactest` by delegating `createSeedEngine` ([02-07:475](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:475)), while `abactest` must import `policy` for `*policy.Engine` and `policy.SeedPolicies()` ([02-07:457](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:457)). The resulting test graph is `policy → abactest → policy`; `task test -- ./internal/access/policy/` cannot compile.

- **HIGH — the downstream privacy tests would still test a fake identity model.** Schema parity proves only that a key is declared, not how its value was derived. 02-08 explicitly forbids fixtures from writing `owner_player_id` or `visible_to_players` directly ([02-08:296](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:296)), but 02-07 defines no character→player fixture map or ALL/ANY derivation. An executor can make every policy test green by injecting already-derived peer values while the production derivation is broken.

- **HIGH — column-backed tier floors have no enforcement policy.** The plan limits `read_profile_attribute` to `resource is property` and `resource.property.name` ([02-07:163](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:163)), then explicitly excludes `characters.name` and `characters.description` from the floor lists ([02-07:194](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:194)). The normative spec says both columns still carry a one-term tier floor ([01-SPEC:1612](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/01-portal-spec/01-SPEC.md:1612)) and requires the decision to remain in ABAC rather than projection code ([01-SPEC:1310](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/01-portal-spec/01-SPEC.md:1310)). `CharacterProvider` already exposes both attributes ([character.go:123](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/attribute/character.go:123)), but no Phase-2 policy evaluates them for a viewer. Given an operator raises description to `guest`, an anonymous profile remains reachable and there is no ABAC decision capable of withholding the description—or Phase 4 must invent an unplanned authorization mechanism.

- **MEDIUM — the conditional player-floor guard has no independent antecedent.** The plan says the test turns red when §8.6 gains a player-floor row ([02-07:221](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:221)), but the posture table exists only as prose in the SPEC, and Task 1 owns only `seed.go`. A test hardcoding “no player members” remains green when the SPEC changes—the exact false-green it claims to prevent.

- **MEDIUM — the attribute-reference gate cites a nonexistent extractor.** The plan says `dsl/refs.go` extracts every reference ([02-07:411](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:411)); that file only returns a boolean indicating whether resource attributes are referenced ([refs.go:6](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/dsl/refs.go:6)). The real collector is unexported in `package policy` ([compiler.go:179](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/compiler.go:179)).

- **Known settled blocker:** the already-confirmed unconditional character-read permit remains in Task 3. I did not re-derive it.

### Suggestions

- Do not make the internal `package policy` smoke test import `abactest`. Keep its local builder, or convert it to external `package policy_test` and use only exported construction APIs.
- Add `internal/testsupport/abactest/abactest_test.go`.
- Define `PropertyProvider` test fixtures with a character→player ownership map and implement the exact D-27 ALL/ANY derivation in the double. Test the double against the real provider on identical fixtures.
- Seed separate viewer-facing column-floor policies over `character:` attributes, with actions that cannot collide with reachability or grid `read`. Test `name` and `description` independently at anonymous/guest/player floors.
- Make §8.6’s governed rows a machine-readable code artifact or generated fixture and assert set equality against the policies.
- Use `collectAttrRefs` from a same-package test or expose a supported compiler validation API; do not cite `refs.go` as an extractor.

### Risk Assessment

**HIGH.** The plan is not executable as written due to the import cycle, and even after that compile fix its test double can still produce convincing false-green privacy tests. The missing column-floor policy is an additional authorization gap.

---

#### Overall

##### Summary

**Not ready to execute.** The fix pass genuinely resolved the two/three-floor contradiction, anonymous subject shape, player-union decision, scalar owner mapping, function seam, and timing of the seed-coverage check. It did not close several mechanism-level blockers. Across this range I count five unresolved HIGH findings from this review, plus the two settled blockers the request instructed me not to re-derive.

##### Overall suggestions

Revision order:

1. Repair the `policy ↔ abactest` test import cycle.
2. Give `abactest.PropertyProvider` the exact production derivation contract consumed by 02-08.
3. Add ABAC policies and tests for the column-backed `name` and `description` floors.
4. Add the missing 02-13 call site and production-stack integration test artifacts.
5. Move viewer-tier provenance to an owned, tested facade construction root.
6. Only then address query amplification, stale citations, and vacuous grep criteria.

##### Overall Risk Assessment

**HIGH.** Execution currently yields at least one compile failure, leaves claimed production wiring unproved, and lacks a policy mechanism for two governed profile fields. No files were modified during this review.

---

### Codex — pass C (02-08, 02-09, 02-10)

#### Cycle 3 review — Pass C

Reviewed plans 02-08, 02-09, and 02-10 against repository commit `87b563913`.

Overall verdict: **NOT READY**. The targeted pass genuinely fixed several cycle-2 defects, especially the fourth-rung test, closed status handling, unreachable-database handling, and description-ledger identity. It still leaves **8 HIGH blockers**: one in 02-08, two in 02-09, and five in 02-10. The most serious new finding is that the exposure audit adjudicates only unenumerated properties even though the policy widens every public character property.

##### Plan 02-08 — Profile visibility conjunction

### Summary

The core design is sound: two evaluations are separated by action and ANDed in Go, which is necessary because the engine combines permits disjunctively ([02-08 plan:143](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:143), [engine.go:591](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/engine.go:591)). The fourth-rung discriminator now tests a policy that exists. Execution is still blocked by the test provider seam: 02-08 requires behavior that 02-07, which owns that provider, never specifies.

### Cycle-2 fix verification

- **C2-11 — genuinely fixed.** The suite now uses the `guest` floor and explicitly excludes the nonexistent player-floor policy ([02-08 plan:221](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:221)). This distinguishes byte-order comparison from set membership because the evaluator implements `>=` as raw Go string ordering, while unresolved list membership returns false ([evaluator.go:185](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/dsl/evaluator.go:185), [evaluator.go:317](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/dsl/evaluator.go:317)).

- **C2-12 — still open.** 02-08 requires `abactest.PropertyProvider` to derive peers from a character→player map ([02-08 plan:291](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:291)). Its producer still promises only provider constructors and schema parity ([02-07 plan:457](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:457)). The real production derivation is separately specified in 02-13 ([02-13 plan:267](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-13-PLAN.md:267)); nothing makes the test double behave likewise.

- **Cycle-2 note 17 — fixed.** The plan now says eleven new entries and two tier-floor policies ([02-08 plan:113](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:113)).

- **Cycle-2 note 21 — still open.** The implementation instruction still literally says `<the access engine interface>` ([02-08 plan:128](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:128)). The repository interface also requires `CanPerformAction`, not only `Evaluate` ([types.go:473](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/types/types.go:473)).

- **Cycle-2 note 22 — still open.** “Not-found-equivalent signal” remains undefined as a result type, sentinel, or error ([02-08 plan:123](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:123)).

### Concerns

- **HIGH — the required property-provider double has no producing task.** Given character-keyed fixture fields, 02-08 expects derived player peers. The 02-07 provider may legally emit the correct schema but no values. The tests then either fail despite correct production code or get “repaired” by writing player ids directly, producing the exact false-green 02-08 warns about. Production currently emits only character-keyed fields ([property.go:104](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/attribute/property.go:104)); the derived implementation lands in 02-13, not in the test-support producer.

- **MEDIUM — the evaluator interface remains unexecutable pseudocode.** If the executor substitutes `types.AccessPolicyEngine`, every recording double must also implement `CanPerformAction`; if they implement only the method the plan describes, compilation fails. If they invent another interface ad hoc, the intended API is no longer plan-controlled.

- **MEDIUM — unreachable-profile behavior has no contract.** Given reachability DENY, callers cannot know whether `VisibleAttributes` returns `nil, nil`, an empty set, a boolean, or an error. Phase 4 therefore has no stable mechanism to map this to the not-found-equivalent response.

- **MEDIUM — the `excluded_from` test can remain green after deleting the forbid.** The acceptance fixture names one character in `excluded_from` and “another” in `visible_to` ([02-08 plan:352](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-08-PLAN.md:352)). Under D-27’s ALL rule, one-of-two `visible_to` does not produce a permit. Removing the forbid still yields engine default-deny ([engine.go:608](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/engine.go:608)), so the test passes for the wrong reason. The normal repository also rejects overlap between the two lists ([property_repo.go:227](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/postgres/property_repo.go:227)).

### Suggestions

- Move the character-keyed derivation contract into 02-07’s `abactest.PropertyProvider` task, including behavior tests for ALL permit peers and ANY forbid peers.
- Declare a local minimal interface containing only `Evaluate(context.Context, types.AccessRequest)`.
- Define a concrete result, such as `(visible []Property, reachable bool, err error)`, or a named `ErrProfileUnreachable`.
- Test the forbid against a fixture where the permit also matches, then assert the matched deny policy. If using overlapping lists, make it an explicit raw-database/corrupt-row defense test because the Go repository rejects that state.

### Risk Assessment

**HIGH.** The conjunction itself is credible, but the primary real-engine tests depend on a missing producer contract and one forbid assertion does not prove its claimed mechanism.

---

##### Plan 02-09 — Admin section registry and gate

### Summary

Gate-before-lookup is the right structural fix for the enumeration oracle, and the status vocabulary now fails closed. Startup wiring is also explicitly owned. The descriptor fix is cosmetic: `Descriptor.Resource` is checked, but `Descriptor.Action` still changes no authorization decision.

### Cycle-2 fix verification

- **C2-15 — genuinely fixed.** `validateEntries` rejects zero and unknown statuses, and `sectionAvailability` is required to use an exhaustive switch with a denying default ([02-09 plan:172](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:172)). This matches the specification’s closed one-available/six-planned vocabulary ([01-SPEC.md:2107](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/01-portal-spec/01-SPEC.md:2107)).

- **C2-16 — still open/cosmetic.** `AssertSectionAccess` evaluates the caller-supplied `action` ([02-09 plan:260](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:260), [02-09 plan:265](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:265)). The post-lookup check compares only `Descriptor.Resource` ([02-09 plan:279](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:279)). Changing `Descriptor.Action` still has no effect.

- **Cycle-2 note 23 — still open.** The mismatch test requires a substitute entry ([02-09 plan:326](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:326)), but the helper hardcodes package `Lookup(sectionID)` ([02-09 plan:276](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:276)).

- **Cycle-2 note 24 — still open.** `All() []Section` is specified without a defensive-copy requirement ([02-09 plan:146](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:146)).

- **Earlier startup-wiring finding — genuinely fixed in the plan.** The production call is now assigned to `BootstrapSubsystem.Prepare` ([02-09 plan:358](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:358)).

### Concerns

- **HIGH — `Descriptor.Action` remains inert.** Given a `characters` descriptor changed from `read` to `write`, a caller passing `read` is still evaluated for `read` and permitted; no mismatch is detected. This violates the plan’s claim that the descriptor participates in authorization. It also leaves unresolved the specification’s two-action model: `read` reaches a section, while mutations require `write` ([01-SPEC.md:2190](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/01-portal-spec/01-SPEC.md:2190)).

- **HIGH — the descriptor-mismatch acceptance test cannot be written against the proposed API.** There is no registry injection seam, no `Registry` value, and no substitute lookup callback. A malformed entry passed to `validateEntries` tests boot validation, not the request-time mismatch branch. The executor must either alter the API outside the plan or omit the test.

- **MEDIUM — callers can mutate the registry after boot validation.** If `All()` returns the package slice directly, a caller can change a descriptor or status after `Validate()` succeeds. The request path catches only resource drift, not an empty action, and the boot guarantee no longer holds.

- **MEDIUM — empty player ids panic before the promised error handling.** The plan says `NewAccessRequest` handles empty subjects, but it first calls `access.PlayerSubject(playerID)` ([02-09 plan:265](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:265)). That helper panics on an empty id ([prefix.go:82](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/prefix.go:82)). Only `sectionID` is explicitly guarded.

- **MEDIUM — the bound invariant would be only partially proved.** The proposed invariant says there is no distinct code, message, or status ([02-09 plan:370](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:370)), while its binding compares message strings and defers `status.Code`/mapped message checks to Phase 4 ([02-09 plan:437](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:437), [02-09 plan:448](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:448)). The invariant rule explicitly warns that meta-tests cannot detect partial bindings ([invariants.md:81](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.claude/rules/invariants.md:81)).

- **MEDIUM — the claimed real-boot failure proof has no owned boot-path test.** Task 3 owns `section/boot_test.go`, not `internal/bootstrap/setup/subsystem_test.go` ([02-09 plan:340](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-09-PLAN.md:340)). The current subsystem suite does not call `Prepare` at all ([subsystem_test.go:18](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/bootstrap/setup/subsystem_test.go:18)). Merely running `task test:int -- ./internal/bootstrap/...` cannot prove a malformed registry aborts through the production call site.

### Suggestions

- Settle the action model explicitly. The cleanest shape is:
  - a section-reach helper that always evaluates and verifies the entry’s `read` descriptor;
  - mutation handlers perform a second `write` evaluation after the opaque reach gate.
  Alternatively, make the descriptor an allowed-action set and validate the requested action against it.
- Add an internal helper accepting `lookup func(string) (Section, bool)`; keep the exported wrapper wired to package `Lookup`.
- Make `All()` return `slices.Clone(all)`.
- Guard empty `playerID` before calling `PlayerSubject`.
- Leave `INV-PRIVACY-11` pending until its entire stated shape is asserted, or narrow its summary to precisely what the helper-level differential test proves.
- Add an injectable section-validator dependency to `BootstrapSubsystemConfig` and test that `Prepare` propagates its sentinel error.

### Risk Assessment

**HIGH.** The oracle ordering and status handling are improved, but the mandatory action descriptor is still decorative and one required request-time test is impossible through the specified API.

---

##### Plan 02-10 — Public-read exposure audit

### Summary

The redesign has good bones: database unavailability now blocks, values and descriptions are split into an operator-only report, and descriptions gain stable row identity. The audit still fails its central purpose. It only adjudicates unenumerated properties, while the policy widens every public character property. It also contains an acceptance criterion that cannot pass, leaks unconstrained property names into git, contradicts locked D-13, and never actually performs approved remediation.

### Cycle-2 fix verification

- **C2-9 — partially fixed, but the safety gate is still self-defeating.** The plan correctly switched its DML count to `rg -o | wc -l` ([02-10 plan:213](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:213)). A new criterion simultaneously requires two `md5(...)` calls and zero occurrences of `ep.value` or `c.description` ([02-10 plan:219](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:219)). Any required `md5(ep.value)` or `length(c.description)` necessarily violates the zero-occurrence check.

- **C2-17 — genuinely fixed.** The description ledger now emits one stable character id/hash/length row per non-empty description ([02-10 plan:178](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:178)).

- **C2-18 — partially fixed.** Property values and descriptions no longer need to enter git, but raw property names still do ([02-10 plan:171](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:171)). The aggregate outputs pasted into the result also include grouping names.

- **C2-19 — partially fixed/moved.** Remediation is now separated behind a checkpoint ([02-10 plan:353](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:353)), but no subsequent task owns or executes the approved writes.

- **Cycle-2 note 26 — partially fixed.** Stable ids, hashes, and lengths are specified. Nullable property values still yield NULL length and hash, despite the acceptance requiring both for every ledger row. `EntityProperty.Value` is explicitly nullable ([property.go:20](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/property.go:20)).

- **Earlier unavailable-database finding — genuinely fixed.** An unreachable target now blocks and cannot produce a discharging result ([02-10 plan:322](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:322)).

### Concerns

- **HIGH — the audit omits most of the population it is supposed to adjudicate.** The widening reaches **any** public character property ([02-07 plan:369](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-07-PLAN.md:369)), and the roadmap requires the audit to establish exactly which existing public rows it exposes ([ROADMAP.md:212](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/ROADMAP.md:212)). Plan 02-10 emits per-row detail and verdicts only for names outside §8.6 ([02-10 plan:171](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:171), [02-10 plan:293](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:293)). Given a sensitive public `profile.biography`, it appears only in aggregate counts and is never read or adjudicated before becoming off-location readable.

- **HIGH — the committed-ledger acceptance is impossible.** The query must contain exactly two `md5(` expressions but may contain zero references to the columns passed into those expressions. A correct executor cannot satisfy both criteria.

- **HIGH — raw property names can enter public git history despite the “sanitized” claim.** `entity_properties.name` is unconstrained `TEXT` in the schema ([000001_baseline.sql:354](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/migrations/000001_baseline.sql:354)); the repository inserts it without validation ([property_repo.go:60](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/postgres/property_repo.go:60)); even the code registry checks only non-emptiness ([registry.go:84](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/property/registry.go:84)). The plan nevertheless classifies the name as a safe “schema-level enumeration” and commits it ([02-10 plan:174](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:174)). A legacy or direct-SQL row named with identifying prose is copied into the audit artifact.

- **HIGH — one permitted description verdict contradicts locked D-13 and has no execution path.** The ledger may claim the description floor “was raised to `player`” ([02-10 plan:300](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:300)), but D-13 requires it to remain anonymous ([02-CONTEXT.md:172](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md:172)). The SPEC says changing the floor requires changing the seeded policy set ([01-SPEC.md:1697](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/01-portal-spec/01-SPEC.md:1697)), while 02-10 owns no policy file and 02-11’s §8.6 amendment covers only policy count ([02-11 plan:201](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:201)).

- **HIGH — approved remediation is never executed.** Task 4 is solely a `checkpoint:decision`; it has no `<action>`, files, command, or verification that writes or restores the enumerated rows ([02-10 plan:353](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:353)). Task 5 immediately proceeds to integration tests ([02-10 plan:398](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:398)). Given a `remediate` verdict, the plan can complete while the sensitive row remains public.

- **MEDIUM — flag-style properties do not satisfy the ledger contract.** The database permits `value NULL` ([000001_baseline.sql:358](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/migrations/000001_baseline.sql:358)). `length(NULL)` and `md5(NULL)` yield NULL, but every ledger row is required to carry a length and hash ([02-10 plan:343](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:343)).

- **MEDIUM — read-only safety is lint, not enforcement.** The scan is case-sensitive and checks only uppercase tokens ([02-10 plan:213](/Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-10-PLAN.md:213)). A lowercase `update`, or a later manual command during the same session, still executes against the target database.

### Suggestions

- Make the property ledger cover **every** `parent_type='character' AND visibility='public'` row. Add `is_spec_enumerated` as metadata; do not use enumeration as the ledger filter.
- Keep raw unenumerated property names only in the operator-only report. Commit `name_length` plus a keyed digest or an audit-specific opaque identifier.
- Replace the impossible column-reference gate with a projection check that rejects bare selected columns but permits approved wrappers, or inspect the generated output columns directly.
- Remove “raise the description floor” from the verdict vocabulary unless the locked D-13 decision is formally changed and the seed-policy edit is assigned to an owning task.
- Follow the remediation checkpoint with an explicit execute task that:
  - accepts only the approved ids;
  - verifies captured prior values;
  - performs parameterized updates;
  - re-runs the ledger;
  - records rollback confirmation without committing text.
- Encode nullable values distinctly, for example a `value_is_null` flag plus a hash over a tagged representation.
- Run the audit inside a database-enforced read-only transaction or with a read-only database role.

### Risk Assessment

**HIGH.** This plan is the privacy gate for a one-way widening, yet it currently misses exposed rows, can leak unconstrained names, contains an impossible acceptance criterion, permits a locked-decision contradiction, and cannot perform the remediation it approves.

---

##### Overall assessment

### Summary

The targeted fix pass is not cosmetic everywhere: C2-11, C2-15, C2-17, the unreachable-database posture, and the basic sanitized-output split are real improvements. But the range remains unshippable. The profile tests depend on an undefined producer behavior, the admin descriptor still does not control action, and the audit does not inspect the complete widened population.

### Priority order

1. Repair 02-10’s audit population and remediation path before any widening can merge.
2. Resolve 02-09’s descriptor action model and add an injectable lookup seam.
3. Move 02-08’s test-provider derivation contract into the plan that owns `abactest`.
4. Then tighten the false-green tests and invariant binding.

### Overall Risk Assessment

**HIGH — NOT READY FOR EXECUTION.** Eight HIGH blockers remain in this three-plan range. No repository files were changed during this review.

---

### Codex — pass D (02-06, 02-12, 02-11)

#### Cycle 3 verdict

Commit `87b563913` is still **NOT READY** for execution. The targeted pass genuinely repaired the goose staging recipe, the two `02-11` automated zero-match gates, CLI root registration, and `02-CONTEXT.md` ownership. It did not close the admission interface blast radius, the executable CLI-test seam, or multi-replica migration safety. I found additional ship-breaking failures: the resolution CLI bypasses the transactional outbox, the migration chain contradicts the existing full-cycle test, legacy NULL identity rows remain vulnerable during wave 4, and the closeout plan can “verify” a locked policy decision by rewriting it to match faulty code.

I did not re-derive the six settled cycle-3 findings. They remain additional blockers wherever they intersect these plans.

##### Plan 02-06 — Admission path

### Summary

The opaque `Admitted` token and the removal of name writes from generic `Update` are good structural ideas. The plan is nevertheless unexecutable and temporarily insecure: its signature census missed real implementers, its integration test does not construct either production composition root, and its transitional query still misses precisely the NFKC-only legacy collision IDENT-09 is intended to stop.

### Cycle-2 fix verification

| Cycle-2 finding | Verdict | Evidence |
|---|---|---|
| C2-1: token does not attest syntax | **Cosmetically fixed / moved** | The plan now delegates syntax to `Gate.Check` and removes the caller’s validator ([02-06:448](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:448>)). That would complete the token contract, but the already-confirmed `world → charname → world` cycle makes the chosen implementation uncompilable. |
| C2-2: block-list cache has no transport | **Mechanism specified** | `02-05` now specifies `Subsystem.Matcher()`, fields on both configs, and one shared subsystem ([02-05:401](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-05-PLAN.md:401>)); `02-06` consumes `BlockList.Matcher()` ([02-06:518](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:518>)). End-to-end execution remains blocked by the settled fixed-array compile finding. |
| C2-3: bootstrap may precede block-list preparation | **Mechanism specified** | `02-05` now orders a `Bootstrap → BlockList` edge ([02-05:381](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-05-PLAN.md:381>)). Same compile blocker applies. |
| C2-5: incomplete interface blast radius | **Still open** | The current manifest still omits the manual writer at [character_genesis_test.go:30](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_genesis_test.go:30>), its caller at [character_genesis_test.go:166](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_genesis_test.go:166>), integration callers such as [character_genesis_integration_test.go:103](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_genesis_integration_test.go:103>), and the full repository mock at [character_test.go:45](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/access/policy/attribute/character_test.go:45>). None appears in [02-06’s manifest](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:7>). |
| Cycle-2 self-referential build grep | **Fixed** | The impossible grep was removed and replaced with a SUMMARY obligation ([02-06:404](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:404>)). |
| “Five identical SQL bodies” | **Still open** | The plan still says all five carry the same query body ([02-06:474](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:474>)). Two are interfaces with no SQL: [character_service.go:22](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_service.go:22>) and [guest_service.go:38](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/guest_service.go:38>). There are three concrete queries, not five. |
| `task build` instead of whole-tree compile | **Still open** | The plan invokes `task build` ([02-06:568](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:568>)); the Taskfile explicitly says this builds only the main binary and that `build:all` is the interface-blast-radius gate ([Taskfile.yaml:476](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/Taskfile.yaml:476>)). |

### Concerns

- **HIGH — the transitional predicate misses NFKC-only legacy collisions.** The proposed fallback is `LOWER(name) = LOWER($1)` when `normalized_name` is NULL ([02-06:478](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:478>)). Migration 000054 deliberately leaves both normalized name and skeleton nullable ([02-01:285](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:285>)), while the required pipeline applies NFKC before case folding ([01-SPEC:854](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/01-portal-spec/01-SPEC.md:854>)). Given a legacy `Ａlice` row with NULL identity columns, an ASCII `Alice` candidate matches neither the normalized branch nor `LOWER(name)`. The skeleton lookup also sees NULL because it queries only `name_skeleton` ([02-01:299](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:299>)). Wave 4 can therefore admit and store the impersonating name despite claiming every intermediate commit is deployable.

- **HIGH — the “production-root” test can pass without exercising a production root.** The plan asks an integration test to build `CharacterService` “the way the harness builds it” ([02-06:544](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:544>)). The harness manually constructs its repositories and services ([harness.go:418](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/testsupport/integrationtest/harness.go:418>), [harness.go:440](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/testsupport/integrationtest/harness.go:440>)); it does not invoke either `internal/bootstrap/setup` composition or `grpcSubsystemConfig`. Thus both production roots may omit the matcher while the integration test remains green. The acceptance requiring nil/populated tests for “each root” ([02-06:582](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-06-PLAN.md:582>)) owns no package-local root test files and cannot access the unexported `package main` config externally.

- **HIGH — the shared interface change still cannot compile across the tree.** `CharacterWriter.Create` currently has the old signature ([character_genesis.go:43](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_genesis.go:43>)); `world.CharacterRepository` also gains `Rename`. The omitted mocks and callers above will fail compilation, while the plan’s main-binary build will not detect the sibling-package break.

- **MEDIUM — genesis can persist one name and emit another.** Genesis builds its outbox intent before calling the writer ([character_genesis.go:166](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_genesis.go:166>), [character_genesis.go:178](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_genesis.go:178>)); the payload takes `char.Name` ([character_genesis.go:203](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/auth/character_genesis.go:203>)). The planned repository instead inserts `Admitted.Display()` and only then refreshes `char.Name`. Given a mismatched character and token, the database stores A while the immutable genesis envelope records B.

### Suggestions

- Move the syntactic validator into `internal/charname` or a dependency-free leaf and keep `world.ValidateCharacterName` as a wrapper, eliminating the settled import cycle.
- Backfill identity before enabling the new admission path, or make both the skeleton and existence lookups normalize legacy raw names while identity columns are NULL.
- Add the omitted test files and run `task build:all`, full `task test`, and full `task test:int`.
- Test bootstrap and gRPC composition in their own packages; keep the integration test for behavior, not as proof of production wiring.
- Set `char.Name = admitted.Display()` before `buildIntent`, or have the intent builder take `Admitted`.

### Risk assessment

**HIGH.** It does not compile as scoped, its root-wiring proof is false-green, and its supposedly safe transitional state admits NFKC-only legacy collisions.

##### Plan 02-12 — Backfill, unique index, and resolution CLI

### Summary

The migration ordering and revised goose staging are substantially better. The operational path is still unsafe: the CLI becomes a third out-of-world writer with no outbox, it cannot guarantee a valid replacement while the backfill transaction is rolled back, the duplicate test cannot invoke it, and the intentionally irreversible Go migration breaks an existing full-cycle integration test that no plan owns.

### Cycle-2 fix verification

| Cycle-2 finding | Verdict | Evidence |
|---|---|---|
| C2-6: staging cannot produce 000055 | **Genuinely fixed** | The plan now leaves the global registry enabled and withholds only SQL migration 56 ([02-12:445](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:445>)). This matches the repository’s registered-migration import ([migrations_register.go:7](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/migrations_register.go:7>)) and correctly distinguishes the synthetic fixture’s paired `WithGoMigrations`/`WithDisableGlobalRegistry` setup ([migrate_gointerleave_integration_test.go:121](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/migrate_gointerleave_integration_test.go:121>), [migrate_gointerleave_integration_test.go:206](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/migrate_gointerleave_integration_test.go:206>)). |
| C2-7: no executable CLI seam | **Still open** | The integration test still says to resolve through `holomush character name set` ([02-12:508](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:508>)). The command package is `package main` ([root.go:4](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/cmd/holomush/root.go:4>)), and `task test:int` only runs Go test packages; it does not build or install the CLI ([Taskfile.yaml:265](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/Taskfile.yaml:265>)). |
| C2-8: write exclusion documented, not enforced | **Still open / cosmetic** | The action says “state and enforce,” but the mechanism is a header comment and SUMMARY instruction ([02-12:243](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:243>), [02-12:260](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:260>)). Goose’s lock covers only migrators ([migrate.go:85](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/migrate.go:85>)); the supported cluster runs a second core against the same database ([compose.cluster.yaml:77](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/compose.cluster.yaml:77>)). |
| Missing Task-1 ownership for five existence edits | **Still open** | Task 1 orders those edits at [02-12:271](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:271>) but its `<files>` list omits them ([02-12:181](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:181>)). |
| Contradictory `migrations_register.go` ownership | **Still open** | It remains in Task 1’s `<files>` ([02-12:181](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:181>)) while the action requires it untouched ([02-12:237](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:237>)). |
| CLI root registration | **Genuinely fixed** | Task 2 now owns `root.go` and requires command-tree reachability ([02-12:350](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:350>)); this matches the actual `NewRootCmd` registration mechanism ([root.go:36](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/cmd/holomush/root.go:36>)). |

### Concerns

- **HIGH — `character name set` bypasses the atomic outbox boundary.** The plan opens a pool and calls `CharacterRepository.Rename` directly ([02-12:388](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:388>), [02-12:415](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:415>)). The repository returns a `MutationDelta`, but no planned application service consumes it or writes an envelope. Production mutations use `worldMutator.mutate`, which commits the state and outbox intent in one transaction ([mutator.go:180](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/mutator.go:180>), [mutator.go:203](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/world/mutator.go:203>)). INV-WORLD-1 requires exactly that atomicity ([invariants.yaml:5017](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/docs/architecture/invariants.yaml:5017>)), and INV-WORLD-4 says there are exactly two sanctioned out-of-world writers, both envelope-emitting ([invariants.yaml:5049](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/docs/architecture/invariants.yaml:5049>)). Given a CLI rename, the row and version change with no audit/outbox event.

- **HIGH — the CLI cannot guarantee D-18 after a failed backfill.** Migration 55 performs the backfill transactionally and rolls it back when collisions exist ([02-12:208](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:208>)). Consequently legacy rows still have NULL `name_skeleton` and `normalized_name`. Yet the CLI validates only through `Gate.Admit` and directly renames ([02-12:360](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:360>)); the gate’s database lookup sees only stored skeletons ([02-01:299](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-01-PLAN.md:299>)). Given another legacy character whose raw name collides with the replacement, the CLI can accept and write it before index 56 exists. That contradicts locked D-18’s guarantee that the replacement satisfies the impending constraint ([02-CONTEXT:213](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md:213>)).

- **HIGH — the existing migration full-cycle integration test is guaranteed to fail.** The test pins the latest version at 53 ([migrate_integration_test.go:27](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/migrate_integration_test.go:27>)) and requires `Down()` to succeed all the way to zero ([migrate_integration_test.go:188](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/internal/store/migrate_integration_test.go:188>)). This plan adds versions 54–56 and deliberately makes migration 55’s Down return `MIGRATION_IRREVERSIBLE` ([02-12:219](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:219>)). No plan owns `migrate_integration_test.go`; full `task test:int` cannot pass as claimed.

- **HIGH — the duplicate-resolution test still has no way to run the command.** A package under `test/integration/charname` cannot import `cmd/holomush`, and no subprocess build strategy is specified. The acceptance at [02-12:535](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-12-PLAN.md:535>) is therefore unexecutable as written.

- **HIGH — cluster deployment safety remains an operator hope.** Given `core2` is live while the primary migrates, it may insert NULL or duplicate identity data between 55 and 56. The proposed tests prove only that rollout then fails loudly; they do not prevent the live write. A repeatedly failing production rollout is still a broken shipped mechanism.

### Suggestions

- Implement a maintenance application service that validates against all legacy raw names, reads the current version, renames through a transaction, and writes the outbox envelope atomically. The CLI should call that service.
- Put command dependencies in an importable package, or build and invoke the real binary explicitly in the integration suite.
- Update `migrate_integration_test.go` in the same plan: pin version 56 and replace the all-the-way-down expectation with the intentional irreversible boundary.
- Enforce writer quiescence mechanically—e.g. a maintenance advisory lock also acquired by character writers—or use an explicitly accepted single maintenance transaction/rollout barrier.
- Correct Task 1’s `<files>` ownership.

### Risk assessment

**HIGH.** The planned operator repair path violates bound world invariants, does not reliably validate replacements, cannot be exercised by its own integration test, and makes the existing migration suite fail.

##### Plan 02-11 — Amendments and closeout

### Summary

The closeout pass improved its mechanics and now owns the artifact it promises to amend. However, it can silently ratify a policy-count implementation defect by rewriting a locked decision to match the code, and its validation census still omits an input it explicitly claims to consume. It is not a trustworthy phase-closing gate yet.

### Cycle-2 fix verification

| Cycle-2 finding | Verdict | Evidence |
|---|---|---|
| C2-9: automated zero-match gates fail on success | **Genuinely fixed for the two 02-11 automated gates** | Both now use `rg -o … | wc -l` ([02-11:241](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:241>), [02-11:352](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:352>)). The separately settled 33 prose criteria remain outside this fix. |
| C2-20: claims D-03 amendment without ownership | **Ownership genuinely fixed** | `02-CONTEXT.md` is now in `files_modified` ([02-11:7](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:7>)), and D-03 now records the two-policy deviation ([02-CONTEXT:53](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md:53>)). The new verification logic is unsafe, described below. |
| “Four versus seven amendments” | **Fixed** | The plan consistently specifies seven amendments and seven new §14 rows ([02-11:254](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:254>)). |
| Missing `02-13-SUMMARY.md` input | **Still open** | `read_first` still stops at plans 01–10 plus 12 ([02-11:265](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:265>)), while Task 1 and Task 2 depend on the option selected and evidence produced by 02-13. |
| Missing dependencies on 01, 03, 07 | **Still open** | `depends_on` still omits them ([02-11:6](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:6>)) even though Task 2 consumes their RED demonstrations. |
| RED-table evidence is executor discipline | **Still open** | The only automated Task-2 check proves the placeholder disappeared ([02-11:352](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:352>)); it does not derive the plan set, parse rule 4, or prove observed exits. |

### Concerns

- **HIGH — the D-03 “check” repairs documentation to match possibly wrong code.** The plan says to count seeded tier policies and, if the count differs, update D-03 ([02-11:221](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:221>)). Locked D-03 requires one policy for each rung having a seeded member and currently derives exactly two ([02-CONTEXT:53](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md:53>)). Given buggy code that emits an extra player policy or omits the guest policy, Task 1 rewrites the locked artifact until equality is green. That is a self-healing false-green, not verification; it can launder an authorization drift into the specification.

- **MEDIUM — Task 2 cannot truthfully reconcile 02-13 without reading its SUMMARY.** Amendment F explicitly says the shipped derivation direction comes from `02-13`’s checkpoint, yet the closeout source list omits the resulting summary. The executor must guess or inspect incidental code instead of using the declared decision record.

- **MEDIUM — the RED-first table is still hand-curated prose.** The plan says the set is derived from every plan’s rule 4 but provides only a placeholder-removal shell check. Given a missing demonstration row, the automated gate remains green.

- **MEDIUM — Task 3 declares “no files modified” and then orders fixes.** Its scope says no files change ([02-11:367](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:367>)), but the action says every reviewer finding must be fixed ([02-11:390](</Volumes/Code/github.com/holomush/.worktrees/v013-milestone/.planning/phases/02-abac-schema-vocabulary/02-11-PLAN.md:390>)). Given an ABAC reviewer code finding, the executor must either violate plan ownership or cannot complete the task.

### Suggestions

- Make a D-03 mismatch a hard failure. Amend the decision only after demonstrating that §8.6 gained or lost a seeded rung and passing an explicit decision checkpoint.
- Add `02-13-SUMMARY.md` to `read_first` and add dependencies on every consumed plan.
- Generate the validation table with a small parser/script and compare plan/task and rule-4 sets by exact equality.
- Make Task 3 a checkpoint: `READY` continues; `NOT READY` stops and creates a scoped fix plan with owned files.

### Risk assessment

**HIGH.** This is the phase’s truth-recording gate, yet one of its checks can normalize faulty authorization code into the locked specification and its evidence census can remain incomplete while green.

##### Overall assessment

**Risk: HIGH — NOT READY.**

Highest-priority corrections:

1. Replace the direct-repository resolution CLI with an outbox-emitting, version-guarded maintenance service.
2. Make legacy NULL identity rows safe before any gated writer is deployed.
3. Repair the complete interface blast radius and use `task build:all`.
4. Give the CLI integration test an executable seam and reconcile the irreversible migration with `migrate_integration_test.go`.
5. Enforce—not merely document—the 55→56 writer exclusion.
6. Convert `02-11`’s D-03 and RED-table checks into hard, derived gates.

No files were modified and no implementation tests were run; this was a read-only plan/source review.

---

## pi Review

### pi — pass A (02-01, 02-02, 02-04, 02-05)

I have everything I need. All plan claims verified against source. Here is my cycle-3 review for plans 02-01, 02-02, 02-04, 02-05.

---

#### Cycle 3 Review — Pass A (plans 02-01, 02-02, 02-04, 02-05)

**Tree:** `87b563913` on `fix/sandbox-restore-runbook-corrections`. Every citation below was opened and read.

##### Overall summary

The fix pass did real work in my range: the 02-05 transport seam (C2-2/C2-3) is a genuine, mechanically sound fix — I traced it through the orchestrator's two-sweep `Prepare`-then-`Activate` semantics (`internal/lifecycle/orchestrator.go:43-65`) and it delivers exactly the guarantee claimed. The 02-04 MEDIUM #8/#9 fixes are real and I verified the decided fixture shape is assemblable from a pool (`internal/testsupport/integrationtest/harness.go:438-448`). But the C2-1 fix in 02-01 is the classic "moved the problem": it resolved token completeness by adding `charname → world`, which is the edge that makes 02-06's writer boundary (`world.CharacterRepository.Create(ctx, char, name charname.Admitted)`, `02-06-PLAN.md:313,395` against `internal/world/repository.go:201`) a compile-fatal cycle — and 02-01 simultaneously installs an acceptance criterion whose stated purpose ("cannot close a cycle") is falsified by the phase's own wave-4 design. On top of that I found two new HIGHs neither prior cycle caught: the gate's lookups have no self-exclusion channel, making the SPEC-mandated case-variant rename (`01-SPEC.md:700-705`) unreachable; and INV-WORLD-5's binding test, as placed by the fix pass, cannot reach the selection path the invariant names. Plus a shared executability defect: **`task generate` does not exist**, and both 02-01 and 02-05 gate on it.

---

##### PLAN 02-01 — TRACER: confusable-name rejection

### Cycle-2 fix verification

**C2-1 (gate subsumes `world.ValidateCharacterName`): COSMETIC-TO-HARMFUL — it moved the problem and left a time bomb.** The subsume text itself is coherent (`02-01-PLAN.md:250-273`), and `internal/world/validation.go:59-104` does own the five syntactic rules as claimed. Three problems:

1. It creates the confirmed `world → charname → world` cycle (verified both edges: this plan's `charname → world` at `02-01-PLAN.md:255-257`; `02-06`'s `charname.Admitted` parameter on `world.CharacterRepository.Create`/`Rename` at `02-06-PLAN.md:11,313,395`). Settled finding — I note it only because of consequences 2 and 3, which are in this plan and were not called out.
2. **Consequence not yet flagged:** 02-01's own acceptance criterion (`02-01-PLAN.md:351`) — `[ "$(go list -deps ./internal/world | rg -o 'holomush/internal/charname$' | wc -l)" -eq 0 ]` — is justified in-text by "so the new `charname → world` edge cannot close a cycle." That is a permanent-looking guard that 02-06 *requires* violating in wave 4. It is green in wave 1 and must be deleted or fail in wave 4; a guard that is RED by design gets deleted, and deleting it removes the actual protection. The criterion's rationale is false about the phase it belongs to.
3. The `NormalizeCharacterName` execution-path trap (confirmed finding) is **untouched by the fix pass** — the contradictory instruction ("delete the body … keeping the symbol only if a compile error would otherwise result") stands verbatim. Verified the full mechanism: the sole production caller is `internal/auth/character_service.go:105` (02-01's own acceptance forbids editing it); the title-casing is pinned by `internal/world/character_test.go:539-586` (owned by 02-04, wave 2, which never mentions it) and by `internal/auth/character_service_test.go:96,133,138` (owned by 02-06, wave 4). And the related acceptance criterion `rg -n 'NormalizeCharacterName' --type go -g '!*_test.go'` "returns no production call site that still performs title-casing" is unachievable until wave 4 — I ran it: 3 hits today, including the `character_service.go:105` call site.

**Cycle-2 MEDIUM #1 (`version_test.go` missing from frontmatter): NOT FIXED.** Task 3's `<files>` names `internal/charname/version_test.go`; the frontmatter `files_modified` (`02-01-PLAN.md:7-23`) still omits it.

**`rg -c` broken-form criteria (confirmed bucket):** 3 instances here (`02-01-PLAN.md:338,340,342`). The corrected `-o | wc -l` form at `:351` is right.

### New concerns

- **HIGH — the gate's lookups have no self-exclusion channel, and the SPEC mandates a rename shape that requires one.** `01-SPEC.md:700-705` is a settled decision: "A request whose normalized uniqueness key matches the current one but whose display form differs — a case or spacing variant — is a **real rename**: it writes the new display form … It does not collide with itself." This plan defines `SkeletonLookup{ SkeletonExists(ctx, skeleton string) (bool, error) }` and `Gate.Check(ctx, submitted)` (`02-01-PLAN.md:241-243`), computing `Skeleton(n.Key)` — the case-folded key. Given character "Alaric" (stored skeleton of key `alaric`), renaming to "ALARIC" produces the identical skeleton, `SkeletonExists` matches the character's **own row**, and the gate returns `NAME_CONFUSABLE`. Phase 3's `RenameCharacter` can only reach the writer through `charname.Admitted`, whose only constructor is the gate (`02-06-PLAN.md:61,66`), so the SPEC-decided case-variant rename is structurally impossible. No plan in the set contains the word "exclude"/"own row" for this (grepped 02-06, 02-12). The census machinery in 02-06 (rule C pins the constructor set) makes retrofitting more expensive, not less. This is the precise "shape decision whose cost explodes after code exists" class. The normalized-name existence half of the same problem belongs to 02-06; the skeleton half is this plan's interface.
- **MEDIUM — `task generate` does not exist.** `Taskfile.yaml` defines `generate:schema`, `generate:luabridge`, `generate:ebnf`, `generate:ebnf:check`, `mocks:generate`, `web:generate`, `docs:gen-events` — no umbrella `generate` (verified by `rg -n '^  [a-z][a-z0-9:_-]*:$' Taskfile.yaml | rg -i gen` and by the absence of `go generate` outside three pipeline stanzas at `:599,610,626`). This plan's must-have truth, Task 3 action ("Wire it into the umbrella `generate` task"), `<automated>` verify, acceptance criterion, `<done>`, and T-02-04 all invoke `task generate`; go-task errors on unknown task names, so the acceptance command fails regardless of correctness.
- **MEDIUM — T-02-04's "CI's drift check catches a stale table" has no executable path.** With `sources:`/`generates:` declared, go-task fingerprints *sources* and skips the task when they're unchanged — a hand-edit to `confusables_table_gen.go` changes no source, so `task generate:confusables` reports up-to-date and regenerates nothing. Actual drift detection in this repo is per-pipeline generate-and-diff stanzas (`.github/workflows/ci.yaml:78-85`, `Taskfile.yaml:1100-1117`), and this plan neither owns `ci.yaml` nor adds a `pr-prep` stanza. A hand edit that changes table *entries* (not the version constant) also evades the `version_test.go` meta-test, which only pins constant↔version-string agreement.
- **LOW — "the repo's seven existing `//go:generate` pipelines"** (`02-01-PLAN.md:181`): there are six (dsl/ast.go, lifecycle/health.go, lifecycle/subsystem.go, dek/checkpoint_fsm.go, luabridge/doc.go, plugin/manifest.go).
- **LOW — Task 3's `sources:` says "`cmd/internal/gen-confusables/main.go` **and** the file holding the pinned Unicode version string"** — the pinned version lives in main.go's URL constant; the second source is the same file or nothing. Cosmetic.

### Suggestions

- Resolve the cycle by moving the syntactic rules *into* `internal/charname` (the validator's only other consumers are `world/character.go`'s `SetName`/`Validate`, which can import charname — the direction 02-06 needs) and inverting 02-01's guard to assert `charname` never imports `world`. As written the plan set has no consistent resolution.
- Add the exclusion channel now: `SkeletonExists(ctx, skeleton string, excludeID ulid.ULID)` (zero ULID = create) or `AdmitForRename(ctx, characterID, submitted)`. Phase 3 is the next phase.
- Task 3: create the umbrella task (02-01 owns `Taskfile.yaml`) including `go generate ./internal/lifecycle/` (which today has no task at all), and add a generate-and-diff stanza for the confusables table to `pr-prep:fast:run` beside the schema/luabridge stanzas.

---

##### PLAN 02-02 — Mixed-script + IDENT-08 pin

### Cycle-2 fix verification

**Cycle-2 MEDIUM #2 (`pipeline.go`/`pipeline_test.go` missing from frontmatter): NOT FIXED.** This plan was untouched by the fix pass (not in `git show 87b563913 --stat`). Task 2's `<files>` still names `internal/charname/pipeline.go, internal/charname/pipeline_test.go` (`02-02-PLAN.md:176`); the frontmatter (`:7-14`) still omits both. Same-wave safety is unaffected (02-01 owns them, wave 1 → wave 2 is sequential), but the manifest is wrong and the collision audit runs off manifests.

**Cycle-1 HIGH #2 (import guard): verified genuinely fixed earlier and still coherent.** The file-scoped guard with the synthetic-fixture control (`02-02-PLAN.md:248-277`) correctly threads the needle — I verified `internal/auth/player.go:24-25,31,160-170` (Min/Max=3/30, the regex, `ValidateUsername`) and that 02-06 does make `character_service.go`/`guest_service.go` call the gate, which the package-wide ban would have made RED. The Unicode-skew rationale checks out: `go.mod` pins `go 1.26.5` and I ran `unicode.Version` under it — **15.0.0**, as the plan claims.

### New concerns

- **MEDIUM — the closed-set logic widens the SPEC's permitted script sets, in the fail-open direction, with no test row that could catch it.** `01-SPEC.md:871-882` permits exactly: single script; "Latin + any of Han, Hiragana, Katakana"; "Latin + Han + Bopomofo"; "Latin + Han + Hangul"; and rejects "**any other combination** of two or more scripts". `02-02-PLAN.md:155-158` instead implements "`{Latin, Han, Bopomofo}` **and its subsets containing Bopomofo**, and `{Latin, Han, Hangul}` **and its subsets containing Hangul**" — permitting `{Latin, Bopomofo}`, `{Han, Bopomofo}`, `{Latin, Hangul}`, `{Han, Hangul}`, all of which the SPEC's closed table rejects. The plan's own read_first says "transcribe its rows, do not paraphrase them" (`:113`). None of the 14 behavior rows exercises a subset case, so the suite as specified cannot detect the divergence. For an anti-impersonation control, permitting combinations the SPEC rejected is the widening direction.
- **LOW — the blank-vs-invisible message split is under-specified about *where* it lives.** 02-01's behavior rows pin *both* a whitespace-only submission and a Cf-only submission to `NAME_EMPTY_NORMAL_FORM` from `charname.Normalize`. 02-02 Task 2 requires the two cases to produce different messages (`:196-204`, acceptance `:215`). If the executor implements the blank check inside `Normalize` (changing its error for `"   "`), 02-01's wave-1 pipeline test breaks in wave 2. The safe implementation is a gate-level pre-check on the raw submission; the plan says only "wire it through."
- **Observation (no action needed):** `NAME_UNASSIGNED_SCRIPT` is unreachable *through the gate* — any rune unknown to Unicode 15.0.0 is also absent from `\p{L}`, so `ValidateCharacterName` rejects it as `NAME_INVALID_SYNTAX` first. The branch is honest defense-in-depth for direct `MixedScript` callers, and the plan's doc-comment rationale acknowledges the regex overlap.

### Suggestions

- Implement the four permitted families as exact set membership (`{Latin} ∪ S`, `S ⊆ {Han,Hiragana,Katakana}`, `S ≠ ∅`; `{Latin,Han,Bopomofo}`; `{Latin,Han,Hangul}`) and add REJECTED behavior rows for `{Latin, Bopomofo}` and `{Han, Hangul}` so the boundary is pinned.
- Add `pipeline.go`/`pipeline_test.go` to frontmatter.
- State explicitly that the blank-submission check is a gate-level pre-check on the raw input, leaving `Normalize`'s wave-1 contract untouched.

---

##### PLAN 02-04 — Lifecycle column & invariant bindings

### Cycle-2 fix verification

**MEDIUM #8 (zero-`Status` assertion on `GetNamesByIDs` doesn't compile): GENUINELY FIXED.** The fix restricts the assertion to `ListAll` with the correct reasoning (`02-04-PLAN.md:129-130,191`). Verified: `GetNamesByIDs` returns `map[ulid.ULID]string` at `internal/world/postgres/character_repo.go:368`; `ListAll` returns `[]*world.Character` at `:403`. The three full-row SELECT lists are exactly at `:33,162,183` as the plan says.

**MEDIUM #9 (placement offer contradicts hard-coded `asserted_by`): GENUINELY FIXED, and the decided shape is feasible.** The (a)/(b) offer is gone; the plan now mandates extending `test/integration/world/world_suite_test.go` (`:242-258`). I verified every `CharacterReapingService` dependency is pool-constructible in that suite, mirroring `harness.go:438-448` (`worldpg.NewPropertyRepository`, `NewBindingRepository`, `NewTransactor`, `NewOutboxStore`, `authpg.NewPlayerRepository`), and `world.NewService` (`internal/world/service.go:116`) takes `ServiceConfig{Engine, repos, Transactor, OutboxWriter}`. The `policytest.AllowAllEngine` ban is enforceable — a real engine is constructible the way `test/integration/access/access_suite_test.go` does it (`policy.NewEngine(resolver, cache, …)`).

**Cycle-1 #4/#13 (INV-WORLD-6 amend-before-bind): verified intact and correct.** The "ONLY path" wording is at `docs/architecture/invariants.yaml:5074-5081` as cited; `CharacterReapingService` is documented as "the SECOND sanctioned out-of-world writer" at `internal/auth/character_reaping.go:90-97` and deletes via `s.deleter.Delete(txCtx, char.ID, char.Version)` at `:263`. The registry-side correction (Task 3) and the SPEC-side correction (02-11 Amendment E, `02-11-PLAN.md:153-157`) are both present.

**`rg -c` broken-form criteria (confirmed bucket):** 4 instances (`:186,278,283,344`).

### New concerns

- **HIGH — INV-WORLD-5's binding test, as placed by the fix pass, cannot reach the selection path the invariant names.** The invariant's claim is exclusion "from character **selection**" (`invariants.yaml:5065-5073`). The only selection path is `CoreServer.SelectCharacter` (`internal/grpc/auth_handlers.go:243`). The fix pass mandates the world suite and rules out the harness — but the world-suite extension builds `world.Service` + `CharacterReapingService`, **not** a `CoreServer` with its player-session plumbing (`resolvePlayerSession`, session stores). Nothing in Task 2's action or acceptance criteria (`:271-285`) names *how* exclusion is observed; the criterion pins only that the spec "contains a direct `INSERT INTO characters` carrying `'idle'`". The natural execution — scan the row, assert `world.Selectable(status)` is false — re-implements the predicate in the test and passes with the `SelectCharacter` wiring entirely absent; and its pre-Task-1 "RED" is then only a compile error (`Selectable` doesn't exist yet), the vacuous-RED shape the review instructions call out. Task 3 then flips the entry to `bound` on the strength of that spec. The cheap real pin exists and is unowned: `internal/grpc/auth_handlers_test.go:210` (`TestSelectCharacter` over `authmocks.NewMockCharacterRepository`, fed by `ListByPlayer` at `:225-237`) is **not** in `files_modified`, and no task requires an idle-row case there.
- **MEDIUM — "replacing any inline predicate" misdescribes `SelectCharacter`; the ownership check must be retained.** The only inline predicate today is **ownership** (`auth_handlers.go:266-279`: `ListByPlayer` + ID match). There is no selectability predicate to replace. An executor reading "route its eligibility decision through `world.Selectable`, replacing any inline predicate" (`:186-188`) could swap the ownership loop for a `Selectable` call — converting a missing-check task into an authorization regression where any player selects any active character. No behavior row pins that ownership is retained.
- **LOW — INV-WORLD-6 has no RED state; rule 4 should say so rather than imply one.** All three assertions (retire-then-reclaim-fails; delete-then-reclaim-succeeds ×2) pass against *today's* code: name reservation is the pre-existing row-existence behavior via the `LOWER(name)` check (`internal/bootstrap/setup/adapters.go:38-50`), and both delete paths exist. Rule 4's text ("MUST be observed failing … the delete-releases-name half must be observed against the current code") hedges without stating the truth. 02-02 Task 3 models the honest form ("cannot be shown RED; say so in the SUMMARY") — 02-04 should adopt it.
- **LOW — Task 3's `<automated>`** (`go run ./cmd/inv-render && git diff --exit-code -- docs/architecture/invariants.md`) fails on its first run by construction — the regeneration *is* the diff. It only passes as a post-commit drift check. Same shape as 02-01 Task 3; executor-discipline, but the gate as written is red at the moment the task's own work lands.

### Suggestions

- Add `internal/grpc/auth_handlers_test.go` to `files_modified` and require a `TestSelectCharacter` row: `ListByPlayer` returns an idle character owned by the player → selection refused; paired with an active control that succeeds. That is the assertion INV-WORLD-5 actually makes, at the tier where the wiring lives.
- Reword Task 1: "**add** a `world.Selectable(c.Status)` check **alongside** the existing ownership match; the ownership predicate MUST remain."

---

##### PLAN 02-05 — Block list

### Cycle-2 fix verification

**C2-2 (no transport for the cache) and C2-3 (Bootstrap can Prepare first): GENUINELY FIXED — the strongest fix in my range.** Verified the whole mechanism: `Matcher()` on the subsystem, `*blocklist.Subsystem` fields on both configs, single construction in `cmd/holomush/core.go` (population sites `core.go:441` and `:675` are both in the file the plan owns), and the `Bootstrap → BlockList` edge. The orchestrator runs *all* `Prepare`s in topological order before any `Activate` (`internal/lifecycle/orchestrator.go:43-65`, and the two-sweep rationale is documented at `internal/lifecycle/subsystem.go:46-53`), so the edge genuinely guarantees the list is compiled before bootstrap's `Prepare` admits the initial admin. `DependsOn()` today returns the six ids at `internal/bootstrap/setup/subsystem.go:104-112` as cited; `Activate`/`Stop` are the no-ops cited at `:238-243`. The `grpcSubsystemConfig` whole-subsystem-pointer claim verifies at `cmd/holomush/sub_grpc.go:67-74`.

**Cycle-1 #5/#5b (strict loader; no-op subsystem): still correctly incorporated.** The `StringSliceN` fail-open is exactly as claimed at `internal/settings/game.go:120-128` (`(nil,false)` for absent *and* JSON decode failure, logged at Debug); `updated_at` was converted to BIGINT epoch-ns by `000044_pregfo6_gap_timestamps_to_bigint.sql:79-85`, so the `(updated_at, md5(value))` indicator query is type-correct (`value` is TEXT — `md5(text)` works); `policy.NewPoller`'s nil-rejection and 10s default are at `internal/access/policy/poller.go:85-100`; the two-signal `VersionQuerier` (timestamp + count) is at `poller.go:23`; the `readBarrier` err-before-close shape is at `cache.go:30-34`; `RegisteredNamespaces` admits `core` at `internal/settings/namespaces.go:15-20`; `GetSystemInfo`/`SetSystemInfo` behave as cited at `internal/store/postgres.go:70-95`.

**C2-4 (`core_subsystems_test.go`/`core_topo_order_test.go` unowned): NOT FIXED — confirmed cycle-3 finding, mechanism fully verified.** The fix pass added the bootstrap/sub_grpc files to the manifest but not these. `[17]stubSubsystem` at `cmd/holomush/core_subsystems_test.go:55` is a fixed-size array — an 18th entry is a compile error; `setFromStubs` (`:79-96`) maps it positionally; `core_subsystems_test.go:117` asserts `len(subs) == 17`; `core_topo_order_test.go:160` asserts "exactly the 17 production subsystems … a subsystem was added or removed without updating this test's construction list" and pins the exact topo order. 02-05's own verify (`task test -- ./internal/charname/... ./internal/lifecycle/ ./cmd/holomush/`) cannot go green without editing both files, and no plan owns them. Additionally, the plan's read_first describes `productionSubsystems` as having a "named-parameter signature" — it is now the `productionSubsystemSet` struct (`core.go:1166-1184`), so the edit is a struct-field addition plus slice append, not a parameter append.

**`rg -c` broken-form criteria (confirmed bucket):** 2 instances (`:243,441`). The corrected `-o | wc -l` form at `:433` is right.

### New concerns

- **MEDIUM — `task generate` does not exist (shared with 02-01).** Task 2 orders "run `task generate` to regenerate `subsystemid_string.go`" and verification repeats it. No such task exists; today that file is regenerated by hand-running `go generate ./internal/lifecycle/` (there is no Taskfile stanza for the lifecycle stringers at all). If 02-1's Task 3 creates the umbrella task without including `./internal/lifecycle/`, this instruction stays broken — and 02-05 does not own `Taskfile.yaml`.
- **LOW — the "shipped shape" justification is half-wrong.** `BootstrapSubsystemConfig` (`internal/bootstrap/setup/subsystem.go:68-80`) carries lazy **provider interfaces** (`PoolProvider`, `PolicyStoreProvider`, …), not subsystem pointers — only `grpcSubsystemConfig` has the pointer shape the plan cites. A concrete `*blocklist.Subsystem` field works (core.go holds the pointer), but the rationale misdescribes the struct; a `BlockListMatcherProvider` interface would match the struct's actual convention.

### Suggestions

- Add `cmd/holomush/core_subsystems_test.go` and `cmd/holomush/core_topo_order_test.go` to `files_modified` (or transfer them to 02-06); the `[17]` array, `setFromStubs`, both count assertions, and the pinned topo order all change with this subsystem.
- Coordinate with 02-01's Task 3 so the umbrella `generate` task includes `go generate ./internal/lifecycle/`; alternatively change this plan's instruction to `go generate ./internal/lifecycle/` with an `# offload-exempt`-style rationale.

---

##### Range risk assessment: **HIGH**

Justification: two confirmed structural blockers intersect this range (the `world ↔ charname` cycle, with 02-01's anti-cycle acceptance criterion falsified by the phase's own wave-4 design; the `NormalizeCharacterName` execution trap, untouched by the fix pass and now verified end-to-end including its unachievable acceptance criterion); the `[17]stubSubsystem` ownership gap makes 02-05's own verification uncompilable; and I add two new HIGHs — the skeleton/existence lookups' missing self-exclusion channel (structurally blocks the SPEC-decided case-variant rename in the very next phase) and INV-WORLD-5's binding test being placed where it cannot reach the selection path (a fabricated-binding risk in the registry). The 02-02 script-set widening and the nonexistent `task generate` gate are MEDIUM but cheap to repair. The fix pass's 02-05 transport work and 02-04 fixture decisions are genuinely good and should be kept.

##### One note on a locked decision (as invited, once, briefly)

The confirmed `world ↔ charname` cycle exists because C2-1's fix chose "the gate subsumes `world.ValidateCharacterName`" while 02-06 puts `charname.Admitted` on `world.CharacterRepository`. Both choices are individually defensible; jointly they are impossible. The clean exit is not to revert either, but to relocate the syntactic rules from `internal/world/validation.go` into `internal/charname` (their only `world` consumers are `Character.SetName`/`Validate`, which can import charname in the direction 02-06 requires) — i.e., treat rule *ownership* as belonging to the name-policy package, not to `world`. That contradicts 02-01's "rule ownership stays with `internal/world`" text, which was written before the cycle was recognized; it does not contradict any locked `02-CONTEXT.md` decision.

---

### pi — pass B (02-03, 02-13, 02-07)

I have everything I need. Here is the cycle-3 review for plans 02-03, 02-13, 02-07.

---

#### Cycle 3 Review — Pass B: plans 02-03, 02-13, 02-07 (ABAC substrate)

**Tree verified at `87b563913`.** Every claim below was checked against source I actually read: `internal/access/prefix.go`, `internal/access/policy/seed.go`, `internal/access/policy/attribute/{player,property,resolver}.go`, `internal/access/policy/{compiler.go,dsl/evaluator.go,dsl/ast.go,dsl/refs.go,engine.go,seed_smoke_test.go,engine_test.go,seed_validation_test.go}`, `internal/access/setup/{setup.go,subsystem.go,seed_coverage.go,buildabacstack_seed_coverage_integ_test.go}`, `internal/store/role_store.go`, `internal/store/migrations/000001_baseline.sql`, `internal/world/{service.go,grpc_server.go}`, `internal/world/postgres/parent_location_resolver.go`, `internal/auth/guest_service.go`, `test/integration/access/access_suite_test.go`, `.golangci.yaml`, `01-SPEC.md` §7.4/§8.2.1/§8.4.1/§8.4.2/§8.5.1/§8.6/§10.4/§10.5, and `02-CONTEXT.md`.

---

##### PLAN 02-03 (prefixes + ViewerTierProvider)

### Summary

A tight, well-grounded vocabulary plan. The omit-don't-sentinel discipline is faithfully specified (key-presence assertions, `has_X` witnesses on every path), the `roles` addition genuinely closes the admin-twin expressibility hole, and the verification-integrity section honestly moves the vacuous seed-coverage RED to 02-07 with the right reason (`seed_coverage.go:28` scans `policy.SeedPolicies()`). The defects are residual stale prose from the split, one self-contradictory constructor signature, and a tier-provenance must-have that was reworded into a tautology rather than scoped.

### Cycle-2 fix verification

| Finding | Verdict | Evidence |
|---|---|---|
| C2-21 (tier provenance not implementable) | **Cosmetically fixed** | `02-03-PLAN.md` must-have now reads "no constructor in internal/access accepts a tier value sourced from a request header, query parameter, cookie or request field." That is true but tautological: `ViewerSubject(tier, playerID string)` takes strings; nothing in `internal/access` ever sees a request, so the property holds of *every* conceivable constructor and proves nothing. The real mitigation remains the doc comment (`:160-164`); T-02-11's disposition "mitigate / closed" is still overstated — the trust boundary lives at Phase 4's facade call sites, which no plan in this phase owns. |
| MEDIUM 16 (dependency note claims `role_store.go`) | **NOT fixed** | `02-03-PLAN.md:75` still says "it touches `internal/access/` and `internal/store/role_store.go`"; frontmatter (`:7-12`) and both task `<files>` lists do not include it. Frontmatter is right, prose is wrong. |
| Scope split to 02-13 (cycle-2's #3/#10/#11 incorporation) | **Fixed, verified** | `02-03` declares only the `PlayerRoleLookup` seam; the interface is untouched (`internal/store/role_store.go:15-23` still declares exactly four methods). |
| MEDIUM 20 (empty-identifier contract) | **Fixed** | Must-have and `<behavior>` now consistently carve out the anonymous rung, with the `assert.NotPanics` acceptance row pinning `ViewerSubject(ViewerTierAnonymous, "")`. |

### Concerns

- **LOW — Self-contradictory constructor spec.** The Task 2 action specifies `type ViewerTierProvider struct{}` (empty) and `func NewViewerTierProvider() *ViewerTierProvider` (zero args), then mandates `WithViewerRoleLookup(PlayerRoleLookup)` "as a functional option mirroring `WithPlayerKindLookup`'s shape exactly (`player.go:55-185`)". `WithPlayerKindLookup`'s shape requires an options field on the struct and a variadic `opts ...Option` parameter (`player.go:30-38,64-77`). An empty struct has nowhere for the option to write, and the zero-arg signature has nowhere to receive it; `02-13` Task 3 calls `NewViewerTierProvider(...)`. Executor-resolvable, but the literal signature is wrong.
- **LOW — Unspecified parse case.** `<behavior>` enumerates `viewer:spectator:<id>` → `INVALID_VIEWER_TIER` but never `viewer:anonymous:<non-empty-id>`. The constructor panics on that shape, yet hand-built subject strings reach `ResolveSubject` too; the action's "remainder of the form `<tier>:<ulid>` is the guest or player rung" leaves the anonymous-with-id remainder unassigned. One line ("an `anonymous` rung with a non-empty id half is `INVALID_VIEWER_TIER`") closes it.
- **LOW — Mis-cited evidence lines** (shared with 02-07/02-13, see MEDIUM 19 below): `02-03-PLAN.md:247,370` cite `dsl/evaluator.go:216` for "missing key is false"; `:216` is inside `evalHas`'s doc comment. The missing-attr-false sites are `resolveAttrRef` (`evaluator.go:401-411`), `evalInList` (`:317-336`), `evalInExpr` (`:339`); `evalHas` itself is `:217`. Also `02-03` key_links cites `seed.go:126` for the admin-read policy; the DSL line is `seed.go:125`.

### Suggestions

1. Fix the constructor text to `func NewViewerTierProvider(opts ...ViewerTierProviderOption) *ViewerTierProvider` with a `roleLookup PlayerRoleLookup` field, explicitly naming `player.go:30-38` as the shape.
2. Restate the tier-provenance must-have as a Phase-4 call-site obligation and downgrade T-02-11's disposition to "accepted here, mitigated at the facade" — the current wording claims a guarantee this plan cannot manufacture.

### Risk: LOW

---

##### PLAN 02-13 (roles seam, derived peers, provider registration)

### Summary

The design content is genuinely good: D-27's ALL/ANY derivation is now recorded, gated by a blocking checkpoint, and pinned by a single-fixture direction-pair assertion that correctly distinguishes the two rules; the func-field seam (not interface widening) matches the shipped `PlayerKindLookup` pattern; the NULL-`player_id` case is handled (`characters.player_id` is nullable, `000001_baseline.sql:74`). But cycle 2's two seam findings against this plan are both still open, and the registration task recreates — one level down — the exact silent fail-closed class the phase exists to kill.

### Cycle-2 fix verification

| Finding | Verdict | Evidence |
|---|---|---|
| C2-13 (`NewPropertyProvider` third call site unowned) | **Still open** (orchestrator-confirmed) | `test/integration/access/access_suite_test.go:182` still calls `attribute.NewPropertyProvider(propRepo, parentLocResolver)` two-arg; the file is in no plan's `files_modified`; Task 2's acceptance still demands `rg -n 'NewPropertyProvider' --type go` show every call site at three arguments. |
| C2-14 (BuildABACStack proof, no owned test artifact) | **Still open** | `02-13` `files_modified` (`:7-18`) contains no test file under `internal/access/setup/`. Task 3's acceptance still requires `task test:int -- ./internal/access/setup/` to run a spec resolving `principal.viewer.tier` and `resource.property.owner_player_id` "through the stack `BuildABACStack` actually returns." The only existing candidate, `buildabacstack_seed_coverage_integ_test.go`, is owned by `02-07` (wave 4 — a later wave). The executor has no file to write the proof in, and creating an unlisted one silently expands the manifest the collision audit runs on. |
| MEDIUM 25 (scalar `owner_player_id` mapping) | **Fixed** | `ResolveOwnerScopes` now returns `map[string][]string` keyed by player id, and the ALL-reduction for the single-element `owner` field is spelled out. |
| MEDIUM 27 (union was an undecided widening) | **Fixed** | D-27 is in `02-CONTEXT.md` (verified on disk), Task 0 is a blocking checkpoint presenting both directions with the widening spelled out. |
| MEDIUM 12 (vacuous grep) | **Still open** (low) | Task 1 acceptance still carries `rg -n 'PlayerRoles' … | rg -c 'RoleStore interface'` returns 0` — a line matching `PlayerRoles` can never also contain `RoleStore interface`, and the `rg -c … returns 0` form is the settled broken family. The sibling method-set test is the real guard; the grep should be deleted. |
| MEDIUM 28 (per-row query cost) | **Partial** | Batched to one `ResolveOwnerScopes` call per field, but still three queries per property-row resolution and no budget. See the perf note below. |

### Concerns

- **HIGH — C2-14 (above).** The Task 3 acceptance criteria are unexecutable as written; every other task in this plan names the file its assertions live in.
- **MEDIUM — Nil `CharacterOwnerResolver` fails silently at a grain the sweep cannot see.** Task 3's mitigation for an unpopulated seam is a doc comment on the `ABACConfig` field. But `warnOnMissingSeedCoverage` (`internal/access/setup/seed_coverage.go:36-60`) validates **namespace** coverage only: `PropertyProvider` is registered, so the `property` namespace is covered, and the viewer twins' references to `resource.property.owner_player_id` produce no WARN — they just evaluate false forever (`resolveAttrRef`, `evaluator.go:401-411`). This is the RESEARCH P-7 / g776 fingerprint one level down (key-level, not namespace-level), in the same file whose house pattern for exactly this class is a loud construction-time WARN naming the missing seam and the affected seeds (`setup.go:191-228`, per holomush-g776/k3ud/72ou). The plan should specify an equivalent WARN (or refuse) when `PropertyRepo != nil && CharacterOwnerResolver == nil`, and a parallel one for `PlayerRoleLookup` — the current text documents the hole instead of sounding it.
- **MEDIUM — The derived-peer resolution runs on every property evaluation, including the grid hot path.** `PropertyProvider.ResolveResource` (`property.go:57`) does not know the principal. After this plan, every `seed:property-*` evaluation on the game grid — colocation-gated reads that fire constantly in-game — pays up to three extra DB round-trips (owner, `visible_to`, `excluded_from` scopes) for attributes only the viewer-flavored policies consume. The per-context LRU (`attribute/cache.go`) dedupes within one `Resolve`, not across evaluations. No plan text budgets or lazy-gates this. At minimum the derivation should be skippable when no viewer twin is in the candidate policy set, or the cost should be recorded as accepted with a number.
- **LOW — Task 0's resume-signal orders edits to unowned files.** "the executor MUST amend `02-CONTEXT.md` D-27 and plan `02-11`'s Amendment F" if union is selected — neither file is in `02-13`'s `files_modified`. Same ownership-gap shape as cycle-2's C2-20, conditional branch only.
- **LOW — Mis-cited lines** (`02-13-PLAN.md:64-67`): `seed.go:120,136,142` → actual `:119,:137,:143` (verified by grep); `evaluator.go:216` → see 02-03 note above.

### Suggestions

1. Add an `internal/access/setup/*_integ_test.go` file to `files_modified` (new file, wave-3-safe name) and point Task 3's proof at it explicitly.
2. Specify the missing-seam WARN for `CharacterOwnerResolver`/`PlayerRoleLookup` in `BuildABACStack`, mirroring the `LocationRepo` branch's text shape at `setup.go:191-199`.
3. Record the grid-path query cost as an accepted trade-off with a per-evaluation bound, or make the peers' resolution conditional.

### Risk: MEDIUM (HIGH if C2-14's proof is treated as load-bearing for 02-07's wave)

---

##### PLAN 02-07 (seed policy family)

### Summary

The policy texts themselves are correct: I verified the transcriptions against SPEC §8.2.1/§8.4.2/§10.4 verbatim, the §8.6 anonymous/guest split of the seeded-default column is exactly right (anonymous among property rows = `profile.pronouns` only; 22 names at guest including 11 media), the action-token separation (`read_profile_attribute` vs `read`) genuinely implements §8.5.1's conjunction, and the DSL shapes parse under the shipped grammar. C2-10's self-contradiction is fixed. But this plan carries the range's worst mechanics: an import-cycle-blocking test-builder instruction, a producer/consumer contract mismatch with 02-08 that cycle 2 already named, a fabricated extractor citation, and a false depguard claim. It also authors the confirmed unconditional character-read permit — whose actual blast radius (alt-linking + location tracking, guests included) is larger than anything the plan or the 02-10 audit scopes, and whose mandated remedy does not exist for character rows.

### Cycle-2 fix verification

| Finding | Verdict | Evidence |
|---|---|---|
| C2-10 (two-vs-three tier floors) | **Fixed** | `02-07-PLAN.md:155` — "add **three** entries: two tier floors and profile reachability"; the action explains the grammar constraint (`dsl/ast.go:232-236` requires ≥1 literal — verified); acceptance uses the corrected `[ "$(rg -o … | wc -l)" -eq 2 ]` form. The D-03 amendment is genuinely on disk (`02-CONTEXT.md:53-59`, "AMENDED 2026-08-04"), so the plan's cross-reference is true. |
| C2-12 (abactest double contract vs 02-08) | **NOT fixed** | `02-08-PLAN.md:~298` still requires: "the `abactest.PropertyProvider` double MUST perform the same derivation against a fixture character→player map rather than having the test write the peers directly." `02-07` Task 4 (`:433-448`, `:507`) still promises only "doubles whose `Schema()` declares exactly the keys the real providers declare" — schema parity, no derivation behavior. The consumer's contract does not exist in the producer, and 02-08's fixtures are forbidden from supplying the peers themselves. D-04's additive-permit regression — the phase's mechanical guard on §8.5.1.1 — runs on this double. |
| MEDIUM 5 (no `abactest_test.go`) | **NOT fixed** | `files_modified` (`:10`) and Task 4 `<files>` (`:420`) list only `abactest.go`; the acceptance demands "a test in `abactest` asserts each double's schema key set equals its real provider counterpart's." A Go test cannot live in a non-`_test.go` file. |
| MEDIUM 15 (stale 02-03 attributions) | **Partially fixed** | Must-haves correctly say 02-13 (`:30`), but key_links `:43` still reads "ViewerTierProvider registered in plan 02-03 … role lookup added in the same plan"; Task 4 read_first `:423` says "the `roles` / `has_roles` attributes added in plan 02-03" (02-13 Task 1's); Task 3 read_first attributes the property-`name` assertion to 02-03 (02-13 Task 2's); threat T-02-43 `:582` repeats it. `/gsd-execute-phase` reads task bodies, and 02-07's `@context` includes `02-03-SUMMARY.md` but **not** `02-13-SUMMARY.md` — the executor is pointed at the wrong summary for the mappings it must transcribe. |
| Cycle-1 #10/#11 incorporation (derived peers, viewer.roles) | **Fixed, verified** | The mapping table (Task 3) correctly re-expresses all four identity terms against the 02-13 vocabulary; I verified each original at `seed.go:119,125,137,143`. |

### Concerns

- **HIGH — `createSeedEngine` cannot delegate to `abactest.NewSeedEngine`; the instruction is an import cycle.** `seed_smoke_test.go:4` is `package policy` (internal test file). `abactest.NewSeedEngine` must import `github.com/holomush/holomush/internal/access/policy` for `Engine`, `SeedPolicies()`, `Compiler`, `Cache`. An internal test file importing a package that imports the package under test is a hard compile error: *"import cycle not allowed in test."* The escape hatch — converting `seed_smoke_test.go` to `package policy_test` — is never mentioned and cascades: the file's five mock-provider constructors return `*mockAttributeProvider`, defined in `engine_test.go:852` (internal), and `createSeedEngine` calls `createTestEngineWithPolicies` (`engine_test.go:888`, internal); both vanish from an external test package, and `seed_validation_test.go:22` (internal, `package policy`) calls `createSeedEngine` too. The acceptance criterion "`createSeedEngine` … delegating to `abactest.NewSeedEngine` rather than carrying a second construction" is unexecutable without an unstated multi-file restructure. (Additionally, `createTestEngineWithPolicies` injects the cache snapshot through package-private fields — `cache.mu.Lock(); cache.snapshot = …` — which no external package can mirror; `abactest` must build through a fake `policystore.PolicyStore` + `Cache.Reload`, a *different* construction path than the one it claims to mirror.)
- **HIGH — The confirmed unconditional permit's real blast radius is untraced, and D-11's remedy does not exist for it.** The settled finding flags `permit(principal is character, action in ["read"], resource is character);` (`02-07-PLAN.md:376`). What nobody has traced: the read path this unlocks projects `PlayerId`, `Name`, `Description`, and `LocationId` (`internal/world/grpc_server.go:127-137`, `characterToProto`; gate at `internal/world/service.go:779-786`). Today, character→player linkage and location are visible only for co-located characters (`seed:player-character-colocation`, `seed.go:51`); after this permit, **any character principal — including every ephemeral guest — can enumerate alt-to-player ownership and live locations for the entire roster**. The plan justifies the permit as carrying `characters.description` (PROFILE-10a); it in fact carries the whole `CharacterInfo`. Two compounding gaps: (a) the 02-10 audit's scope is descriptions + public property rows — not the `PlayerId`/`LocationId` projection, so the phase's own exposure gate never adjudicates the widest thing the permit exposes; (b) the plan's own block comment repeats D-11's remedy — "the fix for any such row is to change the row's `visibility`" — but `characters` has no visibility column (`000001_baseline.sql:72-79`), so for the character half of the widening the mandated remedy is *structurally unavailable*. If PlayerId/LocationId exposure is intended, say so and audit it; if not, the permit needs a `when` clause or the projection needs field-level filtering (Phase 4), and the audit scope needs widening.
- **HIGH — C2-12 (above).** The producer/consumer contract mismatch makes 02-08's D-04 gate unwritable as specified; the fix belongs in 02-07's Task 4 (a deriving double with a fixture char→player map) since 02-07 owns the file.
- **MEDIUM — Fabricated extractor citation.** Task 3's attribute-coverage acceptance says "the DSL's own `refs.go` extractor does this." `internal/access/policy/dsl/refs.go` exports exactly one function, `ReferencesResourceAttrs(block *ConditionBlock) bool` — a boolean preflight helper; it cannot enumerate references. The real extractor is `collectAttrRefs` (`internal/access/policy/compiler.go:170-236`), which is unexported but reachable from `seed_smoke_test.go` (internal test) — note it normalizes to `(root, full.dotted.path)`, so the test must split the first path segment as namespace. Name the right symbol.
- **MEDIUM — The depguard claim is false.** Task 4: "`internal/testsupport/*` is depguard-forbidden in production code, so this is test-only by construction." `.golangci.yaml:135-160`: the rule's `files` **excludes** `**/internal/testsupport/**` from the rule's scope, and the `deny` list names only `eventbustest`, `coretest`, `quarantinetest`, `natstest`, `integrationtest`. `abactest` would not be denied; production code could import it cleanly. Add it to the deny list (one YAML stanza) or drop the claim.
- **MEDIUM — The D-03 re-entry guard test has no home and no data source.** Task 1 owns only `seed.go`; the conditional guard ("green while no §8.6 row is seeded at `player`, RED the moment one is") is an acceptance criterion, but no `_test.go` file is in Task 1's `<files>`, and the antecedent's source of truth is unspecified — parsing `01-SPEC.md`'s §8.6 table from a Go test is the only reading that "turns RED at exactly that moment," and nothing says so. Under-specified twice over.
- **MEDIUM — The tier-floor family cannot express a floor for the two §8.6 column attributes.** The policies match `resource is property` keyed on `resource.property.name`; `name` and `characters.description` are columns with no property row, and `PropertyProvider.ResolveResource` fetches by ULID and declines anything else (`property.go:57-63`), so no evaluation resource exists for a column floor check. Excluding the columns from the name lists is correct for the v0.13 seed (both at `anonymous`; §8.8 pins name/pronouns) — but §7.4 promises "the description's floor is configurable like any other governed attribute" and §8.11 promises "one row of the §8.6 table, no code change." With this family, raising the description's floor is **inexpressible**, and no plan defines the column-floor evaluation path. Either define it (a synthesized resource, or facade-side floor lookup) or have 02-11 amend §7.4/§8.6 to scope configurability to property rows — silently shipping a configuration lever that does nothing for exactly the row §8.11 names as its example is the false-green shape this milestone hunts.
- **LOW — Substring trap in Task 3's twin-content check.** "Asserting the three bare field names are absent" from viewer twins: `resource.property.owner` is a substring of `resource.property.owner_player_id`, and `resource.property.visible_to` of `resource.property.visible_to_players`; a naive `strings.Contains` implementation fails on the *correct* policy text. Specify word-boundary matching (`property\.owner\b`, `property\.visible_to\b`, `property\.excluded_from\b`).
- **LOW — Broken-form remnants** in the settled `rg -c … returns 0` family: Task 1's `gallery.10` check and Task 4's `policytest` check still use it (covered by the orchestrator's confirmed finding; noting only that this task's *other* criteria were converted and these were missed).

### Suggestions

1. Resolve the builder cycle explicitly: either (a) move the mock providers and `createSeedEngine` into `abactest` and convert `seed_smoke_test.go`/`seed_validation_test.go` to `package policy_test` — and say so, with the file list to match — or (b) keep two builders and delete the "one builder" criterion. Also note `abactest` must construct via a fake `policystore.PolicyStore` + `Cache.Reload`, not the test file's private-snapshot injection.
2. Give Task 4 the deriving-double contract 02-08 already consumes: `PropertyProvider(charToPlayer map[string]string, rows …)` deriving the three peers with ALL/ANY, plus a drift test running one fixture through both the double and the real provider (stub resolver) asserting identical emitted key sets — schema parity alone does not bind semantics.
3. Add `internal/testsupport/abactest/abactest_test.go` to `files_modified`; home the schema-parity test there and the D-03 guard test in `seed_smoke_test.go` with a named antecedent source.
4. Widen the 02-10 audit scope (or record a D-11-style decision) for the `PlayerId`/`LocationId` projection the character permit exposes, and strike the inapplicable "change the row's visibility" remedy from the character half of the block comment.
5. Point the coverage test at `collectAttrRefs` (`compiler.go:170`) and state the root/path normalization; add the `abactest` depguard stanza.

### Risk: HIGH

---

##### Overall for the range (03, 13, 07)

The policy *content* — the eleven seed texts, the D-27 derivation direction, the func-field seam, the omit-don't-sentinel discipline — verified clean against the SPEC and the engine semantics (`combineDecisions` deny-overrides/disjunctive-permit at `engine.go:591-611`, missing-attr-false at `evaluator.go:401-411`, scalar-LHS `in` at `evalInExpr:339`, literal-only `containsAny` at `evalContains`, byte-order `compareStrings:185-201`, non-empty list grammar at `ast.go:232-236`). No fabricated Go symbols in the *implementation* paths; the one fabricated citation is a test-tool reference (`refs.go`). The failures are concentrated exactly where cycle 2 predicted: fixes landed in must-haves and narrative but not in task bodies, file manifests, or acceptance criteria (M15/M16/C2-14/M5), and two instructions are physically unexecutable (the `createSeedEngine` delegation cycle; the unowned BuildABACStack proof). The one genuinely new substantive trace is the `seed:profile-public-read-character` blast radius: the permit ships alt-linking and location tracking to every guest, the audit that gates the widening never looks at it, and the remedy the plan prescribes for exposed rows does not exist for the character half.

**Range risk: HIGH** — driven by the two unexecutable 02-07/02-13 instructions, the unowned 02-08 double contract, and the untraced character-permit exposure. None of it requires redesign; all of it requires the fixes to reach task bodies and file manifests.

One note on a locked decision (as instructed, once): D-27's ALL direction makes `owner_player_id` effectively single-character-only, so every multi-alt player — the milestone's IDENT-05 headline persona — sees their own private fields absent on the web profile. It is fail-closed, recorded, and checkpoint-gated (02-13 Task 0 presents the trade honestly), so I do not re-litigate it; I flag only that the checkpoint's "con" understates the frequency — multi-alt is the norm, not the edge — and the maintainer should read it as "the common case is narrower on the web," not "an edge case is narrower."

---

### pi — pass C (02-08, 02-09, 02-10)

All verification done. I have everything I need — here is the cycle-3 review for plans 02-08, 02-09, 02-10.

---

#### Cycle 3 Review — Pass C (plans 02-08, 02-09, 02-10)

**Tree:** `87b563913`, `fix/sandbox-restore-runbook-corrections`. Every citation below was opened and read.

##### Summary

The fix pass is real but uneven. C2-15 (status fail-open) is genuinely and well fixed; the 02-10 audit redesign (C2-17/18/19) is a structural fix, not a cosmetic one; C2-9's automated gates are correctly repaired. But the cycle-2 closing warning — "did this fix reach the task body, the file list, the acceptance criteria?" — is vindicated again: C2-11's guest-floor fix missed the one human-check surface that still demands the impossible player-floor evidence, and C2-12, C2-16, MEDIUM-21/22/23/24 are entirely untouched. More importantly, cycle 3 surfaces a defect nobody has caught in either prior cycle: the `abactest.NewSeedEngine` seam that three plans' behavioral gates now rest on **cannot be constructed from outside `package policy`** — the cycle-1 "unreachable helper" fix moved the helper across the package boundary, but the *construction* it wraps cannot cross it. Additionally, one of 02-08's D-27 direction guards is overdetermined and cannot fail, and 02-10's audit — the phase's only gate on a one-way privacy widening — never reads the population that is actually exposed to the anonymous web.

##### Cycle-2 fix verification (in-range findings)

| Finding | Verdict | Evidence |
|---|---|---|
| C2-9 (`rg -c`/`|| true` gates at 02-10:143,204,147) | **FIXED** | Both `<automated>` gates now use `rg -o … \| wc -l` + `-eq` with a correct explanation of why `|| true` yields `""` (02-10-PLAN.md:217,342). The broken **prose** form persists at 02-08:252-253, 02-09:221,329,405, 02-10:453 — instances of the already-confirmed 33-criteria finding; not re-derived. |
| C2-11 (fourth-rung needed player floor) | **PARTIAL — one surface missed** | Action, behavior, acceptance, threat model all correctly converted to the guest-floor discriminator (02-08:221-241,254,420). **But the Task 2 human-check at 02-08:248 still reads "showing `spectator` clearing the `player` floor"** — no player-floor policy exists (02-07:159-163), so the demanded evidence cannot be produced. See HIGH-1. |
| C2-12 (02-07/02-08 disagree on `abactest.PropertyProvider`) | **STILL OPEN** | 02-07:457-465 and its acceptance (02-07:507-511) promise only `Schema()` key parity for the doubles. Nothing in 02-07 or 02-13 requires the double to *derive* peers from character-keyed fixtures via a character→player map with ALL/ANY semantics, which 02-08:273-282 mandates. `abactest.go` is 02-07's file (wave 4); 02-08 (wave 5) lists only `internal/access/profilevis/*` in `files_modified`. See HIGH-2. |
| C2-15 (status fail-open) | **FIXED, genuinely** | `sectionAvailability` exhaustive switch with denying default + boot rule in `validateEntries` + paired synthetic-value unit tests (02-09:172-199,224-227). Verified SPEC vocabulary is closed at two values (01-SPEC.md:2107-2117). The §10.1 line citation checks out. |
| C2-16 (`Descriptor.Action` inert) | **STILL OPEN** | The fix pass's 02-09 diff is +36 lines, all Status handling. Task 2 still evaluates the *caller-supplied* `action` param and compares only `Descriptor.Resource` (02-09:266-290). §10.4 (01-SPEC.md:2190-2200) requires both `read` and `write` on the section resource; `Descriptor.Action` (`"read"`) is boot-validated non-empty and then decides nothing, anywhere. See MEDIUM-6. |
| C2-17/18/19 (audit redesign) | **FIXED in structure; two new defects** | Sanitized ledger + operator-only detail + separate remediation `checkpoint:decision` (Task 4) is a real redesign. But the redesign under-covers the enumerated public population (HIGH-4) and its new no-text guard fails on correct work (MEDIUM-5). |
| MEDIUM-17 (02-08 stale count) | FIXED | 02-08 now says eleven entries / two floors; matches 02-07's eleven (2 floors + reachable + 5 twins + admin + 2 widening). |
| MEDIUM-21 (engine interface placeholder) | **STILL OPEN** | 02-08:129 still literally `Engine <the access engine interface>`. `types.AccessPolicyEngine` requires `CanPerformAction` too (types/types.go:475-484); the acceptance double implements only `Evaluate`. Unresolved tension. |
| MEDIUM-22 (not-found-equivalent signal undefined) | **STILL OPEN** | 02-08:124 — still "the not-found-equivalent signal", no sentinel/type/code. |
| MEDIUM-23 (substitute-entry injection seam) | **STILL OPEN** | 02-09:326 still requires driving "a substitute entry whose `Descriptor.Resource` disagrees with its id" through `AssertSectionAccess`, which uses package-level `Lookup` over the fixed slice (02-09:276). No seam defined; mutating package state in tests is not specified either. |
| MEDIUM-24 (`All()` defensive copy) | **STILL OPEN** | No mention of copy/defensive anywhere in 02-09 (grep: zero hits). |
| MEDIUM-26 (stable row identity) | FIXED | Ledger design (row id + `md5` + length + verdict) discharges it. |

##### New concerns (cycle 3)

### HIGH-1 — 02-08:248: the C2-11 fix missed the human-check; it demands impossible evidence

`02-08-PLAN.md:248`: *"Paste into the SUMMARY the command and the failure output from the ordinal-implementation run, showing `spectator` clearing the **`player`** floor."* Plan 02-07 deliberately ships no player-rung policy (02-07:159-163 — the DSL list grammar at `internal/access/policy/dsl/ast.go:232-236` forbids the empty list, which is the whole D-03 amendment). Under the temporarily-ordinal policies, `spectator` clears the **guest** floor; there is no player floor for it to clear, so no run can produce the output the human-check demands. The executor either pastes the guest-floor output and fails the check as written, or edits the check. This is precisely the "fix reached the must-have but not the verification surface" pattern cycle 2's closing note named.

### HIGH-2 — `abactest.NewSeedEngine` has no implementable construction as specified; every behavioral gate in this range rests on it

02-07:476 instructs abactest to mirror `createSeedEngine`'s construction exactly. That construction — `createTestEngineWithPolicies` (`internal/access/policy/engine_test.go:888-931`) — builds the policy cache by poking **unexported fields**: `cache.mu.Lock(); cache.snapshot = &Snapshot{...}` (engine_test.go:922-927). I verified the entire exported `Cache` surface (`cache.go:74,95,141,179`): the only exported population path is `NewCache(store, compiler)` + `Reload(ctx)`, which requires a `store.PolicyStore` — a 10-method interface (`internal/access/policy/store/store.go:43-56`). And `NewEngine` takes a concrete `*Cache` (engine.go:71), so a fake cache type can't be substituted. Consequences:

- A non-`_test.go` package at `internal/testsupport/abactest` **cannot** mirror the construction — the unexported poke doesn't cross package boundaries. The workable path (an in-memory `PolicyStore` fake whose `ListEnabled` returns the corpus, then `NewCache` + `Reload`) appears in **no** plan, and no plan owns an exported constructor in `package policy` either.
- The consumers' verify commands are plain unit tests — `task test -- ./internal/access/profilevis/` (02-08) and `./internal/admin/section/` (02-09) — with no Docker/DB provisioning, so the DB-backed-store path is not silently available.
- This is the cycle-1 finding's ghost: "createSeedEngine is unexported and in a `_test.go`" was correctly diagnosed, but the replacement moves the *name* across the boundary while the *mechanism* stays behind it. Three plans' RED-first gates (the phase's entire verification-integrity posture) depend on this helper existing.

### HIGH-3 — 02-08 Task 3's forbid-side ANY-direction subtest is overdetermined: it passes with the derivation deleted

`02-08-PLAN.md` Task 3 acceptance: *"a `restricted` row whose `excluded_from` names ANY character of the viewing player is denied, even when **another** of that player's characters is in `visible_to` — the forbid-side ANY direction."* Trace it under D-27 (02-CONTEXT.md; 02-13-PLAN.md:100-103). Fixture: player P holds C1, C2; row `visible_to=[C2]`, `excluded_from=[C1]`. The permit-side peer is ALL-derived, so `visible_to=[C2]` (one of two characters) yields an **empty** `visible_to_players` — the permit twin never matches, and the read denies by default-deny **whether or not `excluded_from_players` is derived at all**. Delete the forbid-side derivation from the provider and this test stays green. The discriminating fixture is `visible_to=[C1,C2]` (ALL satisfied, permit fires) **and** `excluded_from=[C1]` — then only the forbid stands between the viewer and the row: correct derivation → deny-overrides (`engine.go:591-598`); dropped derivation → **permitted**. This is the "test that cannot fail" shape over the phase's privacy-critical derivation direction.

### HIGH-4 — 02-10's audit never reads the enumerated public rows — the population that reaches the anonymous web

The property ledger (result set 6, 02-10 Task 1) covers only public rows **outside** §8.6's enumeration; the operator-only detail query covers "the same two populations" (unenumerated rows + descriptions). Enumerated public rows — `profile.pronouns`, `profile.biography`, etc. — appear only as **aggregate counts** (result set 1). Yet those are precisely the rows the widening publishes to the **open web**: term A (tier floor) only matches enumerated names, so an enumerated public row at `anonymous` floor is anonymously readable after the conjunction; an unenumerated public row is grid-widened but web-denied (D-10's own reasoning, 02-10 Task 1 result set 3's comment). D-11 states the audit's job is "to find rows that were relying on colocation as de-facto privacy" — that reliance is not correlated with enumeration. Success criterion 4 (ROADMAP) requires establishing "exactly **which** existing `parent_type='character' AND visibility='public'` rows" the policy exposes; a count of the enumerated population is not *which rows*, and no query in the plan ever selects their text for adjudication — the human-check only adjudicates ledger rows. A sensitive `profile.biography` its author wrote assuming only co-located characters could read it passes through uncounted-as-a-row and unread. (Counterargument noted: D-12 frames "the recorded count" as the evidence. That framing contradicts criterion 4's wording and the audit's stated purpose; if the scoping is deliberate it should be recorded as a decision, not implied by omission.) The fix is cheap: one more ledger result set over the enumerated population (`name IN (...)` instead of `NOT IN`), with the detail query emitting text for operator-flagged rows.

### MEDIUM-5 — 02-10's no-player-text guard trips on the intended implementation and misses differently-aliased leaks

`02-10-PLAN.md` Task 1 acceptance: `[ "$(rg -v '^\s*--' … | rg -o '\bep\.value\b|\bc\.description\b' | wc -l)" -eq 0 ]`. The natural, correct ledger emission `md5(ep.value)` **contains** the substring `ep.value` — the gate demands 0, so the intended implementation fails (same failure class as C2-9, new instance, introduced by this fix pass). Conversely `SELECT p.value` under a different alias passes while emitting raw text. The plan text says `md5(value)` unqualified but never mandates aliases. Fix: mandate the exact select spelling (`length(value)`, `md5(value)` — single-table queries make this unambiguous), or replace the negative regex with a positive shape check.

### MEDIUM-6 — 02-09 C2-16 residue: `Descriptor.Action` remains decorative

Step 1 evaluates the caller-supplied `action`; step 3 compares only `Descriptor.Resource` (02-09:262-290). §10.4 gives each section both `read` and `write`; the per-entry `{Action: "read"}` is boot-validated non-empty and never consulted at request time. A descriptor carrying `Action: "nonexistent-verb"` passes boot and changes nothing. Either compare the caller's action against the descriptor (and define what a dual-action section carries), or drop the field and record why. Currently EXT-03's "mandatory descriptor" is half-mandatory data.

### MEDIUM-7 — 02-09's "boot path is exercised" claim is false; the wiring's failure path is structurally untestable as specified

Acceptance: `task test:int -- ./internal/bootstrap/...` exits 0 — *"the boot path is exercised, not just the function."* Verified: the only integration-tagged file under `internal/bootstrap/` is `setup/orphan_check_internal_test.go`, and **no** test in `internal/bootstrap/setup/` calls `Prepare` (grep: zero hits). The claimed exercise does not exist. Worse, the boot test the plan specifies — "asserting the step returns an error … driving `validateEntries` with a substitute slice" — re-tests Task 1's validator, not the wiring: the shipped registry cannot be made invalid, so `Prepare`'s new step can never be observed failing. The only real evidence the call site exists is the textual `rg` criterion. Also `boot.go`'s exported shape is left at "exposing the step" — undefined. Fix options: have the step take `[]Section` (injection seam, mirrors `validateEntries`), or downgrade the claim and rely on the textual check plus review.

### MEDIUM-8 — 02-10 Task 5's subset-corpus control has no mechanism, and a confirmed finding breaks its suite first

(a) The suite's engine is built once in `BeforeSuite` from the Postgres policy store via `policy.Bootstrap` (`test/integration/access/access_suite_test.go:130-200`). There is no subset-corpus seam: `abactest.NewSeedEngine` per 02-07:458 takes `(t, providers…)` and loads the FULL corpus — no filter parameter. The plan says "explicit exclusion by policy name" but supplies no construction; the executor must invent store mutation (`DELETE` + `cache.Reload` + re-`Bootstrap` on shared suite state) or an in-memory store. (b) Consequence of an already-confirmed finding: `access_suite_test.go:182` calls two-arg `attribute.NewPropertyProvider(propRepo, parentLocResolver)` (verified), 02-13 changes the signature to three args, and no plan owns that file — so 02-10's wave-5 gate `task test:int -- ./test/integration/access/` cannot compile until someone outside any plan fixes it.

### MEDIUM-9 — 02-09's INV-PRIVACY-11 entry silently conflicts with the registry's `refs` convention

The plan specifies id/scope/origin_spec/binding/asserted_by and says to model on "a nearby `bound` entry as the shape" — but every bound INV-PRIVACY sibling carries `refs:` (invariants.yaml:2016-2161). If the executor imitates that and adds `{file: "internal/admin/section/gate_test.go", …}`, the provenance guard fails: `test/meta/invariant_registry_test.go:551` requires each ref file in the scope's `owned_paths`/`shared_files`, and INV-PRIVACY's lists (invariants.yaml:205-232) contain no `internal/admin/section/**`. If the executor omits refs, tests pass but the entry is the scope's only ref-less bound entry. The plan must say explicitly: "no refs" (and why), or add the `shared_files` row.

### LOW-10 — 02-08's totality RED demonstration names an operator the DSL doesn't have

"Temporarily give the `guest` tier-floor policy a prefix match over `profile.`" — the DSL's operators are `==,!=,>,>=,<,<=,containsAll,containsAny,in,has` (`internal/access/policy/dsl/evaluator.go:168-250`); there is no `startsWith`/`hasPrefix` (02-07 even textually bans those tokens). The expressible equivalent is the range trick `>= "profile." && < "profile/"` (`'/'` = `'.'+1`). An executor attempting a literal prefix operator gets a parse error and improvises. Spell out the range form.

### LOW-11 — 02-08 placeholders: engine interface (line 129) and the not-found-equivalent signal (line 124) — MEDIUM-21/22 residue

Declare a consumer-local `interface { Evaluate(context.Context, types.AccessRequest) (types.Decision, error) }` (which also makes the recording double's compile-time-drift claim true), and define the reachability-denial return — sentinel, code, or `(found bool)`.

### LOW-12 — 02-10 stale citation: `000001_baseline.sql` lines 30-45 do not contain the `characters` DDL

`holomush_system_info` is at :37-43 and `entity_properties` at :354-377, but `characters` is at :71-78 — outside both cited ranges. The plan's own "re-derive, don't transcribe" instruction mitigates.

### Checked and clear

- `Evaluate(ctx, types.AccessRequest)` signature (engine.go:82), `NewAccessRequest` validation (types.go:143), `combineDecisions` disjunctive permits (engine.go:591-611), `createSeedEngine` unexported in `seed_smoke_test.go` — all as the plans claim.
- `ListPropertiesByParent`'s three-outcome filter (`internal/world/service.go:1144-1169`) — the abort-on-`ErrAccessEvaluationFailed` precedent is real.
- `AssertOperatorAdmin` shape (`internal/admin/auth/operator_admin.go:37-64`) and the lockstep comment (`ingame.go:115-117`) — as cited.
- INV-PRIVACY-11 is the next free id (highest: INV-PRIVACY-10 at invariants.yaml:2163); the scope's `origin_specs` already lists 01-SPEC (:204); pending entries carry no `asserted_by` and the meta-test enforces it (invariant_registry_test.go:217).
- `players.is_guest` confirmed (`000002_player_is_guest.sql`); `entity_properties` has no FK to `characters` (baseline :354-377); `seed_policies_test.go`'s DELETE ordering is as 02-10 describes.
- §8.6's table: 23 `profile.*` names, 11 media names — consistent with 02-08/02-10's "eleven enumerated media names" and the counted-comparison gate.
- `seed:profile-reachable` (02-07:234) transcribes §8.4.2 (01-SPEC.md:1427-1431) verbatim. The two-vs-three C2-10 fix in 02-07 is internally consistent now.
- 02-10 Task 5's paired control discriminates despite the confirmed unconditional permit at 02-07:376, because the control corpus excludes `seed:profile-public-read-character` itself — off-location character reads then fall back to `seed:player-character-colocation` (seed.go:51-55) and deny. No new defect, but note the permit assertion for the character-entity read passes *because of* that unconditional permit — its narrowing (the confirmed finding) will flip this spec, which is correct behavior for the test.
- No import cycle: `internal/bootstrap/setup` → `internal/admin/section` → `internal/access` is acyclic; 02-09 adds no subsystem, so the confirmed `[17]stubSubsystem` finding doesn't intersect it.

##### Suggestions

1. 02-08:248 — one word: `player` → `guest`.
2. Give `abactest.NewSeedEngine` a specified, buildable construction in 02-07: an in-memory `PolicyStore` fake (only `ListEnabled` non-trivial; the other nine methods return errors) + `policy.NewCache` + `Reload`; delete "mirroring createSeedEngine's construction exactly". Also add a subset variant (or a `policies []SeedPolicy` parameter) so 02-10 Task 5's control has a seam — and say which in 02-10.
3. 02-08 Task 3 — change the `excluded_from` fixture to "`visible_to` names ALL of the viewing player's characters AND `excluded_from` names one"; keep the current fixture as an additional overdetermination note if desired.
4. 02-10 — add a ledger result set over the **enumerated** public population (id, name, length, hash); let the detail query emit text for operator-flagged ids. Mandate the unaliased `md5(value)`/`length(value)` spelling or replace the alias-regex guard.
5. 02-09 — resolve `Descriptor.Action` (consult it or drop it); add an injection seam for the request-time mismatch test (`AssertSectionAccess` taking a lookup func defaulting to `Lookup`, or `var all` documented as test-mutable); state `All()` returns a copy; state "no `refs`" for INV-PRIVACY-11 or add the `shared_files` row; repair or delete the "boot path is exercised" claim.
6. 02-08 Task 1 — declare the consumer-local minimal engine interface; define the reachability-denial signal (`ErrProfileUnreachable` or a `found bool`).

##### Risk Assessment: **HIGH** for this range

Two of three plans carry acceptance-surface defects that fail on correct work (HIGH-1, MEDIUM-5) or tests that cannot fail for their stated property (HIGH-3). The shared test-infrastructure seam under every behavioral gate in the range is unbuildable as written (HIGH-2). And 02-10 is the phase's only gate on a genuinely one-way privacy widening whose audit, as specified, never adjudicates the most exposed population (HIGH-4). The fixes are all small and local — none threatens the architecture — but they must land in plan text (task bodies, `<files>`, acceptance criteria), not narrative, or cycle 4 will find them again.

---

### pi — pass D (02-06, 02-12, 02-11)

All verification complete. Here is the cycle-3 review for plans 02-06, 02-12, 02-11.

---

#### Cycle-3 Review — Pass D (plans 02-06, 02-12, 02-11)

**Tree:** `87b563913`, worktree `v013-milestone`. Every finding below was checked against source read in this session; goose API claims were checked against the pinned module (`github.com/pressly/goose/v3@v3.27.3`).

---

##### PLAN 02-06 — Gated name admission

### Summary

The plan's central design (single-constructor `Admitted` token + tree-derived census) is coherent and its source citations are unusually accurate — I verified `character_repo.go:79` (`name = $2`), the sole production caller chain (`service.go:826` → `mutator.go:259` → `repo.Update`), the CAS test's name witness (`character_repo_test.go:729-748`), all five `ExistsByName` sites including the fifth at `auth_suite_test.go:297`, both composition roots, and the fence's skip-set/`.go`-marker claims (`world_sql_fence_test.go:134,164-177,306-309`). But the cycle-2 C2-1 settlement it now depends on makes its core mechanism unbuildable, and its most important cycle-2 blocker (C2-5) was not addressed at all.

### Cycle-2 fix verification

| Finding | Verdict | Evidence |
|---|---|---|
| C2-1 (token proves less than writer requires) | **Fixed in text, broken in mechanism** | 02-01 was amended so `Gate.Check` subsumes `world.ValidateCharacterName` (`02-01-PLAN.md:38,193,250-252,324,350`); 02-06's matching prose landed (must-have truth 2, Task 3's "Delete `createWithMaxAndBind`'s separate `world.ValidateCharacterName` call", `02-06-PLAN.md:448-457`). Consistent — except it mandates `charname → world`, while 02-06's interface change mandates `world → charname`. See HIGH-1. |
| C2-2 (no block-list transport) | **Genuinely fixed** | `02-05-PLAN.md:51,63-64,292-294,401,408-411,423` ships `(*Subsystem).Matcher()`, `BlockList *blocklist.Subsystem` on both configs, populated in `core.go`; 02-06 consumes it with acceptance criteria (`02-06-PLAN.md:580-582`). `grpcSubsystemConfig`'s whole-subsystem-pointer shape verified at `cmd/holomush/sub_grpc.go:67-76`. |
| C2-5 (blast radius omits three implementers/callers) | **NOT fixed — zero mentions** | `02-06-PLAN.md` contains no occurrence of `character_genesis_test`, `character_genesis_integration`, `attribute/character_test`, or `build:all` (grep, zero hits). All three files exist exactly as cycle 2 described: `internal/auth/character_genesis_test.go:30` (`fakeCharWriter.Create` mirrors the changing `CharacterWriter` interface at `character_genesis.go:43-45`), `internal/access/policy/attribute/character_test.go:45` (`mockCharacterRepository.Create`, passed to `NewCharacterProvider` at `:78,82,235`, which takes `world.CharacterRepository` — `character.go:32,59`), `internal/auth/character_genesis_integration_test.go` (`//go:build integration`, 5 `genesis.Create` callers). See HIGH-2. |
| MEDIUM 11 (self-referential `go build ./...` grep) | **Fixed** | `02-06-PLAN.md:404` reworded to a SUMMARY obligation. |
| MEDIUM 14 ("five sites with identical SQL bodies" — two are interfaces) | **NOT fixed** | `02-06-PLAN.md:573` still requires "Every one of the five carries the SAME transitional predicate … asserted by a test that drives one key through all five adapters". `internal/auth/character_service.go:24` and `internal/auth/guest_service.go:39` are interface declarations — they carry no SQL and cannot be "driven". Only three adapters exist (`adapters.go:39`, `harness.go:1549`, `auth_suite_test.go:297`). The acceptance criterion is unexecutable as written. |
| MEDIUM 29 (`task build` for an interface-change plan) | **NOT fixed** | Task 2 action (`02-06-PLAN.md:366`) and Task 3 verify (`:567`) still use `task build`. `Taskfile.yaml:476-485` defines `build:all` as "the wave-gate for interface-change blast radius … `task build` only compiles the main binary". This plan changes `world.CharacterRepository` and two auth interfaces. |
| `rg -c … returns 0` broken gates (settled cycle-3 finding) | **Present here: 2 instances** | `02-06-PLAN.md:396,401`. Confirmed against the global finding; not re-derived. |

### Concerns

**HIGH-1 — The C2-1 settlement is now encoded as two mutually unsatisfiable automated gates.** `02-01-PLAN.md:~351` carries an acceptance criterion: `[ "$(go list -deps ./internal/world | rg -o 'holomush/internal/charname$' | wc -l)" -eq 0 ]` — "`internal/world` does not import `internal/charname`, so the new `charname → world` edge cannot close a cycle" (also `02-01-PLAN.md:322-324`: "The note … MUST NOT introduce an import: `internal/world` importing `internal/charname` would close a cycle"). `02-06-PLAN.md` Task 2 requires `Create(ctx, char *Character, name charname.Admitted)` on `world.CharacterRepository` (`internal/world/repository.go:197-222`, package `world`) — an import of `internal/charname` by `internal/world`. This is the confirmed cycle-3 import cycle; what I add is that **it is no longer a silent design conflict — both sides are `<automated>`-checkable gates, so the phase cannot go green in either direction**, and nobody has said which edge dies. Further untraced consequence: 02-06's own no-cycle criterion (`02-06-PLAN.md:401`, `go list -deps ./internal/world/postgres | rg -c 'holomush/internal/store$'` returns 0) cannot even execute under the cycle — `go list` fails, prints nothing, `rg -c` prints nothing, and "returns 0" is unobservable. The failure will present as a cryptic gate failure in wave 4, not as "your two plans disagree".

**HIGH-2 — C2-5's compounding verification gap is real: every gate in 02-06 passes while the tree doesn't compile.** `internal/access/policy/attribute/character_test.go` is **untagged** (verified, no build line) and in package `attribute`, so it is compiled only by `go test ./internal/access/...`. No command in 02-06 runs that: Task 2 verify is `./internal/world/... ./test/meta/`; Task 3 verify is `./internal/auth/ ./internal/bootstrap/... ./cmd/holomush/ ./test/meta/` + `task test:int -- ./test/integration/auth/`; the plan-level `<verification>` (`02-06-PLAN.md:683-688`) is the same scoped list. After Task 2, `mockCharacterRepository` no longer satisfies `world.CharacterRepository` (missing `Rename`, wrong `Create` signature) → compile error in a package no gate reaches. Similarly `character_genesis_integration_test.go` is integration-tagged inside `internal/auth` — reached only by a *full* `task test:int`, not by Task 3's scoped `-- ./test/integration/auth/`. The break surfaces in 02-13's wave (02-13 owns `internal/access/policy/attribute/`), forcing a later plan to repair a break it didn't make.

**HIGH-3 — 02-06 mandates a test that 02-12 makes unwritable, and neither plan owns its removal.** `02-06-PLAN.md:574` requires "a test asserts a row with `normalized_name IS NULL` and a matching `LOWER(name)` IS reported as existing". `02-12-PLAN.md` lands `000056` (`SET NOT NULL` — the NULL row becomes un-insertable) **and** deletes the NULL branch from the predicate (`02-12-PLAN.md:265-276`). The test's subject and its fixture both cease to exist. 02-12's action and acceptance never mention removing that test; 02-06 never names the file it lives in, so no plan owns it. Grep of `02-12-PLAN.md` for the NULL-visibility assertion: zero hits. This is precisely the "gate that verifies a property in wave 4 says nothing about whether wave 5 preserves it" trap from the review brief.

**MEDIUM-1 — The transitional predicate covers the existence check but not the gate's `SkeletonLookup`; the same window the plan closes for duplicates stays open for confusables, permanently and undetectably.** 02-06's must-have truth 8: "at no point does a character that exists become invisible to the friendly existence check." But `Gate.Check` also calls `SkeletonExists` against the `name_skeleton` column (`02-01-PLAN.md` Layer 4), and every pre-existing row has `NULL` skeleton until `000055` (wave 5). NFKC does not collapse cross-script confusables (Cyrillic А ≠ Latin A byte-wise), so in a wave-4 deployment: `Аlaric` vs existing `Alaric` → skeleton lookup misses (NULL), transitional existence check misses (different keys), admitted. After the backfill, both rows exist; `000055`'s collision detection covers `normalized_name` only, and the skeleton index (`000054`) is non-unique — so the confusable pair is **never detected by anything, ever**. In production this is masked by migrations running before `orch.StartAll` in a single-release deploy, but the plans explicitly hold themselves to the every-commit-deployable bar (T-02-93's own reasoning), and by that bar this is the same failure class, unaddressed. The clean close is one line in `02-12`: `BackfillCharacterIdentity` also reports skeleton-collision sets.

**MEDIUM-2 — The nil-`BlockList` construction-failure test has no owned home.** `02-06-PLAN.md:583` requires "A test constructs each root's config with a nil `BlockList` and asserts construction FAILS". The bootstrap root's natural test file is `internal/bootstrap/setup/subsystem_test.go` — owned by 02-05 (wave 3), absent from 02-06's `files_modified`. The grpc root's home would be a `cmd/holomush` test file — 02-06 owns only `sub_grpc.go` there, no test file. Another unowned-artifact criterion of exactly the shape cycle 2's C2-4/C2-14 hunted.

**MEDIUM-3 — The "one shared helper" for gate construction has no specified home package, and the constraints make the obvious homes wrong.** `02-06-PLAN.md:508-514` requires one helper called from both `internal/bootstrap/setup/subsystem.go` and `cmd/holomush/sub_grpc.go`. It can't live in `internal/charname` (02-05 deliberately declares the block list as a consumer-side interface there precisely so `charname` doesn't import `charname/blocklist` — `02-06-PLAN.md:389-393` asserts this). It can't live in `internal/bootstrap/setup` unless `cmd/holomush` is meant to import that (possible but a new edge). Nobody says. The executor will pick, and the two-root drift the helper exists to prevent starts with the helper's own placement.

**LOW-1 — Guest path gets no 23505→retry mapping.** The 23505→`CHARACTER_NAME_TAKEN` handler lands only in `internal/auth/character_service.go` (`02-06-PLAN.md:556-560`). The guest insert races identically (the retry loop pre-checks, then inserts); post-`000056` a lost guest race surfaces as a raw `CHARACTER_GENESIS_FAILED` instead of a retry. Tiny window, loud failure — but the plan's own "the loop that already exists absorbs a rejection" logic applies to the DB's rejection too.

---

##### PLAN 02-12 — Backfill migration, unique index, resolution CLI

### Summary

The C2-6 repair is the best fix in this pass: I verified the corrected staging recipe against the pinned goose source, the in-tree fixture, and the production provider, and it is mechanically sound. The fixture-repair enumeration matches the tree exactly (16 `INSERT INTO characters` files; the plan's list covers all of them with the two documented exclusions). But C2-7 — the finding that the closing integration spec has no executable CLI seam — was not touched, and two cheap ownership contradictions (MEDIUMs 3 and 4) survived verbatim.

### Cycle-2 fix verification

| Finding | Verdict | Evidence |
|---|---|---|
| C2-6 (staging cannot produce `000055`) | **Genuinely fixed** | New recipe (`02-12-PLAN.md:443-478`): temp dir of real `.sql` 1–54, omit 56, global registry **enabled**. Verified sound: `provider_options.go:169` ("By default, goose will register all Go migrations including those registered globally"); the fixture's pairing comment (`migrate_gointerleave_integration_test.go:121`, options at `:210,:214`); production provider passes no disable flag (`migrate.go:119-127`); `Provider.ListSources()`/`GetDBVersion` exist (`provider.go:185,195`); no registered Go migrations exist today (`ls internal/store/migrations/*.go` → only `doc.go`), so no registry contamination. The staging precondition + observed-mis-staging criterion is a real non-vacuity control. One unstated dependency: the test binary must transitively import `internal/store/migrations` for the registry to be populated — the harness does (`harness.go:100` → `internal/store` → `migrations_register.go:23`), so it works in practice, but if the precondition fires, the plan's "sanctioned repair" (export the migration + both options) is far heavier than the actual one-line fix (blank import in the test file). |
| Cycle-1 #1 / CLI registration on root | **Fixed on paper** | `root.go:37-46` verified (exactly the ten commands the plan names); `root.go` and new `root_test.go` in Task 2 `<files>`; tree-traversal test specified. |
| C2-7 (duplicates spec has no executable CLI seam) | **NOT fixed** | Grep of `02-12-PLAN.md` for `subprocess|exec.Command|go run|TestMain|binary|importable`: zero relevant hits. Task 3 still says "resolve the pair through `holomush character name set`" and acceptance still requires "The duplicates spec resolves the pair through the CLI path" (`02-12-PLAN.md:534`). `cmd/holomush` is `package main` (verified); `test/integration/charname` cannot import it; `task test:int` does not build or install the binary. The plan supplies no mechanism. See HIGH-4. |
| C2-8 (writer exclusion documented, not enforced) | **Unchanged — deliberate but untracked** | T-02-100 (`02-12-PLAN.md:616`) argues the posture honestly (loud failures, single-replica default topology, ACCESS EXCLUSIVE tradeoff named). But MEDIUM 30's enforceable-guard recommendation was not incorporated, and no GitHub issue is filed to track the `compose.cluster.yaml` posture — the residual exists only as plan prose, invisible after the phase closes. |
| MEDIUM 3 (Task 1 `<files>` omits the five existence-check files) | **NOT fixed** | Task 1 `<files>` at `02-12-PLAN.md:181` still lists only migrations + fixture files. Its own action (`:265-276`) orders editing `internal/bootstrap/setup/adapters.go`, `internal/auth/character_service.go`, `internal/auth/guest_service.go`, `internal/testsupport/integrationtest/harness.go`, `test/integration/auth/auth_suite_test.go`; its acceptance (`:313-314`) gates on them. In frontmatter, in no task scope — exactly as cycle 2 said. |
| MEDIUM 4 (`migrations_register.go` contradictory ownership) | **NOT fixed** | Still in Task 1 `<files>` (`:181`) while the action says "the file is deliberately absent from this plan's `files_modified` and touching it is not required" (`:231-235`) and acceptance requires it untouched (`:305`). Cycle 2's suggested one-line fix ("Remove it from `<files>`") was not applied. |
| `rg -c … returns 0` broken gates (settled finding) | **Present here: 7 instances** | `02-12-PLAN.md:309,310,313,314,315,319,413`. Not re-derived. |

### Concerns

**HIGH-4 — The phase's closing integration spec is unexecutable as written (C2-7 stands).** `name_duplicates_test.go` must "resolve the pair through the CLI path and then assert `000056` applies" (`02-12-PLAN.md:534`). Given input: a Go test in `test/integration/charname` (package `charname_test`). The system: the resolution surface is a cobra command in `package main`. Go makes the import illegal; the plan names no subprocess recipe, no importable `internal/cli/...` extraction, and no "call the same seam the command calls" fallback. An executor must either improvise the seam (the thing plans exist to prevent) or quietly downgrade the criterion to calling `Gate.Admit` + `Rename` directly — which would no longer prove the CLI path works, i.e., the test would pass while `holomush character name set` is broken. That is the milestone's named failure shape.

**MEDIUM-4 — The CLI's block-list wiring is under-specified in a way that can silently diverge from production.** Task 2 says "construct the `charname.Gate` against that pool mirroring `internal/bootstrap/setup/subsystem.go`'s boot wiring — including the block list" (`02-12-PLAN.md:388-390`). But the boot wiring post-02-05/02-06 is `cfg.BlockList.Matcher()` on an orchestrator-`Prepare`d subsystem — the CLI has no orchestrator and no config field. The plan doesn't say whether the CLI instantiates `blocklist.Subsystem` and calls `Prepare` itself (D-15's hard-failure-on-bad-pattern posture preserved) or builds a `Cache` directly (posture lost — a bad pattern would silently mean "no list", and note 02-06's `NewCache|Compile` prohibition at `02-06-PLAN.md:582` deliberately covers the two server roots but **not** `cmd_character_name.go`, so nothing forbids the drifted shape). An operator resolving a collision during a failed deploy is precisely the moment "CLI and create path reject different names" must be impossible.

**MEDIUM-5 — `duplicates` subcommand's executor-type story doesn't survive contact with its own plan.** `BackfillCharacterIdentity` takes a "minimal executor interface" that `000055`'s `*sql.Tx` satisfies (`02-06`, `02-12:190-192`) — i.e., `database/sql`-shaped. The CLI "opens its own pool" following `migrate.go` — `store.NewMigrator(url)` is `database/sql`, fine — but `02-12:394` has `duplicates` "call `BackfillCharacterIdentity` inside a transaction and roll it back", while the same command's `set` half uses the pgx-native `CharacterRepository.Rename`. Two handle types in one command, and no plan text says which pool type the CLI opens. If the executor opens a `pgxpool.Pool` (house default everywhere else), the `database/sql`-shaped executor interface doesn't fit and the `duplicates` half stalls. One sentence ("the CLI uses `pgx/stdlib` `database/sql` throughout" or "the executor interface is pgx-shaped and `000055` adapts") settles it; today it's a coin flip.

**LOW-2 — Prose slip:** Task 2 claims "Every other `cmd_*.go` reaches the server over the admin UDS/Connect surface" (`02-12-PLAN.md:385`) and in the same breath cites `cmd/holomush/migrate.go` — which opens its own handle via `store.NewMigrator(url)` (`migrate.go:286`) — as the precedent it follows. The sentence is false by its own citation; harmless but confusing in a paragraph whose whole point is precision about transport.

**LOW-3 — `root_test.go` framing.** "if present, else any existing test asserting the registered command set" — no `root_test.go` exists, but `cmd/holomush/cmd_admin_test.go:20-49` already exercises `NewRootCmd()` five times; the reachability test has a natural neighbor. Cosmetic.

---

##### PLAN 02-11 — SPEC amendments, validation map, phase gate

### Summary

The three targeted fixes (C2-9, C2-20, MEDIUM 18) landed properly — this is the one plan in my range where the fix pass fully worked, including the nice touch that the D-03 count check is now derived from `seed.go` rather than self-satisfying. Every amendment target I checked is real: 41 stale `.up.sql` citations, the three-key viewer table at `01-SPEC.md:1367-1371`, the falsified "only path" at `:2817-2823` (`CharacterReapingService` verified at `internal/auth/character_reaping.go:128`), exactly 31 data rows in the RESEARCH test map (`:1222`), the placeholder row at `02-VALIDATION.md:56`. Two cheap leftovers (MEDIUMs 6, 7) survived, and the fix pass introduced a fresh instance of the very gate defect it was chartered to remove.

### Cycle-2 fix verification

| Finding | Verdict | Evidence |
|---|---|---|
| C2-9 (automated gates fail on success path) | **Fixed in 02-11** | Both `<automated>` gates now `[ "$(rg -o … | wc -l)" -eq 0 ]` (`02-11-PLAN.md:241,352`). But see MEDIUM-6: the fix pass added a **new** broken instance in this same plan. |
| C2-20 (D-03 amendment claimed but unowned/unedited) | **Genuinely fixed** | `files_modified` now includes `02-CONTEXT.md` (`02-11-PLAN.md:9`); the amendment is on disk (`02-CONTEXT.md:59` "AMENDED 2026-08-04"); Task 1 re-verifies the count against `internal/access/policy/seed.go` with both numbers recorded, and the acceptance has the "verified, with both counted numbers" anti-restatement clause (`02-11-PLAN.md:250`). `seed.go` currently has zero `seed:profile-tier-floor-` entries (pre-02-07) — correctly an execution-time check. |
| MEDIUM 18 ("four amendments" vs seven) | **Fixed** | Task name, action, and acceptance all say seven (`02-11-PLAN.md:92,103,255`). |
| MEDIUM 6 (Task 2 `read_first` omits `02-13-SUMMARY.md`) | **NOT fixed** | `02-11-PLAN.md:268` still reads "plans 02-01 through 02-10 and 02-12" while the action (`:272-278`) requires covering 02-13's tasks and its observed RED results. |
| MEDIUM 7 (`depends_on` omits 02-01/02-03/02-07) | **NOT fixed** | Frontmatter (`02-11-PLAN.md:6`) unchanged. Harmless for wave-6 ordering, but the plan's stated inputs (it must tabulate 02-01/02-03/02-07's rule-4 demonstrations) still don't match its dependency list. |
| MEDIUM 31 (RED-count derivation is executor discipline) | **Partially addressed** | The action now enumerates the full expected collection explicitly (`02-11-PLAN.md:280-299`) — I cross-checked it against all 13 plans' rule-4 paragraphs and it matches, including the 02-03→02-07 no-WARN move. Still no automated derivation, but the enumeration makes executor error unlikely. Acceptable. |

### Concerns

**MEDIUM-6 — The fix pass introduced a new instance of the exact defect it was chartered to remove.** `02-11-PLAN.md:248` (Amendment E's acceptance, new this pass): "`rg -n -A 4 'INV-WORLD-6' … | rg -c 'ONLY'` returns 0". `rg -c` prints nothing and exits 1 at zero matches; "returns 0" is unachievable. This is within the settled 33-gate finding, but it is *new since the fix pass* — the fix for C2-9 propagated correctly to the two gates cycle 2 named while a third was authored in the broken form a few lines away. Evidence for the pattern's persistence, and a reason the next pass should grep for the form rather than enumerate instances.

**MEDIUM-7 — The abac-reviewer briefing omits the phase's highest-severity authorization change.** Task 3's briefing (`02-11-PLAN.md:394-403`) names three questions; none is the unconditional `seed:profile-public-read-character` — `permit(principal is character, action in ["read"], resource is character);` with no `when` clause (`02-07-PLAN.md:376`). The orchestrator has already confirmed what that permit does: `internal/world/service.go:785` gates `GetCharacter` on exactly `("read", character)` and `internal/world/grpc_server.go:127-137` projects `PlayerId` and `LocationId` — so the "description widening" also publishes owner identity and location off-location, a consequence D-10/D-11 never name. Question 3 gestures at "two `seed:profile-public-read-*` entries" but does not put the no-`when`-clause permit or the `GetCharacter` projection in front of the reviewer. abac-reviewer is the last gate before merge (T-02-68); the briefing is the one place the plan can aim it. This is a consequence of the confirmed permit finding that nobody has traced into the closeout plan.

**LOW-4 — Task 2's scope sentence.** "Every `02-*-SUMMARY.md` produced by plans 02-01 through 02-10 and 02-12 (this plan is the last to run; 02-12 is wave 5 and lands before it)" — the parenthetical explains 02-12's inclusion but says nothing about 02-13 (wave 3, also lands before it), which reads as if the omission were deliberate. Ties to MEDIUM 6.

---

##### Overall Risk Assessment: **HIGH**

Justification, bluntly:

1. **02-06 is unexecutable as written** — not because of anything in 02-06, but because the fix pass resolved C2-1 by mandating the `charname → world` edge while 02-06's core mechanism mandates `world → charname`, and *both* edges are now pinned by automated acceptance gates (confirmed cycle-3 finding; my addition: the contradiction is now machine-checked on both sides, and 02-06's own `go list` gate can't even run under the cycle). Until someone decides which edge dies — e.g., move `Admitted` to a leaf package both import, or have `charname` re-implement rather than call `ValidateCharacterName` — every wave-4+ plan in the phase is blocked, 02-12's CLI included.
2. **02-06's own verification can go fully green while the tree does not compile** (HIGH-2: unowned, untagged `internal/access/policy/attribute/character_test.go` outside every scoped gate; `task build` instead of `build:all` still prescribed).
3. **02-12's closing spec cannot invoke the surface it exists to prove** (HIGH-4 / C2-7 unaddressed).
4. The remaining items are executor-friction class (unowned test homes, MEDIUMs 3/4/6/7, the broken-gate forms) — individually small, collectively the exact "fix reached the must-have but not the task body / file list / acceptance" pattern cycle 2 named as the thing to hunt.

### Top suggestions (actionable, in order)

1. **Break the cycle deliberately, in plan text, now.** Cheapest sound option: `charname.Admitted` and `Gate` stay in `internal/charname`; move the syntactic rule *implementation* into `internal/charname` and have `world.ValidateCharacterName` become a thin delegate (world already imported by charname's consumers; the edge becomes `charname` → nothing-of-world). Update `02-01:250-252,322-324,351` and `02-06` Task 1/2 accordingly. Alternative: `Admitted` in a leaf `internal/charname/token` imported by both — but then the gate's subsumption still needs the syntactic rule out of `world`.
2. **02-06: add the three C2-5 files to `files_modified`, switch Task 3's compile enumeration to `task build:all`, and add `./internal/access/...` to a scoped test command.** Recast the five-site claim as "five declarations, three query bodies; the comparison test drives the three adapters" (MEDIUM 14 verbatim).
3. **02-12: pick the CLI seam.** Either extract the command body to an importable package (`internal/cli/charactername`, with `cmd_character_name.go` as a thin cobra shell — mirroring how `runMigrateUpLogic` is already factored for tests in `migrate.go:261-268`), or specify `exec.Command("go", "run", "./cmd/holomush", …)` with the DB URL plumbed. Also: state the CLI uses `database/sql` via `pgx/stdlib` for both halves (fixes MEDIUM-5), and state it instantiates and `Prepare`s `blocklist.Subsystem` so D-15's fail-hard posture holds (fixes MEDIUM-4).
4. **02-12: add skeleton-collision reporting to `BackfillCharacterIdentity`** (one more collision-set kind) to close the NULL-skeleton blindness window; and **own the removal of 02-06's NULL-visibility test** — add the file to `files_modified` once 02-06 names it.
5. **02-11: add a fourth briefing question** naming `seed:profile-public-read-character`'s missing `when` clause and the `GetCharacter` `PlayerId`/`LocationId` projection (`service.go:785`, `grpc_server.go:127-137`); fix `:248` to the `-o | wc -l` form; add `02-13-SUMMARY.md` to Task 2 `read_first`.
6. **Sweep, don't enumerate:** `rg -n "rg -c '[^']*'.*returns 0" .planning/phases/02-abac-schema-vocabulary/` — the broken gate form keeps regenerating; fix all instances in one pass.

### One locked-decision note (as invited, once)

D-21's three-migration sequencing is sound, but the phase's own every-commit-deployable discipline (02-06 must-have truth 8, T-02-93) is applied selectively: it bought the transitional existence-check predicate, yet no equivalent was bought for the skeleton lookup (MEDIUM-1) or for the wave-4→5 window's guest-path confusable admission. If per-commit deployability is a real bar, it should be applied to every gate that reads a not-yet-backfilled column, or stated once as "deployable per release, not per commit" so the transitional machinery's actual purpose is honest.

---
