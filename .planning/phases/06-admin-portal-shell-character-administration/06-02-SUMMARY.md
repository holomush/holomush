---
phase: 06-admin-portal-shell-character-administration
plan: 02
subsystem: api
tags: [grpc, abac, authorization, interceptor, protobuf, admin-portal, privacy]

requires:
  - phase: 06-admin-portal-shell-character-administration
    plan: 01
    provides: "AdminPortalService, section.AdminDescriptors + the two-arm interceptor shape switch, NewGRPCServer(GRPCServerConfig), integrationtest.WithGatedGRPCListener"
  - phase: 02-abac-schema-vocabulary
    provides: "internal/admin/section — the seven-entry registry and AssertSectionAccess's four ordered steps"
provides:
  - "AdminGetSection — the ONE endpoint through which all seven sections are reachable, gated, and refusing after the gate; its section id is a request parameter, so the six deferred sections need no stub RPCs"
  - "MethodDescriptor.SectionFromRequest — the third declaration shape, with validateAdminDescriptors now enforcing EXACTLY ONE of three"
  - "the interceptor's third gating arm + a DENYING default on the shape switch; ADMIN_SECTION_NO_SECTION_ID; grpc.AdminSectionFromContext"
  - "grpc.mapAdminSectionError — the single admin-path boundary translation, mapping SECTION_NOT_IMPLEMENTED to FailedPrecondition and everything else to one static PermissionDenied"
  - "CheckPlayerSessionResponse.roles / WebCheckSessionResponse.roles — the non-authoritative nav hint, plus grpc.WithPlayerRoleLookup wired at BOTH composition roots"
  - "integrationtest.Server.CoreServer() — the direct CoreService read the ADMIN-08 boundary precondition needs"
affects: [06-04, 06-05, 06.1-02, 06.1-03]

actuals:
  tokens: 23051
  tasks: 3
  commits: 7

tech-stack:
  added: []
  patterns:
    - "Exhaustive shape switch with a DENYING default, so a shape added later denies rather than acquiring whichever arm was reachable first"
    - "Handler-as-pure-projection: the gate resolves and stashes, the handler reads and projects, proven by stubbing the handler empty"
    - "Fail-quiet-is-fail-closed: an optional nav-hint lookup answers empty on nil or error, because empty draws no entrance"
    - "Two-layer opacity assertion: identical status+message at the wire, distinguishable typed codes in-process — the ordering is only observable at the second"

key-files:
  created:
    - internal/grpc/admin_sections.go
    - internal/grpc/admin_errors.go
    - internal/grpc/admin_sections_test.go
  modified:
    - api/proto/holomush/adminportal/v1/adminportal.proto
    - api/proto/holomush/core/v1/core.proto
    - api/proto/holomush/web/v1/web.proto
    - internal/admin/section/descriptor.go
    - internal/admin/section/descriptor_test.go
    - internal/grpc/admin_interceptor.go
    - internal/grpc/admin_interceptor_test.go
    - internal/grpc/server.go
    - internal/grpc/auth_handlers.go
    - internal/grpc/auth_handlers_test.go
    - internal/web/admin_handlers.go
    - internal/web/auth_handlers.go
    - internal/web/auth_handlers_test.go
    - internal/web/handler.go
    - internal/grpcclient/client.go
    - cmd/holomush/sub_grpc.go
    - cmd/holomush/deps.go
    - cmd/holomush/deps_test.go
    - internal/testsupport/integrationtest/harness.go
    - test/integration/access/admin_section_gate_test.go
    - docs/architecture/invariants.yaml
    - site/src/content/docs/reference/grpc-api.md

key-decisions:
  - "06-02: the ONE RPC whose section id is attacker-controlled carries NO per-handler gate — the interceptor gains a third arm (typed GetSectionId assertion, blank refusal, AssertSectionAccess on the RAW id, resolved Section stashed) and the handler is a pure projection, proven by stubbing it empty and watching both refusals survive"
  - "06-02: the shape switch is exhaustive with a DENYING default (ADMIN_SECTION_NOT_DECLARED), and validateAdminDescriptors now requires EXACTLY ONE of three shapes — none and more-than-one both abort the boot"
  - "06-02: SECTION_NOT_IMPLEMENTED is produced by the INTERCEPTOR (step 4 of the gate call it makes), so it is FailedPrecondition with a static message and NO response body — 06.1-02 must render the planned-section screen from AdminListSections layout data"
  - "06-02: the blank-section_id refusal comes from the interceptor's TrimSpace check, NOT from buf.validate — no protovalidate interceptor exists on any server path, so the shipped annotations are inert at RPC runtime and are schema documentation only"
  - "06-02: TrimSpace decides only whether the id is BLANK; the gate is called with the RAW id, because trimming would be a normalization and §10.1 matching is exact byte equality"
  - "06-02: roles reuses the SHARED attribute.PlayerRoleLookup seam and does not widen the RoleStore interface; a nil lookup or a failing one yields an initialised EMPTY slice and never an error, because a nav hint must not be able to break session restore"
  - "06-02: the wire-level differential CANNOT observe the gate-before-registry ordering — mapAdminSectionError collapses DENY_ADMIN_SECTION and DENY_ADMIN_SECTION_UNREGISTERED onto one status by design — so the ordering is asserted in-process on the typed code instead"

patterns-established:
  - "Anti-vacuity by construction: a table walks section.All() once through four cases and asserts outcome counts by EXACT equality, so a registry that lost a planned section fails rather than silently covering less"
  - "Precondition-that-reads-the-field: a boundary test whose denial path never consults the field under test asserts that field FIRST, so removing its wiring turns the test red instead of leaving it vacuously green"
  - "Demonstrated-RED for every load-bearing seam: the empty-handler stub, the removed harness wiring, and the swapped gate steps were each planted, observed failing, and reverted"

requirements-completed: [EXT-01, EXT-02, EXT-04, ADMIN-08]

coverage:
  - id: D1
    description: "All seven sections — the six deferred ones included — are reachable over the wire through AdminGetSection and refuse with SECTION_NOT_IMPLEMENTED only AFTER the gate permits"
    requirement: EXT-02
    verification:
      - kind: integration
        ref: "test/integration/access/admin_section_gate_test.go#TestTheAdminSectionGateHoldsForEverySectionAtTheWire (case 2, notImplementedCount == 6 / availableCount == 1)"
        status: pass
      - kind: unit
        ref: "internal/grpc/admin_sections_test.go#TestAnAdminNamingAPlannedSectionIsRefusedAfterTheGate (typed SECTION_NOT_IMPLEMENTED, handler never reached)"
        status: pass
    human_judgment: false
  - id: D2
    description: "A non-admin hitting any of the seven sections is denied with DENY_ADMIN_SECTION, never told the section is unimplemented and never told whether the id is registered"
    requirement: EXT-01
    verification:
      - kind: integration
        ref: "test/integration/access/admin_section_gate_test.go#TestTheAdminSectionGateHoldsForEverySectionAtTheWire (cases 1 and 4, deniedCount >= 7)"
        status: pass
      - kind: unit
        ref: "internal/grpc/admin_sections_test.go#TestANonAdminIsDeniedIdenticallyForEverySection (paired positive control per section)"
        status: pass
    human_judgment: false
  - id: D3
    description: "For the same denied caller, the refusal for a registered and an unregistered id is byte-identical in code and message (INV-PRIVACY-11 at the wire), and the ordering that produces it is asserted where it is observable"
    requirement: EXT-01
    verification:
      - kind: integration
        ref: "test/integration/access/admin_section_gate_test.go#TestTheAdminSectionGateHoldsForEverySectionAtTheWire (case 4, require.Equal on exact strings)"
        status: pass
      - kind: unit
        ref: "internal/grpc/admin_sections_test.go#TestADeniedCallerCannotTellARegisteredSectionFromAnUnregisteredOne (typed DENY_ADMIN_SECTION for BOTH ids)"
        status: pass
    human_judgment: false
  - id: D4
    description: "The attacker-controlled section id is gated by the INTERCEPTOR with no per-handler exception, and an unrecognised descriptor shape denies"
    requirement: EXT-04
    verification:
      - kind: unit
        ref: "internal/grpc/admin_interceptor_test.go#TestASectionFromRequestMethodWithNoUsableIDIsRefusedBeforeTheHandler"
        status: pass
      - kind: unit
        ref: "internal/grpc/admin_interceptor_test.go#TestADescriptorCarryingNoRecognisedShapeHitsTheDenyingDefault"
        status: pass
      - kind: other
        ref: "Test 9 demonstration — handler stubbed to return an empty response; the denial and the FailedPrecondition BOTH still passed, the positive control failed"
        status: pass
    human_judgment: false
  - id: D5
    description: "AdminGetSection carries its own AdminDescriptors entry, and a descriptor declaring none or more than one section shape aborts the boot"
    requirement: EXT-04
    verification:
      - kind: unit
        ref: "internal/admin/section/descriptor_test.go#TestEveryServedAdminMethodHasADescriptor (set equality, both directions)"
        status: pass
      - kind: unit
        ref: "internal/admin/section/descriptor_test.go#TestADescriptorDeclaringMoreThanOneSectionShapeAbortsTheBoot"
        status: pass
    human_judgment: false
  - id: D6
    description: "roles ships through a real injection path, is forwarded not computed, and is proven non-authoritative by denying a caller the field said was an admin"
    requirement: ADMIN-08
    verification:
      - kind: integration
        ref: "test/integration/access/admin_section_gate_test.go#TestARolesHintSayingAdminDoesNotSurviveAnABACDenial"
        status: pass
      - kind: unit
        ref: "internal/grpc/auth_handlers_test.go#TestCheckPlayerSessionReportsThePlayersRolesForTheNavHint (admin / role-less / guest / unwired / failing lookup)"
        status: pass
      - kind: unit
        ref: "internal/web/auth_handlers_test.go#TestWebCheckSessionForwardsRolesVerbatim"
        status: pass
    human_judgment: false

duration: 30min
completed: 2026-08-14
status: complete
---

# Phase 06 Plan 02: AdminGetSection and the Non-Authoritative Roles Hint Summary

**All seven admin sections are now reachable, gated, and refusing *after* the gate through one RPC whose section id is a caller-supplied parameter — and that parameter is authorized by the interceptor, not the handler, so the single RPC with an attacker-controlled section id is the one place a per-handler exception could not be forgotten into existence.**

## Performance

- **Duration:** 30 min
- **Started:** 2026-08-14T13:59:40Z
- **Completed:** 2026-08-14T14:29Z
- **Tasks:** 3 completed
- **Files changed:** 35 (incl. regenerated proto/connect/docs artifacts)

## Accomplishments

- **ROADMAP Success Criterion 4 holds at the wire, for all seven sections, through one endpoint.** D-100's shape pays off exactly as intended: the six deferred sections needed no stub RPCs, because the section id is a parameter. One table walks `section.All()` once and puts each entry through four cases — non-admin denial, paired admin positive control, mis-cased id, unregistered id — with outcome counts asserted by exact equality (`notImplementedCount == 6`, `availableCount == 1`), so a registry that lost a planned section fails rather than quietly covering less.
- **The gate for the attacker-controlled id lives in the interceptor.** A third arm reads the id through a typed `GetSectionId()` assertion, refuses a missing accessor or a blank id with the new `ADMIN_SECTION_NO_SECTION_ID`, calls `AssertSectionAccess` with the **raw** id, and stashes the resolved `section.Section`. The shape switch gained a **denying default**, so a fourth shape added later denies instead of inheriting whichever arm happened to be tested first. `validateAdminDescriptors` now requires *exactly one* of three shapes — none and more-than-one both abort the boot.
- **The handler is a pure projection, and that was proven, not asserted.** Stubbing `AdminGetSection` to return an empty response left **both** the non-admin denial **and** the planned-section `FailedPrecondition` passing, while the positive control failed. Both answers originate upstream.
- **One translation layer.** `mapAdminSectionError` in the new `internal/grpc/admin_errors.go` is the single place an admin-path error crosses the gRPC boundary — `SECTION_NOT_IMPLEMENTED` → `FailedPrecondition`, `ADMIN_SECTION_EVALUATION_FAILED` → `Internal`, and everything else → one static `PermissionDenied` via a *default* arm, so a code added later is refused rather than escaping the mapping.
- **`roles` ships through a real seam and is provably non-authoritative.** It had nowhere to attach: `CheckPlayerSessionResponse` carried no roles, `CoreServer` had no role collaborator, and `PlayerRoles` is deliberately off the `RoleStore` interface. Four edits land it together, reusing the **shared** `attribute.PlayerRoleLookup` type and wiring both composition roots. The boundary test makes the two halves disagree on purpose — a real role lookup says `["admin"]` while `DenyAllEngine` refuses — and asserts the roles list **first**, as a precondition.

## Task Commits

1. **Task 1: AdminGetSection — the gate runs, then the registry answers** (TDD) — three commits:
   - `de2c1a297` (feat) — `AdminGetSection` + `WebAdminGetSection` wire contract and regenerated artifacts
   - `c2a8f0518` (test) — RED: build failure on the symbols the tests assert against
   - `e41851069` (feat) — GREEN: descriptor shape, interceptor arm, `mapAdminSectionError`, handler, gateway proxy + client seam
2. **Task 2: `repeated string roles` — nav hint, never a boundary** (TDD) — three commits:
   - `158cb240c` (feat) — field 6 on both session-check responses
   - `136c69a62` (test) — RED: `WithPlayerRoleLookup` undefined; no roles populated or forwarded
   - `ad492314f` (feat) — GREEN: the four-edit injection path + both composition roots
3. **Task 3: lift the iteration into a table and pin its non-vacuity** — `5bbe1d26d` (test)

## Demonstrations Performed and Recorded

Each was planted, observed, and reverted (working tree verified clean afterward):

| Planted mutation | Assertion | Observed |
|---|---|---|
| `AdminGetSection` stubbed to return an empty response | non-admin denial (should still pass) | **PASS** — the denial is upstream |
| same stub | planned-section `FailedPrecondition` (should still pass) | **PASS** — the §10.3 refusal is upstream |
| same stub | admin positive control (should fail) | FAIL — `expected: "characters", actual: ""` |
| `WithPlayerRoleLookup` removed from the **harness** | Test 4's roles **precondition** | FAIL — `[]string{} does not contain "admin"` |
| `gate.go` steps 1 and 2 swapped | wire-level differential | **GREEN** — see criterion defect 1 below |
| same swap | in-process typed-code differential (added in response) | FAIL — `expected "DENY_ADMIN_SECTION"` |
| same swap | `internal/admin/section/gate_test.go` ordering + differential specs | FAIL — 9 subtests |

## Decisions Made

- **`SECTION_NOT_IMPLEMENTED` is produced by the interceptor**, because step 4 of `AssertSectionAccess` runs inside the gate call the interceptor makes. That is what makes the planned-section refusal reachable only *after* the ABAC decision. Consequence for **plan 06.1-02**: a `FailedPrecondition` carries a static message and **no response body**, so the planned-section screen cannot read display metadata from it — render from the already-authorized `AdminListSections` layout data.
- **`TrimSpace` decides only whether the id is blank; the gate receives the raw id.** Trimming the id we authorize would be a normalization, and §10.1 matching is exact byte equality — a normalized near-miss could resolve to a neighbouring section.
- **The blank-id refusal is the interceptor's, not `buf.validate`'s.** `rg -n 'protovalidate' internal/grpc internal/web cmd` returns nothing; no validating interceptor is installed on any server path, so the shipped annotations are inert at RPC runtime. The annotation stays on `section_id` as schema documentation, and the test asserts the interceptor.
- **`roles` fails quiet, and quiet here is closed.** A nil lookup and a failing lookup both yield an initialised empty slice plus a logged warning. An empty list draws no `/admin` entrance, so the quiet answer neither hides an admin nor grants one — and a nav hint cannot break session restore.
- **`integrationtest.Server.CoreServer()` was added rather than a new harness package.** The gated listener mounts only `AdminPortalService` and the `Session` helpers drive game sessions, so there was no way to read `CheckPlayerSession`'s answer at all. One accessor on the existing harness file was the smallest thing that made the ADMIN-08 precondition possible.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `TestAdminDescriptorEntriesAreWellFormed` encodes the two-shape world**
- **Found during:** Task 1
- **Issue:** The shipped well-formedness test asserts `SectionID` is non-empty whenever `EnumeratesAllSections` is false. The new `SectionFromRequest` entry is legitimately neither, so a correct implementation failed it.
- **Fix:** Rewrote its inner check as the same exactly-one-of-three count `validateAdminDescriptors` now enforces, and added `TestADescriptorDeclaringMoreThanOneSectionShapeAbortsTheBoot` for the other half of "exactly one" (four combinations), which nothing covered before.
- **Files modified:** `internal/admin/section/descriptor_test.go`
- **Committed in:** `e41851069`

**2. [Rule 3 - Blocking] The gateway client seam again spans four files**
- **Found during:** Task 1
- **Issue:** `WebAdminGetSection` needs a client to forward to. The plan names only `internal/web/admin_handlers.go`; as 06-01 recorded, the seam also spans `internal/web/handler.go` (`AdminPortalClient`), `internal/grpcclient/client.go`, and `cmd/holomush/deps.go` (the `GRPCClient` interface).
- **Fix:** Added the method to all four plus `mockGRPCClient` in `cmd/holomush/deps_test.go`. `(*Client).AdminGetSection` is deliberately **not** oops-wrapped, for the same reason its `AdminListSections` peer is not: `status.FromError` on a wrapped status replaces the message with the outer error's full text, and both of this RPC's refusals carry static messages.
- **Files modified:** `internal/web/handler.go`, `internal/grpcclient/client.go`, `cmd/holomush/deps.go`, `cmd/holomush/deps_test.go`
- **Committed in:** `e41851069`

**3. [Rule 3 - Blocking] The harness exposes no CoreServer**
- **Found during:** Task 2
- **Issue:** Test 4's precondition must read `roles` off a session-check response. The plan says to call `WebCheckSession`, but the harness stands up no web `Handler`, its gated listener mounts only `AdminPortalService`, and it has no `CoreServer` accessor — so the precondition could not be written at all.
- **Fix:** Added `(*Server).CoreServer()` to the existing harness file (no new package) and read `CheckPlayerSession` directly, documenting that `Handler.WebCheckSession` forwards the field verbatim so the core response is the same value the browser would receive.
- **Files modified:** `internal/testsupport/integrationtest/harness.go`
- **Committed in:** `ad492314f`

**4. [Rule 2 - Missing critical functionality] Nothing in this plan's suite could observe the gate ordering**
- **Found during:** Task 3, while performing the plan's own step-reorder demonstration
- **Issue:** Swapping `gate.go`'s step 1 and step 2 left the wire-level differential **green**. The wire answer is defended by two independent mechanisms — the ordering *and* `mapAdminSectionError` collapsing `DENY_ADMIN_SECTION` and `DENY_ADMIN_SECTION_UNREGISTERED` onto one status — so breaking one leaves the other holding. Collapsing the two codes is correct (T-06-09 requires it), which means the ordering is simply not observable at the transport.
- **Fix:** Added typed-code assertions to the in-process differential: a **denied** caller naming an unregistered id must receive `DENY_ADMIN_SECTION`, never the `DENY_ADMIN_SECTION_UNREGISTERED` diagnostic D-06 reserves for a *permitted* caller. The reorder now turns this red. The wire test's own comment records why it cannot carry the assertion.
- **Files modified:** `internal/grpc/admin_sections_test.go`
- **Verification:** Re-ran the reorder against the strengthened test — FAIL as intended; reverted, `git diff` clean.
- **Committed in:** `5bbe1d26d`

**5. [Rule 2 - Missing critical functionality] `INV-PRIVACY-11` gained a second binding site**
- **Found during:** Task 3
- **Issue:** The plan asks for a `// Verifies:` annotation "if that invariant's statement covers the wire-level differential". `INV-PRIVACY-11`'s summary states the indistinguishability *and* the gate-before-registry ordering explicitly, so it does — but its `asserted_by` named only `internal/admin/section/gate_test.go`.
- **Fix:** Added `// Verifies: INV-PRIVACY-11` above the table and listed the integration file in `asserted_by`. Not fabricated: the annotated test genuinely asserts byte-identical code and message across registered and unregistered ids for a denied caller.
- **Files modified:** `docs/architecture/invariants.yaml`, `test/integration/access/admin_section_gate_test.go`
- **Verification:** `TestBoundInvariantsAreGenuinelyAsserted`, `TestProvenanceGuard`, `TestEveryRegistryInvariantHasBinding` all pass; `inv-render -check` green.
- **Committed in:** `5bbe1d26d`

---

**Total deviations:** 5 auto-fixed (3× Rule 3, 2× Rule 2). No Rule 4 architectural questions arose.
**Impact on plan:** Deviations 1–3 were required to make the plan's own stated outcome reachable. Deviations 4–5 strengthen assertions the plan asked for but could not have been satisfied as written (see below). Nothing under `web/src/` was authored beyond the regenerated `lib/connect/**`, and `api/proto/holomush/admin/` — the UDS-served operator control plane — is untouched.

## Criterion Defects Found (reported, NOT repaired)

Two acceptance criteria cannot be satisfied as written. Per this phase's history of repairs introducing the next defect, they are reported rather than worked around: no assertion was weakened, no unrelated file was edited to make a grep pass, and no security property was traded away.

**1. Task 3: "reordering `gate.go`'s step 1 and step 2 makes the differential assertion FAIL" — UNSATISFIABLE at the wire.**
- The wire differential compares `status.Code` and `status.Convert(err).Message()`. `mapAdminSectionError` maps **both** `DENY_ADMIN_SECTION` and `DENY_ADMIN_SECTION_UNREGISTERED` to `codes.PermissionDenied` with the same package-constant message — deliberately, because a distinguishable message is exactly the registry-probing oracle T-06-09 forbids. So with the two steps swapped, the *code* a denied caller receives changes while the *status* does not, and the wire test stays green.
- **Verified:** with the steps swapped, `task test:int -- ./test/integration/access/...` reported **12 tests, 0 failures**.
- The property actually wanted **does** hold, and is caught by two in-process suites: `internal/admin/section/gate_test.go` (`TestTheGateEvaluatesTheABACEngineBeforeItConsultsTheRegistry`, `TestANonAdminsRefusalIsIdenticalAcrossARegisteredAndAnUnregisteredSectionID`) failed 9 subtests under the swap.
- **Not repaired by weakening the mapper.** Making the two codes distinguishable on the wire would satisfy the criterion by reintroducing the oracle. Instead, deviation 4 above adds the typed-code assertion at the layer where the ordering *is* observable, so this plan's own suite now catches the swap.
- Suggested correction for a future revision: point the criterion at the in-process differential (`internal/grpc/admin_sections_test.go` or `internal/admin/section/gate_test.go`), not the wire one.

**2. Task 1: "`rg -n 'oops.AsOops' test/integration/access/ …` returns zero matches" — OVER-SCOPED; one pre-existing match survives.**
- `test/integration/access/evaluation_test.go:101` uses `oops.AsOops`, landed by `ae745ff3f` ("feat(03-04): land the retirement reactor's authorization surface") — an ABAC-evaluation spec from an earlier phase, unrelated to admin sections.
- The property actually wanted holds. Scoped to the files this plan owns:
  `rg -n 'oops.AsOops' test/integration/access/admin_section_gate_test.go internal/grpc/admin_sections_test.go internal/grpc/admin_interceptor_test.go` → **exits 1**. No wire assertion in this plan reads an oops code.
- Not repaired: rewriting an unrelated phase-03 test to make a phase-06 grep pass is precisely the repair-introduces-the-next-defect pattern.

**Note on flagged assumption 1 (EXT-02 probe row):** left OPEN, as the plan directs. The ROADMAP-derived truths were implemented and asserted in full; the unclassified probe row was not folded into them.

## Known Stubs

None. Every surface is wired end to end and exercised: the proto is served, the interceptor gates it, the handler projects a real registry entry, the gateway proxy forwards to a real client, and `roles` is populated from `store.PostgresRoleStore.PlayerRoles` at both composition roots.

`WebAdminGetSection` has **no browser caller in v0.13**, and that is deliberate rather than a stub — D-100 makes both registry RPCs published wire contract, the wire tests exercise this path, and its doc comment says so explicitly so a future reader does not delete it as unused.

## Threat Flags

None. Every surface introduced is inside the plan's `<threat_model>`: `AdminGetSection` as an enumeration oracle is `T-06-09` (mitigated — gate before lookup, one static message, wire differential); the planned-section leak is `T-06-10`; the `roles` field is `T-06-11`/`T-06-12`/`T-06-13`; the attacker-controlled-id gating is `T-06-14b` (mitigated — interceptor arm, denying default, stub-the-handler control); the harness-wiring gap is `T-06-14c` (mitigated — three-site assertion plus a demonstrated RED).

## Issues Encountered

- **A `git checkout --` overreached during a demonstration revert.** Restoring the harness after the Test-4 RED discarded the whole file's working-tree state, taking the `CoreServer()` accessor with the deliberately-removed wiring line. Both edits were re-applied and verified (`git diff --stat` → 28 insertions, integration green). No commit was affected. Lesson for future demonstrations: restore from the scratchpad copy of the file, not from HEAD, when the file also carries uncommitted work.

## Verification

- `task test -- ./internal/grpc/ ./internal/admin/... ./internal/web/ ./internal/store/ ./cmd/... ./test/meta/` — **2662 tests green** (1 pre-existing skip)
- `task test:int -- ./test/integration/access/...` — **12 tests green**
- `task lint` green; `task lint:proto` green; `task build` green; `task fmt` run and its edits committed
- `task proto && task web:generate && task docs:proto` → `git status --porcelain` over `pkg/proto web/src/lib/connect grpc-api.md` is **empty** (regeneration idempotent)
- `task test -- ./test/meta/ -run TestGRPCReferenceCoversAllServices` passes; `TestBoundInvariantsAreGenuinelyAsserted` / `TestProvenanceGuard` / `TestEveryRegistryInvariantHasBinding` pass
- `task test -- ./internal/store/ -run TestRoleStoreInterfaceMethodSetIsUnchangedByPlayerRoles` passes — the `RoleStore` interface was not widened
- No file deletions in any commit (`git diff --diff-filter=D` empty across `8fb9b1057..HEAD`)

**Still owed before push:** `/holomush-dev:review-abac` MUST run — this plan evaluates `seed:admin-section-access` on a caller-supplied resource id and amends `INV-PRIVACY-11`'s binding record. `crypto-reviewer` is NOT triggered (no `crypto.emits` change).

## Self-Check: PASSED

All 14 claimed key files exist on disk; all 7 claimed commit hashes resolve in `git log`.
