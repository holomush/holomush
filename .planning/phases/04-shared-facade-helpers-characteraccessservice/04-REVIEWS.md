---
phase: 4
reviewers: [codex]
reviewed_at: 2026-08-10T19:43:48Z
plans_reviewed: [04-01-PLAN.md, 04-02-PLAN.md, 04-03-PLAN.md, 04-04-PLAN.md, 04-05-PLAN.md, 04-06-PLAN.md, 04-07-PLAN.md, 04-08-PLAN.md, 04-09-PLAN.md]
source_grounding: true
---

# Cross-AI Plan Review — Phase 4

## Codex Review

## Summary

The plan set is unusually thorough and generally well grounded, especially around privacy-by-omission, transactional outbox routing, generated artifacts, census integrity, and honest treatment of unenforced invariants. However, I found four execution-blocking mismatches in the live code and normative SPEC. The most serious are the proposed authorization for creating profile-property rows, incomplete construction of those rows, loss of property values at the visibility boundary, and substitution of profile reachability for the directory-resource gate. These should be corrected before execution.

## Strengths

- The plans correctly preserve the existing write fence. `world.Service.propertyRepo` is intentionally a read-only `PropertyReader`, while `worldMutator.propertyWriter` holds the write-capable repository ([internal/world/property.go:31](internal/world/property.go:31), [internal/world/mutator.go:151](internal/world/mutator.go:151), [internal/world/fence_test.go:16](internal/world/fence_test.go:16)). Plan 04-09 correctly routes the new mutation through the executor rather than widening the service field.

- Plan 04-09 accurately identifies the atomic registration requirements for a new outbox kind. The current census checks taxonomy declaration, descriptor coverage, and direct `s.mutator` routing in the exported service method ([test/meta/world_envelope_census_test.go:64](test/meta/world_envelope_census_test.go:64), [test/meta/world_envelope_census_test.go:111](test/meta/world_envelope_census_test.go:111), [test/meta/world_envelope_census_test.go:182](test/meta/world_envelope_census_test.go:182)). Keeping those surfaces in one commit is justified.

- The public-profile plan correctly retains the full visibility conjunction. `ListPropertiesByParent` performs only row-keyed `read` filtering ([internal/world/service.go:1395](internal/world/service.go:1395)), while `profilevis.AttributeVisible` separately evaluates tier-floor and row-keyed terms and ANDs them ([internal/access/profilevis/profilevis.go:140](internal/access/profilevis/profilevis.go:140)). Plan 04-04’s insistence on composing both mechanisms is correct.

- The error-opacity testing strategy matches the normative SPEC. The SPEC requires exact wire-level comparison across status, message, and body and rejects chain-walking oops assertions as wire evidence ([01-SPEC.md:2238](.planning/phases/01-portal-spec/01-SPEC.md:2238)). The plans consistently use `status.Code` and `status.Convert`.

- The plans correctly preserve infrastructure failures rather than collapsing them into ordinary denials. The existing property filter aborts on evaluation failure ([internal/world/service.go:1421](internal/world/service.go:1421)), and `profilevis.VisibleAttributes` likewise returns no partial map ([internal/access/profilevis/profilevis.go:193](internal/access/profilevis/profilevis.go:193)).

- Plan 04-08’s descriptor-derived census follows the SPEC’s actual contract: generated descriptors, exact fully-qualified names, set equality, and a symmetric-difference diagnostic ([01-SPEC.md:179](.planning/phases/01-portal-spec/01-SPEC.md:179), [01-SPEC.md:201](.planning/phases/01-portal-spec/01-SPEC.md:201)).

## Concerns

- **HIGH — Plan 04-09 authorizes new property creation against a property resource that does not yet exist.** Task 1 says to call `checkAccess(write, property resource)` before reading or creating the row, relying on `seed:property-owner-write`. That policy permits only when `resource.property.owner == principal.character.id` ([internal/access/policy/seed.go:129](internal/access/policy/seed.go:129)). A new attribute has no property ID or stored owner for the provider to resolve, so the authorization cannot succeed. It also conflicts with the normative mutation table, which gates `UpdateCharacterProfile` on `write` against `character:<id>` ([01-SPEC.md:2016](.planning/phases/01-portal-spec/01-SPEC.md:2016)). Concrete failure: creating the first profile field default-denies or produces an evaluation failure, so IDENT-02 cannot work.

- **HIGH — Plan 04-09 does not define required fields for newly created `EntityProperty` rows.** `EntityProperty` requires an ID, parent identity, value pointer, owner, and valid visibility ([internal/world/property.go:13](internal/world/property.go:13)). The repository passes `p.Visibility` explicitly into the INSERT rather than allowing the database default to apply ([internal/world/postgres/property_repo.go:60](internal/world/postgres/property_repo.go:60)). An empty visibility therefore violates the schema CHECK rather than becoming `public`. More importantly, owner and visibility determine subsequent ABAC behavior. The plan only says “create a row for a name with no row”; it never fixes ID generation, `Owner`, `Visibility`, timestamps, or the game-configured visibility default. Concrete failure: first writes either fail at the database or create rows whose later read/write authorization is wrong.

- **HIGH — Plan 04-04 loses values at the visibility boundary.** `profilevis.Property` contains only `ID` and `Name`; it deliberately has no value field ([internal/access/profilevis/profilevis.go:103](internal/access/profilevis/profilevis.go:103)). `VisibleAttributes` consequently returns only those identity records ([internal/access/profilevis/profilevis.go:199](internal/access/profilevis/profilevis.go:199)). Task 1 then says `projectPublic` should “populate the profile map from the visible set only.” That set cannot supply the values, primary-image payloads, or gallery payloads. Concrete failure: the implementation either cannot populate the response, emits empty values, or reaches back to an unfiltered collection without an explicitly reviewed ID/name join. The plan needs to specify a safe join from the permitted IDs back to the already term-B-filtered `EntityProperty` slice.

- **HIGH — Plan 04-07 substitutes profile reachability for the SPEC’s directory-resource gate.** The normative read-surface table says `ListCharacterDirectory` is gated by a tier floor “on the directory resource” ([01-SPEC.md:1972](.planning/phases/01-portal-spec/01-SPEC.md:1972)). Task 2 instead states that no directory resource is seeded and filters membership by `profilevis.Reachable` for each character. That changes both authorization meaning and configuration behavior: a future directory floor cannot differ from profile reachability, and there is no single authorization decision controlling bulk enumeration. Concrete failure: success criterion 3’s game-configured directory posture is not implemented as specified, and the handler performs a potentially expensive per-character ABAC pass as a substitute.

- **HIGH — Plan 04-02’s ownership-opacity test contradicts live behavior and would fail immediately.** The plan requires malformed ID, repository error, and non-owned ID all to return identical `NotFound`. The existing helper returns `Internal` on `ListByPlayer` failure ([internal/grpc/sceneaccess_service.go:109](internal/grpc/sceneaccess_service.go:109)), and another handler explicitly depends on that distinction for observability ([internal/grpc/sceneaccess_service.go:695](internal/grpc/sceneaccess_service.go:695)). Task 1 simultaneously says to move the body verbatim, so Task 2’s expected behavior cannot pass without reversing the extraction constraint and masking infrastructure failures.

- **MEDIUM — Owner-mutation error mapping conflicts with §9.6.** Plan 04-05 and parts of 04-06 expect a non-owned character to collapse to a not-found-equivalent. The normative error table assigns `CHARACTER_NOT_OWNED` to `PermissionDenied` for owner-audience mutations ([01-SPEC.md:2214](.planning/phases/01-portal-spec/01-SPEC.md:2214)). The shared `ownedCharacter` currently returns `NotFound` for non-ownership, so simply embedding it cannot satisfy the SPEC without facade-specific mapping or a SPEC amendment. Reads and mutations may intentionally differ, but the plans do not express that distinction.

- **MEDIUM — Directory enumeration has no identified service dependency.** `world.Service` exposes `GetCharacter`, location reads, and property reads, but no all-character enumeration method ([internal/world/service.go:825](internal/world/service.go:825), [internal/world/service.go:1199](internal/world/service.go:1199), [internal/world/service.go:1410](internal/world/service.go:1410)). The existing `ListAll` belongs to the auth-facing repository adapter ([internal/auth/character_service.go:38](internal/auth/character_service.go:38)). Plan 04-07 says only to “extend the narrow world reader with the character-enumeration method,” but no such world method exists and it is not listed as a new artifact or modified file. Execution will hit a signature/design decision not covered by the plan.

- **LOW — Plan 04-01’s compile-fence acceptance search checks the wrong symbol spelling.** It asserts that neither narrow interface “names `ListByParent`,” but the intended safe interface must name `ListPropertiesByParent`; this is fine. Task 2 later requires `rg -n 'ListByParent' internal/grpc/` to return nothing. This textual check is brittle because comments or unrelated future code can fail it even while the type fence remains correct. The compiler demonstration is the evidence-bearing check.

## Suggestions

- In **04-09 Task 1**, change the service authorization to the SPEC-defined `write` decision on `character:<id>`. Keep property-row writes behind the mutator, but do not ask the ABAC provider to resolve a nonexistent property. Add a paired denial/permit test for this exact character gate.

- In **04-09 Task 1**, explicitly define construction semantics for new rows:

  - generate the property ID with `idgen.New()`;
  - set `ParentType`, `ParentID`, `Name`, and `Value`;
  - define `Owner` consistently with the character-owner policy;
  - set the game-configured visibility explicitly;
  - define timestamps or document repository-owned stamping.

  Add create/read/update/delete tests that assert those stored fields, not merely row existence.

- In **04-04 Task 1**, specify the projection join: retain `map[propertyID]*world.EntityProperty` from the term-B-filtered slice, run `VisibleAttributes`, then copy values only for returned IDs. Assert that an evaluator cannot introduce an ID absent from the filtered source. Alternatively, extend the narrow visibility result type deliberately, but do not silently re-read raw properties.

- In **04-07 Tasks 1–2**, either implement the directory-resource vocabulary and seeded tier-floor gate required by §9.2, or amend the SPEC through an explicit locked decision before execution. Also name the actual enumeration seam and add its production wiring/file changes to `files_modified`.

- In **04-02 Task 2**, preserve `Internal` for repository failure. Test equality only between malformed/non-owned or nonexistent cases that are intentionally opaque; separately assert that infrastructure failure remains `Internal` and logged.

- In **04-05/04-06**, reconcile `CHARACTER_NOT_OWNED`: mutations should map non-ownership to `PermissionDenied` per §9.6 unless the SPEC is explicitly amended. Do not let the shared scene helper silently dictate a different new API contract.

- Replace brittle `rg`-absence checks with compile- or AST-backed checks only where those checks already form part of the chosen mechanism. Keep `rg` as a diagnostic, not a normative gate.

## Risk Assessment

**Overall risk: HIGH.** The phase architecture is sound and the validation posture is strong, but the current plans contain several blockers on core paths: first-time profile writes cannot authorize or construct valid rows, visible profile values are not carried through the planned projection pipeline, and the directory implements a different authorization model from the normative SPEC. These are localized and repairable in planning, but executing the plans unchanged would either fail compilation/tests or ship behavior that does not satisfy the phase contract.

---

## Consensus Summary

**Single-reviewer cycle.** Only the Codex lane was selected (`--codex`), so there
is no cross-reviewer agreement signal. In place of consensus weighting, every
Codex finding was **independently re-verified against source by the orchestrator**
before being recorded; each verdict below carries the orchestrator's own
`path:line` grounding. Findings that survived that check are treated as
confirmed; the one that did not survive verbatim is annotated with the
correction.

### Verified Strengths

- **The property write fence is preserved by construction.** `world.Service`
  holds a read-only `PropertyReader` (`internal/world/service.go:104`,
  `internal/world/property.go:35-42`) while the write-capable repository lives on
  the executor (`internal/world/mutator.go:152`). 04-09 routes its new write
  through the mutator and explicitly forbids widening the field or touching
  `internal/world/fence_test.go`.
- **Atomic outbox-kind registration is correctly scoped.** The census enforces
  three directions (`test/meta/world_envelope_census_test.go:64`, `:187`, `:212`),
  and 04-09's acceptance criteria name all five surfaces that must land in one
  commit, including the `s.mutator`-routing service method the descriptor names.
- **The §8.5.1 conjunction is not collapsed.** `ListPropertiesByParent` applies
  term B only (`internal/world/service.go:1420-1421`), and
  `profilevis.AttributeVisible` evaluates term A and term B unconditionally and
  ANDs them in Go (`internal/access/profilevis/profilevis.go:157-181`). 04-04
  composes both and forbids collapsing them.
- **Infrastructure failure never degrades to a sparse success.**
  `VisibleAttributes` aborts and returns a nil map on any evaluation failure
  (`internal/access/profilevis/profilevis.go:199-215`); the plans mirror that as
  `codes.Internal`.
- **Wire-opacity assertions follow §9.6.1 rather than the oops chain.** 01-SPEC
  `:2238-2262` rejects `errutil.AssertErrorCode` / `oops.AsOops(...).Code()` as
  wire evidence; the plans consistently use `status.Code` and
  `status.Convert(err).Message()`.

### Confirmed Concerns

Listed most-severe first. Each was re-grounded by the orchestrator.

#### HIGH-1 — 04-02's ownership-opacity test contradicts the code 04-02 Task 1 forbids changing

`04-02-PLAN.md:25` (must_have) and Task 2's behavior list require
`ownedCharacter` to return **the same `NotFound` status and message** for (a) an
unparseable ULID, (b) a repository error, and (c) a non-owned id, "asserted as an
equality between the three messages, not three separate literal comparisons."

Live code returns `codes.Internal` for case (b):
`internal/grpc/sceneaccess_service.go:115-118`. `04-02-PLAN.md:23` (must_have)
simultaneously requires the extraction to be a **byte-identical struct move**
("not a rewrite of the call sites") and `:130` asserts
`git diff -U0 … | rg '^[-+].*s\.(resolveAndGate|ownedCharacter)\('` returns no
match. The two must_haves cannot both hold: Task 2's test fails against Task 1's
mandated output.

Worse, satisfying Task 2 would **regress a deliberate, documented distinction**.
`internal/grpc/sceneaccess_service.go:695-699` carries the comment: *"ownedCharacter
returns Internal on an infra failure (ListByPlayer) and NotFound when the character
is genuinely not owned. Propagate the infra failure as-is for observability; only
the not-owned case collapses to the connection-ownership denial."*

**Failure scenario:** the executor writes Task 2's three-way equality assertion, it
goes RED, and the only way to green it is to mask `ListByPlayer` infrastructure
failure as `NotFound` — silently breaking INV-SCENE-63's observability contract
across all 21 existing `s.ownedCharacter(...)` call sites.

**Fix:** amend `04-02-PLAN.md:25` and Task 2's fifth behavior to a **two-way**
equality between (a) unparseable ULID and (c) non-owned/nonexistent, plus a
separate assertion that (b) repository failure remains `codes.Internal` and is
logged. Nothing else in the plan changes.

#### HIGH-2 — 04-09 gates the profile-attribute write on a property resource that does not exist at create time

`04-09-PLAN.md:203-205` requires `checkAccess` with action `write` "against the
property resource, so the shipped `seed:property-owner-write` is what decides."

That seed permits only `when { resource.property.owner == principal.character.id }`
(`internal/access/policy/seed.go:129-134`). Resolving `resource.property.*`
requires `PropertyProvider.ResolveResource` to fetch the row —
`internal/access/policy/attribute/property.go:97` calls `p.repo.Get(ctx, id)`
and wraps any failure as `PROPERTY_FETCH_FAILED`. For a **new** attribute there is
no row and no id: `access.PropertyResource(propPath)`
(`internal/access/prefix.go:255`) has no argument to take, and the provider would
fail closed rather than permit.

There is no precedent to copy: `world.Service` has **no** property write command
today (`rg 'func \(s \*Service\) .*Propert' internal/world/service.go` yields only
`ListPropertiesByParent` at `:1410`) and `worldMutator` has **no** property
closure.

This also diverges from the normative gate. 01-SPEC `:2018` states
`CharacterAccessService.UpdateCharacterProfile` is gated by **ABAC `write` on
`character:<id>`** — which is exactly what the shipped sibling
`world.Service.UpdateCharacterDescription` does
(`internal/world/service.go:848-850`, via `access.CharacterResource` and
`prefixCharacter`).

**Failure scenario:** the first write of any `profile.*` attribute for a character
default-denies or returns `PROPERTY_ACCESS_EVALUATION_FAILED`; IDENT-02 cannot
work.

**Fix:** change 04-09 Task 1 step 3 to `checkAccess(write, access.CharacterResource(characterID), prefixCharacter)`,
matching 01-SPEC §9.3 and the shipped `UpdateCharacterDescription` precedent. Keep
the row writes behind the mutator. Add a paired permit/deny test on that character
gate. If a per-property gate is genuinely wanted for the **update-existing** path,
state it as a second, additive check and say what the create path uses.

#### HIGH-3 — 04-09 claims a visibility default that does not exist, and never specifies new-row construction

`04-09-PLAN.md:104` and `:273` both instruct the executor to read
"`internal/world/property.go` — `EntityProperty` and **the visibility defaulting
`Create` applies**", and `:273` says the update-existing and empty-string-delete
tests "assert against" it.

`Create` applies **no** visibility defaulting.
`internal/world/postgres/property_repo.go:180-191`:

```go
func applyVisibilityDefaults(p *world.EntityProperty) error {
	if p.Visibility != "restricted" {
		return nil
	}
	...
}
```

It defaults only `VisibleTo` / `ExcludedFrom` for rows **already** marked
`restricted`. `Visibility` itself is passed explicitly into the INSERT
(`property_repo.go:60-65`), so the column's `DEFAULT 'public'`
(`internal/store/migrations/000001_baseline.sql:361`) is **bypassed** and an empty
string violates
`CHECK (visibility IN ('public','private','restricted','system','admin'))`
(`:362`).

The plan likewise never fixes `ID`, `Owner`, `Visibility`, or timestamps for a
newly created row, yet `Owner` is precisely what every property policy keys on
(`seed.go:117`, `:129`).

**Failure scenario:** the first `profile.*` create either fails at the database
CHECK constraint, or lands with `Owner == nil` / an arbitrary visibility, so every
subsequent read and write decision on that row is wrong.

**Fix:** delete the "visibility defaulting `Create` applies" clause from
`04-09-PLAN.md:104` and `:273`. Add an explicit construction contract to Task 1:
`ID` from `idgen.New()` (per the repo's entity-PK convention), `ParentType`
`"character"`, `ParentID`, `Name`, `Value`, `Owner` set to the owning character id
so `seed:property-owner-write` and `seed:property-private-read` resolve, and an
explicit `Visibility`. Have Task 2 assert the stored fields, not merely row
existence.

#### HIGH-4 — 04-07 substitutes per-character reachability for the §9.2 directory gate, on a factually wrong premise

`04-07-PLAN.md:186-191` instructs: *"01-SPEC section 9.2 names the gate as a tier
floor on the directory; **no directory-shaped resource is seeded**, and
reachability is exactly the facet that governs whether a character's profile
resolves for a viewer… **State that reasoning in the handler's doc comment** so a
later reader does not go looking for a directory policy that does not exist."*

The premise is wrong as written. All three of these exist at HEAD:

- `access.CharacterDirectoryResource()` — `internal/access/prefix.go:273`
- resource type `character_directory` — referenced at
  `internal/access/policy/seed_profile_visibility_test.go:683-689`
- `seed:directory-list-characters` — `internal/access/policy/seed.go:575-579`,
  `permit(principal is character, action in ["list_character_directory"], resource is character_directory)`, bound to INV-ACCESS-9

What is *actually* missing is a **viewer-tier** directory floor: the shipped policy
is `principal is character`, and the v0.13 viewer seeds cover only
`resource is property` (`seed.go:651`, `:657`) and `resource is profile`
(`seed.go:679`). So the plan's conclusion is defensible; its stated reason is not —
and the plan instructs enshrining the false statement in a **production doc
comment**.

Substantively, the substitution also changes the authorization model:
01-SPEC `:1972` specifies one tier-floor decision on the directory resource; the
plan performs N per-character `profilevis.Reachable` evaluations instead, so a
future directory floor cannot be configured to differ from profile reachability,
and bulk enumeration has no single controlling decision.

**Failure scenario:** success criterion 3's game-configured directory posture is
not implementable without re-architecting, and the handler carries a comment
asserting a policy family that does exist does not exist.

**Fix:** either (a) seed a viewer-tier `list_character_directory` permit on
`character_directory` mirroring `seed:profile-reachability` and gate the handler on
that single decision, or (b) keep the reachability filter but **correct the
rationale**: say that `character_directory` and `seed:directory-list-characters`
exist but are `principal is character`-scoped, that no viewer-tier directory floor
is seeded in v0.13, and record the deviation from 01-SPEC §9.2 as an explicit
locked decision with a 01-SPEC amendment row.

#### HIGH-5 — 04-04's projection cannot carry attribute values

`04-04-PLAN.md:127` states *"The resulting map is the only source the response is
built from"*, and `:148` *"In `projectPublic`, populate the profile map from the
visible set only."*

`profilevis.Property` carries **only** `ID` and `Name`
(`internal/access/profilevis/profilevis.go:103-115`), and `VisibleAttributes`
returns `map[string]Property` (`:199`). No value, no image payload, no gallery
entry is reachable from that map.

**Failure scenario:** executed literally, `projectPublic` emits a profile map with
correct keys and empty values; the "a row whose value is the empty string is
omitted" rule at `:149` then omits **every** field, and the response silently
carries no profile at all. The alternative improvisation — reaching back to an
unfiltered source — is exactly what criterion 5 forbids.

**Fix:** state the join explicitly in 04-04 Task 1: retain a
`map[propertyID]*world.EntityProperty` built from the **term-B-filtered** slice
returned by `ListPropertiesByParent`, call `VisibleAttributes`, then copy values
only for ids present in the returned map. Add an assertion that an id in the
visible map but absent from the filtered source is an error, not a silent skip.

#### MEDIUM-1 — 04-07 needs a character-enumeration seam that does not exist and is not in `files_modified`

`04-07-PLAN.md:213-215` says only *"Extend the facade's narrow world reader with
the character-enumeration method this handler needs."*

The narrow reader is defined over `world.Service`
(`04-01-PLAN.md:208`: `ListPropertiesByParent` + `GetCharacterDescription`, both
`world.Caller`-taking `world.Service` methods). `world.Service` exposes no
all-character enumeration — only `GetCharacter`
(`internal/world/service.go:825`) and `GetCharactersByLocation` (`:1199`). The
existing directory seam is `auth.CharacterService.ListAll`
(`internal/auth/character_service.go:38-41`), a different type on a different
interface.

`04-07-PLAN.md:7-24` (`files_modified`) lists neither `internal/world/service.go`,
`internal/world/repository.go`, nor `cmd/holomush/sub_grpc.go`, so there is no
place for the new seam or its wiring to land.

**Fix:** name the enumeration seam concretely (a new ABAC-gated
`world.Service.ListCharacters`, or an explicit second narrow interface satisfied
by `auth.CharacterService.ListAll`), state its signature, and add the production
files to `files_modified`.

#### MEDIUM-2 — owner-audience mutation denial conflicts with §9.6

`04-06-PLAN.md:116` and `:123` require a caller who does not own the character to
receive the **not-found-equivalent** on `UpdateCharacterProfile` /
`UpdateCharacterDescription`.

01-SPEC `:2214` assigns `CHARACTER_NOT_OWNED` → **`PermissionDenied`**, scoped to
*"An `owner`-audience **mutation** names a character the calling player does not
own."* The uniform-`NotFound` rule at `:2216` is scoped to
`CHARACTER_PROFILE_NOT_FOUND`, i.e. the **public read** surface, and §9.6's
rationale (`:2226-2229`) is explicitly about profile reachability.

04-05's use of the not-found-equivalent for `GetMyCharacter` reads is a separate
question; the conflict is on the mutation surface.

**Fix:** in 04-06, map non-ownership on mutations to `CHARACTER_NOT_OWNED` /
`PermissionDenied` per §9.6, or amend 01-SPEC §9.6 with a locked decision that
states why owner-audience mutations adopt read-surface opacity — and, if the
latter, note that the embedded `playerGate.ownedCharacter` returns `NotFound`
(`internal/grpc/sceneaccess_service.go:124`) so the facade needs its own mapping
layer either way.

#### LOW-1 — `rg`-absence checks used as normative gates

`04-01-PLAN.md:337` and `04-07-PLAN.md:225` assert `rg -n 'ListByParent' internal/grpc/`
returns no match as evidence the compile fence holds. The check is sound today
(`ListPropertiesByParent` does not contain the substring `ListByParent`), but a
comment or an unrelated future identifier can turn it red while the type fence is
intact. The evidence-bearing check is the deliberate-compile-failure demonstration
at `04-01-PLAN.md:316`. Keep the `rg` as a diagnostic, not the gate.

### Divergent Views

None — single-reviewer cycle. Where the orchestrator's re-verification diverged
from the Codex text, the correction is folded into the finding above (HIGH-4's
premise, and HIGH-2's promotion from "conflicts with the mutation table" to
"unimplementable at create time").

### Not Re-Litigated

The following were supplied to the reviewer as adjudicated and were **not**
challenged with contrary evidence; they stand:
`INV-PRIVACY-10` staying `binding: pending` (01-SPEC `:1855-1867`); no new
Phase-4 exposure audit (D-77, GH #4937); criterion 5 enforced by a narrow-interface
compile fence rather than a lint rule (D-79); `read_description` carrying no
`action_schema.go` registration; and 04-06's facade-side version compare being a
knowing TOCTOU narrowing rather than a guarantee.

---

## Verification coverage (source-grounding pass)

`plan_review.source_grounding` is `true`. Effective authority adapter:
`gsd-tools drift-guard authority --raw` → **`intel`**. Severity mapping under that
authority: `VERIFIED` → none; `MISSING` → needs-acknowledgement (no hard block);
`AMBIGUOUS` → MEDIUM; `UNCHECKABLE` → INFO.

**Scope.** Every symbol cited by `04-01`…`04-09` was enumerated by kind and
resolved against the worktree at `v013-phase-03`. Symbols listed under each plan's
*"Artifacts this phase produces"* section were **excluded** — they are created by
this phase and do not exist at HEAD.

### File paths — 74 distinct citations

All 74 resolve. Of those, 20 are ABSENT at HEAD and every one is named in a plan's
artifacts list (`api/proto/holomush/characteraccess/v1/characteraccess.proto`, the
nine `internal/grpc/characteraccess_*.go` / `player_gate*.go` files,
`internal/web/character_handlers.go`, `internal/world/{mutator,service}_profile_test.go`,
four `test/integration/access/character_*_test.go`, two `test/meta/*_census_test.go`).
The remaining 54 exist. **VERIFIED: 74. MISSING: 0.**

One apparent miss was an extraction artifact, not a plan defect:
`docs/reference/grpc-api.md` is the tail of
`site/src/content/docs/reference/grpc-api.md`, which exists.

### Functions, methods, and types — VERIFIED (quoted `file:line`)

| Symbol | Resolved at |
| --- | --- |
| `access.ViewerSubject` | `internal/access/prefix.go:178` |
| `access.CharacterResource` | `internal/access/prefix.go:201` |
| `access.PropertyResource` | `internal/access/prefix.go:255` |
| `access.CharacterDirectoryResource` | `internal/access/prefix.go:273` |
| `access.ProfileResource` | `internal/access/prefix.go:282` |
| `abactest.NewSeedEngine` | `internal/testsupport/abactest/abactest.go:68` |
| `profilevis.Property` | `internal/access/profilevis/profilevis.go:106` |
| `profilevis.Evaluator.Reachable` | `internal/access/profilevis/profilevis.go:130` |
| `profilevis.Evaluator.AttributeVisible` | `internal/access/profilevis/profilevis.go:157` |
| `profilevis.Evaluator.VisibleAttributes` | `internal/access/profilevis/profilevis.go:199` |
| `profilevis.CodeEvaluationFailed` / `CodeProfileUnreachable` | `…/profilevis.go:73` / `:75` |
| `world.MaxNameLength` / `MaxDescriptionLength` | `internal/world/validation.go:20` / `:21` |
| `world.ValidateDescription` | `internal/world/validation.go:96` |
| `world.Selectable` | `internal/world/lifecycle.go:83` |
| `world.Caller` / `world.HumanCaller` | `internal/world/caller.go:41` / `:88` |
| `world.EntityProperty` | `internal/world/property.go:16` |
| `world.PropertyReader.ListByParent` | `internal/world/property.go:41` |
| `world.CharacterRepository.Update` | `internal/world/repository.go:216` |
| `world.Service.checkAccess` | `internal/world/service.go:233` |
| `world.Service.propertyRepo` (typed `PropertyReader`) | `internal/world/service.go:104` |
| `world.Service.GetCharacter` | `internal/world/service.go:825` |
| `world.Service.UpdateCharacterDescription` | `internal/world/service.go:844` |
| `world.Service.GetCharactersByLocation` | `internal/world/service.go:1199` |
| `world.Service.ListPropertiesByParent` | `internal/world/service.go:1410` |
| `worldMutator.characterWriter` | `internal/world/mutator.go:152` |
| `worldMutator.updateCharacter` | `internal/world/mutator.go:287` |
| `world.WriteCommandDescriptor` / `writeCommands` | `internal/world/mutator.go:72` / `:89` |
| `wmodel.MutationDelta` | `internal/world/wmodel/mutation_delta.go:53` |
| `outbox.AppSchemaVersion` (= 2, plan bumps to 3) | `internal/world/outbox/taxonomy.go:27` |
| `outbox.IsDeclared` | `internal/world/outbox/taxonomy.go:211` |
| `auth.PlayerRepository` | `internal/auth/player.go:175` |
| `auth.PlayerSessionRepository` | `internal/auth/player_session.go:86` |
| `auth.CharacterRepository` | `internal/auth/character_service.go:26` |
| `SceneAccessServer.ownedCharacter` | `internal/grpc/sceneaccess_service.go:109` |
| `SceneAccessServer.resolveAndGate` | `internal/grpc/sceneaccess_service.go:130` |
| `SceneAccessServer.beginDispatch` | `internal/grpc/sceneaccess_service.go:152` |
| `SceneAccessServer.pluginManager` | `internal/grpc/sceneaccess_service.go:56` |
| `Handler.WebListAllCharacters` | `internal/web/auth_handlers.go:298` |
| `webv1connect.WebServiceHandler` | `pkg/proto/holomush/web/v1/webv1connect/…:972` |
| `errutil.AssertErrorCode` | `pkg/errutil/testing.go:15` |
| `errutil.LogErrorContext` | `pkg/errutil/log.go:24` |
| `excludingPolicyStore` / `.removed` | `test/integration/access/profile_public_read_test.go:49` / `:56` |
| `integrationtest.Server.NewSceneAccessServer` | `internal/testsupport/integrationtest/harness.go:1173` |
| `findRepoRoot` (only fn in the helper file) | `test/meta/meta_helpers_test.go:33` |
| `TestWorldEnvelopeCensusMatchesServiceMutatingMethods` | `test/meta/world_envelope_census_test.go:187` |
| `TestGRPCReferenceCoversAllServices` | `test/meta/grpc_api_coverage_test.go:51` |
| `applyVisibilityDefaults` | `internal/world/postgres/property_repo.go:180` |
| `PropertyProvider.ResolveResource` | `internal/access/policy/attribute/property.go:87` |

### Proto / RPC symbols — VERIFIED

| Symbol | Resolved at |
| --- | --- |
| `CoreService.ListAllCharacters` | `api/proto/holomush/core/v1/core.proto:107` |
| `WebService.WebListAllCharacters` | `api/proto/holomush/web/v1/web.proto:187` |

### ABAC seed policies — VERIFIED

| Policy | Resolved at |
| --- | --- |
| `seed:property-owner-write` | `internal/access/policy/seed.go:129-134` |
| `seed:property-private-read` | `internal/access/policy/seed.go:117-122` |
| `seed:directory-list-characters` | `internal/access/policy/seed.go:575-579` |
| viewer term-A floors (anonymous / guest) | `internal/access/policy/seed.go:650` / `:656` |
| profile reachability floor | `internal/access/policy/seed.go:678-680` |

### Explicit `path:line` citations in the plans — VERIFIED

| Plan citation | Confirmed |
| --- | --- |
| `internal/command/types.go:70` (`UpdateCharacterDescription` is plugin ABI) | Yes — the interface method is at `:70` |
| `internal/plugin/hostfunc/cap_property.go:34`, `:140` | Yes — interface method at `:34`, call at `:140` |
| `internal/world/service.go:844-881` (`UpdateCharacterDescription`, no `expectedVersion`) | Yes — signature at `:844`, `char.Version` read at `:852`, handed to `s.mutator.updateCharacter` at `:871` |
| `test/meta/grpc_api_coverage_test.go:51-74` | Yes |

### MISSING — 3 (severity: needs-acknowledgement; no hard block)

These are behaviour claims made by plan prose that source does not support. Each
feeds a HIGH finding above.

1. **"the visibility defaulting `Create` applies"** — `04-09-PLAN.md:104`, `:273`.
   `applyVisibilityDefaults` (`internal/world/postgres/property_repo.go:180-191`)
   defaults only the `visible_to` / `excluded_from` lists, and only for rows
   already marked `restricted`. No `Visibility` defaulting exists. → HIGH-3.
2. **"no directory-shaped resource is seeded"** — `04-07-PLAN.md:187`.
   `character_directory` (`internal/access/prefix.go:273`) and
   `seed:directory-list-characters` (`internal/access/policy/seed.go:575`) both
   exist; only a *viewer-tier* floor on that resource is absent. → HIGH-4.
3. **"the character-enumeration method this handler needs"** on the narrow world
   reader — `04-07-PLAN.md:213`. No such method exists on `world.Service`
   (`internal/world/service.go` exposes `GetCharacter` `:825` and
   `GetCharactersByLocation` `:1199` only), and no production file that could
   carry it is in `04-07`'s `files_modified`. → MEDIUM-1.

### AMBIGUOUS — 1 (severity: MEDIUM)

- **`checkAccess` "against the property resource"** — `04-09-PLAN.md:203`. The
  resource identifier is unresolvable for a create (`access.PropertyResource`,
  `internal/access/prefix.go:255`, takes a property id that does not yet exist)
  and unspecified for a multi-attribute update (one decision, or one per row?).
  → HIGH-2.

### UNCHECKABLE / skipped — and why

- **Third-party and stdlib identifiers** — `status.Code`, `status.Error`,
  `status.Convert`, `codes.{Aborted,Internal,NotFound,PermissionDenied,Unimplemented}`,
  `connect.CodeUnimplemented`, `oops.AsOops`, `proto.Marshal`, `slog.ErrorContext`,
  `require.Equal`, `assert.NotContains`, `t.Fatalf`. Out of repo scope; each is a
  standard member of a pinned dependency in `go.mod`. Severity INFO. **Not treated
  as verified by inspection** — treated as out of scope.
- **Database column references** — `characters.description`, `players.username`,
  `entity_properties.visibility`. Verified structurally against
  `internal/store/migrations/000001_baseline.sql` (the `visibility` column and its
  CHECK are at `:361-362`), but no per-column line audit was performed for the
  other two. Severity INFO.
- **Attribute-name literals** — `profile.pronouns`, `profile.biography`,
  `profile.rp_preferences`, `profile.image.primary`, and the deliberate negative
  `profile.not_in_the_table`. These are policy *data*, not code symbols; the twelve
  §7.2 names were cross-checked against the seeded term-A allow-lists
  (`internal/access/policy/seed.go:651`, `:657`) and match. Severity INFO.
- **Generated TypeScript client surfaces** under
  `web/src/lib/connect/holomush/**` — not enumerated. The only cited live TS file,
  `web/src/lib/scenes/directoryClient.ts`, exists. Severity INFO.
- **`CharacterAccessService.RenameCharacter`** — cited by the plans as a
  *neighbouring* RPC from 01-SPEC §9.3 `:2020`, not implemented in Phase 4 and not
  in any plan's artifacts list. It is a forward reference to a later phase, so it
  is neither MISSING nor produced here. Severity INFO.
- **Codex's own citation of `01-SPEC.md:179` / `:201`** for the census contract was
  spot-checked at the section level only, not line-exact. Severity INFO.

**A clean line in this section never means "nothing was checked."** 74 file paths,
48 Go symbols, 2 proto RPCs, 5 seed policies and 4 explicit `path:line` citations
were resolved individually; 3 MISSING and 1 AMBIGUOUS were found and each is
escalated into a finding above.

---

# Cross-AI Plan Review — Phase 4 · Cycle 2

**Reviewed:** 2026-08-10 · **Reviewer lane:** `codex` (`--codex`, source-grounded) ·
**Plans:** `04-01`…`04-09` at commit `3f06a8447`

Cycle 1 (above) raised 5 HIGH + 3 actionable non-HIGH. All eight were incorporated
into PLAN.md. This cycle audits whether each landed in **executable** plan content
— task steps, behaviors, acceptance criteria — rather than in prose only, and
hunts for defects introduced **by** the fixes.

## Codex Review

## Summary

Cycle 2 resolves all eight cycle-1 findings in executable plan content. The deepest
repairs — HIGH-2 through HIGH-5 — match the live repository mechanisms. However,
two new dependency/wiring gaps prevent the plans from being executable as written:
the public profile path has no dependency capable of resolving authenticated viewer
sessions before Wave 3, and the directory handler has no dependency capable of
evaluating its new directory-resource ABAC gate. I also found a medium-risk
protobuf determinism assertion that tests behavior the default marshaler does not
guarantee. Overall verdict: HIGH risk until the two missing seams are planned
explicitly.

## Cycle-1 disposition audit (Codex verdicts)

| Finding | Verdict | Evidence |
|---|---|---|
| HIGH-1 uniform NotFound masked infra failure | RESOLVED | Live helper returns `NotFound` for malformed/non-owned and `Internal` for `ListByPlayer` failure (`internal/grpc/sceneaccess_service.go:109`, `:114`). `04-02` Task 2 now tests two-way authorization opacity separately from the `Internal` path; acceptance criteria require both the code and the message inequality. |
| HIGH-2 create gated on nonexistent property resource | RESOLVED | `04-09` Task 1 now requires one `checkAccess(write, access.CharacterResource(...), prefixCharacter)` before the read, matching `internal/access/prefix.go:201` and avoiding `PropertyResource`, whose provider requires an existing row. The first-write test starts with zero property rows. |
| HIGH-3 nonexistent visibility default / unspecified row construction | RESOLVED | Repository passes `p.Visibility` straight into SQL (`internal/world/postgres/property_repo.go:60`); the "defaults" helper handles only restricted lists (`:178`). `04-09` now specifies every created field including explicit `Visibility: "public"`, plus stored-field assertions and preservation of ID/Owner/Visibility on update. |
| HIGH-4 false "no directory resource is seeded" premise | RESOLVED | Singleton constructor at `internal/access/prefix.go:271`; character-scoped seed at `internal/access/policy/seed.go:567`. `04-07` Task 2 recognises both, adds a viewer-scoped floor, and tests that the gate precedes enumeration. |
| HIGH-5 visibility result carried no values | RESOLVED | `profilevis.Property` is `{ID string, Name string}` (`internal/access/profilevis/profilevis.go:103`); `world.EntityProperty.ID` is a ULID and `Value` is `*string` (`internal/world/property.go:16`). `04-04` Task 1 builds `byID[prop.ID.String()]` and the evaluator input from the same `ListPropertiesByParent` slice, then uses the evaluator result solely as an admissibility set. D-79 is preserved: the only source remains the narrow world-service interface. |
| MEDIUM-1 missing directory enumeration seam | RESOLVED | `ListAll(ctx) ([]*world.Character, error)` at `internal/auth/character_service.go:38`; production and harness satisfiers at `internal/bootstrap/setup/adapters.go:151` and `internal/testsupport/integrationtest/harness.go:1903`. `04-07` declares and wires the exact narrow seam. |
| MEDIUM-2 mutation denial vs §9.6 | RESOLVED | SPEC rows cited correctly (`01-SPEC.md:2218`, `:2219`, `:2223`). `ListByPlayer` stamps only `CHARACTER_LIST_FAILED` (`internal/world/postgres/character_repo.go:340`), so nonexistent and non-owned ids genuinely collapse before the facade maps mutations to uniform `PermissionDenied`. |
| LOW-1 grep-absence as compile-fence proof | RESOLVED | `04-01` Task 2 makes the recorded compiler failure the evidence-bearing gate and labels grep as diagnostic; `04-07` repeats the distinction. |

## New Concerns

### HIGH — Public viewer identity cannot be resolved in Waves 1–2

`04-01` defines `CharacterAccessServer` with only two dependencies — a narrow world
reader and a profile-visibility evaluator (`04-01-PLAN.md:208-209`) — and a
constructor taking them positionally. It then says `GetCharacterProfile` should
"resolve the viewer tier", but no session or player repository is held and no
resolver dependency is declared.

This becomes an ordering failure in `04-04`, which is wave 2, `depends_on: ["04-01"]`
only, and whose behaviors require guest and player rungs (`04-04-PLAN.md:109`,
`:248`, `:249`). The three authentication repositories do not reach
`CharacterAccessServer` until `04-05` Task 1 (wave 3), where the server embeds
`playerGate` (`04-05-PLAN.md:113`, `:363`).

The existing resolution path proves the dependencies are necessary: `resolveAndGate`
calls the session repository and then `playerRepo.GetByID`
(`internal/grpc/sceneaccess_service.go:130`, `:139`). Public reads cannot reuse
`resolveAndGate` because it *denies* guests (INV-SCENE-64), yet they must still
distinguish anonymous / guest / player.

Consequences: wave 1 can implement anonymous only; `04-04` cannot execute its
guest/player positive controls with the planned server shape; and the
`player_session_token` field on `GetCharacterProfileRequest`
(`04-01-PLAN.md:188`) has no planned consumer until wave 3.

### HIGH — The new directory ABAC gate has no evaluator seam

`04-07` correctly adds `seed:viewer-directory-list-characters` and mandates ONE
decision on `access.CharacterDirectoryResource()` before `ListAll`
(`04-07-PLAN.md:252`). But `CharacterAccessServer` holds no dependency exposing a
raw `Evaluate`.

Its visibility dependency exposes only `Reachable` and `VisibleAttributes`
(`04-01-PLAN.md:209`). `profilevis.Evaluator`'s exported method set is exactly
`Reachable` (`internal/access/profilevis/profilevis.go:130`), `AttributeVisible`
(`:157`) and `VisibleAttributes` (`:199`) — `evaluate` (`:234`) and `check` (`:278`)
are unexported. `04-07`'s own artifacts list adds only
`characterAccessDirectoryReader` and its constructor parameter; no access-engine
interface, parameter, or wiring appears anywhere in `04-01`…`04-09`
(`rg -n 'Evaluate\(|policy\.Engine|types\.AccessRequest'` over the plan set returns
only the prose at `04-07-PLAN.md:252`).

**This defect was introduced by the HIGH-4 fix**: the fix added the gate but not the
means to call it. It is security-sensitive — an executor improvising with
`Reachable` alone recreates exactly the §9.2 substitution HIGH-4 was raised to
remove.

### MEDIUM — The `04-04` join's "exactly one call site" criteria are unsatisfiable as written

`04-04-PLAN.md:209` requires `rg -n 'ListPropertiesByParent' internal/grpc/characteraccess_service.go`
to return "exactly one call site", and `:210` the same for `VisibleAttributes`.
Both symbols are also **declared** in that same file: `04-01-PLAN.md:208-209` puts
`characterAccessWorldReader` and `characterAccessProfileVisibility` at the head of
`internal/grpc/characteraccess_service.go`, and `04-01-PLAN.md:450` confirms both
types and `GetCharacterProfile` live there. Each grep therefore returns at least two
matches — the interface method declaration and the call — before any doc comment is
counted.

The intent (one enumeration, two views) is right; the command cannot express it.
This is the brittle-grep class cycle-1's LOW-1 named, reintroduced by the HIGH-5 fix.

### MEDIUM — Default protobuf marshaling does not guarantee deterministic map bytes

`PublicCharacter.profile` is `map<string, string>` (`04-01-PLAN.md:187`).
`04-04-PLAN.md:38` (a must-have truth) and `:115` (a behavior) require two calls to
`proto.Marshal` to produce identical bytes and treat that as proof that map
iteration order cannot reach the wire.

The pinned implementation documents determinism as **opt-in**:
`google.golang.org/protobuf v1.36.11` (`go.mod:58`) declares
`MarshalOptions.Deterministic` with the doc "Setting this option guarantees that
repeated serialization of the same message will return the same bytes"
(`proto/encode.go:25-40`). Plain `proto.Marshal` establishes no such contract, so
the test can pass accidentally and proves nothing about production ConnectRPC
encoding. (`04-07`'s analogous claim is safe — its response carries a *sorted
repeated* field, not a map. `04-04`'s D-80 sentinel `assert.NotContains` uses at
`:247-249` are also unaffected: byte *absence* does not depend on ordering.)

### MEDIUM — `04-07` declares the seed meta-test file as modified and simultaneously requires it unmodified

`04-07-PLAN.md:16` (`files_modified`) and `:168` (Task 2 `<files>`) both list
`internal/access/policy/seed_profile_visibility_test.go`, while the acceptance
criterion at `:341` requires
`git status --porcelain internal/access/policy/seed_profile_visibility_test.go`
to be **empty**. The executor cannot satisfy both.

Worse, the criterion is diff-shaped and passes vacuously after commit — precisely
the failure mode `04-02-PLAN.md:131` and `:134` diagnose and immunise against
("`git diff --stat` is empty after any commit and so proves nothing about whether
those assertions were edited"). The same vacuous form survives elsewhere and should
be swept in the same edit: `04-04-PLAN.md:213` and `:214` (a verbatim **duplicate**
of one another, an artifact of the HIGH-5 edit), `04-09-PLAN.md:300`, `:301`, `:407`,
`04-08-PLAN.md:134`, and `04-01-PLAN.md:280`.

*Not* in scope of this finding: the regenerate-then-check forms
(`04-01:277`, `04-04:387`, `04-07:146`, `04-05:297`) and the
temporary-change-was-reverted forms (`04-05:226`, `04-08:231`, `:322`) are sound —
they run a generator or a revert first, so a non-empty result is real evidence.

### LOW — `04-02` remains implementation-first

`04-02` Task 1 moves the helpers (`type="auto"`), Task 2 adds their direct tests
(`tdd="true"`). This is in tension with the repo's tests-before-implementation rule.
Mitigating: Task 1 is a byte-identical struct move under method promotion with
existing coverage through `./internal/grpc/` and `./internal/web/`, so there is no
new behavior to drive. Either move the gate tests ahead of the extraction (targeting
`SceneAccessServer` first, then retargeting the same assertions to `playerGate`), or
record the refactor carve-out explicitly in the plan.

## Suggestions

1. **Add a public-viewer resolution seam in `04-01`, before `04-04`.** Either give
   `CharacterAccessServer` the session/player repositories immediately with an
   optional-session resolver that does **not** guest-gate, or declare a narrow
   `characterAccessViewerResolver`. Test all three outcomes in `04-01`: missing
   token → anonymous; guest session → guest with player id; non-guest session →
   player with player id. Keep `playerGate` for the owner surfaces — its guest
   denial makes it unusable for the public read — but do not postpone the
   repositories the public surface needs to wave 3.
2. **Add a directory-gate evaluator dependency in `04-07`.** Prefer a narrow
   consumer interface, e.g. `Evaluate(ctx, types.AccessRequest) (types.Decision, error)`;
   wire the production ABAC engine and the harness engine into
   `NewCharacterAccessServer`; add it to `04-07`'s artifacts list. Add acceptance
   criteria proving deny, evaluation-failure and permit are distinguishable and that
   both deny and failure occur **before** `ListAll`.
3. **Rewrite `04-04:209-210` as source-state checks that can pass.** e.g.
   `rg -o 'r\.ListPropertiesByParent\(' internal/grpc/characteraccess_service.go | wc -l`
   returns 1 (a receiver-qualified call, which the interface declaration cannot
   match), or scope the count with `--no-filename` over the handler function body.
   Count occurrences with `-o | wc -l`, not `-c`.
4. **Narrow the `04-04` determinism claim.** Either marshal with
   `proto.MarshalOptions{Deterministic: true}` and state that the test validates
   logical projection determinism under deterministic test encoding — not production
   wire-byte stability — or compare messages semantically and assert deterministic
   ordering only for the repeated gallery field. Do not claim ordinary protobuf map
   encoding is byte-stable.
5. **Resolve the `04-07` files_modified contradiction** (drop the seed meta-test file
   from `:16` and `:168` if it genuinely is not edited) and replace `:341` with a
   source-state check — e.g. assert `viewerTwins` still holds exactly five entries
   and that the new policy name does not carry the `seed:viewer-property-` prefix.
   Sweep the sibling vacuous forms listed in that finding.
6. **Decide `04-02`'s TDD posture explicitly** — reorder, or record the
   verbatim-refactor carve-out in the plan so it is a decision rather than an
   omission.

## Risk Assessment

**Overall risk: HIGH.** The cycle-1 corrections are substantive and well grounded;
none has regressed. But two new missing dependency seams sit directly on
authorization-critical paths. One prevents the tier model from functioning for
authenticated viewers in the wave where it is tested; the other leaves the new
directory ABAC gate impossible to call without unplanned architecture. These are
execution blockers, not documentation polish. After those are wired — and the
protobuf determinism assertion narrowed — the plan set should be close to
convergence.

---

## Consensus Summary — Cycle 2

**Single-reviewer cycle.** Only the Codex lane was selected (`--codex`), so there is
no cross-reviewer agreement signal. As in cycle 1, every Codex finding was
**independently re-verified against source by the orchestrator** before being
recorded, and the orchestrator ran its own adversarial pass over the fixes. Two
findings below (MEDIUM — unsatisfiable "exactly one call site" greps; MEDIUM —
`04-07` files_modified contradiction) are **orchestrator-originated**, not Codex's.

### Cycle-1 dispositions — orchestrator re-verification

All eight cycle-1 findings are **FULLY RESOLVED**. Each was re-grounded:

- **HIGH-1** — `04-02-PLAN.md:25` states the truth as a **two-way** authorization
  equality; `:26` states the `Internal` truth separately and cites the caller that
  depends on it. Task 2's behaviors at `:167-168` and acceptance criteria at
  `:210-211` carry both. Live code confirms the split: `internal/grpc/sceneaccess_service.go:112`
  (unparseable → `NotFound`), `:115-117` (`ListByPlayer` error → `slog.ErrorContext`
  + `Internal`), `:124` (not owned → `NotFound`), and the depending caller's comment
  and `if status.Code(err) == codes.Internal` branch at `:695-706` — **all citations
  exact**. The `24`/`21` call-site counts in `:23` and `:129-130` are correct at HEAD
  (`rg -o … | wc -l` returns 24 and 21; `-c` agrees, so no one-line-two-calls trap).
  The guest literal count of 5 (`:134`) is correct.
- **HIGH-2** — `04-09-PLAN.md` Task 1 guard 3 gates on
  `access.CharacterResource(characterID.String())` with `prefixCharacter`, copying
  `internal/world/service.go:848-850` **verbatim and exactly** (`resource :=
  access.CharacterResource(...)` at `:848`, `s.checkAccess(ctx, subjectID, "write",
  resource, prefixCharacter)` at `:849`). The rejected alternative is documented with
  correct grounding: `access.PropertyResource` takes a property id
  (`internal/access/prefix.go:255`) and `PropertyProvider.ResolveResource` fetches
  the row and fails closed with `PROPERTY_FETCH_FAILED`
  (`internal/access/policy/attribute/property.go:87`, `:97`, `:100`). The deciding
  policy `seed:player-self-access` is `permit(principal is character, action in
  ["read","write"], resource is character) when { resource.character.id ==
  principal.character.id }` (`internal/access/policy/seed.go:39-42`) — correct for a
  character subject writing its own character. 01-SPEC §9.3 assigns exactly this gate
  at `:2019`.
- **HIGH-3** — the seven-row construction table is present and every cell is grounded.
  `idgen.New()` exists (`internal/idgen/id.go:35`). `applyVisibilityDefaults` returns
  early unless already `restricted` and fills only `VisibleTo`/`ExcludedFrom`
  (`internal/world/postgres/property_repo.go:180-191`). `p.Visibility` is passed
  explicitly into the INSERT (`:60-65`), so the column `DEFAULT 'public'` at
  `internal/store/migrations/000001_baseline.sql:361` never applies and the CHECK at
  `:362` rejects an empty string. `updated_at` is SQL-side `NOW()` (`:56-62`), matching
  the table's `CreatedAt` note. The `Visibility: "public"` choice is justified against
  `seed:viewer-property-public-read` (`seed.go:756-760`, `visibility == "public"`) and
  `Owner` against `seed:property-owner-write` (`:129-133`) / `seed:property-private-read`
  (`:117-121`). The update-preserves-`ID`/`Owner`/`Visibility`/`CreatedAt` rule is stated.
- **HIGH-4** — the false premise is withdrawn in-plan and the correction is grounded:
  `access.CharacterDirectoryResource()` at `internal/access/prefix.go:273`,
  `seed:directory-list-characters` at `internal/access/policy/seed.go:574-579` with
  `SeedVersion: 1` at `:578`. The `SeedVersion: 1` reasoning is **correct**:
  `internal/access/policy/bootstrap.go:91` is exactly
  `if !opts.SkipSeedMigrations && existing.SeedVersion != nil && *existing.SeedVersion < seed.SeedVersion {`
  — a per-policy upgrade trigger, so a brand-new policy has no prior row and ships at 1.
  The seed meta-tests are genuinely unaffected: `TestExactlyTheFiveReadSideRowKeyedPoliciesHaveAViewerTwin`
  scopes on the `seed:viewer-property-` prefix (`seed_profile_visibility_test.go:421-447`),
  which the new name does not carry; `TestSeedPropertyOwnerWriteHasNoViewerTwin`
  (`:452-466`) does sweep every `seed:viewer-` policy but requires only a scoped action
  clause with no `write`/`delete`, which `action in ["list_character_directory"]`
  satisfies; `TestNoPhase2SeedIntroducesACharacterResourceTypePermit` (`:692`) pins the
  `character` resource **type** and its own doc comment states `character_directory` is
  a different type. The twin does not over-grant: it mirrors `seed:profile-reachable`'s
  clearing-set shape (`seed.go:678-681`) and permits one read-like action on one
  singleton resource.
- **HIGH-5** — the two-view join is real and the type bridge is correct.
  `profilevis.Property` is `{ID string; Name string}` (`internal/access/profilevis/profilevis.go:106-115`);
  `world.EntityProperty.ID` is `ulid.ULID` (`internal/world/property.go:16-17`), so
  `byID` keyed on `.String()` matches `Property.ID` exactly.
  `VisibleAttributes` returns `map[string]Property` keyed on `prop.Name` and populated
  only from the caller-supplied slice (`profilevis.go:199-222`), so iterating its values
  and looking `.ID` up in `byID` is sound, and the "id in the visible map, absent from
  `byID` → `codes.Internal`" rule is well-founded. Criterion 5 holds — both views come
  from the single `ListPropertiesByParent` call, the plan bans a second enumeration,
  bans widening `profilevis.Property`, and bans any repository reach. The D-79 compile
  fence is untouched: the narrow interfaces still name no `ListByParent`
  (`04-01-PLAN.md:210-212`, `:278`). The threat register carries the join as T-04-17
  (`04-04-PLAN.md:428`).
- **MEDIUM-1** — `characterAccessDirectoryReader { ListAll(ctx) ([]*world.Character, error) }`
  is the **verbatim** signature of `auth.CharacterRepository.ListAll`
  (`internal/auth/character_service.go:41`). Satisfiers exist without new production
  code: `bootstrapsetup.CharRepoAdapter.ListAll` (`internal/bootstrap/setup/adapters.go:151`),
  already built as `authCharRepo` at `cmd/holomush/sub_grpc.go:446`; and the harness's
  `authCharRepoAdapter.ListAll` (`internal/testsupport/integrationtest/harness.go:1903`).
  The plan's "pass `s.charRepo` in the harness" instruction is correct —
  `Server.charRepo` is typed `auth.CharacterRepository` (`harness.go:138`) and is
  already passed to `NewSceneAccessServer` at `:1181`.
- **MEDIUM-2** — the gate error matrix (`04-02-PLAN.md:223-269`) is the phase's single
  source of truth and its citations are right. 01-SPEC §9.6's table header is at `:2214`;
  `CHARACTER_NOT_FOUND` → `NotFound` at `:2218`; `CHARACTER_NOT_OWNED` → `PermissionDenied`,
  scoped to *"An `owner`-audience mutation"*, at `:2219`; `CHARACTER_PROFILE_NOT_FOUND`
  at `:2223`. **The "no SPEC amendment needed" claim holds.** `CHARACTER_NOT_FOUND` is
  stamped only on by-id fetch/update paths — `internal/world/postgres/character_repo.go:53`,
  `:169`, `:253`, `:288`, `:377`, `:421`, `:527`, an **exact** match for the plan's list —
  while `ownedCharacter` resolves through `ListByPlayer` (`character_repo.go:339-350`),
  whose only stamp is `CHARACTER_LIST_FAILED` at `:345`. A nonexistent id is therefore
  indistinguishable from a non-owned one **by construction**, and the plan explicitly
  scopes the delete-after-authorization race out.
- **LOW-1** — demoted correctly. `04-07-PLAN.md:344-347` reclassifies the
  `rg -n 'ListByParent'` check as "**Diagnostic, not a gate**… a smoke alarm" over the
  deliberate-compile-failure demonstration; `04-01` makes the recorded compiler
  diagnostic the evidence-bearing gate.

### Agreed Concerns (cycle 2) — most severe first

Both HIGHs were raised by Codex and independently confirmed by the orchestrator.

1. **HIGH — no viewer-identity resolution seam before wave 3.** Confirmed:
   `04-01-PLAN.md:208-209` declares exactly two dependencies and `:450` lists exactly
   those two types plus `resolveViewerTier`/`mapProfileError` as the file's artifacts;
   `04-04` is `wave: 2`, `depends_on: ["04-01"]`, and its behaviors at `:109`, `:248`,
   `:249` demand guest and player rungs; the session/player repositories arrive only at
   `04-05` (`wave: 3`) via the embedded `playerGate` (`04-05-PLAN.md:113`, `:363`).
   **Strengthening the finding:** even wave 3 does not obviously supply the seam —
   `playerGate.resolveAndGate` *denies* guests with `PermissionDenied`
   (INV-SCENE-64), and the public read must yield a **guest** rung, so the public path
   needs a resolver that `resolveAndGate` cannot be.
2. **HIGH — the §9.2 directory gate has no evaluator seam, and the gap was created by
   the HIGH-4 fix.** Confirmed: `profilevis.Evaluator` exports only `Reachable`
   (`internal/access/profilevis/profilevis.go:130`), `AttributeVisible` (`:157`) and
   `VisibleAttributes` (`:199`) — `evaluate` (`:234`) and `check` (`:278`) are
   unexported; `04-07`'s artifacts list adds only `characterAccessDirectoryReader`; and
   a plan-set-wide search for `Evaluate(`, `policy.Engine` or `types.AccessRequest`
   returns only the prose instruction at `04-07-PLAN.md:252`. The natural improvisation
   — gate on `Reachable` — reinstates precisely the substitution HIGH-4 removed.
3. **MEDIUM (orchestrator) — `04-04:209-210`'s "exactly one call site" greps cannot
   pass.** `ListPropertiesByParent` and `VisibleAttributes` are *declared* in the same
   file the criteria search (`04-01-PLAN.md:208-209`, `:450`), so each grep returns ≥2.
4. **MEDIUM — plain `proto.Marshal` gives no map determinism guarantee**
   (`04-04-PLAN.md:38`, `:115` vs `google.golang.org/protobuf@v1.36.11 proto/encode.go:25-40`,
   `go.mod:58`; the field is `map<string, string> profile = 4` at `04-01-PLAN.md:187`).
5. **MEDIUM (orchestrator) — `04-07` declares the seed meta-test file modified (`:16`,
   `:168`) while requiring it clean (`:341`)**, on a criterion that is additionally
   vacuous post-commit; plus the sibling vacuous forms at `04-04:213`/`:214` (a verbatim
   duplicate pair), `04-09:300`/`:301`/`:407`, `04-08:134`, `04-01:280`.
6. **LOW — `04-02`'s implementation-then-test ordering** is in tension with the repo's
   TDD rule, mitigated by the move being byte-identical with existing coverage.

### Divergent Views

None — single-reviewer cycle. Where the orchestrator's verification extended a Codex
finding (the guest-denial property of `resolveAndGate` under HIGH #1) or originated a
finding Codex did not raise (#3 and #5), that is marked inline.

### Not Re-Litigated

The five adjudicated positions supplied to the reviewer — `INV-PRIVACY-10` staying
`binding: pending` (01-SPEC `:1855-1867`); no new Phase-4 exposure audit (D-77,
GH #4937); criterion 5 enforced by the D-79 narrow-interface compile fence; no
`action_schema.go` registration for `read_description`; and `04-06`'s facade-side
version compare as a knowing TOCTOU narrowing — were **not** challenged with contrary
evidence and stand.

---

## Verification coverage (source-grounding pass) — Cycle 2

`plan_review.source_grounding` is `true`. Effective authority adapter:
`gsd-tools drift-guard authority --raw` → **`intel`**. Severity mapping under that
authority: `VERIFIED` → none; `MISSING` → needs-acknowledgement (no hard block);
`AMBIGUOUS` → MEDIUM; `UNCHECKABLE` → INFO.

**Scope.** Cycle 1 verified the full symbol corpus (74 file paths, 48 Go symbols, 2
proto RPCs, 5 seed policies). This cycle re-verified **every symbol touched by a
cycle-1 fix**, plus the symbols the new findings turn on. Symbols under each plan's
*"Artifacts this phase produces"* section are excluded — they do not exist at HEAD.

### VERIFIED — symbols and claims introduced or re-cited by the cycle-1 fixes

| Symbol / claim | Cited | Resolved at | Status |
| --- | --- | --- | --- |
| `ownedCharacter` three-branch shape | `04-02:26`, `:190-197` | `internal/grpc/sceneaccess_service.go:112` / `:115-117` / `:124` | VERIFIED |
| depending caller's `Internal` branch + comment | `04-02:26`, `:195` | `internal/grpc/sceneaccess_service.go:695-706` | VERIFIED (exact) |
| 24 `s.resolveAndGate(` / 21 `s.ownedCharacter(` | `04-02:23`, `:129-130` | measured `rg -o … \| wc -l` → 24 / 21 | VERIFIED |
| `guests cannot access scenes` × 5 | `04-02:134` | `rg -o … internal/web/ \| wc -l` → 5 | VERIFIED |
| 01-SPEC §9.6 rows | `04-02:245-250` | `01-SPEC.md:2214` (header), `:2218`, `:2219`, `:2223` | VERIFIED (corrected citations are right) |
| `CHARACTER_NOT_FOUND` stamp sites | `04-02:254-256` | `internal/world/postgres/character_repo.go:53,169,253,288,377,421,527` | VERIFIED (exact set) |
| `ListByPlayer` stamps only `CHARACTER_LIST_FAILED` | `04-02:255-256` | `internal/world/postgres/character_repo.go:339-350`, code at `:345` | VERIFIED |
| `access.CharacterResource` / `prefixCharacter` gate shape | `04-09` Task 1 guard 3 | `internal/world/service.go:848-850` | VERIFIED (exact) |
| `seed:player-self-access` | `04-09` guard 3 | `internal/access/policy/seed.go:38-43` | VERIFIED |
| 01-SPEC §9.3 `write` on `character:<id>` | `04-09` guard 3 | `01-SPEC.md:2019` | VERIFIED |
| `access.PropertyResource` takes a property id | `04-09` rejection rationale | `internal/access/prefix.go:253-259` | VERIFIED |
| `PropertyProvider.ResolveResource` fails closed | `04-09` rejection rationale | `internal/access/policy/attribute/property.go:87`, `:97`, `:100` | VERIFIED |
| `idgen.New()` | `04-09` construction table | `internal/idgen/id.go:35` | VERIFIED |
| `applyVisibilityDefaults` scope | `04-09:196-199` | `internal/world/postgres/property_repo.go:180-191` | VERIFIED |
| explicit `p.Visibility` in INSERT | `04-09:199` | `internal/world/postgres/property_repo.go:60-65` | VERIFIED |
| SQL-side `NOW()` for `updated_at` | `04-09` construction table | `internal/world/postgres/property_repo.go:56-62` | VERIFIED |
| `visibility` DEFAULT + CHECK | `04-09:199-200` | `internal/store/migrations/000001_baseline.sql:361-362` | VERIFIED |
| `seed:viewer-property-public-read` | `04-09` construction table | `internal/access/policy/seed.go:755-760` | VERIFIED |
| `seed:property-owner-write` / `-private-read` | `04-09` construction table | `internal/access/policy/seed.go:128-133` / `:116-121` | VERIFIED |
| `s.mutator` routing shapes | `04-09` "exported Service command" | `internal/world/service.go:340-349`, `:859-871` | VERIFIED |
| `access.CharacterDirectoryResource()` | `04-07:180`, `:238` | `internal/access/prefix.go:271-273` | VERIFIED |
| `seed:directory-list-characters` + `SeedVersion: 1` | `04-07:181-182`, `:246` | `internal/access/policy/seed.go:574-579`, `:578` | VERIFIED |
| per-policy `SeedVersion` upgrade compare | `04-07:243-246` | `internal/access/policy/bootstrap.go:91` | VERIFIED (exact) |
| `seed:profile-reachable` clearing-set shape | `04-07:183-184` | `internal/access/policy/seed.go:677-682` | VERIFIED |
| `viewerTwins` prefix scoping | `04-07:177` | `seed_profile_visibility_test.go:421-447` | VERIFIED |
| `TestSeedPropertyOwnerWriteHasNoViewerTwin` sweeps `seed:viewer-` | `04-07:177` | `seed_profile_visibility_test.go:452-466` | VERIFIED — new seed passes (scoped action, no write/delete) |
| `TestNoPhase2SeedIntroducesACharacterResourceTypePermit` | `04-07:178` | `seed_profile_visibility_test.go:675-692` | VERIFIED — `character_directory` explicitly excluded |
| `auth.CharacterRepository.ListAll` signature | `04-07:304-307` | `internal/auth/character_service.go:38-41` | VERIFIED (verbatim) |
| production + harness `ListAll` satisfiers | `04-07:308-312` | `internal/bootstrap/setup/adapters.go:151`; `internal/testsupport/integrationtest/harness.go:1903` | VERIFIED |
| `cmd/holomush/sub_grpc.go:446` `authCharRepo` | `04-07:309` | exact line | VERIFIED |
| harness `Server.charRepo` is `auth.CharacterRepository` | `04-07:312` | `internal/testsupport/integrationtest/harness.go:138`, passed at `:1181` | VERIFIED |
| `profilevis.Property` is `{ID, Name}` | `04-04:141` | `internal/access/profilevis/profilevis.go:106-115` | VERIFIED |
| `world.EntityProperty.ID` is `ulid.ULID`, `Value *string` | `04-04` join | `internal/world/property.go:16-23` | VERIFIED — the `.String()` bridge is type-correct |
| `VisibleAttributes` returns the admissibility set | `04-04:141-144` | `internal/access/profilevis/profilevis.go:199-222`, keyed on `prop.Name` | VERIFIED |

### MISSING — 3 (severity: needs-acknowledgement; no hard block)

Each feeds a finding above. None is a stale citation; each is a **seam the plans
require but do not declare**, or a guarantee the cited dependency does not make.

1. **A viewer-identity resolution seam on `CharacterAccessServer` before wave 3.**
   `04-01-PLAN.md:208-209`/`:450` declare exactly two dependencies; no session
   repository, player repository, or resolver interface appears until `04-05:113`
   (wave 3). → HIGH #1.
2. **An ABAC-engine seam capable of evaluating the §9.2 directory gate.** No
   `Evaluate`-bearing interface, constructor parameter, or wiring exists anywhere in
   `04-01`…`04-09`; `profilevis.Evaluator` exports no generic `Evaluate`
   (`internal/access/profilevis/profilevis.go:130`, `:157`, `:199`; `:234` and `:278`
   unexported). → HIGH #2.
3. **A determinism guarantee from plain `proto.Marshal`.**
   `google.golang.org/protobuf@v1.36.11 proto/encode.go:25-40` documents
   `MarshalOptions.Deterministic` as the opt-in that carries the guarantee; the default
   path makes no such promise. → MEDIUM #4.

### AMBIGUOUS — 0

### Contradictions found within the plan set — 1 (severity: MEDIUM)

- `04-07-PLAN.md:16` and `:168` list
  `internal/access/policy/seed_profile_visibility_test.go` as modified; `:341` requires
  its `git status --porcelain` to be empty. → MEDIUM #5.

### Unsatisfiable acceptance criteria — 2 (severity: MEDIUM)

- `04-04-PLAN.md:209` and `:210` — the searched symbols are declared in the searched
  file (`04-01-PLAN.md:208-209`, `:450`), so neither grep can return one match. → MEDIUM #3.

### Vacuous acceptance criteria — 8 (severity: MEDIUM, one finding)

Diff/status-shaped criteria whose purpose is to prove a file was **not** edited, which
are empty both before the edit and after any commit: `04-07:341`, `04-04:213`, `:214`
(duplicate pair), `04-09:300`, `:301`, `:407`, `04-08:134`, `04-01:280`. Explicitly
**excluded** as sound: the regenerate-then-check forms (`04-01:277`, `04-04:387`,
`04-07:146`, `04-05:297`) and the revert-then-check forms (`04-05:226`, `04-08:231`,
`:322`). → MEDIUM #5.

### UNCHECKABLE / skipped — and why

- **Third-party and stdlib identifiers** — `status.Code`, `status.Convert`,
  `codes.*`, `proto.Marshal`, `require.Equal`, `assert.NotContains`. Out of repo scope;
  each is a member of a pinned dependency in `go.mod`. The one exception is
  `proto.MarshalOptions`, which was read directly at
  `google.golang.org/protobuf@v1.36.11 proto/encode.go:25-40` because a finding turns
  on its contract. Severity INFO.
- **Generated TypeScript surfaces** under `web/src/lib/connect/holomush/**` — not
  re-enumerated this cycle; unchanged from cycle 1. Severity INFO.
- **Cycle-1's already-verified corpus** — the 74 file paths and the symbol tables above
  were verified in cycle 1 and were not re-walked in full; only symbols a cycle-1 fix
  touched or a cycle-2 finding depends on were re-resolved. Severity INFO.
- **`TestEveryAttributeAnewSeedReferencesIsSuppliedByARegisteredProvider`**
  (`seed_profile_visibility_test.go:933`) iterates a hardcoded `phase02SeedNames` list,
  so the new `seed:viewer-directory-list-characters` is **not** swept into it. This is
  not a break — `principal.viewer.tier` is supplied by
  `attribute.NewViewerTierProvider()` and is already exercised by
  `seed:profile-reachable` — but the new seed inherits no automatic attribute-supply
  guard. Noted, not raised. Severity INFO.

**A clean line in this section never means "nothing was checked."** Thirty-three
symbols/claims were individually re-resolved this cycle; three MISSING seams, one
intra-plan contradiction, two unsatisfiable criteria and eight vacuous criteria were
found, and each is escalated into a finding above.
