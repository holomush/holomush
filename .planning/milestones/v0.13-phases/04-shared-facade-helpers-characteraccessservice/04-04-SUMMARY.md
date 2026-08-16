---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 04
subsystem: api
tags: [grpc, abac, protobuf, connectrpc, privacy, ginkgo, fieldmask]

requires:
  - phase: 01-portal-spec
    provides: "01-SPEC §2.2 per-audience messages, §7.2/§7.3 the twelve prose names and eleven media names, §7.5 absence-not-emptiness, §8.5.1 the conjunction, §8.6 the totality rule, §8.8 the minimum-identity floor, §8.9/§8.10 absence and outage handling, §9.2-§9.5 the owner and mutation surface"
  - phase: 02-abac-schema-vocabulary
    provides: "internal/access/profilevis, the seed:profile-tier-floor-* term-A family, the seed:viewer-property-* term-B twins, seed:profile-reachable"
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: "plan 04-01's CharacterAccessServer, its two narrow dependency interfaces, the D-83 viewer-identity seam, projectPublic, and the Web* proxy shape"
provides:
  - "CharacterAccessServer.resolveVisibleProfile — the one-enumeration / two-views join of the admissibility set and the value source"
  - "projectPublic media routing: profile.image.primary and the ten zero-padded gallery slots, emitted in ascending slot-name order"
  - "Proto messages OwnCharacter, ListMyCharacters{Request,Response}, GetMyCharacter{Request,Response}, UpdateCharacterProfile{Request,Response}, UpdateCharacterDescription{Request,Response}"
  - "Proto RPCs CharacterAccessService.{ListMyCharacters,GetMyCharacter,UpdateCharacterProfile,UpdateCharacterDescription} and their four Web* proxy pairs"
  - "Handler.{WebListMyCharacters,WebGetMyCharacter,WebUpdateCharacterProfile,WebUpdateCharacterDescription} and the matching gateway client leg"
  - "internal/grpc/characteraccess_profile_test.go — 24 unit tests over the real seeded corpus, including the criterion-2 sentinel byte scan"
  - "test/integration/access P7/P8/P9 — full-stack sentinel absence, the system-visibility denial, and the reachability-floor boundary"
affects: [04-05, 04-06, 04-07, phase-05-character-identity-ui]

actuals:
  tokens: 74400
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "One enumeration, two views: a row-id value index and an evaluator input built from the SAME term-B-filtered slice in one pass"
    - "Admissibility set separated from value source — the evaluator names WHICH rows may publish and carries no value to publish"
    - "Divergence between the two views is an error, never a silent skip or a zero-value emit"
    - "Distinctive-sentinel absence assertion over marshaled bytes, each paired with a positive control at a clearing rung"
    - "Floor discovered by probing the rung ladder rather than hardcoded, so the test follows a configuration change"

key-files:
  created:
    - internal/grpc/characteraccess_profile_test.go
  modified:
    - internal/grpc/characteraccess_service.go
    - internal/grpc/characteraccess_projection.go
    - api/proto/holomush/characteraccess/v1/characteraccess.proto
    - api/proto/holomush/web/v1/web.proto
    - internal/web/character_handlers.go
    - internal/web/handler.go
    - internal/grpcclient/client.go
    - cmd/holomush/deps.go
    - cmd/holomush/deps_test.go
    - test/integration/access/character_profile_read_test.go
    - pkg/proto/holomush/characteraccess/v1/**
    - pkg/proto/holomush/web/v1/**
    - web/src/lib/connect/holomush/characteraccess/v1/characteraccess_pb.ts
    - web/src/lib/connect/holomush/web/v1/web_pb.ts
    - site/src/content/docs/reference/grpc-api.md

key-decisions:
  - "VALUES are joined back out of a row-id index over the SAME slice that fed the evaluator. profilevis.Property is {ID, Name} and carries no value, so a projection built from the visible map alone yields a response with the right keys and empty values — a leak-shaped bug that any key-presence assertion would pass."
  - "A row id in the visible map with no row in the index returns Internal. Both sets come from one slice, so a divergence means the evaluator returned something the enumeration never supplied; emitting a zero value is the empty-value regression and skipping silently renders a broken evaluator as a legitimately sparse profile (§8.10)."
  - "Media rows are routed to primary_image / gallery rather than duplicated into the text map. §7.2's twelve prose names and §7.3's eleven media names are disjoint sets, and a media reference in the text map would be rendered as prose by any client walking it."
  - "The determinism claim is scoped precisely: proto.Equal is the real assertion, byte equality is made under MarshalOptions{Deterministic: true}, and the sentinel scan deliberately stays on plain proto.Marshal because byte ABSENCE is not an ordering property."
  - "Two of this plan's own acceptance greps were DEFECTIVE and were corrected rather than satisfied by reshaping the artifact — see 'Acceptance-criteria corrections' below."

patterns-established:
  - "Row-keyed attribute provider for the unit tier: per-row bags are BUILT by abactest.PropertyProvider (so the derived player-keyed peers stay the shipped derivation) and dispatched by property:<id>, because the shipped helper is static and would evaluate every row against one row's name."
  - "Floor discovery by ladder probe: the lowest rung whose read succeeds IS the floor, so raising seed:profile-reachable moves the test instead of turning it red."

requirements-completed: [PROFILE-03, PROFILE-05, PROFILE-10]

coverage:
  - id: D1
    description: "The public profile is built exclusively from the viewer-filtered slice, with both conjunction terms live and values joined back from that same slice by row id"
    requirement: "PROFILE-10"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileCarriesTheStoredValueOfAPermittedAttribute (asserts the VALUE and that listCalls == 1)"
        status: pass
      - kind: other
        ref: "rg -v '^\\s*//' internal/grpc/characteraccess_service.go | rg -o '\\.ListPropertiesByParent\\(' | wc -l == 1; same pipeline for VisibleAttributes == 1"
        status: pass
    human_judgment: false
  - id: D2
    description: "A row id the evaluator admitted but the enumeration never supplied is an Internal error, not a partial success"
    requirement: "PROFILE-10"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileReportsAVisibleRowMissingFromTheEnumerationAsInternal"
        status: pass
    human_judgment: false
  - id: D3
    description: "A field whose tier floor the viewer does not clear is absent from the MARSHALED BYTES, proven with a seeded sentinel and a paired positive control"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileWithholdsABelowFloorFieldFromTheMarshaledBytes (two sentinels, each with a guest-rung control)"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P7 over a real entity_properties row"
        status: pass
    human_judgment: false
  - id: D4
    description: "A blank field and a withheld field are indistinguishable: empty-string and NULL values are omitted from the map, never emitted present-and-empty"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileOmitsAnEmptyValuedRowFromTheProfileMap (two-value map lookup, not an equality against \"\")"
        status: pass
    human_judgment: false
  - id: D5
    description: "The totality rule and exact-whole-string name matching: an unenumerated name is denied rather than defaulted, and profile.image.gallery.0 is a different row from .00"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileDeniesAPropertyNameInNoTierFloorList, #TestGetCharacterProfileMatchesGallerySlotNamesAsExactWholeStrings"
        status: pass
    human_judgment: false
  - id: D6
    description: "An unrecognized viewer-tier token produces NO viewer principal at all, paired with a player-rung positive control through the full handler"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileDeniesAnUnrecognizedViewerTierWithNoPrincipalAtAll"
        status: pass
    human_judgment: false
  - id: D7
    description: "A property row carrying visibility 'system' is denied at every rung; the same row at 'public' publishes"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileDeniesASystemVisibilityRowAtEveryRung"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P8"
        status: pass
    human_judgment: false
  - id: D8
    description: "A viewer exactly at the profile's reachability floor receives name AND pronouns; one rung below receives the uniform not-found-equivalent"
    requirement: "PROFILE-05"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileResolvesNameAndPronounsAtTheSeededReachabilityFloor (floor discovered by probing the ladder)"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_profile_read_test.go#P9 (corpus narrowed to guest+player so a below-floor rung exists at all)"
        status: pass
    human_judgment: false
  - id: D9
    description: "An infrastructure failure in the enumeration returns Internal, never a legitimately sparse profile"
    requirement: "PROFILE-10"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileReportsAnEnumerationFailureAsInternal"
        status: pass
    human_judgment: false
  - id: D10
    description: "The projection is deterministic: two identical calls are proto-equal and encode identically under MarshalOptions{Deterministic: true}; gallery element order is ascending by slot name"
    requirement: "PROFILE-10"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_profile_test.go#TestGetCharacterProfileIsDeterministicAcrossIdenticalCalls, #TestGetCharacterProfileEmitsGalleryEntriesInAscendingSlotOrder"
        status: pass
    human_judgment: false
  - id: D11
    description: "The remaining Phase-4 owner and mutation contract surface is declared, documented, generated and committed in one regeneration cycle"
    verification:
      - kind: other
        ref: "task lint:proto green; task build green; git status --porcelain over pkg/proto, web/src/lib/connect and site/src/content/docs/reference empty after a fresh task proto && task web:generate && task docs:proto"
        status: pass
    human_judgment: false
  - id: D12
    description: "Criterion 3's CONFIGURATION-side clause — that a game configuration cannot raise name or pronouns above the profile's reachability floor — has no enforcing mechanism in v0.13"
    requirement: "PROFILE-05"
    verification:
      - kind: manual_procedural
        ref: "01-SPEC §8.8 records this explicitly; routed to operator review — see 'The unenforced half of criterion 3' below"
        status: pass
    human_judgment: true

duration: 24min
completed: 2026-08-11
status: complete
---

# Phase 04 Plan 04: The full public profile read Summary

**Twelve prose fields and eleven media rows now reach the response through the shipped term-A/term-B conjunction, and a field below the viewer's floor is provably absent from the marshaled bytes — with its value joined back from the same viewer-filtered slice that decided it was admissible, rather than from anywhere else.**

## Performance

- **Duration:** 24 min
- **Started:** 2026-08-11T14:59:44Z
- **Completed:** 2026-08-11T15:23:28Z
- **Tasks:** 3 completed
- **Files modified:** 16 (11 hand-written, 5 generated trees)

## Task Commits

1. **Task 1 (TDD): Compose the viewer-filtered property slice into the public profile** — `c2272e01b`
2. **Task 2 (TDD): Assert absence at the marshaled bytes and exhaustiveness at the tier switch** — `4e328956a`
3. **Task 3: Land the remaining Phase-4 owner and mutation proto messages and their proxies** — `92e484214`

## RED demonstrated (TDD)

Both TDD tasks were written test-first. Task 1's test file was written and run before any production change:

```
=== FAIL: internal/grpc  (0.00s)
FAIL	github.com/holomush/holomush/internal/grpc [build failed]

=== Errors
internal/grpc/characteraccess_profile_test.go:70:15: undefined: characterParentType
internal/grpc/characteraccess_profile_test.go:117:16: undefined: characterParentType
```

## Accomplishments

### The join is the whole point of this plan

`resolveVisibleProfile` calls `ListPropertiesByParent` **exactly once**, with `world.HumanCaller(viewerSubject)`, and walks the returned slice **once**, building two things from it:

- `byID map[string]*world.EntityProperty` — the **value source**;
- `[]profilevis.Property` — the **evaluator input**.

`VisibleAttributes` then returns the **admissibility set**. `profilevis.Property` is `{ID, Name}` and nothing else, so it names *which* rows may publish and carries **nothing to publish**. Values come back out of `byID`.

This distinction is not pedantry. A projection built from the visible map alone produces a response carrying the **right keys with empty values** — and that response satisfies every assertion phrased as "the profile map contains `profile.pronouns`". The value-carriage test therefore asserts the *value*, and it is the reason the index exists at all.

Term B is evaluated twice under this composition (once by the enumeration, once inside the conjunction). `A and B and B` is `A and B`: both gates ship, no verdict changes, and the redundancy costs one policy evaluation. The alternative — enumerating with a non-viewer caller — either requests the system bypass (handing the facade every row) or empties the compile fence of meaning. `rg -n 'SystemCaller' internal/grpc/characteraccess_*.go` returns nothing.

### Absence is proven at the bytes, with paired positive controls

Two package-level sentinels, 44 and 49 characters, hyphenated and uppercase so a match in a marshaled body cannot be framing or a field name:

- `SENTINEL-BIOGRAPHY-BELOW-ANON-FLOOR-4f9c2ba7`
- `SENTINEL-RP-PREFERENCES-BELOW-ANON-FLOOR-1d63e805`

and a third for the full-stack spec: `SENTINEL-INTEGRATION-BIOGRAPHY-BELOW-ANON-FLOOR-9b41ce07`.

Each is seeded **non-empty**, marshaled at the anonymous rung and asserted absent, then marshaled at the guest rung on the identical fixture through the identical path and asserted **present**. Without the second leg the first cannot distinguish "the floor worked" from "nothing was seeded" — the vacuity PORTAL-10 names.

### The determinism claim is scoped rather than overstated

Three distinct properties, asserted three distinct ways, with the distinction written into the source so a later reader does not unify them:

| Property | Assertion | Why this one |
| --- | --- | --- |
| Two identical calls agree | `proto.Equal` | The real claim: no Go map-iteration order reaches the projection |
| Their encodings agree | `proto.MarshalOptions{Deterministic: true}` | Plain `proto.Marshal` promises nothing about `map<string, string>` ordering, so a plain byte comparison could agree by luck |
| A withheld value is absent | plain `proto.Marshal` | Byte **absence** is not an ordering property — no permutation of a message lacking the sentinel can produce bytes containing it |

### The remaining contract surface landed in one regeneration cycle

`OwnCharacter` is a **distinct message**, never `PublicCharacter` with extra fields. Four RPCs and their four `Web*` proxy pairs are declared, every element carrying a Go-grounded doc comment. Plans 04-05 and 04-06 now add Go handlers only.

`expected_version` is a plain `int32` scalar on both mutation requests — not `optional`, not wrapped. §9.4.2 sends absent and explicit zero down the same rejection branch, so proto3's inability to distinguish them costs nothing here.

## The unenforced half of criterion 3 — recorded, not faked

**Criterion 3's configuration-side clause has no enforcing mechanism in v0.13, and this plan does not claim one.**

01-SPEC §8.8 requires that a game configuration **MUST NOT** raise `name` or `pronouns` above the profile's own reachability floor. §8.8 then records, explicitly, that nothing enforces this: the ABAC engine is deny-overrides, so an admin-authored `forbid` row with `source='admin'` beats the seeded permit, and an operator who writes one against `name` or `pronouns` puts them out of reach of a viewer who reached the profile — with nothing in the system objecting.

That failure is **closed** (the viewer sees less, never more), so it is not a disclosure hole, and §8.12 ships no editing surface, so the only way to author such a row in v0.13 is a direct write to `access_policies`. It is nonetheless a guarantee resting on **operator discipline** rather than on a mechanism, and this plan routes it to **manual operator review** rather than to a test.

What the tests here prove is the **facade half only**: on the shipped corpus, a viewer standing exactly at the reachability floor receives both `name` and `pronouns`. Consistent with that scope, **no `// Verifies: INV-PRIVACY-10` annotation was added anywhere** — `rg -n 'Verifies: INV-PRIVACY-10' internal/ test/` returns no match, and `docs/architecture/invariants.yaml` was not touched.

## Acceptance-criteria corrections

Two of this plan's own acceptance greps were defective as written. Both were **corrected rather than satisfied by reshaping the artifact** — the same false-red class plan 04-01 hit with its `player_id`/`location_id` grep, and the failure mode `.claude/rules/references/plan-review-learnings.md` names: the criterion goes red exactly when the plan is followed correctly.

| Criterion as written | Why it is defective | Corrected form | Result |
| --- | --- | --- | --- |
| `rg -n 'SystemCaller' internal/grpc/` returns no match | Three **pre-existing** matches live in `internal/grpc/location_follow.go:189,200,213`, unrelated to the character-access facade and untouched by this plan. The criterion can never return zero, and would not detect a facade regression if it did. | `rg -n 'SystemCaller' internal/grpc/characteraccess_service.go internal/grpc/characteraccess_projection.go internal/grpc/characteraccess_profile_test.go` | **0** |
| `rg -n 'visibility_hint\|hidden_fields\|field_mask_hint\|withheld' <proto>` returns no match | One match, at `characteraccess.proto:27` — inside plan 04-01's doc comment on `GetCharacterProfile`, describing what is deliberately absent (*"so a withheld profile cannot be distinguished…"*). The criterion's intent is that no such **field** is declared; a prose comment is not a field. | `rg -v '^\s*//' <proto> \| rg -c 'visibility_hint\|hidden_fields\|field_mask_hint\|withheld'` — the same comment-stripping idiom the plan's own enumeration-count criterion uses | **0** |

Every other acceptance criterion passed as written, including the enumeration count (`1` for each of `.ListPropertiesByParent(` and `.VisibleAttributes(`), the `profilevis`-unmodified check (`Property` still exactly `{ID, Name}`; still exactly three exported `Evaluator` methods), `int32 expected_version` × 2, `^  rpc ` × 5, `func (h *Handler) Web` × 5, and zero matches for the forbidden Phase-5/6 RPC names.

## Files Created/Modified

**Created**

- `internal/grpc/characteraccess_profile_test.go` — 24 tests under `-run Profile`, driving the real seeded corpus through `abactest.NewSeedEngine` and the real `profilevis.Evaluator`, with only the world reader doubled

**Modified**

- `internal/grpc/characteraccess_service.go` — `resolveVisibleProfile`, `characterParentType`, `codeProfileJoinDivergence`
- `internal/grpc/characteraccess_projection.go` — media routing, `profileGallerySlotNames`, `isProfileGallerySlotName`
- `api/proto/holomush/characteraccess/v1/characteraccess.proto` — `OwnCharacter` and the eight request/response messages, four RPCs
- `api/proto/holomush/web/v1/web.proto` — four `Web*` RPCs and their eight messages, none carrying a session-token field
- `internal/web/character_handlers.go`, `internal/web/handler.go` — four proxies and the widened `CharacterAccessClient`
- `internal/grpcclient/client.go`, `cmd/holomush/deps.go`, `cmd/holomush/deps_test.go` — the gateway client leg (see Deviations)
- `test/integration/access/character_profile_read_test.go` — specs P7, P8, P9 (file now carries ten specs)
- Generated: `pkg/proto/holomush/{characteraccess,web}/v1/**`, `web/src/lib/connect/holomush/{characteraccess,web}/v1/**`, `site/src/content/docs/reference/grpc-api.md`

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The `CharacterAccessClient` widening breaks the gateway client leg and the `GRPCClient` mock**

- **Found during:** Task 3
- **Issue:** The plan's file list for Task 3 names only `internal/web/character_handlers.go`, but the four proxies call `h.characterAccess.<Method>`, which requires the four methods on the `CharacterAccessClient` interface. Widening that interface leaves `*grpcclient.Client` no longer satisfying it, and `cmd/holomush.GRPCClient` (which also declares the character-access RPCs) leaves `mockGRPCClient` incomplete. `task build` fails without all three.
- **Fix:** Added the four forwarding methods to `grpcclient.Client` in the shape its `GetCharacterProfile` sibling already uses (`oops.Code("RPC_FAILED").With("method", …)`), the four signatures to `cmd/holomush.GRPCClient`, and four nil-returning stubs to `mockGRPCClient`. This is the identical deviation plan 04-01 recorded as its own #2; the interface is simply wider now.
- **Files modified:** `internal/grpcclient/client.go`, `cmd/holomush/deps.go`, `cmd/holomush/deps_test.go`
- **Verification:** `task build` exit 0; `task test -- ./cmd/... ./internal/web/` green
- **Committed in:** `92e484214`

**2. [Rule 3 - Blocking] `abactest.PropertyProvider` cannot express a multi-row fixture**

- **Found during:** Task 1
- **Issue:** The plan directs the unit tests to drive the real seeded corpus through `abactest.NewSeedEngine`. That helper's `PropertyProvider` is a `staticProvider` — it returns the **same** attribute bag for every `property:<id>` resource — so a fixture with two rows would evaluate both against one row's `name` and `visibility`, and every per-row assertion in the file would be silently meaningless (and would *pass*).
- **Fix:** A `rowKeyedPropertyProvider` in the test file that dispatches by `property:<id>`. The per-row bags are still **built by `abactest.PropertyProvider`**, so the derived player-keyed peers (§8.5.2, D-27's ALL/ANY directions) stay the shipped derivation rather than a second one written here. `abactest` itself was not modified.
- **Files modified:** `internal/grpc/characteraccess_profile_test.go`
- **Verification:** the per-attribute floor test denies `profile.biography` at anonymous and publishes it at guest — impossible with a static provider
- **Committed in:** `c2272e01b`

**3. [Rule 1 - Scoping] Media names are routed out of the text profile map**

- **Found during:** Task 1
- **Issue:** The plan says to fill `primary_image` and `gallery` from the media rows, and separately that the `profile` map carries the viewer-permitted `profile.*` attributes. A literal reading would put an admitted `profile.image.primary` in **both**, and a client walking the map would render an opaque media handle as prose.
- **Fix:** `projectPublic` routes the eleven §7.3 media names to their own fields and omits them from the map, matching names as **exact whole strings** so `profile.image.gallery.0` and `.00` stay distinct rows. Asserted by the gallery-order test's closing `assert.Empty(t, …GetProfile())`.
- **Files modified:** `internal/grpc/characteraccess_projection.go`
- **Committed in:** `c2272e01b`

### Scope Decisions

**4. Behavior 6's below-floor leg lives in the integration tier, and the unit test says so**

In the shipped corpus the reachability floor is the **bottom** rung, so no rung below it exists to drive; a below-floor viewer can only be produced by narrowing the corpus, which the unit tier's full-corpus `abactest.NewSeedEngine` cannot do. The unit test discovers the floor by probing the ladder (so a configuration change moves it rather than reddening it) and asserts name + pronouns there; spec **P9** carries the below-floor leg, using `profileCorpusStore` to raise the floor to guest so the anonymous rung genuinely falls below it. The unit test's doc comment names P9 by path, so the split is discoverable rather than a silent gap.

**5. No length caps were added to the mutation request fields**

01-SPEC §9.3 attaches "server-enforced length caps (IDENT-02)" to `UpdateCharacterProfile`. This plan does not claim IDENT-02 and §9 supplies no numbers at the point Task 3 transcribes from, so adding `buf.validate` `max_len` constraints now would mean **choosing** values rather than transcribing them — which Task 3 explicitly forbids ("do not choose a field type"). The caps belong to plan 04-06 with its handler. `min_len = 1` was added to the required id and token fields only, matching the shipped `WebGetCharacterProfileRequest` precedent.

**6. `requirements mark-complete` flipped the checkboxes but not the traceability rows**

`gsd-tools query requirements.mark-complete PROFILE-03 PROFILE-05 PROFILE-10` reported `surface: "checkbox", applied: true` for all three and `surface: "traceability", applied: false` for all three, returning `write_set_complete: false`. The three table rows were filled in by hand to `Complete`, matching the shape the tool itself writes elsewhere in the same table (`IDENT-04`, `IDENT-10` both read `| … | Phase N | Complete |`) — a value fill in an existing tool-owned shape, not invented structure.

This is not a one-off: `PROFILE-04` carries the same split today (checkbox `[x]` from plan 04-01, traceability row still `Pending`), so the half-write appears to be consistent SDK behavior in this repo rather than a transient failure. Worth an upstream report; **not** worth a local workaround beyond filling the value.

---

**Total deviations:** 4 auto-fixed (3 × Rule 3, 1 × Rule 1) + 2 recorded scope decisions
**Impact on plan:** No scope creep. Deviations 1 and 2 were required to keep the tree green and the tests non-vacuous respectively; deviation 3 closes a gap between two sentences of the plan; the two scope decisions record where a behavior landed and why a field constraint was withheld.

## Issues Encountered

**A concurrent plan's in-flight edits made the whole-repo lint gate unreadable mid-run, and it was reported rather than fixed.**

Plan 04-09 was executing on the same working tree. Partway through Task 1, `task lint` failed with:

```
internal/world/mutator.go:106:54: undefined: kindCharacterProfileUpdate (typecheck)
```

`internal/world/` is 04-09's exclusive territory. Because `internal/grpc` imports `internal/world`, the breakage transitively blocked a scoped lint of this plan's own package as well. Per the execution contract this was **reported and stepped around, not repaired**: no file under `internal/world/` was modified by this plan (`git show --stat` over all three commits confirms it), every `git add` named explicit paths, and no `git add -A` was issued. 04-09 subsequently landed `757a03205`, after which `task lint` returns exit 0 on the combined tree.

## Known Stubs

None in this plan's code. Two states are **declared but not yet implemented**, both by design and both stated at their site:

- The four new `CharacterAccessService` RPCs answer `Unimplemented` through the embedded `UnimplementedCharacterAccessServiceServer` until plans 04-05 and 04-06 land their handlers. The proxies pass that through unchanged rather than fabricating a response. Recorded in the RPC doc comments, in the proxy block comment, and in `grpcclient`'s.
- `primary_image` and `gallery` remain empty in practice because v0.13 mints no media identifier (§9.7). The **routing** is implemented and tested; there is simply no row for it to route. This is the spec's position, not an unfinished path.

## Threat Flags

None. Every surface added is inside the plan's own threat register: the composition is T-04-17 and T-04-15, the byte-level absence is T-04-02, the tier switch is T-04-09, the totality and `system`-visibility denials are T-04-16, the enumeration outage branch is T-04-04, and the message shapes are T-04-10. The four new RPCs are declared-only and reach no handler in this plan.

## Notes for the Phase Gate

- **`/holomush-dev:review-abac` is not newly triggered by this plan** — no `internal/access/` file was modified and no seed policy was added. The conjunction is consumed exactly as Phase 2 shipped it: `profilevis.Property` still carries exactly `{ID, Name}`, and `*Evaluator` still exposes exactly three methods.
- **Criterion 3's configuration-side clause is routed to manual operator review**, not to a test — see the dedicated section above. `INV-PRIVACY-10` gains no binding here.
- Plans 04-05 and 04-06 add **Go handlers only**; the proto surface, its generated Go and TypeScript, and the API reference are current and idempotent under regeneration.

## Self-Check: PASSED

- `internal/grpc/characteraccess_profile_test.go` verified present on disk.
- All three commit hashes verified present in `git log --oneline --all`: `c2272e01b`, `4e328956a`, `92e484214`.
- Gates re-run on the committed tree: `task test` (exit 0), `task lint` (exit 0), `task lint:proto` (green), `task build` (exit 0), `task test:int -- ./test/integration/access/...` (exit 0), and `git status --porcelain pkg/proto web/src/lib/connect site/src/content/docs/reference` empty after a fresh `task proto && task web:generate && task docs:proto`.
