---
phase: 06-admin-portal-shell-character-administration
verified: 2026-08-14T21:40:00Z
status: passed
score: 5/5 must-haves verified (criterion 3 rescoped, not fixed — see re_verification)
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 4/5 must-haves verified
  resolved_by: scope-amendment
  resolved_at: 2026-08-14
  note: >-
    The code did NOT change. ROADMAP phase-6 criterion 3 and REQUIREMENTS ADMIN-06 were both
    amended to drop the events_audit PROJECTION clause and to state that before-values are
    lifecycle-only (profile updates are new-values-only by D-103 erasure-safety). The phase
    now meets its criteria as written. The projection work is unchanged and unfixed; it is
    tracked in #4971 and belongs to the eventbus relay wiring, not to this phase. Anyone
    auditing this phase should read the amended criterion, not the original.
    IMPORTANT — `passed` here means "meets its criteria as amended", NOT "nothing is
    outstanding". The gap entries below are retained UNCHANGED, with the statuses the verifier
    originally assigned, and TWO OF THEM ARE STILL OPEN IN THE TREE. Gap 2 (traceability rows
    stuck at Pending) is a standing GSD requirements.mark-complete writer bug affecting three
    phases now — tracked in #4974, not a phase-6 deliverable. Gap 3 (WR-04: one
    charname-normalized parameter bound to both predicate arms, so non-ASCII usernames silently
    return empty) is a real defect in shipped code — tracked in #4972. Neither was part of the
    rescope decision; only criterion 3 was.
gaps:
  - truth: "RESOLVED BY RESCOPE — ROADMAP criterion 3 / ADMIN-06 originally claimed the `events_audit` row is PROJECTED from the admin mutation's envelope by the asynchronous audit projection"
    status: resolved_by_scope_amendment
    reason: >-
      The transactional half is real, wired and proven end to end, and remains in scope. The
      projection half was UNREACHABLE by construction: the outbox relay publishes through a bare
      JetStreamPublisher, only eventbus.RenderingPublisher writes the App-Rendering header,
      and audit.writeAuditRow refuses a message without it (AUDIT_MISSING_HEADER). No admin
      mutation has ever produced an events_audit row, and no test asserts one does — the two
      events_audit assertions in the phase both assert ZERO. Rather than leave the phase open on
      work that belongs to the eventbus relay, the projection clause was struck from both the
      ROADMAP criterion and ADMIN-06 on 2026-08-14 and filed as #4971. The findings below are
      retained verbatim as the evidence behind that decision — they describe the code as it
      still stands.
    artifacts:
      - path: "internal/world/setup/relay_subsystem.go:94"
        issue: "publisher := s.cfg.EventBus.Publisher() — resolves to NewJetStreamPublisher (internal/eventbus/publisher.go:395-400), which stamps no App-Rendering header"
      - path: "internal/eventbus/audit/projection.go:328-331"
        issue: "renderingJSON == \"\" arm returns AUDIT_MISSING_HEADER — a world envelope reaching the projection is rejected, never persisted"
      - path: "internal/world/outbox/wire.go:154"
        issue: "EnvelopeToEvent builds the eventbus.Event with no Rendering and no App-Rendering header"
      - path: "test/integration/access/admin_characters_write_test.go:354,472"
        issue: "the only two events_audit assertions in the phase are assert.Zero — the projection half has no positive coverage anywhere"
    missing:
      - "A decision, not a patch: either the world relay gains rendering metadata (and world envelopes start landing in events_audit), or 01-SPEC §14 row 9 / ADMIN-06 / ROADMAP criterion 3 are restated for world mutations. Both are larger than a phase-6 fix."
      - "A GitHub issue and a WINDOWS.md row — the gap currently lives only in 06-05-SUMMARY.md prose and one pin test; the phase ledger has no row for it."
      - "ADMIN-06's checkbox at .planning/REQUIREMENTS.md:207 is [x] while its projection clause (lines 209-211) does not hold. Either the checkbox is reverted or the requirement text is amended."
  - truth: "The requirements traceability table records which phase delivered each requirement"
    status: failed
    reason: >-
      All twelve phase-6/6.1 rows still read `Pending` despite ten requirements being
      delivered and their checkboxes written. This is the standing `table_unmatched` writer
      gap in requirements.mark-complete (WINDOWS #25/#26; same failure previously seen for
      AUTHZ-01 in phase 02.1 and IDENT-04 in phase 3).
    artifacts:
      - path: ".planning/REQUIREMENTS.md:393-404"
        issue: "ADMIN-01..08 and EXT-01..04 all read `| ... | Phase 6 | Pending |` / `| ... | Phase 6.1 | Pending |`"
    missing:
      - "Ten rows (ADMIN-01, 02, 04, 05, 06, 08, EXT-01..04) set to their delivered state — via the tool, not by hand-editing the parsed artifact."
      - "ADMIN-03's routing reconciled: ROADMAP.md:566 assigns it to Phase 6, REQUIREMENTS.md:354/355/395 assign it to 6.1 only. It straddles both."
  - truth: "Admin character search matches the operator's term against both searched columns"
    status: partial
    reason: >-
      WR-04 from 06-REVIEW.md is still OPEN in the tree. One charname-normalized parameter is
      bound to BOTH predicate arms; players.username is stored verbatim (no charname pipeline
      on the registration path), so a username in NFD, carrying a compatibility codepoint, a
      Cf format rune, or a doubled space is unmatchable. It degrades to an empty result set,
      not an error — the phase's own stated review lens.
    artifacts:
      - path: "internal/world/postgres/character_repo_admin.go:189-195"
        issue: "`(c.normalized_name ILIKE $n ESCAPE '\\' OR p.username ILIKE $n ESCAPE '\\')` — one doubly-transformed parameter, two differently-transformed columns"
      - path: "internal/world/postgres/character_repo_admin.go:79-86"
        issue: "the repository doc block asserts the same-pipeline contract, which is true for the name arm and false for the username arm"
    missing:
      - "Bind two parameters (charname key for the name arm, trim-only raw term for the username arm), or at minimum correct the doc block so it stops asserting a property one arm lacks."
deferred: []
human_verification: []
---

# Phase 6: Admin Portal Shell & Character Administration — Verification Report

**Phase Goal:** Stand up `/admin` as an ABAC-gated trust boundary with character administration as its working section, six deferred sections registered / gated / refusing **after** the gate, and audit emission with before-values on every admin mutation.
**Verified:** 2026-08-14T21:40:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification
**Scope note:** the phase was SPLIT on 2026-08-13; all `web/src/**` work moved to Phase 06.1 (`ROADMAP.md:598-633`). Nothing below is failed for a missing UI. Where a criterion's *user-visible* satisfaction depends on a Svelte artifact, it is attributed to 06.1 and the **server contract** is what is judged here.

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A non-admin calling an admin RPC directly is denied; ABAC on `admin_section:`, never `PlayerHasRole`, never a route-guard; denial asserted over the wire with a generic message; typed `DENY_*` asserted with `errutil.AssertErrorCode`; paired positive control | ✓ VERIFIED | Gate is a real `engine.Evaluate` on `access.AdminSectionResource` (`internal/admin/section/gate.go:161-192`); `PlayerHasRole` appears in the admin path only inside a prohibition comment (`gate.go:286`). Wire denial + generic message + leak fence: `test/integration/access/admin_section_gate_test.go:60-89`. Paired positive control: `:103-180`. Typed codes: `errutil.AssertErrorCode(t, err, "DENY_ADMIN_SECTION")` at `internal/admin/section/gate_test.go:143,189-199` and `internal/grpc/admin_interceptor_test.go:135`. Interceptor is the single gate with a DENYING default and no ungated arm (`internal/grpc/admin_interceptor.go:171-255`), wired at the one production root (`cmd/holomush/sub_grpc.go:429-438`) |
| 2 | An admin lists, searches, opens and edits characters; field-mask allowlist excludes roles; admin disable/delete moves through the SAME lifecycle states as player retire; irreversible `DeleteCharacter` reachable from no player-facing button | ✓ VERIFIED (server contract; UI is 06.1) | Eight RPCs served and registered (`cmd/holomush/sub_grpc.go:913-917`), eight gateway proxies (`internal/web/admin_handlers.go:35,80,115,156,197,233,282,316`), client forwarders (`internal/grpcclient/client.go:495-590`). Thirteen-path exact-string allowlist (`internal/grpc/admin_characters_write.go:127-…`), role fence walks generated descriptors non-vacuously (`test/meta/admin_character_message_role_fence_test.go:93-110`). Retire/unretire route through the canonical `world.Service.RetireCharacter`/`UnretireCharacter` (`admin_characters_write.go:369,400`; `internal/world/service.go:1383`). Zero `DeleteCharacter` RPCs in any proto (`rg DeleteCharacter api/proto/` returns three comments and no `rpc`), and `delete` is omitted from the seed policy (`internal/access/policy/seed.go:1034`) so the guarantee also holds at the policy layer. **Caveat:** WR-04 search defect open — see gaps |
| 3 | Every admin mutation emits its audit envelope in the same transaction as the state change, carrying before-values and the acting player id; the `events_audit` row is PROJECTED from that envelope by the asynchronous audit projection, which is the only writer | ⚠️ PARTIALLY MET — the projection clause is NOT MET | **See the per-clause breakdown below.** |
| 4 | All six deferred sections registered, role-gated, and returning `NOT_IMPLEMENTED` AFTER the gate; a non-admin is *denied*, not told unimplemented; a meta-test asserts set equality between the registry and the descriptor set | ✓ VERIFIED | Seven entries, one available + six planned (`internal/admin/section/registry.go:118-124`). Wire proof per section with all four cases and explicit anti-vacuity counters — `require.Equal(t, 6, notImplementedCount)` / `require.Equal(t, 1, availableCount)` (`test/integration/access/admin_section_gate_test.go:180-259`): non-admin gets `PermissionDenied` + the static message for EVERY section, admin gets `FailedPrecondition` for the six planned. Set equality with anti-vacuity guards on BOTH sets (`internal/admin/section/descriptor_test.go:74-101`), registry id-set equality (`registry_test.go:249`), zero-descriptor rejection (`registry_test.go:123-170`), and boot abort wired at a real production call site (`internal/admin/section/boot.go:34` ← `internal/bootstrap/setup/subsystem.go:156`). The unary-only blind spot is closed by an explicit stream fence (`descriptor_test.go:126`) |
| 5 | The `roles` hint on `WebCheckSessionResponse` is non-authoritative — a caller acting on a role it names still meets a denial at the admin RPC | ✓ VERIFIED | Hint produced at `internal/grpc/auth_handlers.go:793,817-830`, forwarded verbatim by the gateway with an explicit no-lookup comment (`internal/web/auth_handlers.go:305-309`). Differential proof makes the two halves DISAGREE on purpose — real role store reports `["admin"]` as a load-bearing precondition, `DenyAllEngine` denies the same caller anyway (`test/integration/access/admin_section_gate_test.go:284-313`) |

**Score:** 4/5 truths verified (0 present, behavior-unverified)

### Criterion 3 — per-clause adjudication

The prompt asks for precision about which half holds. Answer: **the transactional half and the single-writer half hold; the projection half does not, and it is unreachable rather than merely untested.**

| Clause | Verdict | Evidence |
|--------|---------|----------|
| Envelope emitted **in the same transaction** as the state change | ✓ MET | `TestARolledBackAdminMutationLeavesNeitherAnEnvelopeNorAnAuditRow` (`test/integration/access/admin_characters_write_test.go:415-474`) forces a real duplicate-key failure on the envelope insert *after* the status write has landed in the same transaction, then asserts status, version and envelope count all unchanged. `TestAdminRetireEmitsOneTransactionalEnvelopeCarryingThePlayerActor` (`:123`) is the positive side |
| Carries the acting **player** id (not only the character) | ✓ MET | `adminWriteCaller` → `world.HumanCaller(access.PlayerSubject(playerID))` (`internal/grpc/admin_characters_write.go:202-204`); D-104's `player:<id>` reaches the envelope Actor verbatim (`internal/world/payloads.go:317`) |
| Carries the **before-values** | ⚠️ PARTIAL — lifecycle only | `CharacterLifecycleChangePayload.BeforeStatus` is real (`internal/world/payloads.go:347-351`, set at `:504`). But `CharacterProfileUpdateChangePayload` (`:358-375`) is explicitly **new-values-only** — it records changed attribute *names* and "never the values themselves". A profile edit therefore carries **no** before-value. This is a deliberate privacy decision (D-103's prose exception), not an oversight, but the criterion as written is not fully satisfied by it |
| The `events_audit` row is **projected** from that envelope | ✗ NOT MET | Independently confirmed, not taken from the SUMMARY: the relay's publisher is `NewJetStreamPublisher` (`internal/world/setup/relay_subsystem.go:94` → `internal/eventbus/publisher.go:395-400`); the only writer of `App-Rendering` is `RenderingPublisher` (`internal/eventbus/rendering_publisher.go:84-103`, and `publisher.go:335-338` says so explicitly); `writeAuditRow` refuses a message without it (`internal/eventbus/audit/projection.go:328-331`). Pinned by `TestARelayedWorldEnvelopeCarriesNoRenderingMetadata` (`internal/world/outbox/taxonomy_test.go:240-261`), **which I ran: PASS**. No positive projection assertion exists anywhere in the phase — the only two `events_audit` assertions are `assert.Zero` |
| The audit projection is the **only writer** to `events_audit` | ✓ MET | `TestOnlyTheAuditProjectionInsertsIntoEventsAudit` (`test/meta/world_sql_fence_test.go:664`) with three fail-first negative controls (`:762,788,810`), and a two-file allowlist whose second entry (`retention_partitions.go`) the executor found and justified rather than suppressed |

**Verdict on criterion 3: PARTIALLY MET.** The executor's claim is accurate in both directions — it did not overclaim the transactional half and did not paper over the projection half. `INV-WORLD-9` is worded to claim only what is proven, and the boundary is pinned by a test that goes RED if anyone closes it. That is the right handling of an unclosable gap; it does not make the criterion met.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `api/proto/holomush/adminportal/v1/adminportal.proto` | The wire contract, no delete, no streams | ✓ VERIFIED | 555 lines; `:106-107` records the deliberate absence of `AdminDeleteCharacter` |
| `internal/grpc/admin_interceptor.go` | The single fail-closed gate | ✓ VERIFIED | 291 lines; four refusal arms + exhaustive switch with denying default |
| `internal/admin/section/registry.go` | Seven entries, closed status vocabulary | ✓ VERIFIED | `:118-124` |
| `internal/admin/section/descriptor.go` | Method→section table, no zero value means allow | ✓ VERIFIED | 212 lines; validated at boot |
| `internal/admin/section/boot.go` | Boot abort on a malformed registry | ✓ VERIFIED | Called from `internal/bootstrap/setup/subsystem.go:156` — a real production path, not a unit-test-only validator |
| `internal/grpc/admin_characters_read.go` | Three gated read RPCs | ✓ VERIFIED | 433 lines; `:110,:137,:178` |
| `internal/grpc/admin_characters_write.go` | Three gated write RPCs + 13-path mask | ✓ VERIFIED | 536 lines; `:253,:358,:389` |
| `internal/world/postgres/character_repo_admin.go` | Bounded admin projection, closed ORDER BY | ✓ VERIFIED | 326 lines; closed sort switch, bound parameters. WR-04 open on the username arm |
| `internal/store/migrations/000057_…sql` | Trigram indexes for the two searched columns | ✓ VERIFIED | Plain (non-concurrent) `CREATE INDEX`, transaction-safe, extension not re-declared |
| `internal/web/admin_handlers.go` | Eight gateway proxies, no gateway decision | ✓ VERIFIED | 343 lines; `:314` records why there is no `WebAdminDeleteCharacter` |
| `internal/access/policy/seed.go` | `seed:admin-section-access` + `seed:admin-character-administration` | ✓ VERIFIED | `:987` and `:1034` |
| `test/meta/world_sql_fence_test.go` | events_audit single-writer fence | ✓ VERIFIED | 424 lines added; fail-first controls present |
| `test/meta/admin_character_message_role_fence_test.go` | Roles excluded from the admin messages | ✓ VERIFIED | Reflection walk with an explicit anti-vacuity `require.NotEmpty` |
| `web/src/**` admin surface | — | n/a — **Phase 06.1** | `ROADMAP.md:576` (`UI hint: no`), `:598-633` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/holomush/sub_grpc.go:429` | `internal/grpc/admin_interceptor.go:159` | `NewAdminSectionInterceptor(Engine, SessionRepo)` | ✓ WIRED | Built from the one factory that "refuses to build without the admin gate" |
| `cmd/holomush/sub_grpc.go:913` | `AdminPortalServer` | `RegisterAdminPortalServiceServer` | ✓ WIRED | Reader = `charRepo`, profile reader = `propertyRepo`, **writer = `worldService`** (not a repo) |
| `cmd/holomush/gateway.go:322` | `internal/grpcclient/client.go:101` | `web.WithAdminPortalClient(grpcClient)` | ✓ WIRED | Gateway holds a client, never a service |
| `internal/bootstrap/setup/subsystem.go:156` | `section.ValidateAtBoot` | startup step, non-nil aborts | ✓ WIRED | EXT-03's "fails at boot" is a real call site |
| Admin write handler | `world.Service` checkAccess on `character:<id>` | `s.characterWriter.*` | ✓ WIRED | Second gate below the interceptor; satisfied only by `seed:admin-character-administration` |
| Admin envelope | `events_audit` row | outbox relay → audit projection | ✗ **NOT WIRED** | The link is severed at the `App-Rendering` header — see criterion 3 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `AdminListSections` | section list | `section.All()` + per-entry admission filter | Yes | ✓ FLOWING |
| `AdminListCharacters` | `page.Rows` | `AdminListCharacters` → real SQL with joined `players.username` + separate scalar count | Yes | ✓ FLOWING (asserted at the wire, `admin_characters_read_test.go:128`) |
| `AdminGetCharacter` | `row` | `AdminGetCharacterRow` repository projection | Yes | ✓ FLOWING |
| `WebCheckSessionResponse.roles` | `Roles` | `navRolesFor` → real `playerRoleLookup` | Yes | ✓ FLOWING (proven load-bearing by the differential test's precondition) |
| audit trail for an admin mutation | `events_audit` rows | outbox envelope → relay → projection | **No** | ✗ DISCONNECTED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| The criterion-3 boundary is real, not narrative | `task test -- -run 'TestARelayedWorldEnvelopeCarriesNoRenderingMetadata' ./internal/world/outbox/` | `✓ internal/world/outbox (1.406s)` — DONE 1 test | ✓ PASS (confirms the projection half is unreachable) |
| Build / full unit suite / lint / lint:proto | — | green per phase gates | ✓ PASS (established, not re-run) |
| Integration: access (35 specs), charname (24/24), world | — | green per phase gates | ✓ PASS (established, not re-run) |

### Probe Execution

No `scripts/*/tests/probe-*.sh` are declared by any phase-6 plan and none exist for this surface. Step 7c: SKIPPED (no phase-declared probes).

### Requirements Coverage

| Requirement | Source Plan | Status | Evidence |
|-------------|-------------|--------|----------|
| ADMIN-01 — `/admin` RoleAdmin-gated via ABAC, never `PlayerHasRole`, never a route-guard | 06-01 | ✓ SATISFIED | `gate.go:161-192`; `admin_interceptor.go:171-255`; `sub_grpc.go:429` |
| ADMIN-02 — every admin RPC re-asserts its gate through one shared helper, typed `DENY_*` | 06-01 | ✓ SATISFIED | Shape moved from per-handler call to a structural interceptor (D-99) — *stronger* than the literal wording: forgetting a declaration denies (`ADMIN_SECTION_NOT_DECLARED`), and `TestEveryServedAdminMethodHasADescriptor` proves no served method escapes |
| ADMIN-03 — admin can list/search characters, view detail, edit fields | 06-04 + 06-05 (server); 06.1 (UI) | ⚠️ SATISFIED SERVER-SIDE, traceability wrong | All three read RPCs + all three write RPCs exist, gated and wired. But the requirement **straddles**: `REQUIREMENTS.md:354/355/395` routes it to 6.1 *only*, while `ROADMAP.md:566` lists it under Phase 6, and commit `d2391fc83` (a phase-6 plan) checked its box. Search has WR-04 open |
| ADMIN-04 — field-mask allowlist that excludes roles | 06-05 | ✓ SATISFIED | 13 exact-string paths; role fence walks generated descriptors |
| ADMIN-05 — admin disable reuses player retire's lifecycle states; `DeleteCharacter` never wired to a player-facing button | 06-05 | ✓ SATISFIED | Canonical `world.Service.RetireCharacter` is the single command; no delete RPC in any proto; `delete` omitted from the seed policy. Note: that command currently has exactly one production caller (the admin path) — v0.13 ships no player-initiated retire RPC, so parity holds by *construction* rather than by two callers |
| ADMIN-06 — audit envelope in-transaction with before-values and acting player; row projected by the audit projection, the only writer | 06-05 | ✗ **BLOCKED (partial)** | In-transaction ✓, acting player ✓, single-writer fence ✓, before-values ⚠️ (lifecycle only), **projection ✗**. Checkbox at `REQUIREMENTS.md:207` is `[x]` and should not be while lines 209-211 do not hold |
| ADMIN-08 — roles for nav hiding only, never the authorization boundary | 06-02 | ✓ SATISFIED | `auth_handlers.go:793,817`; differential proof at `admin_section_gate_test.go:284` |
| EXT-01 — seven entries, one available + six planned | 06-01 | ✓ SATISFIED | `registry.go:118-124`; `registry_test.go:44,56,249` |
| EXT-02 — six deferred sections registered, role-gated, `NOT_IMPLEMENTED` *after* the gate | 06-02 | ✓ SATISFIED | Wire proof with counter assertions, `admin_section_gate_test.go:180-259` |
| EXT-03 — descriptor with no default and no zero value meaning allow; fails at compile time or boot | 06-01 | ✓ SATISFIED | `validateEntries` + `validateAdminDescriptors` via `ValidateAtBoot`, called from `subsystem.go:156` |
| EXT-04 — meta-test asserts set equality between registry and descriptor set | 06-01 | ✓ SATISFIED | `descriptor_test.go:74-101` (both sets anti-vacuity-guarded); `registry_test.go:249`, `:68` |
| ADMIN-07 | — | n/a — Phase 06.1 | Correctly unchecked at `REQUIREMENTS.md:213` |

**Orphaned requirements:** none. `REQUIREMENTS.md:354` maps exactly the ten this phase owns; the eleventh (ADMIN-03) is the straddle described above, and it *is* claimed by phase-6 plans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | `TBD` / `FIXME` / `XXX` across all 64 non-generated files changed in the phase | — | **None found.** Clean |
| — | — | `TODO` / `HACK` / `PLACEHOLDER` across the same set | — | **None found.** Clean |
| `internal/world/postgres/character_repo_admin.go` | 189-195 | One transformed parameter bound to two differently-transformed columns (WR-04) | ⚠️ Warning | Search silently returns empty for non-ASCII usernames; recorded as a gap |
| `internal/admin/section/descriptor.go` | 82 | Exported mutable global mutated in place by a test (WR-05) | ℹ️ Info | Test-isolation hazard; no production impact |
| `internal/grpc/admin_characters_read.go` | — | Unbounded `page` / 32-bit offset overflow (IN-08) | ℹ️ Info | From 06-REVIEW.md, still open |

06-REVIEW.md's WR-01 (ungated streaming RPC) and WR-02 (cleartext transport downgrade) were both fixed in `202bd34dd` and I confirmed the guards are live: `TestTheAdminPortalServiceServesNoStreamingMethods` (`descriptor_test.go:126`) and the `NewGRPCServer` refusal path referenced from `sub_grpc.go:429-438`.

## Adjudication of the four contested claims

**1. Criterion 3's projection half — the claim is CORRECT.** Verified independently of the SUMMARY by reading all three links in the chain and running the pin test. Criterion 3 is **PARTIALLY MET**: transactional ✓, acting-player ✓, single-writer ✓, before-values ⚠️ (lifecycle only — profile edits are new-values-only by design), **projection ✗ and unreachable**. The executor's handling was correct — `INV-WORLD-9` claims only what is proven, the two tests that would have asserted the false property were *not* written, and the boundary is pinned so anyone who closes it is pointed at the contract they newly satisfy. What is missing is escalation: no GitHub issue, no WINDOWS.md row. An architectural decision recorded only in one SUMMARY's prose will not survive the milestone.

**2. ADMIN-03's traceability is WRONG, and the error is in both directions.** `REQUIREMENTS.md:395` routes ADMIN-03 to Phase 6.1 only, and `:354` excludes it from Phase 6's count — but `ROADMAP.md:566` lists ADMIN-03 in Phase 6's Requirements line, and commit `d2391fc83` (plan 06-04, a phase-6 plan) is what checked its box at `:197`. The requirement genuinely straddles: "list and search characters, view character detail" is delivered by 06-04's three read RPCs and "edit character fields" by 06-05's three write RPCs, all in phase 6; the operator-visible half (the table, the Sheet) is 06.1's. **It should read `Phase 6 + 6.1`**, with the phase-6 half marked delivered and the row not closed until 06.1 ships. As it stands the box is `[x]` while the phase the table says owns it has not run.

**3. The traceability table was NOT written — for ALL TWELVE rows, not five.** `REQUIREMENTS.md:393-404`: every phase-6 and phase-6.1 row still reads `Pending`. The checkboxes were written correctly (ADMIN-01/02/03/04/05/06/08 and EXT-01..04 are `[x]`; ADMIN-07 correctly `[ ]`), across four commits — `8fb9b1057`, `a3b97f27f`, `d2391fc83`, `57353edf8`. So the `table_unmatched` writer gap is wider than WINDOWS #25/#26 record: those rows name five requirements (ADMIN-01/02, EXT-01/03/04), but ADMIN-04, 05, 06 and 08 are equally unwritten. **Ten rows are Pending despite delivery**; ADMIN-03's is Pending-and-misrouted; ADMIN-07's Pending is correct. The plans' decision not to hand-edit a tool-owned parsed artifact was right (`.claude/rules/planning-artifacts.md`) — the fix belongs upstream in `requirements.mark-complete`.

**4. The dead `read` arm removal is SAFE — confirmed.** `seed.go:1034` now reads `action in ["write", "retire", "unretire"]`. Nothing in the phase depended on `read`: all three admin read RPCs go through `AdminGetCharacterRow` / `AdminListCharacters` / `AdminSearchCharacters` on `internal/world/postgres/character_repo_admin.go`, and `rg 'checkAccess|Evaluate\('` over that file and `internal/world/character_admin.go` returns **zero** — the admin read path evaluates no world-layer policy at all. The three surviving actions are exactly what `AdminCharacterWriter` exposes (`admin_characters_write.go:337,369,400`). The narrowing also closed a real latent widening: a `read` permit for a player principal would have pre-authorised `world.Service.GetCharacter`, whose projection returns `PlayerId` and `LocationId` — the fields D-75 narrowed away — for the first player-flavoured read caller added later, with no test able to catch it.

## Outstanding items from the phase ledgers

| Source | Item | State |
|--------|------|-------|
| `deferred-items.md` §1 | `test/integration/charname` RED from migration 000057 | **RESOLVED, ledger stale.** Fixed in `313be9e22` — the fixture now asserts the withheld version rather than an upper bound (`name_uniqueness_test.go:127-134`), and the lane is 24/24 green. `deferred-items.md` and WINDOWS **#29** both still describe it as outstanding and should be closed |
| WINDOWS #23 | 06-01 criterion defect: unsatisfiable `rg otelgrpc` grep | Open — a criterion-authoring defect, not a code defect. No phase-goal impact |
| WINDOWS #24 | 06-01 criterion defect: `NewGRPCServerInsecure` survives in a historical plan doc | Open — correctly not repaired; editing a retired doc to pass a grep is the repair-introduces-defect pattern. No phase-goal impact |
| WINDOWS #25 / #26 | `requirements.mark-complete` table_unmatched | Open — **and understated**; see contested claim 3. Recorded as a gap |
| WINDOWS #27 / #28 | `abac-reviewer` not run by the executors (Task tool disabled) | **SATISFIED** — the orchestrator ran it over the finished surface and it returned READY. Both rows should be marked fixed |
| WINDOWS #29 | charname RED | **SATISFIED** — see row 1. Should be marked fixed |
| 06-REVIEW.md | WR-03, WR-04, WR-05 and IN-01..08 | Open. Only **WR-04** touches a phase success criterion (recorded as a gap); the rest are quality debt |
| 06-05-SUMMARY.md `:194-204` | The criterion-3 projection decision | **Open, and untracked** — no issue, no ledger row. Recorded as a gap |

## Gaps Summary

Phase 6 delivered a genuinely strong trust boundary. Criterion 1's gate is not a helper anyone can forget — it is a table-driven interceptor whose default arm denies, whose declaration table is set-equal to the served method set with anti-vacuity guards on both sides, and whose one structural blind spot (streaming RPCs, which no unary interceptor and no `Methods`-only census can see) is closed by an explicit fence rather than left implicit. Criteria 4 and 5 are proven at the wire with paired positive controls and counter assertions that make a vacuous pass impossible. Criterion 2's server contract is complete: eight RPCs, eight proxies, a thirteen-path exact-string mask, a role fence that walks generated descriptors, and a delete that is absent from the proto *and* from the policy so an RPC-level omission cannot be quietly undone.

Three things stop this being a clean pass.

**The load-bearing one is criterion 3.** Half of it is genuinely proven — an admin mutation's envelope commits or rolls back with its state change, under a real duplicate-key fault, asserted end to end. The other half is not merely untested; it cannot happen. The world outbox relay publishes through a bare `JetStreamPublisher`, only `RenderingPublisher` writes `App-Rendering`, and the audit projection refuses a message without it. So no admin mutation has ever produced an `events_audit` row, and the only two `events_audit` assertions in the phase both assert **zero**. The executor found this empirically, reported it rather than papering over it, worded `INV-WORLD-9` to claim only the proven properties, and pinned the boundary with a test that goes RED the moment anyone closes it — which is the right handling of a gap larger than the plan that found it. But ADMIN-06's checkbox is `[x]` over text that explicitly requires the projection, and the decision it wants ("either the relay gains rendering metadata, or §14 row 9's model for world mutations is restated") exists in one SUMMARY's prose with no issue and no ledger row behind it. That is how an architectural gap becomes invisible at milestone audit.

**The second is bookkeeping that has now failed three phases running.** All twelve phase-6/6.1 traceability rows read `Pending` while ten of their requirements shipped. The plans were right not to hand-edit a tool-owned parsed artifact, and the WINDOWS rows are honest — but they name five requirements when the real count is ten, and ADMIN-03 additionally sits in the wrong phase in two of three places. The fix is upstream in `requirements.mark-complete`, not here.

**The third is WR-04**, the one open review Warning that touches a success criterion: the admin search binds one charname-normalized parameter to both predicate arms, and `players.username` is stored verbatim. For an ASCII corpus it is invisible; for a username in NFD, or carrying a compatibility codepoint, a Cf format rune, or a doubled space, the search returns nothing rather than erroring — precisely the "degrades to empty" failure mode this phase's own review lens was written to catch.

None of these is a reason to redo the phase, and none is a UI absence — the split to 06.1 is respected throughout and phase 6's server contract is what was judged. Criterion 3 is the one that needs a decision before the milestone closes.

---

_Verified: 2026-08-14T21:40:00Z_
_Verifier: Claude (gsd-verifier)_
