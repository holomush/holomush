---
phase: 06-admin-portal-shell-character-administration
plan: 01
subsystem: api
tags: [grpc, abac, authorization, interceptor, protobuf, admin-portal, connectrpc]

requires:
  - phase: 02-abac-schema-vocabulary
    provides: "internal/admin/section — the seven-entry registry, AssertSectionAccess, ValidateAtBoot, and seed:admin-section-access (resource-TYPE scoped, player-flavored)"
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: "the Web* gateway-proxy shape and the character routing census this plan extends with a placement guard"
provides:
  - "holomush.adminportal.v1.AdminPortalService with AdminListSections — the player-session admin portal wire contract, a distinct package and trust boundary from the UDS-served holomush.admin.v1.AdminService"
  - "section.AdminDescriptors — the fail-closed method→section declaration table, plus LookupMethodDescriptor and validateAdminDescriptors wired into ValidateAtBoot"
  - "section.AssertSectionAdmission / PortalProbeSectionID — the policy-gate step alone, extracted (not copied) from AssertSectionAccess"
  - "grpc.NewAdminSectionInterceptor — the ONE place every adminportal method is authorized, with four fail-closed arms and no ungated arm"
  - "grpc.NewGRPCServer(GRPCServerConfig) — the single Core/Portal server factory, which cannot build an ungated server"
  - "grpc.AdminPortalServer + the variadic AdminPortalServerOption seam plans 06-04/06-05 extend"
  - "web.Handler.WebAdminListSections — the gateway proxy that computes nothing"
  - "integrationtest.WithGatedGRPCListener — the harness's first network transport, built through the production factory"
affects: [06-02, 06-04, 06-05, 06.1-02, 06.1-03]

actuals:
  tokens: 78850
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Structural gate: authorization driven by a declaration table + interceptor, so forgetting the declaration DENIES"
    - "Composite refusal error (adminRefusal): satisfies GRPCStatus() directly for wire opacity and Unwrap()s to the typed oops for in-process code assertion"
    - "A factory that refuses to build rather than a constructor that must be remembered"

key-files:
  created:
    - api/proto/holomush/adminportal/v1/adminportal.proto
    - internal/admin/section/descriptor.go
    - internal/grpc/admin_interceptor.go
    - internal/grpc/admin_service.go
    - internal/web/admin_handlers.go
    - test/meta/admin_rpc_placement_test.go
    - test/integration/access/admin_section_gate_test.go
    - internal/admin/section/descriptor_test.go
    - internal/admin/section/descriptor_completeness_test.go
    - internal/grpc/admin_interceptor_test.go
    - internal/grpc/server_interceptor_test.go
  modified:
    - internal/admin/section/gate.go
    - internal/admin/section/boot.go
    - internal/grpc/server.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/harness.go
    - docs/architecture/invariants.yaml

key-decisions:
  - "06-01: the gate is mounted by ONE server factory that returns GRPC_SERVER_ADMIN_GATE_MISSING on a nil interceptor; the two zero-caller constructors collapsed into it and NewGRPCServerInsecure was deleted, so the mount is structural rather than remembered"
  - "06-01: wire opacity and internal-code identity are asserted at two layers and never collapsed — adminRefusal implements GRPCStatus() DIRECTLY (a wrapped status has its message replaced by the outer error's text) and Unwrap()s to the typed oops"
  - "06-01: single-wrap is asserted as unwrap-chain DEPTH, not by code — oops Code() resolves the DEEPEST code, so under a double wrap both spellings agree with each other and disagree with the truth"
  - "06-01: AdminListSections IS deniable — the interceptor runs AssertSectionAdmission for the EnumeratesAllSections descriptor too, so a non-admin gets PermissionDenied rather than an empty 200"
  - "06-01: the enumeration filter is AssertSectionAdmission, never AssertSectionAccess, whose availability step would drop all six planned sections out of the nav"
  - "06-01: section display names are DERIVED (section.DisplayName) rather than stored — a second stored label would be a second vocabulary that could drift from the id"

patterns-established:
  - "Fail-closed declaration table: a served method with no entry is refused before subject resolution, and set equality against the SERVED set is asserted in both directions"
  - "Probe-with-asserted-immateriality: a concrete section id is used to evaluate a resource-TYPE-scoped policy, and the immateriality of that choice is asserted rather than assumed, so the probe goes RED the moment per-section grants land"
  - "Placement fences at two granularities: a per-facade census guard plus a repo-wide proto fence with an explicit, reasoned allow-set that is proven green at HEAD before it can mean anything"

requirements-completed: [ADMIN-01, ADMIN-02, EXT-01, EXT-03, EXT-04]

coverage:
  - id: D1
    description: "A non-admin calling AdminListSections directly over gRPC — bypassing the browser and the gateway — is denied at the wire with codes.PermissionDenied and a static, field-free message"
    requirement: ADMIN-01
    verification:
      - kind: integration
        ref: "test/integration/access/admin_section_gate_test.go#TestANonAdminCallingTheAdminPortalDirectlyOverGRPCIsDeniedAtTheWire"
        status: pass
      - kind: integration
        ref: "test/integration/access/admin_section_gate_test.go#TestAnAdminCallingTheAdminPortalDirectlyOverGRPCReceivesEverySection (paired positive control)"
        status: pass
      - kind: unit
        ref: "internal/grpc/admin_interceptor_test.go#TestTheInterceptorRefusesANonAdminWithTheTypedDenyCode (typed DENY_ADMIN_SECTION, in-process)"
        status: pass
    human_judgment: false
  - id: D2
    description: "An admin-portal method with no method→section descriptor is refused with ADMIN_SECTION_NOT_DECLARED before any session lookup, and that arm is proven reachable by mutating the real table"
    requirement: ADMIN-02
    verification:
      - kind: unit
        ref: "internal/admin/section/descriptor_completeness_test.go#TestInterceptorAdminMethodWithoutDescriptorFailsClosed"
        status: pass
      - kind: unit
        ref: "internal/grpc/admin_interceptor_test.go#TestAnAdminMethodWithNoDescriptorIsRefusedBeforeSubjectResolution"
        status: pass
    human_judgment: false
  - id: D3
    description: "The descriptor table is held set-EQUAL to the served method set in both directions, and a malformed entry aborts the boot"
    requirement: EXT-04
    verification:
      - kind: unit
        ref: "internal/admin/section/descriptor_test.go#TestEveryServedAdminMethodHasADescriptor"
        status: pass
      - kind: unit
        ref: "internal/admin/section/descriptor_test.go#TestValidateAtBootRejectsAMalformedMethodDescriptor"
        status: pass
    human_judgment: false
  - id: D4
    description: "The seven-entry registry is verified (not rebuilt), and the admission verdict is immaterial to which section id is probed"
    requirement: EXT-01
    verification:
      - kind: unit
        ref: "internal/admin/section/gate_test.go#TestTheAdmissionProbeIDIsImmaterialToTheVerdict"
        status: pass
      - kind: unit
        ref: "internal/admin/section/gate_test.go#TestAdmissionPermitsAPlannedSectionThatAccessRefuses"
        status: pass
    human_judgment: false
  - id: D5
    description: "Admin RPCs are fenced off the character facade AND off every other service, by a repo-wide proto placement fence proven green at HEAD"
    requirement: EXT-03
    verification:
      - kind: unit
        ref: "test/meta/admin_rpc_placement_test.go#TestEveryAdminPrefixedRPCLivesInAnAdminPackage"
        status: pass
      - kind: unit
        ref: "test/meta/characteraccess_routing_census_test.go#TestCharacterAccessRoutingCensusExcludesAdminRPCs"
        status: pass
    human_judgment: false
  - id: D6
    description: "No production Core/Portal composition builds a gRPC server outside the one factory, and that factory cannot produce an ungated server"
    requirement: ADMIN-01
    verification:
      - kind: unit
        ref: "internal/grpc/server_interceptor_test.go#TestTheServerFactoryRefusesToBuildAnUngatedServer"
        status: pass
      - kind: other
        ref: "rg -n 'grpc\\.NewServer\\(' cmd internal/grpc -g '!*_test.go' — exactly one line, internal/grpc/server.go (three at HEAD)"
        status: pass
    human_judgment: false

duration: 33min
completed: 2026-08-14
status: complete
---

# Phase 06 Plan 01: Admin Portal ABAC Gate Summary

**`/admin` is now an ABAC-gated trust boundary end to end — a non-admin speaking gRPC directly to the core server is refused `PermissionDenied` with a static, field-free message by an interceptor that cannot be omitted, because the one server factory refuses to build without it.**

## Performance

- **Duration:** 33 min
- **Started:** 2026-08-14T13:16:52Z
- **Completed:** 2026-08-14T13:49:49Z
- **Tasks:** 2 completed
- **Files modified:** 36 (incl. generated proto/connect artifacts)

## Accomplishments

- **The security spine, proven where the threat lives.** ROADMAP Success Criterion 1 is asserted over a real gRPC connection through a bufconn server built by the *production* factory, with a paired admin positive control on the same RPC and the same registry. Wire opacity (`codes.PermissionDenied` + exact static message + absent substrings) and internal-code identity (`errutil.AssertErrorCode(…, "DENY_ADMIN_SECTION")`, in-process) are asserted at their own layers and never collapsed.
- **The gate is structural, not remembered.** Two zero-caller constructors that had already drifted from the live composition collapsed into one `NewGRPCServer(GRPCServerConfig)` that returns `GRPC_SERVER_ADMIN_GATE_MISSING` on a nil interceptor. `rg 'grpc\.NewServer\(' cmd internal/grpc -g '!*_test.go'` went from three lines to one.
- **Forgetting a declaration denies.** `section.AdminDescriptors` is the method→section table; a served method with no entry is refused *before* subject resolution (proven by a session-repo call count of zero against a declared-entry control whose count is one), and the table is held set-equal to `AdminPortalService_ServiceDesc.Methods` in both directions.
- **`AdminListSections` can deny.** The interceptor runs `AssertSectionAdmission` for the enumerating descriptor too, so a non-admin is refused before the handler runs — demonstrated by stubbing the handler to return an empty response and watching the denial test *still pass* while the positive control fails.
- **Two placement fences.** A census guard keeps admin RPCs off the character facade without diluting its audience proof; a repo-wide proto fence closes the class (an admin RPC on any third service) with an explicit two-package allow-set, proven green at HEAD.

## Task Commits

1. **Task 1: End-to-end "an admin sees their section list"** (tracer, TDD) — three commits:
   - `27759774e` (feat) — the `holomush.adminportal.v1` wire contract + regenerated artifacts
   - `5afd0a298` (test) — RED: every assertion failing against HEAD
   - `fcdd2395a` (feat) — GREEN: descriptor table, admission extraction, interceptor, factory, handler, gateway proxy, harness listener
2. **Task 2: Make the fail-closed arm provably live, and fence admin RPCs off the character facade** — `65ff9b2a6` (test)

## Files Created/Modified

**Wire contract**
- `api/proto/holomush/adminportal/v1/adminportal.proto` — `AdminPortalService` / `AdminListSections`; a deliberately separate package and trust boundary from the UDS-served `holomush.admin.v1.AdminService`
- `api/proto/holomush/web/v1/web.proto` — `WebAdminListSections`, reusing `adminportal.AdminSection` so no second web-side section shape exists

**Authorization**
- `internal/admin/section/descriptor.go` — `MethodDescriptor`, `AdminDescriptors`, `LookupMethodDescriptor` (exact match, no case folding), `validateAdminDescriptors`, `PortalProbeSectionID`, `DisplayName`
- `internal/admin/section/gate.go` — step 1 extracted verbatim into `assertSectionAdmission`; exported `AssertSectionAdmission`; the pre-D-99 "the redundancy is the point" doc comment replaced with the shipped model
- `internal/admin/section/boot.go` — `validateAdminDescriptors` wired into `validateAtBoot`
- `internal/grpc/admin_interceptor.go` — four fail-closed arms, the ctx stash, `adminRefusal`
- `internal/grpc/admin_service.go` — `AdminPortalServer` + the variadic option seam 06-04/06-05 extend
- `internal/grpc/server.go` — the single factory; `NewGRPCServerInsecure` deleted

**Composition**
- `cmd/holomush/sub_grpc.go` — production builds from the factory, keeping `grpcProxy.Handler()` via `Extra`
- `internal/web/admin_handlers.go`, `internal/web/handler.go`, `internal/grpcclient/client.go`, `cmd/holomush/{gateway,deps}.go` — the gateway proxy and its client plumbing
- `internal/testsupport/integrationtest/{harness,session}.go` — `WithGatedGRPCListener`, `GatedGRPCConn`, `Session.PlayerSessionToken`

**Proofs**
- `internal/admin/section/descriptor_test.go`, `descriptor_completeness_test.go`, `gate_test.go` (extended)
- `internal/grpc/admin_interceptor_test.go`, `server_interceptor_test.go`
- `test/integration/access/admin_section_gate_test.go`
- `test/meta/admin_rpc_placement_test.go`, `characteraccess_routing_census_test.go` (extended)
- `docs/architecture/invariants.yaml` / `.md` — `INV-ACCESS-16`, `INV-PRIVACY-12`, both `bound`

## RED Demonstrations Performed and Recorded

Each was planted, observed failing, and reverted (working tree verified clean afterward):

| Planted bug | Assertion that caught it | Observed |
|---|---|---|
| Second `oops.Code("INTERNAL").Wrap(...)` around the refusal | `TestTheInterceptorRefusalIsWrappedExactlyOnce` | FAIL — `expected: 1, actual: 2` |
| `AdminListSections` stubbed to return an empty response | denial test (should still pass) | **PASS** — the refusal is upstream |
| same stub | admin positive control (should fail) | FAIL — `"[]" should have 7 item(s), but has 0` |
| Descriptor entry naming an unserved method | `TestEveryServedAdminMethodHasADescriptor` | FAIL — names `AdminNeverServedRPC` (EXTRA direction) |
| Descriptor key renamed | same test | FAIL — names `AdminListSections` (MISSING direction) |
| `validateAdminDescriptors()` call removed from `validateAtBoot` | `TestValidateAtBootRejectsAMalformedMethodDescriptor` | FAIL — the wiring is load-bearing |
| `rpc AdminPurgeCharacter` planted on `characteraccess.proto` | `TestEveryAdminPrefixedRPCLivesInAnAdminPackage` | FAIL — names the method and its package |
| adminportal proto moved aside (simulating HEAD) | same fence | **GREEN** — the fence is not red on arrival |

## Decisions Made

- **`adminRefusal` is a composite error.** The plan requires the same value to be a `PermissionDenied` status carrying a static message *and* to expose the typed oops code in-process. `oops.Wrap(statusErr)` cannot do this: `status.FromError` on a *wrapped* status returns a new status with `Message` replaced by the outer error's full text, which would leak the typed code onto the wire. `adminRefusal` satisfies `GRPCStatus()` directly (message preserved) and `Unwrap()`s to the oops.
- **Section display names are derived, not stored.** The registry carries no label. Rather than rebuild the registry (which the plan forbids), `section.DisplayName(id)` capitalises the id — lossless for all seven single-word ids, and no second vocabulary to drift.
- **The inner error code is the assertable one.** `errutil.AssertErrorCode` resolves the *deepest* code, so the boot-wiring test asserts `ADMIN_METHOD_DESCRIPTOR_INVALID` (the validator's) rather than `ADMIN_METHOD_DESCRIPTORS_INVALID` (the wrapper's) — matching the shipped convention at `boot_test.go:42`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The plan cites a constant that does not exist**
- **Found during:** Task 1
- **Issue:** The plan specifies `const PortalProbeSectionID = IDCharacters`. `IDCharacters` is not defined anywhere in `internal/admin/section/` — the registry spells ids as bare string literals in `all`.
- **Fix:** Declared `const PortalProbeSectionID ID = "characters"` directly; tests use the literal.
- **Files modified:** `internal/admin/section/descriptor.go`, `internal/admin/section/descriptor_test.go`
- **Verification:** `TestTheCharactersSectionResolvesThroughAdminSectionResource` asserts the probe names a registered section.
- **Committed in:** `fcdd2395a` / `5afd0a298`

**2. [Rule 2 - Missing critical functionality] `AdminSection.display_name` had no source**
- **Found during:** Task 1
- **Issue:** The plan mandates `string display_name = 2` on `AdminSection`, but `section.Section` carries only `ID`, `Status` and `Descriptor`. No label exists anywhere for the admin registry (`web/src/lib/nav/sections.ts` is the *workspace* nav — room/scenes — not admin).
- **Fix:** Added `section.DisplayName(ID)`, deriving the label from the id, documented as derived-not-stored for the same reason `entry()` derives the descriptor resource. The registry was not rebuilt.
- **Files modified:** `internal/admin/section/descriptor.go`
- **Verification:** The integration positive control asserts every returned section carries a non-empty `display_name`.
- **Committed in:** `fcdd2395a`

**3. [Rule 3 - Blocking] Gateway client plumbing not enumerated**
- **Found during:** Task 1
- **Issue:** `WebAdminListSections` needs a client to forward to. The plan names only `internal/web/admin_handlers.go`, but the gateway's client seam spans `internal/grpcclient`, `cmd/holomush/deps.go` (the `GRPCClient` interface), and `cmd/holomush/gateway.go` (handler construction).
- **Fix:** Added `AdminPortalClient` + `WithAdminPortalClient` on the web Handler, `(*Client).AdminListSections` on `internal/grpcclient` (deliberately *not* oops-wrapped, so the static refusal message survives the boundary), and the `GRPCClient` interface method + wiring. `mockGRPCClient` in `cmd/holomush/deps_test.go` gained the method to keep the interface satisfied.
- **Files modified:** `internal/web/handler.go`, `internal/grpcclient/client.go`, `cmd/holomush/{deps,gateway}.go`, `cmd/holomush/deps_test.go`
- **Verification:** `task build` and `task test -- ./cmd/... ./internal/web/ ./internal/grpcclient/` green.
- **Committed in:** `fcdd2395a`

**4. [Rule 3 - Blocking] Test-support accessors the integration test requires**
- **Found during:** Task 1
- **Issue:** `Session.playerSessionToken` is unexported with no reader, so a spec driving a facade RPC directly cannot authenticate.
- **Fix:** Added `Session.PlayerSessionToken()`, documented as a read of harness-minted state, never a mint.
- **Files modified:** `internal/testsupport/integrationtest/session.go`
- **Committed in:** `fcdd2395a`

**5. [Rule 3 - Blocking] `GRPCServerConfig` trips revive's stutter rule**
- **Found during:** Task 1 lint
- **Issue:** revive flags `grpc.GRPCServerConfig` and suggests `ServerConfig`. Renaming would deviate from a plan-named artifact that 06-04/06-05 may reference.
- **Fix:** Kept the plan's name behind a line-scoped `//nolint:revive` naming the reason (it is named for `NewGRPCServer`, the pre-existing exported spelling the factory keeps). No config widening.
- **Files modified:** `internal/grpc/server.go`
- **Committed in:** `fcdd2395a`

**6. [Rule 1 - Bug] `descriptor_completeness_test.go` cannot be `package section`**
- **Found during:** Task 2
- **Issue:** The plan puts the "drive a real interceptor invocation" test in `internal/admin/section/`, but the interceptor lives in `internal/grpc`, which imports `section` — an internal test package would be an import cycle.
- **Fix:** The file is `package section_test`, which may legally import a package that imports the package under test.
- **Files modified:** `internal/admin/section/descriptor_completeness_test.go`
- **Committed in:** `65ff9b2a6`

---

**Total deviations:** 6 auto-fixed (4× Rule 3, 1× Rule 2, 1× Rule 1)
**Impact on plan:** All were required to make the plan's own stated outcome reachable. No scope creep: nothing under `web/src/` was authored (verified — `git diff --name-only web/src` yields nothing outside the regenerated `lib/connect/**`), and `api/proto/holomush/admin/` is provably untouched.

## Criterion Defects Found (reported, NOT repaired)

Three acceptance criteria cannot be satisfied as written. Per the phase's own history of repairs introducing new defects, these are reported rather than worked around — no historical doc was edited and no pre-existing comment was deleted to make a grep pass.

**1. `rg -n 'otelgrpc' internal/grpc/server.go` returns zero matches — UNSATISFIABLE, and was already red at HEAD.**
- At HEAD the file carried four `otelgrpc` matches, one of which (`:76`) is a *pre-existing doc comment* about the otelgrpc-installed server interceptor span, unrelated to the two dead constructors. The criterion could only pass by deleting that comment.
- Worse, the plan's own action **mandates adding a second** otelgrpc-naming comment ("write that sentence into the factory's doc comment so the omission reads as deliberate"). The action and the criterion contradict.
- **The property actually wanted holds.** The discriminating (comment-filtered) form the plan uses elsewhere for exactly this reason:
  `rg -v '^\s*//' internal/grpc/server.go | rg -q 'otelgrpc'` → **exits 1**. No live `otelgrpc` reference remains; the import is gone and no `StatsHandler` is installed.
- Suggested correction for a future revision: use the comment-filtered form.

**2. `rg -n 'NewGRPCServerInsecure' .` returns zero matches — one match survives, in a historical planning doc.**
- `docs/superpowers/plans/2026-03-31-observability-and-telemetry.md:499` ("Also update `NewGRPCServerInsecure` with the same stats handler.") — a retired plan describing past work, now naming a deleted symbol.
- **The property actually wanted holds:** `rg -n 'NewGRPCServerInsecure' . -g '!docs/**'` → **exits 1**. The constructor is absent from all code; there is no second way to build a server.
- Not repaired: editing a historical planning doc to make a grep pass is the "repair that introduces the next defect" pattern this phase has been bitten by twice. If the stale instruction is worth fixing, it is a docs-hygiene change on its own.

**3. Task 2's boot-wiring criterion names "the descriptor-validation code" ambiguously.**
- `errutil.AssertErrorCode` resolves the **deepest** code in the chain, and `validateAtBoot` wraps the validator's error. Asserting the outer spelling (`ADMIN_METHOD_DESCRIPTORS_INVALID`) fails against a correct implementation.
- Resolved by asserting the inner `ADMIN_METHOD_DESCRIPTOR_INVALID`, matching the shipped convention (`boot_test.go:42` asserts the validator's code, not the wrapper's). Recorded in the test's own comment.

**Minor note (not a defect):** Task 2's criterion says the fence "reuses `findRepoRoot` and the shared `protoPackageDecl` / `protoServiceDecl` regex vars". The fence reuses `findRepoRoot` and `protoPackageDecl` and declares exactly one new var (`protoRPCDecl`); it has no use for `protoServiceDecl` because it operates at rpc granularity, not service. The single-definition assertions the criterion actually makes all hold.

## Issues Encountered

- **ULID fixtures.** Initial hand-written player-id literals were 27 characters and panicked in `ulid.MustParse`. Replaced with valid 26-character Crockford values.
- **Pre-existing harness noise.** `WithRealABAC` logs `no partition of relation "access_audit_log"` during WAL replay (465 entries, non-fatal, logged as such by production). Pre-existing, unrelated to this plan, out of scope per the scope-boundary rule — not fixed, not repaired.

## Known Stubs

None. Every surface this plan created is wired end to end and exercised: the proto is served, the interceptor is chained by the production factory, the handler enumerates from the real registry, and the gateway proxy forwards to a real client. `AdminPortalServerOption` is a construction seam with no options yet — that is the documented shape 06-04/06-05 extend, not a stub, and the constructor is fully functional without one.

## Threat Flags

None. Every surface introduced is inside the plan's `<threat_model>`: the new RPC is `T-06-01` (mitigated by the interceptor + factory), the descriptor table is `T-06-02`, the proto fence is `T-06-02b`, and the named limitation of that fence is `T-06-02d` (`accept`), written into the fence's own doc comment.

## Verification

- `task test -- ./internal/admin/... ./internal/grpc/... ./test/meta/` — **1465 tests green** (1 pre-existing skip)
- `task test:int -- ./test/integration/access/...` — **3 tests green** (the Ginkgo suite + this plan's two)
- `task lint` — green; `task lint:proto` — green; `task fmt` run and its edits committed
- `task proto && task web:generate && task docs:proto && go run ./cmd/inv-render` → `git status --porcelain` over `pkg/proto web/src/lib/connect grpc-api.md invariants.md` is **empty** (regeneration is idempotent)
- `task build` green
- No file deletions in any commit (`git diff --diff-filter=D` empty across the range)

**Still owed before push (plan `<verification>`):** `/holomush-dev:review-abac` MUST run — this plan touches `internal/access` vocabulary through `access.AdminSectionResource` and the `seed:admin-section-access` evaluation path. `crypto-reviewer` is NOT triggered (no `crypto.emits` change).

## Self-Check: PASSED

All 9 claimed key files exist on disk; all 4 claimed commit hashes resolve in `git log`.
