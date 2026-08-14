---
phase: 06-admin-portal-shell-character-administration
plan: 05
subsystem: api
tags: [grpc, abac, admin-portal, world, outbox, audit, privacy, protobuf]

requires:
  - phase: 06-admin-portal-shell-character-administration
    plan: 04
    provides: "AdminGetCharacterRow, AdminCharacterReader/AdminProfileReader and the AdminPortalServerOption seam this plan extends with a writer"
  - phase: 06-admin-portal-shell-character-administration
    plan: 02
    provides: "mapAdminSectionError, the interceptor's fixed-SectionID arm, and the opaque static refusal message the world-layer denial reuses"
provides:
  - "AdminUpdateCharacter / AdminRetireCharacter / AdminUnretireCharacter on holomush.adminportal.v1.AdminPortalService, each gated `characters` + ActionWrite"
  - "adminProfileMaskablePaths — the 13-path §10.6 allowlist (description PLUS twelve profile.*), accessor-on-the-entry, exact-string only"
  - "seed:admin-character-administration — the world-layer gate that lets the D-104 player-flavoured admin caller pass world.Service.checkAccess; four actions, no delete"
  - "world.AuditContext + LifecycleOption/ProfileUpdateOption + WithAuditContext/WithDescription/WithSkipUnchangedProperties"
  - "CharacterLifecycleChangePayload.BeforeStatus/.Section/.Action and CharacterProfileUpdateChangePayload.Section/.Action; taxonomy AppSchemaVersion 4, three kinds at SchemaVersion 2"
  - "grpc.AdminCharacterWriter + WithAdminCharacterWriter, wired at BOTH composition roots"
  - "TestAdminCharacterMessagesCarryNoRoleBearingField — §10.6's designated schema-level descriptor fence"
  - "TestOnlyTheAuditProjectionInsertsIntoEventsAudit — the events_audit single-writer fence"
  - "INV-WORLD-9 and INV-PRIVACY-13, both bound"
affects: [06.1-03, 06.1-04]

actuals:
  tokens: 56000
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Option-as-interface: one WithAuditContext constructor satisfying two distinct option families, because two `func(*T)` types cannot share a name"
    - "Narrowness preserved by assertion rather than by signature: a variadic tail the facade never uses, pinned by require.Empty in its own test double"
    - "Suppression inside the authoritative transaction, not in a handler precheck — a pre-transaction comparison reads rows it holds no lock on"
    - "A fence whose allowlist entries are asserted to be inside the inspected set, so an entry cannot quietly become dead configuration"

key-files:
  created:
    - internal/world/options.go
    - internal/world/service_profile_options_test.go
    - internal/grpc/admin_characters_write.go
    - internal/grpc/admin_characters_write_test.go
    - test/meta/admin_character_message_role_fence_test.go
    - test/integration/access/admin_characters_write_test.go
  modified:
    - api/proto/holomush/adminportal/v1/adminportal.proto
    - api/proto/holomush/web/v1/web.proto
    - internal/access/policy/seed.go
    - internal/access/policy/seed_test.go
    - internal/access/policy/seed_profile_visibility_test.go
    - internal/admin/section/descriptor.go
    - internal/admin/section/registry.go
    - internal/grpc/admin_service.go
    - internal/grpc/characteraccess_service.go
    - internal/grpc/characteraccess_write_test.go
    - internal/web/admin_handlers.go
    - internal/web/handler.go
    - internal/grpcclient/client.go
    - cmd/holomush/deps.go
    - cmd/holomush/deps_test.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/harness.go
    - internal/world/payloads.go
    - internal/world/service.go
    - internal/world/outbox/taxonomy.go
    - test/meta/world_sql_fence_test.go
    - docs/architecture/invariants.yaml

key-decisions:
  - "06-05: the section REGISTRY entry — not only the method descriptor — must declare ActionWrite. Every registry row was built ActionRead, and assertSectionAccess step 3 refuses a request whose rank exceeds the section's declared maximum, so all three write RPCs were refused ADMIN_SECTION_ACTION_NOT_DECLARED regardless of policy. `characters` alone now declares write."
  - "06-05: the empty-mask no-op compares expected_version in the handler, because that branch reaches NO domain command and the authoritative CAS therefore never runs. This is the one clause where the admin path deliberately diverges from the player facade."
  - "06-05: the description write needs no new repository primitive — characterWriter.Update already writes characters.description from char.Description, so char.SetDescription before the existing mutator call gives one transaction, one CAS and one version bump for free."
  - "06-05: INV-WORLD-9 claims the transactional property and the single-writer property ONLY. It does not claim world envelopes are projected into events_audit, because they are not: the relay publishes through a bare JetStreamPublisher and audit.writeAuditRow requires the App-Rendering header that only RenderingPublisher writes."
  - "06-05: the events_audit allowlist is TWO files, not one. retention_partitions.go carries a real Go INSERT the plan's evidence command missed because it is `public.`-qualified."

requirements-completed: [ADMIN-04, ADMIN-05, ADMIN-06]

status: complete
---

# Phase 06 Plan 05: Admin Character Writes Summary

Three gated write RPCs behind a 13-path exact-string allowlist, a world-layer seed policy that closes a default-deny the whole surface sat behind, and an audit payload that records which fields changed without recording what they say.

## Performance

- **Duration:** ~3h (across a session-limit restart between Tasks 2 and 3)
- **Tasks:** 3 completed
- **Commits:** 3

## Task Commits

1. **Task 1: the eight-step payload widening** — `61b4fb246` (feat)
2. **Task 2: three write RPCs, the allowlist, the seed policy** — `a15aab715` (feat)
3. **Task 3: the transactional proofs and the audit-writer fence** — `d5c907955` (test)

## Accomplishments

- **The write path was default-denied one layer below the interceptor, and now is not.** `seed:admin-character-administration` is the only policy in the corpus that lets a player-principal reach a `character:` resource. Without it every admin write passed the section gate and was refused inside `world.Service.checkAccess`.
- **One request is one domain write, even for a mixed mask.** `description` travels as a `WithDescription` option into the existing `UpdateCharacterProfileAttributes` — one `checkAccess`, one transaction, one version bump, one names-only envelope. The integration proof asserts all three at once, so a two-call implementation fails on the version increment, the envelope count and the prose-absence assertion simultaneously.
- **`description` never routes through `UpdateCharacterDescription`,** whose `kindCharacterUpdated` payload declares a `description` STRING. Routing it there would write player prose into the retained `events_audit`.
- **A non-empty mask of only unchanged values is a true no-op** — enforced inside the transaction by an admin-only option, because the domain rewrites an equal-valued row unconditionally and a handler precheck could not have suppressed anything. The player path's shipped contract is proven byte-identical by its own control.
- **§10.6's two designated durable verifications both ship**: the 13-path set equality (both directions) and the proto-DESCRIPTOR role fence, which sees a field a source grep structurally cannot.

## Demonstrations Performed and Recorded

Each was planted, observed, and reverted; the working tree was verified clean afterwards.

| Planted mutation | Assertion | Observed |
|---|---|---|
| `AppSchemaVersion` 4 → 3 | taxonomy | FAIL — `expected: 4` |
| `KindCharacterRetired` SchemaVersion 2 → 1 | taxonomy | FAIL |
| `KindCharacterUnretired` SchemaVersion 2 → 1 | taxonomy | FAIL |
| `KindCharacterProfileUpdate` SchemaVersion 2 → 1 | taxonomy | FAIL |
| Add `Values map[string]string` to the profile payload | key-set half | FAIL — "should have 4 item(s), but has 5" |
| Same field tagged `json:"-"` | reflected-tag half | FAIL — "elements differ" (proves BOTH halves are live) |
| Delete `description` from the allowlist | MISSING direction | FAIL — "Should be empty, but was [description]" |
| Add `profile.nickname` to the allowlist | EXTRA direction | FAIL — "Should be empty, but was [profile.nickname]" |
| `repeated string roles = 90;` on `AdminCharacter` + `task proto` | descriptor fence | FAIL — `"roles" to NOT match "(?i)(role\|grant\|permission\|capability)"` |
| Drop `&& !descriptionChanged` from the empty-partition return | description-only mask | FAIL — the character writer is never reached |
| Make the skip unconditional | player-path control | FAIL — the row is no longer rewritten |
| Facade passes an option | `recordingWorldMutator` | FAIL ×4 — "character-access callers MUST pass no ProfileUpdateOption" |
| Add `"delete"` to the seed policy | two seed tests | FAIL — "should have 4 item(s), but has 5" and the shape map |
| Remove the seed policy entirely | pin test | FAIL |
| `INSERT INTO events_audit` in a scratch **production** file | audit fence | FAIL — names the file |
| The same string in a scratch **`_test.go`** file | audit fence | PASS — the exclusion is scoped, not a hole |
| Move the poison outbox row off the claimed position | rollback test | FAIL — "An error is expected but got nil" |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The section REGISTRY declared read-only, so no write RPC was reachable at all**

- **Found during:** Task 3, first integration run — all eight specs failed with `PermissionDenied: admin section access denied`.
- **Issue:** `internal/admin/section/registry.go`'s `entry()` built EVERY row with `Descriptor{Action: ActionRead}`. `assertSectionAccess` step 3 refuses a request whose action rank exceeds the section's declared maximum, so `ActionWrite` on `characters` returned `ADMIN_SECTION_ACTION_NOT_DECLARED` — collapsed by `mapAdminSectionError` onto the same opaque refusal. The method descriptor and the registry descriptor are INDEPENDENT gates and both must admit write. The plan listed `descriptor.go` in `files_modified` but not `registry.go`.
- **Fix:** a `writeEntry` constructor used by `characters` alone; the six planned sections keep `ActionRead` via a simplified `entry`. Two shipped assertions correctly went red and were updated rather than deleted — `TestTheCharactersSectionCarriesSpec101sReadDescriptor` became `...DeclaresWriteAndEveryOtherSectionDeclaresRead` (both halves, so changing `entry` itself cannot hide it), and the action-ladder test's "undeclared action" case moved onto `stats`, which still declares read. Its ordering dependency is now explicit: step 3 runs before step 4, so a *planned* section still yields `ADMIN_SECTION_ACTION_NOT_DECLARED` rather than `SECTION_NOT_IMPLEMENTED`.
- **Files modified:** `internal/admin/section/registry.go`, `registry_test.go`, `gate_test.go`
- **Committed in:** `d5c907955`

**2. [Rule 1 - Bug] The empty-mask no-op skipped the version guard**

- **Found during:** Task 3 — the stale empty-mask case returned success where the plan's Test 3b requires `Aborted`.
- **Issue:** `requireAdminGuardedVersion` only checks `> 0`. The staleness check lives in the domain CAS, and the empty-mask branch reaches no domain command at all.
- **Fix:** `adminWritePrecondition` now returns the version of the row it already read, and the empty-mask branch compares against it. Scoped to that branch deliberately: adding a handler-side comparison ahead of every CAS would be a second, weaker check on paths that already have an authoritative one.
- **Files modified:** `internal/grpc/admin_characters_write.go`
- **Committed in:** `d5c907955`

**3. [Rule 3 - Blocking] A FOURTH seed-census site the plan's sweep could not surface**

- **Found during:** Task 2, after adding the policy.
- **Issue:** the plan scheduled three sites and mandated a pre-edit sweep for a fourth. The sweep pattern (`SeedPolicies|expectedNames|Len\(t, seeds|permitCount|forbidCount`) returned `seed_profile_visibility_test.go` lines but only as generic `SeedPolicies()` iterations. The real fourth site is `TestNoPhase2SeedIntroducesACharacterResourceTypePermit` — D-29's mechanical gate, which pins the exact NAME SET and per-entry TARGET SHAPE of every `resource is character` permit. A correct implementation turns it red.
- **Fix:** added the entry to both the name list and the shape map, with its own written argument in the file's established convention — the principal is `player` (not `character`, so D-29's "every ephemeral guest" concern does not reach it), it is the world-layer half of a gate the caller has already passed, and `delete` is absent. **Also recorded:** `bootstrap_test.go:368` asserts `forbidCount == 10` independently; the new policy is a permit, so it stays 10 and needed no edit — but it is a fifth enumeration site and is named here so a future permit-adding plan does not rediscover it.
- **Files modified:** `internal/access/policy/seed_profile_visibility_test.go`
- **Committed in:** `a15aab715`

**4. [Rule 3 - Blocking] `WithAuditContext` cannot be one function over two option types**

- **Found during:** Task 1.
- **Issue:** the plan requires `WithAuditContext` on BOTH `LifecycleOption` and `ProfileUpdateOption`. Two distinct `func(*T)` option types cannot share a constructor name in Go.
- **Fix:** each option family is a one-method interface, and `WithAuditContext` returns an `AuditContextOption` that satisfies both. This is the grpc-go functional-option shape, not an invention.
- **Files modified:** `internal/world/options.go`
- **Committed in:** `61b4fb246`

**5. [Rule 3 - Blocking] An integration caller of `BuildCharacterLifecyclePayload` outside `files_modified`**

- **Found during:** Task 1. `test/integration/world/character_retire_atomicity_test.go:56` calls the widened builder and would not compile.
- **Fix:** passes `world.StatusActive` as the before-status. It is also this plan's rollback precedent, so it was read in full first.
- **Committed in:** `61b4fb246`

**6. [Rule 1 - Bug] The rollback fixture read a column and a row that do not exist**

- **Found during:** Task 3. `world_feed_counter` has `next_position`, not `last_position`; and the row is upserted lazily, so on a fresh database there is none until an envelope has been written.
- **Fix:** read `next_position`, and prime the counter with one real committed write first. Then proven falsifiable by moving the poison position — without that check the test could have passed for the wrong reason.
- **Committed in:** `d5c907955`

## Criterion Defects Found (reported, NOT repaired)

**1. Task 3's `events_audit` allowlist is one file short, and the plan's own evidence command cannot see the fourth writer.**

The plan states the allowlist is `projection.go` alone, and C2-29 explicitly REJECTED enumerating the retention-partition mover on the grounds that *"the mover is migration SQL and partition DDL, not a Go INSERT. That correction stands."* That is false. `internal/eventbus/audit/retention_partitions.go:546` carries a real Go `INSERT INTO public.events_audit`.

C3-29 re-verified the evidence and still missed it, because the command it prescribes is unqualified:

```
rg -n 'INSERT INTO events_audit' --glob '*.go' -g '!*_test.go'    →  3 hits (as the plan says)
rg -ni 'INSERT\s+INTO\s+(public\.)?events_audit\b' ... same globs →  4 hits
```

**Not repaired by weakening the fence.** The backfill is allowlisted with its argument written down — it copies rows the projection already wrote, verbatim including `codec`/`dek_ref`/`dek_version`, under `ON CONFLICT DO NOTHING` on the same ULID-derived key — and both entries are asserted to be INSIDE the inspected set, so neither can quietly become dead configuration. The `(?:public\.)?` alternation is in the fence's regexp, which is why it found what the plan's evidence did not.

**2. ROADMAP criterion 3's "the row is PROJECTED from that envelope" does not hold today, for a reason that has nothing to do with this plan.**

The transactional half is real and proven. The projection half is not reachable:

- `outbox.EnvelopeToEvent` builds the event with no `Rendering` — asserted by the new `TestARelayedWorldEnvelopeCarriesNoRenderingMetadata`.
- the relay publishes through `EventBus.Publisher()`, a bare `JetStreamPublisher`. Only `eventbus.RenderingPublisher` writes `App-Rendering`, and it cannot be in the relay's path: its `Lookup` resolves the wire type against plugin `verbs[].type` and hard-fails `EMIT_UNKNOWN_VERB` on a world kind like `character_retired`.
- `audit.writeAuditRow` returns `AUDIT_MISSING_HEADER` without `App-Rendering`.

So a world-outbox envelope reaching the host projection is rejected, not persisted. **Reported rather than papered over:** `INV-WORLD-9` is worded to claim only the transactional property and the single-writer property, both of which are proven, and it says in its own summary that it does not claim the projection clause. Task 3's Test 3 (`Eventually` on the audit row) and Test 4 (replay dedup) were therefore NOT written as passing assertions of a property that does not hold. **Test 4's property is already pinned in-tree at the right layer** by `TestWriteAuditRowDedupsSameEventAcrossStoreTimes` (`internal/eventbus/audit/projection_idempotency_integration_test.go`), which drives the ULID-derived composite PK directly — so authoring a second one would have duplicated shipped coverage, which the repo's own review learnings warn against.

**This wants a decision, not a fix here:** either the world relay gains rendering metadata (and world envelopes start landing in `events_audit`), or §14 row 9's model for world mutations is restated. Both are larger than this plan.

**3. `rg -n 'DeleteCharacter' internal/grpc/ internal/web/` cannot return "no match".**

The plan applies a `rg -v '^\s*//'` comment filter to the `Roles` and `UpdateCharacterDescription` criteria for exactly this reason, and then omits it here — while simultaneously requiring doc blocks that explain why no admin delete exists. Four matches, all in mandated comments. Comment-filtered, the property holds:

```
rg -v '^\s*//' internal/grpc/admin_characters_write.go internal/grpc/admin_service.go \
  internal/web/admin_handlers.go | rg -q 'DeleteCharacter'   →  exits 1 (absent from code)
```

Nothing was deleted from any comment to make it pass.

**4. `seed:admin-character-administration`'s `read` action is wider than anything this plan traverses — blast radius stated.**

The plan justifies the four actions as *"exhaustive over what the admin RPCs traverse"*. Three are: `write` (`UpdateCharacterProfileAttributes`), `retire`, `unretire`. **`read` is not traversed by any admin RPC in this plan** — the admin reads go through `AdminGetCharacterRow`, a raw repository projection that evaluates no world-layer policy (06-04's deliberate design).

What `read` grants that nothing here uses: an admin *player* subject can now pass `world.Service.GetCharacter`, whose `characterToProto` projection returns `PlayerId` and `LocationId` for any character — live grid position for the whole roster. That is the same surface D-29 defers, granted to admins rather than to guests.

**Kept as the plan wrote it, deliberately.** The DSL text was written into the plan expressly so `abac-reviewer` could gate it on paper; narrowing a reviewed security policy mid-execution defeats that, and four separate assertions (the exact DSL, the four-member action list, the D-29 shape map, the delete-denial) pin it. **The orchestrator's `abac-reviewer` pass should adjudicate whether `read` stays.** Dropping it is a three-character edit plus four assertion updates.

## Threat Flags

None. No new network endpoint, auth path, file access pattern or schema change beyond the three declared RPCs, all of which sit behind the existing interceptor plus the newly-closed world-layer gate.

## Outstanding: the ABAC domain gate did NOT run here

`/holomush-dev:review-abac` could not be invoked — the `Task` tool is disabled in this executor session. Per the orchestrator's instruction it will run `abac-reviewer` over the complete phase diff. Recorded in `.planning/WINDOWS.md` as an `unrun-verify`.

**What it should look at, in priority order:**

1. `seed:admin-character-administration`'s `read` arm — see criterion defect 4.
2. The section registry widening: `characters` now declares `ActionWrite` as its maximum. The six planned sections are unchanged.
3. The world-layer denial mapping reusing `adminDeniedMessage`, so the two authorization layers are indistinguishable on the wire.
4. `AdminCharacterWriter`'s deliberate absences — no `DeleteCharacter`, no `UpdateCharacterDescription`.

## Also outstanding: `test/integration/charname` is RED at HEAD, and it is not this plan's

8 of 24 specs fail on a fixture precondition broken by **plan 06-04's migration 000057**. `git diff --name-only 61b4fb246~1 -- internal/store/migrations/ test/integration/charname/ internal/charname/` is empty — this plan touches none of it. Root cause, evidence and a suggested repair are in `deferred-items.md`. This is the SECOND breakage from that migration; the first was `internal/store`'s census, fixed post-merge in `b6de718d`.

## Verification

| Check | Result |
|---|---|
| `task test` (FULL suite, not package-scoped) | 11840 tests, 4 pre-existing skips |
| `task test:int -- ./test/integration/access/... ./test/integration/world/...` | green (35 + the world Ginkgo suite) |
| `task test:int` (whole tree) | 2 failures, both `test/integration/charname` — pre-existing, see above |
| `task lint` | exit 0 |
| `task lint:proto` | exit 0 |
| `task fmt` | run; output committed |
| `task lint:invariants` (`inv-render -check`) | exit 0 |
| `task test -- ./test/meta/ -run 'TestEveryRegistryInvariantHasBinding\|TestBoundInvariantsAreGenuinelyAsserted\|TestProvenanceGuard'` | pass |
| Generated artifacts (`pkg/proto`, web `*_pb.ts`, `grpc-api.md`) regenerated and committed | clean |

## Known Stubs

None. Every field on every message this plan ships is populated from a real source, and every option it adds has a production supplier: `WithAuditContext` and `WithSkipUnchangedProperties` from the admin handler, `WithDescription` from the mixed-mask path.

One deliberate NON-stub worth naming: `TestAdminCharacterMessagesCarryNoRoleBearingField` carries no `// Verifies:` annotation. It proves an elevation-of-privilege property, not `INV-PRIVACY-13`'s retention property, and annotating it with an invariant it does not assert would be a false-green the registry rules forbid. It is invariant-shaped and probably wants an id of its own — that is a suggestion, not something minted unplanned here.

## Self-Check: PASSED

All six created files exist on disk; all three commit hashes resolve.
