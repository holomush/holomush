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
