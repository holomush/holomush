---
phase: 3
reviewers: [codex, pi]
reviewed_at: 2026-08-07T16:35:01Z
plans_reviewed:
  - 03-01-PLAN.md
  - 03-02-PLAN.md
  - 03-03-PLAN.md
  - 03-04-PLAN.md
  - 03-05-PLAN.md
source_grounding:
  enabled: true
  authority: grep
  authority_note: "config declares source_grounding_authority: grep; intel's api-map.json is empty on this Go repo (API-SURFACE.md: 'intel extraction is regex/JS-only or not yet populated'), so grep/ripgrep + Read is the adapter used throughout."
---

# Cross-AI Plan Review — Phase 3: World Character Commands

Both lanes were prompt-fed the full source-grounding instruction set and both had repo
access (neither output carries the `REVIEWED-WITHOUT-REPO-ACCESS` marker), so both
verdicts count at full consensus weight.

## Codex Review

### Summary

The phase decomposition is thoughtful and most repository claims are well grounded, but the plans are not execution-ready. I found four cross-plan blockers that the earlier checker missed:

1. Plan 03-01’s proposed service implementation ignores the caller’s `expectedVersion`.
2. Plan 03-03’s deterministic stale-write test cannot reach the CAS conflict using the proposed service behavior.
3. Plan 03-02 wires both subsystems before their production dependencies exist, while Plans 03-04/05 do not revisit `cmd/holomush/core.go`.
4. Plan 03-05 deletes flushed KV entries without revision protection, allowing concurrent activity to be lost.

Overall risk: **HIGH** until these contracts are repaired.

### Strengths

- The plans correctly identify `worldMutator.mutate` as the mandatory transactional seam. It performs the mutation and `WriteIntent` inside one transaction at `internal/world/mutator.go:197`, and the census explicitly maps each service command to one kind at `internal/world/mutator.go:78`.

- The distinction between `Rename` and retirement is correct. Existing service mutations obtain a reader view and route writes through `s.mutator`; `UpdateCharacterDescription` demonstrates this at `internal/world/service.go:799`.

- The subsystem lifecycle design follows the actual contract: durable consumers belong in `Prepare`, while consume loops belong in `Activate`, as specified at `internal/lifecycle/subsystem.go:72` and `internal/lifecycle/subsystem.go:117`.

- Relocating the consumer retry helper is justified. It is currently package-private at `internal/eventbus/audit/projection.go:167`, while the two callers preserve distinct error wrapping at `internal/eventbus/audit/projection.go:128` and `internal/eventbus/audit/plugin_consumer.go:194`.

- The retirement idempotency basis is real: `DeleteByCharacter` atomically deletes one active/detached session and returns `(nil, nil)` when absent at `internal/store/session_store.go:809`.

- The ABAC analysis is accurate. Existing self-access covers only `read` and `write` at `internal/access/policy/seed.go:39`; admin access uses an open action at `internal/access/policy/seed.go:105`; system policies currently cover only locations and exits at `internal/access/policy/seed.go:203`.

- Plan 03-05 correctly keeps `last_active_at` outside `worldMutator`. The field is explicitly operational and currently read-only at `internal/world/character.go:35`.

### Concerns

#### Plan 03-01

- **HIGH — `expectedVersion` is discarded.**  
  The plan gives `RetireCharacter(..., expectedVersion int)` but instructs the service to call `characterRepo.Get` and pass `char.Version` into `setCharacterStatus`. That copies the existing internally-versioned preference pattern at `internal/world/service.go:845`, where the method intentionally has no caller-supplied version. It does not implement IDENT-10. A stale caller can arrive after another mutation, read the new version internally, and overwrite successfully.

- **HIGH — semantic-state checks can mask stale-version conflicts.**  
  The plan checks “already retired” before attempting CAS. After another writer retires the row, a stale request would return `CHARACTER_ALREADY_RETIRED`, not the required `WORLD_CONCURRENT_EDIT`. INV-WORLD-7 explicitly requires stale existing-row mutations to surface the typed conflict at `docs/architecture/invariants.yaml:5111`.

- **MEDIUM — common `SetStatus` clears the default pointer during unretire too.**  
  Task 1 puts `UPDATE players SET default_character_id = NULL` inside the generic `SetStatus` implementation, then Task 2 reuses it for `StatusActive`. D-34 applies to retirement, not every lifecycle write. The API should make the retire-only side effect explicit.

- **MEDIUM — concurrent-retire coverage is claimed in the plan but not actually assigned here.**  
  The must-have says two concurrent retires are proven, while Plan 03-01’s concrete tests focus primarily on repository behavior and Plan 03-03 owns the two-replica proof. Clarify whether Plan 03-01 provides a deterministic unit-level collision test or leaves that truth exclusively to 03-03.

#### Plan 03-02

- **HIGH — production wiring is scheduled before the required effect interfaces exist.**  
  Plan 03-02 modifies the sole composition root at `cmd/holomush/core.go:856`, but the session ender, presence emitter, mover, and last-active writer are introduced only in Plans 03-04/05. Those later plans do not list `cmd/holomush/core.go`. Consequently, the new subsystem objects can be registered but cannot receive their production dependencies.

- **HIGH — the same wiring gap affects the activity writer.**  
  Plan 03-05 says the flusher receives an injected `LastActiveWriter`, but it modifies only `internal/world/setup`, not the composition root. Current production construction creates concrete subsystem values before registration at `cmd/holomush/core.go:408` and `cmd/holomush/core.go:506`. No later task installs the writer into the activity subsystem.

- **MEDIUM — constructor and dependency-provider design is underspecified.**  
  Because bootstrap’s start location is only available after preparation and panics beforehand at `internal/bootstrap/setup/subsystem.go:299`, the plan needs explicit lazy provider fields for bootstrap, world service, session store, and presence emitter. Merely declaring `DependsOn` does not provide those objects.

- **MEDIUM — exported mutable `consumer.Backoffs` creates process-wide test races.**  
  The existing mutable variable is package-local and tests swap it at `internal/eventbus/audit/projection_unit_test.go:183`. Exporting it makes mutation available to all packages and unsafe for parallel tests. Prefer an unexported default plus an option/injected retry schedule.

#### Plan 03-03

- **HIGH — the proposed sequential interleave cannot prove the intended CAS collision.**  
  The plan says both replicas read version N externally, then A completes, then B invokes `RetireCharacter(..., N)`. But the proposed service performs another internal `Get`, just as current mutations do at `internal/world/service.go:807`. B therefore sees the already-retired row and either uses the new version or fails the semantic status guard. The external reads do not control the version used inside the command.

- **HIGH — temporarily removing the production CAS predicate is an unsafe RED demonstration.**  
  This requires editing Plan 03-01-owned production code during a test-only plan and risks leaving a dirty or accidentally committed neutralization. A deterministic injected barrier/failpoint or repository-level test double is safer and repeatable.

- **MEDIUM — “gap-free” retire-only positions may be a false assertion.**  
  The invariant applies to the complete per-game feed, not an arbitrary subset. Genesis or setup envelopes may occupy positions between selected retirement rows. Query the full epoch/game feed and assert global contiguity, or assert that each retire envelope participates in a globally contiguous sequence. INV-WORLD-3 defines ordering over the complete feed at `docs/architecture/invariants.yaml:5051`.

#### Plan 03-04

- **HIGH — crash idempotency is incomplete.**  
  Deleting the session first is a destructive idempotency marker. If the process crashes after deletion but before `EmitLeave` or `EmitSessionEnded`, redelivery observes `(nil, nil)` and permanently skips both notifications. The repository confirms absence returns nil at `internal/store/session_store.go:817`. The plan therefore cannot guarantee the advertised “ends, notifies, and moves” contract under process failure.

- **HIGH — move failure disposition is unspecified.**  
  `MoveCharacter` can fail authorization, destination validation, or persistence at `internal/world/service.go:985`. The plan specifies NAK only for session deletion failure. If move failure is ACKed, the retired character remains in the old location permanently; if NAKed, redelivery will not reproduce the already-consumed leave/session-ended effects. This state machine needs an explicit retry/checkpoint design.

- **MEDIUM — “exactly one leave” and best-effort emission conflict.**  
  The task says presence failures are logged and processing continues. That permits zero leaves, while the plan’s must-have and integration acceptance speak in exact totals. Decide whether notification is required or best-effort and make the success criterion consistent.

- **MEDIUM — integration harness wiring is not covered.**  
  The existing integration harness builds much of its stack explicitly, including session store and world services around `internal/testsupport/integrationtest/harness.go:539`. Plan 03-04 modifies only the test file, not the harness. Confirm the harness actually uses the production orchestrator path containing the new subsystem; otherwise the proposed full-stack test cannot observe it.

#### Plan 03-05

- **HIGH — unconditional KV deletion loses newer activity.**  
  The flusher reads value/revision R, writes timestamp T to PostgreSQL, then deletes the key. If the listener writes T2 after the read but before deletion, an unconditional delete removes T2. The pinned NATS API supports revision-conditional deletion specifically for this race: `LastRevision` deletes only if the latest revision matches the observed revision at `jetstream/kv_options.go:96`. The plan must retain each entry’s revision and delete with `jetstream.LastRevision(revision)`.

- **HIGH — the injected writer cannot be satisfied by a bare free function without an adapter.**  
  Go functions do not implement method-bearing interfaces. The plan says `LastActiveWriter` is “satisfied by Task 1’s function,” but it must either be a named function type, an interface with a concrete adapter, or a closure field. Production wiring is also absent, as noted under 03-02.

- **MEDIUM — “one round trip” and partial per-ID failure are incompatible without a specified batch mechanism.**  
  A slice loop calling `Exec` is N round trips. A `pgx.Batch` reduces round trips but still needs careful result draining and error association. The task promises both batching and per-ID failures without specifying the actual mechanism.

- **MEDIUM — `Stop` final-flush ordering can race the listener.**  
  The plan says `Stop` performs a final flush and then drains the ticker goroutine, but the listener may still write KV during or after that flush. Stop must first stop and join all producers, then flush, then release substrate.

- **MEDIUM — invariant binding disposition should be settled in planning, not execution.**  
  INV-WORLD-4 is currently `bound` with a concrete enumerated `asserted_by` set at `docs/architecture/invariants.yaml:5057`. Leaving the executor to decide whether to add a binding or file an issue risks shipping a broadened bound invariant whose fourth-writer enumeration is not genuinely asserted.

### Suggestions

1. **Revise 03-01 Task 1 and Task 2**

   - Pass the caller’s `expectedVersion` unchanged to the repository CAS.
   - After reading the character, compare `char.Version` with `expectedVersion` before lifecycle-state guards; stale input must return `WORLD_CONCURRENT_EDIT`.
   - Keep `expectedVersion == 0` as the explicit repository-only unguarded path.
   - Split the writer methods or add an explicit option so only retire clears `default_character_id`.

2. **Revise 03-03 Task 1**

   - Base the test on the corrected caller-supplied version behavior.
   - A may commit first; B must still use N and receive `WORLD_CONCURRENT_EDIT` before the “already retired” check.
   - Replace temporary production-code neutralization with a deterministic repository-level RED test or an explicit test hook that cannot ship enabled.
   - Assert feed contiguity across the full `(game_id, epoch)` feed.

3. **Move final composition wiring out of 03-02**

   - Let 03-02 add IDs, zero-resource constructors, substrate acquisition, and placeholder dependency-provider fields.
   - Add `cmd/holomush/core.go` to 03-04 and 03-05, or create a separate wave-2 integration-wiring plan depending on both.
   - Explicitly wire the real world service, session store, presence emitter, bootstrap start-location provider, and last-active writer.
   - Update the integration harness if it does not boot the production composition root.

4. **Revise 03-04 Task 2**

   - Define a durable per-event progress state or otherwise make session deletion, notification, and move recoverable across crashes.
   - Specify ACK/NAK behavior for every effect:
     session teardown, leave, session-ended, and move.
   - If presence emission remains best-effort, remove “exactly one” from the phase truth and test only “at most one.” If notification is required, it needs durable retry state.

5. **Revise 03-05 Tasks 1–2**

   - Model activity entries as `(characterID, timestamp, revision)`.
   - Delete using `jetstream.LastRevision(revision)` and treat revision mismatch as “new activity arrived; retain/re-read.”
   - Stop listener and ticker loops first, join them, then perform the final flush.
   - Specify either a `pgx.Batch` implementation or drop the “one round trip” claim.
   - Use a named function type or concrete adapter for `LastActiveWriter`.
   - Add an integration test that writes a newer KV revision during a flush and proves it survives.

6. **Revise 03-05 Task 3**

   - Decide now which test genuinely binds the fourth-writer enumeration.
   - Add that test to `asserted_by` only if it checks the boundary/enumeration, not merely that the column changes.
   - Treat missing genuine coverage as a plan blocker rather than deferring the decision to execution.

### Risk Assessment

**Overall risk: HIGH.**

The plan structure and repository research are strong, and same-wave file ownership is mostly non-overlapping. However, the current design would either fail to compile/wire or silently violate its core guarantees:

- stale caller versions can be ignored;
- the resilience test cannot produce its expected error;
- production effect dependencies are never installed;
- reactor crashes can permanently lose notifications;
- KV flushing can delete newer activity.

These are correctness and durability defects on the phase’s principal requirements, not cosmetic planning issues.

---

## pi Review

I read the referenced code in the worktree (`internal/lifecycle/subsystem.go`, the census meta-test, `cmd/holomush` cascade sites, `internal/eventbus/audit/*`, `internal/store/session_store.go`, `internal/access/policy/seed.go` + `engine.go`, the pinned nats.go v1.52.0 module source, and the baseline migration) before reviewing. Nearly every load-bearing claim in the plans **verified true**.

### 1. Summary

This is an unusually well-grounded plan set. The research phase did real work: the 18-subsystem count, the 13-site cascade, the unexported `createConsumerWithRetry`, the `FileStorage = iota` zero value (verified at `nats.go@v1.52.0/jetstream/stream_config.go:611`), the census's dual assertions (verified at `test/meta/world_envelope_census_test.go:64-109`), and the `CharacterReader` compile fence (`internal/world/service.go:101`) all check out. The plan structure (spine → substrate → proof → effects → flush) matches the dependency graph, and PORTAL-10's RED-first discipline is threaded through every plan. The real residual risks are narrow: a same-wave cross-plan compile dependency, the system seed's actual breadth being wider than the plan's own prohibition states, and a subtle KV flush race. All are fixable with small edits.

### 2. Strengths

- **Census routing is correct, and the plan proves it.** `TestWorldEnvelopeCensusMatchesServiceMutatingMethods` does AST-detect the `s.mutator` selector, and `Service.characterRepo` is indeed typed `CharacterReader` (`internal/world/service.go:101`), so the Rename shape literally cannot compile from `Service`. Plan 03-01 Task 1's `UpdateCharacterPreferences` precedent is the right call, and the acceptance criterion grepping for `s.mutator` inside the `RetireCharacter` body targets exactly what the detector looks for.
- **The 13-site cascade is real and the plan refuses to trust its own arithmetic.** All `[18]stubSubsystem` sites verified (`core_subsystems_test.go:63,64,88`), both `!= 18` assertions (`:126,:292`), both `require.Len(t, graph, 18, …)` (`core_subsystems_test.go:425`, `core_topo_order_test.go:161`), and the pinned 18-element `want` (`core_topo_order_test.go:194`). 03-02 Task 3's instruction to *re-derive the sites with rg and read the failure diff rather than hand-place the new IDs* is exactly right given the `sort.Slice` tie-break.
- **Assumption A6 was resolved correctly.** `DeleteByCharacter` returns `(nil, nil)` on no row (`internal/store/session_store.go:813-827`), which makes the reactor's redelivery gate sound. Better: the unique partial index `idx_sessions_active_character ON sessions (character_id) WHERE status IN ('active','detached')` (`000001_baseline.sql:225-226`) guarantees at most one active/detached session per character, so the single-returned-`Info` leave fanout is lossless — a failure mode the plans don't even have to discuss.
- **The D-46 move preserves the error-code contract.** Both wrap functions verified in place (`projection.go:146-147`, `plugin_consumer.go:131-132`); the plan's "moved helper returns a bare error, no `oops` wrapping" rule is the correct mechanism, with grep-level acceptance criteria.
- **The KV storage correction is right.** `FileStorage StorageType = iota` confirmed; the plans correctly invert CONTEXT's hazard and add the `WithStorage` seam mirroring `eventbus.NewSubsystemWithStorage`.
- **ABAC mechanics verified.** `parseEntityType` splits on `:` (`engine.go:542-548`), so `"system:retirement"` matches `principal is system`; `seed:admin-full-access` is indeed a bare `action` permit (`seed.go:107`), so no admin seed is needed; `StartLocationID()` does panic pre-Prepare (`internal/bootstrap/setup/subsystem.go:303`), correctly driving `DependsOn(SubsystemBootstrap)`.
- **Honest requirement-mapping.** 03-05 explicitly flags that no REQ-ID covers `last_active_at` and names the roadmap criterion as the acceptance authority instead of laundering it under IDENT-04.

### 3. Concerns

- **[HIGH] The `seed:system-retirement-character` DSL contradicts the plan's own prohibition.** 03-01's prohibitions say the seeds "MUST NOT be widened into a general world-write grant for **any** system subject." But `permit(principal is system, action in ["read","write"], resource is character)` matches `parseEntityType`'s prefix rule (`engine.go:542-548`) for **every** `system:*` subject — including `"system:bootstrap"`, which today can write locations/exits but deliberately not characters. The seed as specified grants *all* current and future prefixed system subjects character read/write; it is scoped per-resource-type but not per-principal. This may be an acceptable deliberate widening (the bootstrap subject is fully trusted), but the plan asserts narrowness it does not deliver, and the acceptance criterion (`rg 'principal is system'` shows three permits, none with bare `action`) will pass while the widening is in place. Either add a `when { … }` principal-identity condition if the DSL supports one (the existing seeds only ever condition on `principal.character.*` attributes, so this may require verifying a `system` attribute namespace exists — `seed_coverage.go:43-60` will fail loudly at construction if it doesn't), or amend the prohibition text to state the actual grant.
- **[MEDIUM] Same-wave compile dependency: 03-02 (wave 1) needs the `character_retired` kind that 03-01 (wave 1) creates.** 03-02 Task 2's reactor "ignores any `Type` other than `character_retired`" — but that string/constant is minted by 03-01, which is in the **same wave** with `depends_on: []`. If 03-02 references `outbox.KindCharacterRetired` or the local mirror, it cannot compile until 03-01 merges; if it uses a string literal, it duplicates a wire constant with no compile-time tether. Either declare `depends_on: ["03-01"]` on 03-02 (and let 03-01 parallelize with nothing), or have 03-02 use the literal with a comment that 03-04 converts to the constant.
- **[MEDIUM] KV flush read-delete race can strand a newer timestamp.** 03-05 Task 2: flusher reads entry (rev N, value T1), listener writes T2>T1 (rev N+1), flusher writes T1 to Postgres, then deletes the key — T2 is lost until the character's next post-throttle event. The column is explicitly approximate (doc-comment lag sentence), so the impact is bounded staleness, not corruption — but a one-line mitigation exists: re-`Get` immediately before `Delete` and skip the delete if the revision advanced, or compare the deleted entry's revision to the flushed one. Worth adding since the plan already mandates duplicate-tolerance elsewhere.
- **[MEDIUM] `seed:player-self-retire` only covers the caller's *active* character.** The predicate `resource.character.id == principal.character.id` (`seed.go:41` pattern) means a player managing alts from a roster (IDENT-05's multi-alt surface) can only retire the character they are currently embodying — retiring a *different* alt they own is denied. That may be the intended v0.13 shape (no player-facing RPC exists yet; A1 is flagged for confirmation), but the plan presents the seed as discharging IDENT-04's "their own character" and it does not, in the multi-alt reading. The plan should state explicitly which reading it satisfies.
- **[LOW] The D-34 `players` clear runs on *unretire* too.** Putting `UPDATE players SET default_character_id = NULL …` inside `CharacterRepository.SetStatus` means every status write — including unretire and any future caller — executes it. It's a harmless no-op (`NULL` already), but it widens the auth-table-write exception beyond D-34's actual scope. Consider gating the clear on `status == StatusRetired` or splitting the repo method.
- **[LOW] The INV-WORLD-6 registry defect is never filed by any plan.** CONTEXT's deferred section says "File this" and 03-01's preamble says it "is filed separately," but no task in any plan runs `gh issue create`. It will be silently dropped.
- **[LOW] 03-04 Task 3's redelivery strategy is under-specified between two very different proofs.** "Either by driving the consumer's handler a second time with the same message or by letting an un-acked message redeliver" — the first tests handler idempotency, the second tests the JetStream ack path; the latter needs `AckWait` short enough not to blow the spec timeout. Pick one (recommend the explicit re-invocation plus a NAK-path unit test, which 03-04 Task 2 already has) and say so.

### 4. Suggestions

1. **03-01, Task 3:** Add a `when` clause scoping the system seed to the retirement principal if the DSL supports a principal-identity attribute; otherwise amend the prohibition text to "scoped to `resource is character`, `action in ["read","write"]`, matching any `system:*` principal — a deliberate widening recorded here" and add a test asserting `"system:bootstrap"` gaining character-write is *observed* (so it can't drift further silently). The `abac-reviewer` gate will flag this anyway; better to pre-empt it.
2. **03-02, frontmatter:** Add `depends_on: ["03-01"]` (or Task 2 uses the literal `"character_retired"` with a `// TODO(03-04): replace with outbox.KindCharacterRetired` comment). Given 03-02 has no other wave-1 peer dependency, serializing the two costs little.
3. **03-05, Task 2:** Add a revision-check before bucket `Delete` (re-`Get`, skip delete if `LastRevision` advanced), and a unit test for the interleaved-write case.
4. **03-01, Task 3 behavior list:** State explicitly that the self-retire seed authorizes only the caller's currently-active character, and that roster-level cross-alt retire is Phase 5/6 surface design, not this seed.
5. **03-01, Task 1:** Gate the `players.default_character_id` clear on the retiring direction, or document in the repo method's doc comment that the clear is unconditional-for-any-status and why that's safe.
6. **03-01 (or 03-05):** Add a task step filing the INV-WORLD-6 rename-half issue: `gh issue create -R holomush/holomush --label bug`.
7. **03-04, Task 3:** Choose the explicit re-invocation redelivery proof and delete the "or un-acked redelivery" alternative.

### 5. Risk Assessment

**Overall: MEDIUM-LOW.** The plans achieve all five roadmap success criteria with verified mechanisms, the wave structure is parallel-safe on files (03-01 ∩ 03-02 = ∅; 03-04 ∩ 03-05 = ∅ within wave 2), and the two plans that amend bound invariants (03-05 on INV-WORLD-4) carry the regenerate-and-don't-fabricate-binding discipline. The two findings that matter before execution are the same-wave kind dependency (a guaranteed compile break if 03-02 runs first and references the constant) and the system-seed breadth (a real, if probably acceptable, authorization widening that the plan currently mis-describes). Both are one-edit fixes. Nothing here contradicts a CONTEXT decision — the plans faithfully implement the D-33/D-36/D-42 corrections rather than the superseded originals.

---

## Consensus Summary

Both reviewers independently confirm the phase's **research is unusually well grounded** —
the 18-subsystem count, the 13-site `cmd/holomush` cascade, the census's dual `go/ast`
assertions, the `CharacterReader` compile fence, the `FileStorage = iota` correction, and
`DeleteByCharacter`'s `(nil, nil)` contract all verify true against the tree. Where they
diverge is on **whether the plans are executable as written**: Codex reads the same-wave
composition gap and the discarded `expectedVersion` as correctness defects on the phase's
principal requirements (HIGH), while pi reads the plan set as near-ready with two one-edit
fixes (MEDIUM-LOW). The source-grounding pass below sides with Codex and adds three
defects neither lane found.

### Agreed Strengths

- **Census routing is correct and the plan proves it, rather than asserting it.**
  `TestWorldEnvelopeCensusMatchesServiceMutatingMethods` genuinely AST-detects the
  `s.mutator` selector (`test/meta/world_envelope_census_test.go:159-177`), and
  `Service.characterRepo` is typed `CharacterReader` (`internal/world/service.go:101`),
  so the `Rename` shape literally cannot compile from `Service`. Both reviewers call the
  `UpdateCharacterPreferences` precedent the right choice.
- **The 13-site cascade is real and 03-02 refuses to trust its own arithmetic.** Both
  reviewers verified every `[18]stubSubsystem` site and both `require.Len(t, graph, 18…)`
  sites, and both single out the instruction to *re-derive* the topo positions from the
  failure diff (rather than hand-placing them) as the correct response to the
  `sort.Slice` tie-break.
- **The D-46 relocation preserves the error-code contract by mechanism, not by hope.**
  Both wrap functions stay in place and the moved helper returns a bare error; both
  reviewers verified `wrapConsumerCreateError` (`projection.go:146`) and
  `wrapPluginConsumerCreateError` (`plugin_consumer.go:131`).
- **The corrected D-42 storage hazard is right.** `FileStorage StorageType = iota`
  (`nats.go@v1.52.0/jetstream/stream_config.go:611`) — the plans correctly invert
  CONTEXT's original hazard and mirror the existing `NewSubsystemWithStorage` seam.
- **ABAC mechanics check out.** `parseEntityType` splits on `:` (`engine.go:542-548`), so
  `"system:retirement"` does match `principal is system`; `seed:admin-full-access` is a
  bare `action` permit (`seed.go:107`), so no admin seed is needed; `StartLocationID()`
  does panic pre-Prepare (`internal/bootstrap/setup/subsystem.go:299-303`).

### Agreed Concerns

1. **[HIGH] The KV flush deletes without revision protection, losing newer activity.**
   *(Codex HIGH, pi MEDIUM — both independent.)* 03-05 Task 2 reads an entry, writes to
   Postgres, then deletes the key. A listener `Put` landing between the read and the
   delete is destroyed. The pinned API has the exact fix: `jetstream.LastRevision(rev)`
   (`nats.go@v1.52.0/jetstream/kv_options.go:98` — "deletes if the latest revision matches
   the provided one"). The plan must carry each entry's revision through the flush and
   delete conditionally.
2. **[HIGH] Production wiring for the effect collaborators is unowned.** *(Codex HIGH ×2;
   the source-grounding pass confirms independently.)* 03-02 (wave 1) is the only plan
   that touches `cmd/holomush/core.go`, but the session ender, presence emitter,
   character mover (03-04) and last-active writer (03-05) do not exist yet at that point —
   and neither 03-04 nor 03-05 lists `core.go` in `files_modified`. The subsystems get
   registered but never receive their production dependencies.
3. **[MEDIUM] The `players.default_character_id` clear fires on unretire too.** *(Codex
   MEDIUM, pi LOW.)* 03-01 Task 1 puts the `UPDATE players` inside the generic
   `SetStatus`, and Task 2 reuses `SetStatus` for `StatusActive`. It is a harmless no-op
   today, but it widens D-34's auth-table-write exception past its stated scope with no
   gate. Either condition the clear on `status == StatusRetired` or say in the doc comment
   that it is unconditional and why that is safe.

### Divergent Views

- **Overall risk.** Codex: **HIGH** ("the current design would either fail to compile/wire
  or silently violate its core guarantees"). pi: **MEDIUM-LOW** ("two findings that
  matter... both are one-edit fixes"). The divergence traces to a single question —
  whether `RetireCharacter` plumbs the caller's `expectedVersion` into the CAS. Codex
  traced the plan's own instruction (`characterRepo.Get` "supplies the CAS version") and
  concluded it does not; pi did not examine that seam. The source-grounding pass below
  confirms Codex's reading is the one the plan text supports, which makes HIGH the
  better-evidenced verdict.
- **`seed:system-retirement-character` breadth.** pi raises a HIGH that Codex does not:
  `permit(principal is system, …, resource is character)` matches **every** `system:*`
  principal — including `"system:bootstrap"`, which today can write locations and exits
  but deliberately not characters. Verified: `parseEntityType` returns only the prefix
  (`internal/access/policy/engine.go:542-548`), and the two existing system seeds
  (`seed.go:206-217`) have the same shape. The grant is narrow **per resource type** but
  not **per principal**, while 03-01's prohibition claims it "MUST NOT be widened into a
  general world-write grant for any system subject." Either the DSL gains a
  principal-identity `when` clause or the prohibition text must be amended to describe the
  grant actually being made. This is exactly what the `abac-reviewer` gate will raise.
- **Same-wave `character_retired` dependency.** pi raises a MEDIUM Codex does not: 03-02
  Task 2's reactor switches on `character_retired`, a constant 03-01 mints — and both are
  wave 1 with `depends_on: []`. Either 03-02 declares `depends_on: ["03-01"]` or it uses
  the bare literal with a comment. (File sets do not overlap, so the waves are
  *file*-parallel-safe; this is a *symbol* dependency the wave graph does not model.)
- **The RED demonstration in 03-03.** Codex flags "temporarily neutralize the version
  predicate in production code" as an unsafe RED protocol from a test-only plan (risk of a
  committed neutralization); pi does not comment. Codex's alternative — a repository-level
  test double or a failpoint that cannot ship enabled — is the safer shape.

---

## Source-Grounding Pass

**Authority: `grep`.** `.planning/config.json` sets
`plan_review.source_grounding_authority: "grep"`. The `intel` adapter was *not* used:
`.planning/intel/API-SURFACE.md` states verbatim: *"**Incomplete:** api-map.json has no
entries (intel extraction is regex/JS-only or not yet populated). Treat absence here as
'unknown', not 'does not exist'."* On this Go repo the intel adapter therefore resolves
**every** Go symbol as UNCHECKABLE and would verify nothing. All verdicts below come from ripgrep + Read
against the working tree at `9badae86e`.

**Scope.** Every type, method, function, constant, file path, struct field, SQL identifier
and CLI/task invocation cited by the five PLAN.md files, **excluding** symbols listed under
each plan's *"Artifacts this phase produces"* section (this phase creates them).

Under a `grep` authority, **signatures cannot be asserted** — a signature verdict is
reported as UNCHECKABLE, never MISSING. MISSING is `needs-acknowledgement`, AMBIGUOUS is
MEDIUM, UNCHECKABLE is INFO.

### Result

**56 / 56 cited file paths VERIFIED.** No path in any plan is absent from the tree.

**0 MISSING symbols.** Every cited type, method, function, constant and field resolves.
This is a genuinely strong result for a plan set of this size — including an 8-for-8
exact-line match on the `EmitLeave` call-site survey in 03-04.

Selected high-load-bearing verdicts (full list is the plans' own `read_first` blocks; the
table records the ones a defect would hinge on):

| Symbol / claim | Verdict | Evidence |
|---|---|---|
| `Service.characterRepo` is a `CharacterReader`, not a `CharacterRepository` | VERIFIED | `internal/world/service.go:101` |
| `worldMutator.mutate(ctx, intent, write)` runs write + `WriteIntent` in one tx | VERIFIED | `internal/world/mutator.go:197-217` |
| census AST cross-check detects the `s.mutator` selector | VERIFIED | `test/meta/world_envelope_census_test.go:136` (the call) and `:162-178` (the detector) |
| census `t.Fatalf` on two commands sharing one kind | VERIFIED | `test/meta/world_envelope_census_test.go:87` (plan cites `:85`) |
| `characterUpdatePayload` is `{character_id, description}` | VERIFIED | `internal/world/outbox/taxonomy.go:156-159` |
| `AppSchemaVersion = 1` + its bump obligation | VERIFIED | `internal/world/outbox/taxonomy.go:12-19` |
| `classifyCASZeroRow`, `primaryDeltaVersioned` | VERIFIED | `internal/world/postgres/helpers.go:81, 138` |
| `Rename` MUST-NOT-route-through-mutate doc rule | VERIFIED | `internal/world/postgres/character_repo.go:201-206` |
| `players.default_character_id` FK is `ON DELETE SET NULL` | VERIFIED | `internal/store/migrations/000001_baseline.sql:64, 82-84` |
| `players` is NOT in `fencedWorldTables`; `characters` IS | VERIFIED | `test/meta/world_sql_fence_test.go:62-66` |
| `writerBoundaryDir = "internal/world/postgres"` | VERIFIED | `test/meta/world_sql_fence_test.go:134` |
| `internal/world/setup` on the composition allowlist | VERIFIED | `test/meta/world_import_graph_test.go:79` |
| 18 `SubsystemID`s; new ids go at END of block | VERIFIED | `internal/lifecycle/subsystem.go:17-49` |
| three `[18]stubSubsystem`, two `len(subs) != 18`, two `Len(t, graph, 18` | VERIFIED | `core_subsystems_test.go:63,64,88,126,292,425`; `core_topo_order_test.go:161` |
| pinned `want` has 18 elements | VERIFIED | `cmd/holomush/core_topo_order_test.go:195-213` (plan cites `:194-212`) |
| `createConsumerWithRetry`, `consumerCreateBackoffs` (`var`, not `const`) | VERIFIED | `internal/eventbus/audit/projection.go:167, 93` |
| both distinct wrap codes survive in place | VERIFIED | `projection.go:146`; `plugin_consumer.go:131` |
| `NewSubsystem` / `NewSubsystemWithStorage` seam | VERIFIED | `internal/eventbus/subsystem.go:63-70` |
| `FileStorage` is the zero value | VERIFIED | `nats.go@v1.52.0/jetstream/stream_config.go:609-613` |
| `Keys` is memory-heavy; `ListKeys` streams and may report duplicates | VERIFIED | `nats.go@v1.52.0/jetstream/kv.go:174-183` |
| `CreateOrUpdateKeyValue` / `CreateOrUpdateConsumer` exist | VERIFIED | `nats.go@v1.52.0/jetstream/kv.go:59`; `internal/eventbus/audit/plugin_consumer.go:195` |
| `DeleteByCharacter` returns `(nil, nil)` when absent (assumption A6) | VERIFIED | `internal/store/session_store.go:809-827` |
| six `SessionEndedCause*` constants + inline field comment | VERIFIED | `internal/core/session_ended_payload.go:19, 24-30` |
| `PresenceEmitter` 2-method consumer-defined interface | VERIFIED | `internal/auth/auth_service.go:26-29` |
| all eight surveyed `EmitLeave` call sites, exact lines | VERIFIED | `auth_service.go:248`; `lifecycle_handler.go:199,252`; `sub_grpc.go:844,870`; `command_handler.go:268,330`; `auth_handlers.go:716` |
| `StartLocationID()` panics before `Prepare` | VERIFIED | `internal/bootstrap/setup/subsystem.go:299-303` |
| `"system:bootstrap"` raw-literal precedent | VERIFIED | `internal/bootstrap/setting.go:134` |
| `MoveCharacter` checks `"write"`; post-commit hook is degradation | VERIFIED | `internal/world/service.go:990, 1035-1045` |
| `parseEntityType` splits on `:` | VERIFIED | `internal/access/policy/engine.go:542-548` |
| `EffectSystemBypass` requires `IsSystemContext` else `SYSTEM_SUBJECT_REJECTED` | VERIFIED | `internal/access/policy/engine.go:92-102` |
| `seed:admin-full-access` carries a bare `action` | VERIFIED | `internal/access/policy/seed.go:105-109` |
| exactly two `principal is system` permits today | VERIFIED | `internal/access/policy/seed.go:209, 215` |
| `BackfillCharacterIdentity` + `BackfillExecutor` precedent | VERIFIED | `internal/world/postgres/identity_backfill.go:76, 99` |
| `last_active_at BIGINT NOT NULL DEFAULT 0` in 000054 | VERIFIED | `internal/store/migrations/000054_…sql:32-33` |
| `world.NeverActive int64 = 0` | VERIFIED | `internal/world/lifecycle.go:40` (plan cites `:39`) |
| `TestWorldModelResilience` + `quarantinetest.Enabled()` gate | VERIFIED | `test/integration/resilience/resilience_suite_test.go:49-55` |
| `m12SpecTimeout = 3 * time.Minute` | VERIFIED | `test/integration/resilience/m12_lastwritewins_test.go:24` |
| `markerLineRE` quarantine-marker regex | VERIFIED | `test/meta/quarantine_registry_test.go:31` |
| `task test:int` forwards `{{.CLI_ARGS}}` (03-04's claim to confirm) | VERIFIED | `Taskfile.yaml:289-290` — the plan is right and the older repo note is stale |
| INV-WORLD-4 entry, its "count was TWO until 02-12" sentence | VERIFIED | `docs/architecture/invariants.yaml:5057-5083` |

### Defects found by the source-grounding pass (not raised by either lane)

**SG-1 [HIGH] — three `-run` acceptance criteria are false greens.**
`test/integration/world/` contains exactly **one** Go test function and one `RunSpecs`:
`TestWorld` (`test/integration/world/world_suite_test.go:62-66`). Every spec in that
package is a Ginkgo `Describe` inside it. Therefore:

- 03-01 Task 2: `task test:int -- -run TestCharacterLifecycle ./test/integration/world/`
- 03-04 Task 3: `task test:int -- -run TestRetirementReactor ./test/integration/world/`
- 03-05 Task 3: `task test:int -- -run TestCharacterActivityFlush ./test/integration/world/`

…each match **no test function**. Because `test:int` genuinely forwards `CLI_ARGS`
(`Taskfile.yaml:289-290`), `go test -run` filters everything out and the package exits **0
having run nothing**. All three are stated as *acceptance criteria*, so all three pass
vacuously. Worse, the obvious "fix" — adding `func TestRetirementReactor(t *testing.T)`
with its own `RunSpecs` — puts a second `RunSpecs` in one package, which is the exact
hazard 03-03 correctly forbids for the resilience package (`03-03` acceptance:
`rg -c 'RunSpecs' … returns 1`). The three plans need either `-ginkgo.focus=` /
`task test:int:focus`, or a package-level run with no `-run` filter.

**SG-2 [HIGH] — `integrationtest.Start(t)` cannot carry 03-04 Task 3's relay flow.**
03-04 Task 3 mandates "let the outbox relay carry the envelope — do not publish a synthetic
`character_retired` event to shortcut the relay." But the harness states verbatim that it
does not run the relay:

> `// … The relay itself is a` / `// separate subsystem not started by this harness.`
> — `internal/testsupport/integrationtest/plugins.go:265-266`

The harness hand-assembles its stack (`harness.go:346+`) rather than booting
`cmd/holomush`'s `productionSubsystems`, so neither `SubsystemCharacterRetirement` nor
`SubsystemCharacterActivity` runs there, and no plan adds a `StartOption` to enable them.
Two knock-ons: 03-05 Task 3's requirement to "force the bucket to `jetstream.MemoryStorage`
through the 03-02 `WithStorage` seam" has no seam to reach through from the harness, and
`test/integration/world/` does not use `integrationtest` at all today (it builds its own
`testEnv`, `world_suite_test.go:68+`), so both new specs would introduce a second,
competing stack into one Ginkgo suite. Either a harness `StartOption` lands (owned by a
task, in `files_modified`) or the specs move to a suite that does boot the relay — the
resilience suite already does (`test/integration/resilience/chaos_helpers_test.go`).

**SG-3 [MEDIUM] — 03-05 Task 3's invariants.yaml line range points at the wrong
invariant's `binding`/`asserted_by`.** The read_first is unusually emphatic: *"`:5057-5115`
— INV-WORLD-4 in full. Read to the END of the entry: the range MUST include `binding:`
(~:5105) and every `asserted_by:` entry (~:5106+)."* Actual boundaries:

| Entry | Lines |
|---|---|
| INV-WORLD-4 | 5057–5083 (`binding:` at **5078**, `asserted_by:` at **5079-5081**) |
| INV-WORLD-5 | 5084–5094 |
| INV-WORLD-6 | 5095–5107 (`binding:` at **5104**, `asserted_by:` at **5105-5107**) |
| INV-WORLD-7 | 5108– |

The cited `~:5105` / `~:5106+` are **INV-WORLD-6's** binding fields, and the range spills 8
lines into INV-WORLD-7. An executor following the instruction literally reasons about
INV-WORLD-6's binding (`test/integration/world/character_lifecycle_test.go`) while amending
INV-WORLD-4 — and INV-WORLD-6 is precisely the entry 03-01 Task 2 forbids disturbing. Fix
the range to `5057-5083` and the field pointers to `:5078` / `:5079-5081`.

**SG-4 [MEDIUM] — 03-02 Task 1's own acceptance criterion forces work its action never
instructs.** The criterion is `rg -c 'createConsumerWithRetry|consumerCreateBackoffs'
internal/eventbus/audit/` returns **0**. There are **24** such occurrences today, and five
live tests call the unexported helper directly — `TestCreateConsumerWithRetry*` at
`internal/eventbus/audit/projection_unit_test.go:197, 210, 226, 240, 257`, plus the
attempt-count assertion at `:231` and a doc reference at
`subsystem_prepare_retry_integration_test.go:36`. The action only says to update
`withShortBackoffs`; it never says to move or delete those tests, which would be compile
errors after the helper leaves the package. Add an explicit "migrate the five
`TestCreateConsumerWithRetry*` tests into `internal/eventbus/consumer`" step (they are
exactly the coverage the task's "add a small unit test in the new package" bullet asks for
— it is a move, not new writing).

**SG-5 [LOW] — a stale in-tree doc comment names a command D-44 deleted.**
`internal/world/postgres/character_repo.go:204-205` reads *"Phase 3's `RenameCharacter` calls
this method directly and supplies the intent."* D-44 removed `RenameCharacter` from Phase 3
and from v0.13, and 03-01 explicitly freezes `CharacterRepository.Rename` "exactly as it
is" — so the false forward-reference survives the phase. 03-01 Task 1 sends the executor to
read this very comment (`read_first`: `character_repo.go:191-205`) to confirm a
Rename-specific rule, which makes it a live trap. One-line fix; add it to 03-01 Task 1 or
file it.

### Corroboration of the two lanes' top findings

The pass independently confirms the `expectedVersion` defect Codex raised and pi did not,
and it is the single largest risk in the set:

- Every existing `*Service` character mutation reads its own CAS version:
  `UpdateCharacterPreferences` passes `char.Version` (`internal/world/service.go:878`),
  `MoveCharacter` does the same, and `UpdateLocation` carries the expected version *inside
  the `*Location` struct* (`service.go:311-333`) — which is exactly why the m12 spec works
  (`m12_lastwritewins_test.go:142-155`: two structs holding the same read version).
  03-01's `RetireCharacter(…, expectedVersion int)` is a **new parameter shape with no
  precedent in the file**, and the action text says `s.characterRepo.Get` "supplies the CAS
  version" — under which the caller's `expectedVersion` is dead and 03-01's own stale-version
  and `expectedVersion == 0` behaviors are unimplementable.
- **Even if it is plumbed**, 03-03's interleave still cannot produce `WORLD_CONCURRENT_EDIT`:
  the sequence is explicitly *sequential* ("drive replica A's retire to completion, then
  drive replica B's retire"), so by the time B runs, `Get` returns `status='retired'` and
  03-01's pre-write guard returns `CHARACTER_ALREADY_RETIRED` before the CAS is ever
  reached. 03-03's assertion (`errutil.AssertErrorCode` against `WORLD_CONCURRENT_EDIT`) and
  ROADMAP success criterion 3 both fail. The ordering of the version check and the
  lifecycle-state guard must be pinned in 03-01, and 03-03's spec must target it.
- INV-WORLD-7 (`docs/architecture/invariants.yaml:5108-5116`) is the standing guarantee at
  stake: *"rejects a stale value with the typed WORLD_CONCURRENT_EDIT signal so exactly ONE
  of two concurrent mutations sharing a version succeeds."*

pi's `principal is system` HIGH is likewise confirmed mechanically: `parseEntityType`
returns only the prefix (`engine.go:542-548`), so a `resource is character` system permit is
reachable by `"system:bootstrap"` as well as `"system:retirement"`.

### Verification coverage

Nothing was silently skipped. Everything not VERIFIED is listed here.

| Item | Verdict | Why |
|---|---|---|
| All symbols under each plan's *"Artifacts this phase produces"* tables (~40 across the 5 plans: `KindCharacterRetired`, `SetStatus`, `consumer.CreateWithRetry`, `SubsystemCharacterRetirement`, `retirementReactor`, `UpdateCharacterLastActive`, `activityFlusher`, `SessionEndedCauseRetired`, the new test files, …) | **EXCLUDED by rule** | This phase creates them; absence is expected, not a defect. |
| Every function/method **signature** cited or proposed (e.g. `SetStatus(ctx, id, status, expectedVersion) (*wmodel.MutationDelta, error)`, `CreateWithRetry`, `UpdateCharacterLastActive`) | **UNCHECKABLE** (INFO) | A `grep` authority resolves existence, not shape. Reported as UNCHECKABLE per the severity rule — never as MISSING. The `expectedVersion` finding above is argued from **plan prose and existing call-site behavior**, not from a signature assertion. |
| `TestWorldSQLFence` (cited as a test name in 03-05 Task 1's acceptance criteria) | **AMBIGUOUS** (MEDIUM→downgraded) | No function of that exact name; 8 candidates share the prefix (`test/meta/world_sql_fence_test.go:256, 276, 293, 314, 323, 337, 349, 372`). Harmless in context — the criterion is "the package run exits 0", not `-run TestWorldSQLFence` — but the name as written matches nothing. |
| `SeedPolicy` struct shape cited at `internal/access/policy/seed.go:39-43` | **VERIFIED with drift** (INFO) | The struct is declared at `:7-12`; `:39-43` is the first seed *literal*. Field set `{Name, Description, DSLText, SeedVersion}` is correct either way. |
| Minor ±1–3 line drift on ~8 citations (`lifecycle.go:39` vs `:40`; `census_test.go:85` vs `:87`; `subsystem.go:47-48` vs `:45-46`; `want` `:194-212` vs `:195-213`; `resilience_suite_test.go:48-50` vs `:50-52`; `session_ended_payload.go:20` vs `:19`; `character_repo.go:225-256` ends one line short of `primaryDeltaVersioned` at `:257`; `Selectable` at `:83` sits just past the cited `39-80`) | **VERIFIED with drift** (INFO) | Every one lands on or adjacent to the claimed construct. Normal post-merge drift; no plan reasons incorrectly from any of them. |
| GitHub issue references `#4788`, `#4791`, `#4902`, `holomush-l015`, `holomush-ghg1` | **UNCHECKABLE** (INFO) | Out-of-tree identifiers; not resolvable under a `grep` authority. Not load-bearing for any acceptance criterion. |
| `.planning/phases/03-world-character-commands/03-0N-SUMMARY.md` (`@`-context refs in 03-03/04/05) | **EXCLUDED by rule** | Produced by the upstream plans during execution. |
| `03-RESEARCH.md` §-number cross-references (§2.3, §3.2, §4.2, §6.2) | **VERIFIED by spot-check** | The two load-bearing ones were checked against the pinned source: §4.2's `Keys`-vs-`ListKeys` claim and the `FileStorage = iota` claim both match `nats.go@v1.52.0` verbatim. |

**Coverage statement:** 56 file paths and ~95 in-tree symbols resolved; **0 MISSING**;
1 AMBIGUOUS; signatures + 5 out-of-tree issue ids UNCHECKABLE. A clean MISSING column here
reflects genuine grounding, not an unexecuted check — the defects above are contradictions
*between* verified facts, which is the class a symbol-existence check cannot reach.
