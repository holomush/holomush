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

---

# Cross-AI Plan Review — Phase 4 · Cycle 3 (final)

**Reviewer:** Codex (`--codex`), source-grounded (`plan_review.source_grounding: true`), plus an
independent orchestrator re-grounding pass.
**Reviewed at:** 2026-08-10
**Plans reviewed:** 04-01 … 04-09 (all nine), with `04-CONTEXT.md`, `04-PATTERNS.md`,
`04-RESEARCH.md`, `.planning/REQUIREMENTS.md` and the ROADMAP Phase-4 section in prompt.
**Loop position:** cycle 3 of 3. Trend 8 → 6 → **2**.

## Cycle-2 disposition audit

Every cycle-2 finding was re-grounded against source, not against the plan's own claim.

| # | Cycle-2 finding | Verdict | Evidence |
|---|---|---|---|
| HIGH-A | No viewer-identity seam before wave 3 | **FULLY RESOLVED** | `04-01:243` places both auth repositories on the constructor; `04-01:252-282` declares `viewerIdentity` + `resolveViewerIdentity`; `04-01:331-345` updates **both** wiring sites and `04-01`'s `files_modified` lists `cmd/holomush/sub_grpc.go` and `internal/testsupport/integrationtest/harness.go`. The dependencies are genuinely in scope: `policyEngine := s.cfg.ABAC.Engine()` at `cmd/holomush/sub_grpc.go:352`, `authPlayerRepo`/`authPlayerSessionRepo` at `:356-357`, harness `accessEngine: pe` at `harness.go:876`. The deliberate non-use of the guest-denying gate is real: `sceneaccess_service.go:144-146` returns `PermissionDenied` for `player.IsGuest`. |
| HIGH-B | §9.2 directory gate had no evaluator seam | **FULLY RESOLVED** | `04-07:270` declares the one-method seam; `04-07:288-291` names both wiring files; `04-07:170` carries both in `<files>`. The signature matches `(*policy.Engine).Evaluate` and is byte-identical to the shipped precedent `profilevis.PolicyEvaluator` at `internal/access/profilevis/profilevis.go:99-101`. The DENY-with-`infra:`-id-and-**nil**-error shape is real (`policytest/helpers.go:120-131`) and the handling precedent is `profilevis.evaluate` (`:234-273`). |
| MEDIUM-C | `04-04:209-210` unsatisfiable | **FULLY RESOLVED** | Receiver-qualified and comment-filtered at `04-04:232`; counts call expressions, not declarations. |
| MEDIUM-D | plain `proto.Marshal` map determinism | **FULLY RESOLVED** | Primary claim moved to `proto.Equal`; byte equality scoped to `proto.MarshalOptions{Deterministic: true}`; the criterion-2 sentinel scan's retention of plain `Marshal` carries its stated reason. |
| MEDIUM-E | `04-07` seed meta-test contradiction + vacuous criterion | **FULLY RESOLVED** | The file is read-only in `04-07`; `:427` is a source-state check; the seven sibling vacuous forms across `04-01/04-04/04-08/04-09` are swept. |
| LOW-F | `04-02` implementation-first | **FULLY RESOLVED** | `04-02:121` records the verbatim-refactor carve-out as a bounded decision with its three grounds and requires the pre-existing suite green before new assertions. |

**Post-cycle-2 corrections, re-verified:**

| Correction | Verdict | Evidence |
|---|---|---|
| `SeedVersion: 7` → `1` | **VERIFIED** | `04-01:356` asserts `SeedVersion: 1`; propagated to `04-PATTERNS.md:348` and `:562`. `internal/access/policy/bootstrap.go:91` — `*existing.SeedVersion < seed.SeedVersion` — is a **per-policy** compare, so a brand-new policy correctly starts at 1. |
| `sceneaccess_service.go:144-146` guest denial | **VERIFIED** | `if player.IsGuest {` at `:144`, `status.Error(codes.PermissionDenied, "guests cannot access scenes")` at `:145`. |
| `profilevis.go:99-101` | **VERIFIED** | `type PolicyEvaluator interface {` `:99`, `Evaluate(...)` `:100`, `}` `:101`. |
| `04-07` `NewInfraFailureEngine` two-method rationale | **VERIFIED** | `policytest/helpers.go` declares `Evaluate` and `CanPerformAction`; the two-method type satisfies the one-method seam, and `04-07:205` now says exactly that. |
| `04-05:179` struct-shape check | **VERIFIED** | Rewritten to `rg -n -A 12 'type CharacterAccessServer struct'` asserting a bare embedded `playerGate` and no duplicate direct fields, with the reason the occurrence count was false stated inline. |
| `04-02` guest literal "five occurrences across two files" | **VERIFIED** | `internal/web/scene_handlers_test.go` and `internal/web/status_interceptor_test.go`. |
| `harness.go:1197` accessor correction | **NOT PROPAGATED** (non-blocking) | `04-01:344` and `04-07:287` still cite `AccessEngine()` at `:1191`. `:1191` is that method's **doc-comment first line**; the `func` is at `:1197`. Harmless to an executor — recorded, not raised as a finding. |

## Concerns

### HIGH-1 — The routing census cannot see the mutations' ownership check, and `04-06` asserts that it can

`04-06:190-200` requires both mutation handlers to call a new unexported helper
`ownedCharacterForMutation`, which in turn calls the embedded `s.ownedCharacter` and rewrites
its `NotFound` into `CHARACTER_NOT_OWNED`/`PermissionDenied`. `04-06:200` then claims
"04-08's routing census [is] untouched — the helper routes THROUGH the gate".

That claim is false against `04-08`'s own predicate. `04-08:187-193` defines membership as:
"a method routes … through ownership when it likewise references the ownership method name"
**as a selector on its own receiver**. `UpdateCharacterProfile` and `UpdateCharacterDescription`
will reference `s.ownedCharacterForMutation`, not `s.ownedCharacter`. The predicate inspects the
handler's own body and does not follow the call — the same structural limitation `04-09:104`
documents for the world-envelope census's `bodyReferencesSelector` detector.

Consequences, all bad:
- If both mutations are in the checked-in expected ownership set, the census is **permanently RED**
  even when the code is correct.
- If they are omitted to make it green, criterion 1's set-equality proof silently **excludes the two
  write surfaces** — the highest-risk RPCs in the phase.
- A later edit deleting `s.ownedCharacter` from the wrapper leaves both mutations ungated with the
  census still green.

`04-08` compounds this by never naming the members of its "checked-in expected set" — no plan text
lists `UpdateCharacterProfile`/`UpdateCharacterDescription` as expected members, so the conflict is
invisible until execution. Its RED demonstration (`04-08:218-221`) also exercises only the
**guest-gate** predicate ("temporarily delete the guest-gate call"), never the ownership predicate.

**PLAN.md change needed (`04-08`, with a one-line cross-reference in `04-06`):**
1. Derive and assert exactly one approved wrapper, `ownedCharacterForMutation`, and assert its own
   body directly references `s.ownedCharacter`.
2. Count an exported handler as ownership-routed when it references **either** `ownedCharacter`
   **or** that verified wrapper as a selector on its own receiver.
3. Enumerate the expected owner-audience set explicitly in `04-08`, including both mutations.
4. Add a second RED demonstration that deletes `s.ownedCharacter` from the **wrapper** and captures
   the failure verbatim — the guest-gate RED does not exercise this predicate.
5. Amend `04-06:200`'s "untouched" sentence to point at the wrapper-aware predicate instead.

### HIGH-2 — `04-06`'s only domain seam is a dangling referent, and its two wiring sites are outside its `files_modified`

`04-06` refers three times to "the facade's **mutate-side narrow interface**" —
`04-06:244` ("the write half needs `UpdateCharacterDescription` added to the mutate-side
interface"), `:264` ("add the method to the facade's mutate-side narrow interface"), and `:397`
(artifacts). **No such interface exists in any plan.** Exhaustively, the phase declares four narrow
consumer-side interfaces and no more: `characterAccessWorldReader` and
`characterAccessProfileVisibility` (`04-01:235-236`, both read-only —
`ListPropertiesByParent` + `GetCharacterDescription`, and `Reachable` + `VisibleAttributes`),
`characterAccessPolicyEvaluator` (`04-07:270`), and `characterAccessDirectoryReader` (`04-07:267`).
Neither wave-2/3/5 plan (`04-04`, `04-05`, `04-07`) introduces a mutate-side type, and `04-09` is
domain-side only (`files_modified` = `internal/world/**` exclusively).

So `04-06` — the whole of criterion 4, at wave 4 — reaches `world.Service.UpdateCharacterDescription`
(`internal/world/service.go:844`) and 04-09's new `UpdateCharacterProfileAttributes` through a seam
that no plan declares, whose production satisfier is never named, and whose injection is never
specified. The plan leaves the executor to choose between two materially different shapes:

- **(i) a new interface + field + constructor parameter** — which requires editing
  `cmd/holomush/sub_grpc.go` and `internal/testsupport/integrationtest/harness.go`. **Neither
  appears in `04-06`'s `files_modified`** (it lists exactly four files: `characteraccess_service.go`,
  `characteraccess_write.go`, `characteraccess_write_test.go`,
  `test/integration/access/character_write_test.go`). Every sibling plan that added a constructor
  parameter — `04-01:23-24`, `04-05:12-13`, `04-07:20-21` — correctly lists both. `04-06` is the
  only one that does not, and it is the only one that needs a new dependency at wave 4. Executing
  (i) under this scope contract puts `task build` red with no in-scope file to fix it in.
- **(ii) widening `characterAccessWorldReader`** with write methods — zero wiring, because
  `*world.Service` already satisfies both — but contradicts the type's name, its `04-01:237-239`
  doc-comment contract, and `04-06`'s own "mutate-side" wording.

This is a load-bearing substrate seam deferred to execution time — the exact failure mode
`.claude/rules/references/plan-review-learnings.md` records as a blocking plan gap, and the same
family as cycle-2's HIGH-A and HIGH-B.

Secondary, same fix: `04-06` Task 2's `<files>` block (`:236`) omits
`internal/grpc/characteraccess_service.go`, the file its own `<action>` at `:264` edits.

**PLAN.md change needed (`04-06`):**
1. Name the interface explicitly (e.g. `characterAccessWorldMutator`), give its exact method set —
   `UpdateCharacterDescription(ctx, world.Caller, ulid.ULID, string) error` per `service.go:844`,
   plus 04-09's profile-attribute command with its `expectedVersion` parameter — and state that
   `*world.Service` is the satisfier at both sites.
2. Add `cmd/holomush/sub_grpc.go` and `internal/testsupport/integrationtest/harness.go` to
   `files_modified`, and state that the same `*world.Service` value 04-01 already passes for
   `characterAccessWorldReader` is passed again (one added argument, no new construction site —
   `04-01:345`'s own rule).
3. Add `internal/grpc/characteraccess_service.go` to Task 2's `<files>`.
4. Add an acceptance criterion asserting the interface is declared with exactly those methods and
   that `task build` is green at both wiring files.

*Verified as NOT a defect while checking this:* the facade-side version compare in `04-06`'s
option (a) **is** implementable — `playerGate.ownedCharacter` returns `*world.Character`
(`sceneaccess_service.go:107-125`) and `CharacterRepository.ListByPlayer` selects the `version`
column (`internal/world/postgres/character_repo.go:340-351`), so `char.Version` is populated. The
narrowing has a real value to compare against.

## Settled positions — not re-raised

`INV-PRIVACY-10` pending; no new Phase-4 exposure audit (D-77); criterion 5 via compile fence
(D-79); `read_description` unregistered in `action_schema.go`; `04-06`'s TOCTOU narrowing;
`04-02`'s verbatim-refactor carve-out; HIGH-A's no-wave-change resolution under D-83
(`04-CONTEXT.md:283-291` reads as claimed — re-read this cycle).

## Verification coverage

Symbols excluded from grounding when listed under a plan's "Artifacts this phase produces".

| Symbol / claim | Cited at | Result | Evidence |
|---|---|---|---|
| `policyEngine` in scope at production wiring | `04-01:343`, `04-07:282` | VERIFIED | `cmd/holomush/sub_grpc.go:352` — `policyEngine := s.cfg.ABAC.Engine()` |
| `authPlayerRepo` / `authPlayerSessionRepo` in scope | `04-01:338-341` | VERIFIED | `cmd/holomush/sub_grpc.go:356-357` |
| `authCharRepo := bootstrapsetup.NewCharRepoAdapter(pool, charRepo)` | `04-07:186` | VERIFIED | `cmd/holomush/sub_grpc.go:446` |
| harness `accessEngine` field assignment | `04-01:344`, `04-07:287` | VERIFIED | `internal/testsupport/integrationtest/harness.go:876` — `accessEngine: pe` |
| `AccessEngine()` accessor at `:1191` | `04-01:344`, `04-07:287` | AMBIGUOUS | Doc comment begins `:1191`; `func (s *Server) AccessEngine()` is at `:1197`, `return s.accessEngine` at `:1198`. Non-misleading; recorded only. |
| harness `NewSceneAccessServer` constructor idiom at `:1173` | `04-07:187` | VERIFIED | `internal/testsupport/integrationtest/harness.go:1173`, `requirePlugins` `:1174`, `t.Fatalf` `:1176`, `holoGRPC.NewSceneAccessServer(` `:1178` |
| guest denial `sceneaccess_service.go:144-146` | `04-01:279`, `04-05:29` | VERIFIED | `:144` `if player.IsGuest {`, `:145` `PermissionDenied` |
| `ownedCharacter` returns `*world.Character` incl. version | `04-02:74`, `04-06:186` | VERIFIED | `internal/grpc/sceneaccess_service.go:107-125` |
| `ListByPlayer` selects `version`; stamps only `CHARACTER_LIST_FAILED` | `04-02:281` | VERIFIED | `internal/world/postgres/character_repo.go:340-351` |
| `resolvePlayerSessionWithRepo` package-level | `04-01:264` | VERIFIED | `internal/grpc/auth_handlers.go:174` |
| `NewSceneAccessServer` signature unchanged ⇒ callers compile | `04-02:106-110` | VERIFIED (incomplete citation) | Direct constructions are `cmd/holomush/sub_grpc.go`, `harness.go:1178` **and `internal/testsupport/integrationtest/session.go:719`** — a third direct site the plan does not name. Harmless because the signature is unchanged; `session.go:1004` and `test/integration/scenes/scene_name_resolution_test.go:101` go through the zero-arg harness method. |
| `profilevis.PolicyEvaluator` one-method shape | `04-07:176` | VERIFIED | `internal/access/profilevis/profilevis.go:99-101` |
| `profilevis.evaluate` three-outcome collapse | `04-07:176` | VERIFIED | `internal/access/profilevis/profilevis.go:234-273` |
| DENY + `infra:` id + nil error is real | `04-07:205` | VERIFIED | `internal/access/policy/policytest/helpers.go:120-131` (declares `Evaluate` and `CanPerformAction`) |
| `bootstrap.go:91` per-policy SeedVersion compare | `04-PATTERNS.md:348`, `04-01:356` | VERIFIED | `internal/access/policy/bootstrap.go:91` |
| `world.Service.UpdateCharacterDescription` signature, no `expectedVersion` | `04-06:32`, `:240`; `04-09:106` | VERIFIED | `internal/world/service.go:844` |
| `worldMutator.propertyWriter` already injected | `04-09:94` | VERIFIED | `internal/world/mutator.go` (field + `newWorldMutator` injection) |
| The phase's four narrow interfaces are exhaustive | derived | VERIFIED | `rg -o 'characterAccess[A-Za-z]*' *-PLAN.md \| sort -u` yields exactly `characterAccessWorldReader`, `characterAccessProfileVisibility`, `characterAccessPolicyEvaluator`, `characterAccessDirectoryReader` |
| A "mutate-side narrow interface" is declared somewhere | `04-06:244`, `:264`, `:397` | **MISSING** | No declaration in any plan — HIGH-2 |
| `04-08` expected owner-audience set names the mutations | `04-08:208-211` | **MISSING** | `rg -n 'UpdateCharacterProfile\|UpdateCharacterDescription' 04-08-PLAN.md` returns nothing — HIGH-1 |
| `world.CharacterDescription` / `world.Service.GetCharacterDescription` absent at HEAD | `04-01:235` | MISSING, **expected** | Phase-created artifacts (`04-01:195-197`, `:539`); `internal/world/service.go` and `internal/world/character.go` are in `04-01`'s `files_modified` |
| All `CharacterAccessServer` symbols absent at HEAD | all plans | MISSING, **expected** | Phase-created artifacts |
| Runtime behaviour of unwritten code | — | UNCHECKABLE | Not treated as verified anywhere above |

A clean line here never means "nothing was checked". Twenty-four symbols/claims were individually
re-resolved this cycle against source; two blocking gaps were found and both are escalated above.

## Risk Assessment

**MEDIUM-HIGH.** Both remaining findings are localized, mechanical, and confined to two plans
(`04-06`, `04-08`) in waves 4 and 6 — nothing in waves 1-3 is blocked, so execution can begin.
Neither is an architectural error: HIGH-1 is a predicate that must learn one approved wrapper name,
HIGH-2 is a seam that must be named and two `files_modified` entries that must be added. Left
unfixed, HIGH-1 makes criterion 1's proof either permanently red or silently incomplete over the
two write RPCs, and HIGH-2 puts `task build` red at wave 4 with no in-scope file to repair it in.

---

# Cross-AI Plan Review — Phase 4 · Cycle 4 (closing cycle)

**Reviewers:** codex (`gpt-5`-class, `model_reasoning_effort=xhigh`, read-only sandbox, full repo
access) + orchestrator source-grounding pass.
**Reviewed at:** 2026-08-10.
**Plans reviewed:** all nine; material re-audit scoped to `04-06-PLAN.md` and `04-08-PLAN.md`.
**Scope note:** the loop was extended one cycle past its max. Trend across cycles: 8 → 6 → 2 → **3**.

## What actually changed since cycle 3

`git diff 494e91256..HEAD -- .planning/phases/04-.../` touches **five** plans, not two:

| Plan | Lines | Change |
|---|---|---|
| `04-06-PLAN.md` | +137 | cycle-3 HIGH-1 + HIGH-2 fixes |
| `04-08-PLAN.md` | +160 | cycle-3 HIGH-1 fix |
| `04-01-PLAN.md` | ±1 | citation drift: `AccessEngine()` `:1191` → `:1197` |
| `04-07-PLAN.md` | ±1 | same citation |
| `04-02-PLAN.md` | +6 | scene-facade construction sites 2 → 3, naming `session.go:719` |

The three citation fixes are **VERIFIED correct**: `internal/testsupport/integrationtest/harness.go:1197`
is `func (s *Server) AccessEngine() types.AccessPolicyEngine`, and the third scene construction site is
`internal/testsupport/integrationtest/session.go:719` (`facade := holoGRPC.NewSceneAccessServer(`),
alongside `cmd/holomush/sub_grpc.go:826` and `harness.go:1178`. The other four plans are byte-identical
to their cycle-3 state.

## Cycle-3 disposition audit

| Cycle-3 finding | Disposition | Evidence |
|---|---|---|
| **HIGH-1** — routing census blind to the mutation wrapper | **FULLY RESOLVED** *(as scoped)* | `04-08:207-247` adds the one-member `ownershipWrapperMethods` literal, the wrapper-integrity assertion that runs before the comparison, the two-shape membership rule, and the 4/3/4 enumeration; `04-08:322-336` adds RED demos (b) and (c). `04-06:274-289` replaces the false "untouched" sentence with an explicit "NOT untouched" paragraph pinning the name and receiver. |
| **HIGH-2** — dangling mutate-side seam + missing wiring files | **FULLY RESOLVED** | `04-06:139-190` declares `characterAccessWorldMutator` with exactly two methods, names `*world.Service` as satisfier, and moves both wiring files into Task 1's `<files>`; frontmatter `files_modified` 4 → 6. |

Both cycle-3 findings are closed. The three findings below are **new**, and two of the three are
defects **created by** the cycle-3 fixes — the pattern every prior cycle exhibited.

---

## Codex Review

### Summary

> Cycle 4 is not converged. Fix B's mutate-side interface and wiring are sound, and fix A correctly
> handles the two current mutation handlers through a verified wrapper. However, fix A still cannot
> detect a newly added owner RPC that bypasses every gate, and 04-06 incorrectly assumes
> `world.Service.UpdateCharacterDescription` performs validation. Both are release-blocking.

### Verdict on cycle-3 fix A — BROKEN (as an unqualified claim); SOUND for the attack it was asked to close

Codex confirms the wrapper repair itself:

- The wrapper assertion reuses the shipped selector predicate, which checks `<receiver>.<method>` in
  the method's own AST body (`test/meta/world_envelope_census_test.go:162`).
- The wrapper must exist exactly once and call `s.ownedCharacter` **before** the ownership comparison
  (`04-08:230-234`).
- The 4/3/4 sets are correct for Phase 4, with `ListMyCharacters` correctly excluded from the
  ownership set (`04-08:276-283`).

Codex's three attack outcomes, which the orchestrator re-derived independently and **concurs with**:

1. A future owner handler calling `ownedCharacterForMutation` but skipping `resolveAndGate` **does**
   fail — the ownership-derived set gains a member absent from the 3-member literal.
2. Gutting the wrapper **does** fail, at the integrity assertion — and only there, which is exactly
   the asymmetry RED demo (c) is designed to exhibit (`04-08:330-335`).
3. A future owner handler bypassing **both** gates appears in **no** derived set. The static 4/3 sets
   are unchanged and the census passes. → see NEW-HIGH-3.

### Verdict on cycle-3 fix B — SOUND

- `UpdateCharacterDescription`'s signature is exactly `(context.Context, Caller, ulid.ULID, string) error`
  (`internal/world/service.go:844`).
- `04-09:218-221` specifies `UpdateCharacterProfileAttributes` with `Caller`, character ULID, expected
  version, and the attribute map — matching `04-06:153-163`'s interface shape.
- `04-06` `depends_on` includes `04-09` (`04-06:6`), so `*world.Service` carries both methods at the
  wave-4 commit.
- Production and harness each already retain one `*world.Service`; threading it through a second narrow
  interface is valid, and declaring both methods in Task 1 while Task 2 only consumes one is a sound
  task boundary.

### Second-order claims

1. **VERIFIED as planned topology.** `session.go` constructs only `NewSceneAccessServer`; the character
   facade does not exist at HEAD, so the final two-site count is an execution-time check.
2. **VERIFIED narrowly.** No fourth shared helper is needed; `bodyReferencesSelector` is package-visible
   in `package meta` and performs exactly the required check. The precedent parses source rather than
   importing `internal/grpc`. *(Does not close the universe gap.)*
3. **VERIFIED.** Waves unchanged; no same-wave `files_modified` collision. Wave 2 is the only
   multi-plan implementation wave: `04-04` owns gRPC/proto, `04-09` owns `internal/world`.

### Codex concerns

- **HIGH — routing census has a false-negative universe.** A wholly ungated new RPC is absent from all
  predicate-derived sets, leaving the static expected sets unchanged. Walking all exported methods only
  supplies an anti-vacuity count; the plan never asserts that every `CharacterAccessService` method
  belongs to an audience partition (`04-08:179`, `:305`).
- **HIGH — description validation does not exist on the mandated domain path.** `04-06:406-408` says
  `ValidateDescription` already runs and forbids re-implementing validation. In reality
  `UpdateCharacterDescription` assigns `char.Description` directly and calls the mutator without
  validation (`internal/world/service.go:863`); the repository writes the value straight into the UPDATE
  (`internal/world/postgres/character_repo.go:150-153`). The validated path is `Character.SetDescription`
  (`internal/world/character.go:102`), which the service does not call. The planned 4001-byte,
  invalid-UTF-8 and control-character tests cannot pass under the prescribed implementation.
- **LOW — 04-06 describes future-wave interfaces as already present.** It says four interfaces exist
  before `04-06`, two of them from `04-07` (`04-06:141`) — but `04-07` is wave 5 and depends on `04-06`
  (`04-07:5-6`). At execution time the mutator is the **third** interface, not the fifth.

### Codex risk assessment — HIGH

> The current plan can falsely certify a future completely ungated owner RPC, and the mandated
> description path presently accepts input the plan claims it rejects.

---

## Orchestrator source-grounding pass

Independent verification, run in parallel with Codex and before reading its output.

### Confirmations of the cycle-3 fixes

- `bodyReferencesSelector` — `test/meta/world_envelope_census_test.go:162`,
  `func bodyReferencesSelector(body *ast.BlockStmt, recv, field string) bool`, `package meta`
  (`:4`). Exactly one declaration across `test/meta/` (`rg -o 'func bodyReferencesSelector\(' test/meta/ | wc -l` → `1`).
- The predicate compares `sel.Sel.Name == field` **exactly** (`:174`), so `s.ownedCharacterForMutation`
  does **not** match `ownedCharacter`. This is precisely why the wrapper set is necessary, and it also
  means the wrapper set cannot accidentally widen via prefix collision. The plan's reasoning is correct.
- `04-08:347`'s trailing-`(` note is correct: `rg -o 's\.ownedCharacter\('` cannot match
  `s.ownedCharacterForMutation(`, and the `func (s *CharacterAccessServer) ownedCharacterForMutation(`
  declaration has no `s.` prefix, so the "returns 2" count is right.
- `test/meta` has **no** transitive dependency on `internal/grpc`:
  `go list -deps -test ./test/meta/ | rg -c 'holomush/internal/grpc'` → no match, and
  `rg -n 'holomush/internal/grpc' test/meta/` → no match. The three RED demos are parse-reds, as claimed.
- `internal/world/postgres/character_repo.go:342` — `ListByPlayer` selects `version`, so
  `04-06:399-401`'s "no second read is needed" is grounded.
- `SceneAccessServer.ownedCharacter` returns `codes.NotFound` / `codes.Internal`
  (`internal/grpc/sceneaccess_service.go:109-125`) exactly as `04-06`'s read_first states.
- Intra-wave `files_modified` collision check re-run across all nine plans: **zero**. Wave map is
  W1 {01,02,03} · W2 {04,09} · W3 {05} · W4 {06} · W5 {07} · W6 {08}; the only multi-plan
  implementation wave (W2) partitions cleanly into `internal/grpc`+proto (04-04) vs `internal/world` (04-09).
- The wave-2 proto-ahead-of-handler ordering compiles: the repo pattern embeds
  `sceneaccessv1.UnimplementedSceneAccessServiceServer` (`internal/grpc/sceneaccess_service.go:48`),
  so `04-04` may declare all four owner RPCs in the proto at wave 2 while `04-05`/`04-06` implement
  them at waves 3/4. **Not a finding.**
- `04-06:158-161`'s "transcribe verbatim from `04-09-SUMMARY.md`, which is authoritative" correctly
  absorbs the residual risk that `04-09` never pins its signature in-plan. `expectedVersion int` matches
  the shipped `RetireCharacter`/`UnretireCharacter` convention (`internal/world/service.go:929`, `:1029`),
  and `04-09`'s "empty string removes the row" truth is consistent with `04-06`'s `map[string]string`
  "(empty string means delete)". **Not a finding.**

### New findings

#### NEW-HIGH-1 — `world.Service.UpdateCharacterDescription` performs **no** validation; `04-06` Task 2 is built on a false premise

*Independently found by Codex and by the orchestrator. Highest-confidence finding this cycle.*

`04-06:406-408` states the handler "does not re-implement validation — `ValidateDescription` already
runs on that path, so IDENT-02a inherits the 4000-byte cap, the UTF-8 check and the control-character
rejection for free (D-82)." The corresponding `must_haves` truth (`04-06:35`) asserts the write
"inherits that path's validation."

**Source refutes this at HEAD:**

- `internal/world/service.go:863` — `char.Description = description`. A bare field assignment. No
  `SetDescription`, no `ValidateDescription`, anywhere in the method (`:844-880`).
- `internal/world/character.go:102-107` — `func (c *Character) SetDescription(desc string) error` is
  the validating setter, and its own doc comment (`:98-101`) says *"Direct field access to Description
  is acceptable for repository hydration from the database. This setter should be used by application
  code to ensure validation."* The service violates that contract.
- `SetDescription` has **zero production callers**:
  `rg -n 'SetDescription\(' --glob '!*_test.go' internal/ plugins/ cmd/` matches only the declaration
  and the unrelated `internal/property/entity_mutator.go` location/object mutators.
- The mutator does not validate either: `worldMutator.updateCharacter`
  (`internal/world/mutator.go:287-291`) forwards straight to `characterWriter.Update`.
- `internal/world/postgres/character_repo.go:150-153` — `UPDATE characters SET description = $2 …`
  with `char.Description` passed verbatim.
- `ValidateDescription` itself is real and does implement all three checks
  (`internal/world/validation.go:96-110`) — it is simply never reached from this path.

**Consequence, concrete.** `04-06` Task 2's behaviors (`:361`, `:363`) require "a description exceeding
4000 bytes is rejected with an invalid-argument status" and "invalid UTF-8 and a control character
other than newline or tab are each rejected", asserted at the unit tier against a **domain double**
(`:365`). A double of a domain method that does not validate cannot produce those rejections, and the
same action text forbids the only other available site ("does not re-implement validation"). The task
is unsatisfiable as written. `internal/world/service.go` is **not** in `04-06`'s `files_modified`
(6 entries, none in `internal/world/`), so there is no in-scope file to repair it in — the identical
shape as cycle-3's HIGH-2.

**Required PLAN.md change.** Add `internal/world/service.go` and `internal/world/service_test.go` (or
the existing character-service test file) to `04-06` Task 2's `<files>` and to the plan's
`files_modified`; replace the direct assignment at `service.go:863` with `char.SetDescription(description)`,
returning the `*world.ValidationError`; place the 4000/4001-byte, invalid-UTF-8, control-character and
empty-input boundary tests at the **world** layer, and keep the facade's job as the generic
validation-error → `codes.InvalidArgument` mapping it already describes. Collision check: `04-06` is
the sole wave-4 plan and `04-09` (wave 2) is the only other plan touching `internal/world/service.go`,
so the addition is collision-free. State in the plan that this is a **pre-existing repo gap** the web
write surface is the first caller to expose, not a Phase-4 regression.

#### NEW-HIGH-2 — `04-08` Task 2 never enumerates the accepted facade receiver-type set, and cycle-3's 4/3/4 pin makes the wrong guess fatal

*Orchestrator finding; Codex reached the same conclusion as its Suggestion 3.*

`04-08` refers to "the accepted facade-type set" / "accepted receiver types" **five times**
(`:176`, `:179`, `:198-199`, `:231`, `:503`) and never enumerates it. Two independent signals point the
implementer at the **wrong** answer:

- Task 2's `<read_first>` (`:166`) lists `internal/grpc/sceneaccess_service.go` — *"the second facade
  embedding the same gate"*.
- Task 1 specifies the helper as *"a facade-aware receiver-name predicate taking a **set** of accepted
  receiver type names"* (`:503`, `:104-109`) — the entire point of generalizing away from
  `serviceReceiverName`'s single hard-coded name is that more than one is expected.

**Ground truth at HEAD:** `internal/grpc/sceneaccess_service.go` contains **24** `s.resolveAndGate(`
call sites and **21** `s.ownedCharacter(` call sites (`rg -n 's\.resolveAndGate\(' internal/grpc/ | wc -l`
→ 24, `:164` through `:1009`). If `SceneAccessServer` is in the accepted set, the derived guest-gate set
is ≈28 against the newly-pinned 4-member literal and the ownership set ≈24 against the 3-member literal —
the census is **permanently RED with ~45 extra members**, and `04-08:350`'s new criterion ("member counts
4 / 3 / 4") forecloses the previously self-correcting escape of deriving the expected sets from whatever
universe was walked.

The worse branch is not the RED: it is an implementer resolving the RED by **widening the expected sets**
with 24 scene entries, which converts the census into a snapshot of the scene facade and destroys the
character-audience reasoning the file header is supposed to carry. Nothing in the plan forbids that.

Evidence the author intended character-only: all three enumerated sets contain only
`CharacterAccessServer`/`Handler` names (`:276-286`); the deliberate-exclusion reasoning names only the
**two character** public-audience RPCs (`:288-290`, `:356`) and never mentions the scene facade at all.

**Required PLAN.md change.** One sentence in `04-08` Task 2's `<action>`: the accepted facade
receiver-type set is exactly `{"CharacterAccessServer"}`, and `sceneaccess_service.go` is in
`<read_first>` only as the *shape* precedent for gate promotion, not as a census member. Optionally add
an acceptance criterion pinning the literal.

#### NEW-HIGH-3 — the routing census cannot detect a wholly ungated new RPC, but `04-08`'s `must_haves` claims it can

*Codex finding; orchestrator concurs after independent derivation.*

`04-08:24` states as a truth: *"A new owner-audience RPC added later that skips the gate fails the
census rather than shipping."* The census cannot deliver that. Membership on **both** halves is derived
by "does this method's body reference the gate name" (`:198-205`); comparison is against static literals
(`:276-286`). A new exported method on `*CharacterAccessServer` that references **neither** gate name is
in **neither** the derived set nor the expected set → symmetric difference is empty → **GREEN**.

The census structurally cannot assert a full partition either, because the two public-audience RPCs are
legitimate non-members (`:180`, `:356`) — so "every exported method is a member" is false by design and
the plan supplies no third bucket.

The residual coverage is Task 3's descriptor census, which catches any new **character-returning** RPC
by set equality against the checked-in inventory. That is enough for the ROADMAP's *literal* criterion 1
("a census with set equality over every character-returning RPC"), but **not** for `04-08`'s own broader
`must_have`. A future write RPC returning a non-character-shaped response — precisely the class
`UpdateCharacterProfile`/`UpdateCharacterDescription` are in, and the class the cycle-3 fix's own framing
("Phase 4's two write RPCs are inside criterion 1's proof") invites a reader to believe is now closed —
escapes both censuses.

Note this is a **pre-existing** structural limit; what is new is that the cycle-3 fix added a `must_have`
(`:24` is older, but `:25-26` amplify it) asserting a guarantee the mechanism does not provide.

**Required PLAN.md change — either is acceptable, and the second is cheap:**

- *(preferred)* Derive the RPC universe from the service descriptor (Task 3 already loads
  `protoregistry.GlobalFiles`) and assert an exact **audience partition**: `descriptor methods ==
  public ∪ owner`, with `public = {GetCharacterProfile, ListCharacterDirectory}` and `owner` the
  4-member set the guest-gate literal already names. Derive the guest-gate expectation **from** the owner
  partition rather than as an independent literal, and add a fourth RED demonstration that adds a wholly
  ungated exported handler. This makes the `must_have` true.
- *(minimum)* Downgrade the `must_have` at `:24` to what the mechanism actually proves — a new
  owner-audience RPC that reaches *one* gate but not the other, or that routes through an unapproved
  indirection, fails; a **character-returning** RPC added anywhere fails via Task 3 — and record the
  residual (an ungated, non-character-returning facade RPC) as a named, accepted limit with a GitHub
  issue, the way `04-06` handles its TOCTOU narrowing.

### Actionable non-HIGH

- **MEDIUM — `04-06` Task 1 `<read_first>` names two interfaces that do not exist at wave 4.**
  `04-06:141` tells the implementer to read "the four narrow consumer-side interfaces the phase declares
  before this plan", attributing `characterAccessDirectoryReader` and `characterAccessPolicyEvaluator` to
  `04-07`. `04-07` is **wave 5** and `depends_on` includes `04-06` (`04-07:5-6`); those two interfaces are
  declared in `04-07` (`:55-58`, `:176`) and `04-05` declares none. At `04-06`'s commit only
  `characterAccessWorldReader` and `characterAccessProfileVisibility` (`04-01:235-236`, `:540`) exist, so
  `characterAccessWorldMutator` is the **third** interface, not the fifth. *Fix:* correct the count and the
  attribution in `04-06:139-145` and in the "A fifth interface does not weaken the fence" sentence
  (`04-06:~190`). The D-79 fence argument itself survives unchanged — it is about the *absence* of
  property-repository methods, not about the count.
- **LOW — two `04-08` Task 2 acceptance criteria pull against each other.** `:353` forbids the words
  `exempt` / `Exempt` / `skip` / `allowlist` *"anywhere in this file, in code or in a comment"*, while
  `:266-267` instructs the implementer to *"state this reasoning in the file header"* for the
  three-count argument that sits immediately beside the "No exemption list" paragraph (`:305-310`), and
  `:350` asks for a comment explaining why `ListMyCharacters` is absent from the ownership set — whose
  most natural phrasing uses "skipped". *Fix:* add a one-line note that the header reasoning must be
  phrased without those four tokens, or drop `skip` from the banned list (it is the weakest of the four).
- **LOW — the comment-filtering idiom `rg -v '^\s*//'` filters only whole-line comments.** The counting
  criteria at `04-08:347` (4 / 2 / 1), `04-06:311` (12) and `04-06:439` (2) can still be inflated by a
  **trailing** `// …` comment on a code line or by a `/* … */` block, which is exactly the rationale-comment
  vector the filter was added to close. *Fix:* say so explicitly ("no trailing or block comment may
  contain the counted token") or count with `gofmt`-stripped input. Non-blocking — the risk is a
  false-GREEN count, not a false-RED.

## Consensus

### Agreed strengths (both reviewers)

- The cycle-3 wrapper mechanism is genuinely sound for the three attacks it was asked to close:
  closed one-name literal, verified-not-trusted integrity assertion, derived-side-only additivity.
  The "rename a rogue helper to `ownedCharacterForMutation`" attack is closed by the *declared exactly
  once* clause (`04-08:176`), which neither reviewer initially expected to be load-bearing.
- Fix B's seam is correct end to end: signature verbatim at `internal/world/service.go:844`, satisfier
  `*world.Service` already in scope at both sites, `depends_on` carries `04-09`, and the Task-1-declares /
  Task-2-consumes boundary keeps every commit self-green. The declared variance from the cycle-3
  review's own fix item 3 is **sound**, not a dodge.
- All three second-order claims verified.

### Agreed concerns

- **NEW-HIGH-1** (description validation) — found independently by both, with identical `path:line`
  evidence. Blocking.
- **NEW-HIGH-3** / **NEW-HIGH-2** — Codex ranks the universe gap HIGH and the receiver-set ambiguity a
  suggestion; the orchestrator ranks the receiver-set ambiguity HIGH (it produces a permanently-RED or
  silently-degraded census at execution) and concurs on the universe gap. Both are listed HIGH here.

### Divergent views

- **Severity of the receiver-set ambiguity.** Codex treats it as an implementation-time clarification
  (Suggestion 3); the orchestrator escalates it because cycle-3's new 4/3/4 pin removed the degree of
  freedom that previously let a wrong guess self-correct. The remediation is identical either way — one
  sentence — so the divergence does not change the required edit.
- **Whether NEW-HIGH-3 is a *plan* defect or a *criterion* defect.** Codex frames it as the plan failing
  criterion 1; the orchestrator notes the ROADMAP criterion is scoped to *character-returning* RPCs and is
  satisfied by Task 3, so the defect is in `04-08`'s own broader `must_have`. This matters for the fix:
  the minimum remedy is to correct the claim, not necessarily to build the partition.

## Verification coverage

| Claim / symbol | Cited at | Status | Evidence |
|---|---|---|---|
| `bodyReferencesSelector` exists, `package meta`, exact-name compare | `04-08:164`, `:349` | VERIFIED | `test/meta/world_envelope_census_test.go:162`; `package meta` `:4`; `sel.Sel.Name == field` `:174`; one declaration in `test/meta/` |
| `test/meta` does not import/depend on `internal/grpc` | `04-08:318-320` | VERIFIED | `rg -n 'holomush/internal/grpc' test/meta/` → none; `go list -deps -test ./test/meta/ \| rg -c 'holomush/internal/grpc'` → none |
| `world.Service.UpdateCharacterDescription` signature verbatim | `04-06:154-156` | VERIFIED | `internal/world/service.go:844` |
| `world.Service.UpdateCharacterDescription` **validates** its input | `04-06:35`, `:406-408` | **REFUTED** | `service.go:863` bare assignment; `mutator.go:287-291`; `postgres/character_repo.go:150-153`; `character.go:102` `SetDescription` has zero production callers — NEW-HIGH-1 |
| `UpdateCharacterProfileAttributes` shape from `04-09` | `04-06:157-163` | VERIFIED (by construction) | `04-09:218-221`; `expectedVersion int` matches `service.go:929`, `:1029`; `04-06:158-161` defers verbatim to `04-09-SUMMARY.md` |
| `04-06` `depends_on` includes `04-09` | `04-06:6` | VERIFIED | `depends_on: ["04-02", "04-04", "04-05", "04-09"]` |
| Scene facade has three construction sites | `04-02:106-113` | VERIFIED | `cmd/holomush/sub_grpc.go:826`, `harness.go:1178`, `session.go:719` |
| `session.go` names no `CharacterAccessServer` | `04-06:299` | VERIFIED (at HEAD) | `rg -n 'CharacterAccessServer' internal/testsupport/integrationtest/session.go` → no match |
| `AccessEngine()` at `harness.go:1197` | `04-01:344`, `04-07:287` | VERIFIED | `func (s *Server) AccessEngine() types.AccessPolicyEngine` at `:1197`; `accessEngine: pe` at `:876` |
| `ListByPlayer` selects `version` | `04-06:399-401` | VERIFIED | `internal/world/postgres/character_repo.go:342` |
| `ownedCharacter` returns NotFound / Internal | `04-06:114` | VERIFIED | `internal/grpc/sceneaccess_service.go:109-125` |
| Accepted facade receiver-type set | `04-08:176`, `:179`, `:198`, `:231`, `:503` | **AMBIGUOUS** | Never enumerated; `read_first:166` + plural-set helper point at `SceneAccessServer`, which carries 24 `s.resolveAndGate(` / 21 `s.ownedCharacter(` — NEW-HIGH-2 |
| "A new owner-audience RPC that skips the gate fails the census" | `04-08:24` | **REFUTED** | Derived-set membership + static-literal comparison ⇒ a doubly-ungated method is in neither set (`04-08:198-205`, `:276-286`) — NEW-HIGH-3 |
| Four facade interfaces exist before `04-06` | `04-06:141` | **REFUTED** | `characterAccessDirectoryReader` / `characterAccessPolicyEvaluator` are declared in `04-07` (wave 5, `depends_on` 04-06); only `04-01:235-236`'s two exist at wave 4 |
| No wave moved; zero intra-wave `files_modified` collision | claimed | VERIFIED | W1{01,02,03} W2{04,09} W3{05} W4{06} W5{07} W6{08}; W2 partitions `internal/grpc`+proto vs `internal/world` |
| Wave-2 proto-ahead-of-handler compiles | derived | VERIFIED | `Unimplemented…ServiceServer` embedding precedent, `internal/grpc/sceneaccess_service.go:48` |
| `s\.ownedCharacter\(` cannot match the wrapper call | `04-08:347` | VERIFIED | Literal `(` after `ownedCharacter`; declaration has no `s.` prefix |
| Runtime behaviour of unwritten Phase-4 code | all plans | UNCHECKABLE | Phase-created artifacts; not treated as verified anywhere above |

Drift-guard authority for this repo is `intel` (`gsd-tools drift-guard authority --raw`), under which
`AMBIGUOUS` → MEDIUM and `MISSING` → needs-acknowledgement, neither hard-blocking. The two **REFUTED**
rows are escalated above `AMBIGUOUS` deliberately: a plan asserting a mechanism that source contradicts
is a stronger failure than an unresolvable citation.

## Risk Assessment

**HIGH.** Three findings, all localized to two plans, none architectural — but one of them
(NEW-HIGH-1) makes `04-06` Task 2 unsatisfiable as written with no in-scope file to repair it in, and
it exposes a real pre-existing data-integrity gap: the in-world description write path accepts
over-cap, invalid-UTF-8 and control-character input today, and Phase 4 is the first surface to open it
to the web. NEW-HIGH-2 costs one sentence and, unfixed, yields either a permanently-red census at
wave 6 or a census silently widened to 45 scene entries. NEW-HIGH-3 costs either one partition
assertion or one corrected `must_have`.

Waves 1-3 remain unblocked; every finding lands in wave 4 (`04-06`) or wave 6 (`04-08`). Execution of
waves 1-3 may begin, but `04-06` and `04-08` should not be executed until these three are incorporated.

---

# Cross-AI Plan Review — Phase 4 · Cycle 5 (closing cycle)

**Reviewers:** codex (`gpt-5`-class, `model_reasoning_effort=xhigh`, read-only sandbox, full repo
access) + orchestrator source-grounding pass + a dedicated adversarial pass against `04-08`'s
partition.
**Reviewed at:** 2026-08-10.
**Plans reviewed:** all nine; material re-audit scoped to `04-06-PLAN.md` and `04-08-PLAN.md`.
**Scope note:** the loop was extended twice past its `--max-cycles 3`. Trend: 8 → 6 → 2 → 3 → **3**.

## What actually changed since cycle 4

`git show --stat b97d049a1` touches exactly **two** plans — `04-06-PLAN.md` (+284/−…) and
`04-08-PLAN.md` (+199/−…). The other seven are byte-identical to their cycle-4 state
(`git diff 66b41c330..HEAD --name-only` lists only those two). No re-audit of the seven was
performed and none is claimed.

## Cycle-4 disposition audit

| Cycle-4 finding | Disposition | Evidence |
|---|---|---|
| **NEW-HIGH-1** — description write performs no validation | **FULLY RESOLVED** *(the fix; its justification is not — see M1)* | New Task 2 at `04-06:361-495` routes `service.go:862`'s bare `char.Description = description` through `char.SetDescription` with `oops.Code("CHARACTER_INVALID").Wrap(err)`, positioned after `checkAccess` (`service.go:849`) and after the repository read (`:852`). Task count 2 → 3; `files_modified` gains `internal/world/service.go` + `service_test.go`; GH #4954 cited 13×. |
| **NEW-HIGH-2** — accepted receiver-type set never enumerated | **FULLY RESOLVED** | `04-08:212-222` enumerates it once: facade `{"CharacterAccessServer"}`, web `{"Handler"}`. `sceneaccess_service.go` reframed "**SHAPE PRECEDENT ONLY … it is NOT a census member**" at `:180`; exclusion consequence at `:227-237`; criterion pinning `SceneAccessServer` to the comment at `:471`. |
| **NEW-HIGH-3** — census could not detect a wholly ungated RPC | **PARTIALLY RESOLVED** | The partition (`04-08:344-369`) genuinely closes the attack it was built for, and RED demo (d) at `:451-461` proves it. But it closes only the *unary, concrete, `CharacterAccessServer`-receiver* case — see **H2** and **H3**. |
| **MEDIUM** — interface count (third, not fifth) | **FULLY RESOLVED** | `04-06:131`, `:166-176` now say "**two**" pre-existing and "**third**"; the fence sentence at `:216-220` was made count-independent ("**Adding an interface does not weaken the fence, at any count**"). Verified: `04-07` is `wave: 5`, `depends_on: [… "04-06"]`. |
| **LOW** — `skip` dropped from the banned-token list | **FULLY RESOLVED** | `04-08:481` bans `exempt\|Exempt\|allowlist` only; `:482` records the reason at length. rg regex is valid Rust-regex (bare `\|`). |
| **LOW** — comment-filter discipline on counting criteria | **PARTIALLY RESOLVED** | Landed on `04-08:474-475` (three counts) and `04-06:344`. Two counting criteria were missed — see **L2**. |

Two of cycle 4's six fixes carry a defect **created by the fix itself** — H1 and H3 both exist only
because `b97d049a1` added the public-audience literal. That is the fifth consecutive cycle exhibiting
the pattern.

---

## Codex Review

### Summary

> Five of the six cycle-4 fixes are resolved. The description-validation change is sound and its
> compatibility cost is adequately covered. […] The audience partition, however, still has one
> structural blind spot: a newly declared proto RPC inherited from the generated
> `UnimplementedCharacterAccessServiceServer` has no `*ast.FuncDecl` on `CharacterAccessServer`, so
> it is absent from the AST-derived universe and can remain ungated without failing the partition.
> This blocks execution because it contradicts criterion 1's future-RPC guarantee.

### Codex's HIGH — adjudicated PARTIALLY UPHELD, and superseded by H2

Codex's mechanism claim is **VERIFIED**: `04-08:432` states "This census parses `internal/grpc` and
`internal/web` with `go/ast`", `:274` resolves members via `*ast.FuncDecl`, and the universe is scoped
to the receiver literal `{"CharacterAccessServer"}` (`:220`). Promoted methods are therefore invisible,
and `SceneAccessServer` embeds `UnimplementedSceneAccessServiceServer` at
`internal/grpc/sceneaccess_service.go:47` as the precedent.

Two corrections to codex's framing:

1. **The specific case codex constructs is inert.** An RPC declared in the proto and left
   unimplemented is satisfied by the embedded `Unimplemented…Server` and returns `codes.Unimplemented`
   — it serves no data. The moment anyone implements it, a `*ast.FuncDecl` with receiver
   `*CharacterAccessServer` appears and the partition fires. So this variant alone is not a
   data-exposure hole.
2. **ROADMAP criterion 1 is not the criterion it breaks.** Criterion 1 is scoped to "every
   **character-returning** RPC", and that scope is proven by **Task 3**, which derives from the
   *global proto registry* (`04-08:522-527`), not from source AST — so it sees a proto-declared
   RPC regardless of promotion and goes RED in the extra direction unless its inventory row lands in
   the same change (D-72). The claim codex's case actually falsifies is `04-08`'s own broader `<done>`
   at `:492` ("the audience partition proves the character facade has **no RPC outside those two
   audiences**").

Codex's finding is nonetheless **correct in kind**, and the dedicated adversarial pass found two
further escapes from the same root cause, one of which is live rather than inert. All are consolidated
as **H2** below.

---

## Current concerns

### H1 — HIGH — `04-08:485` and `04-08:355-360` are mutually unsatisfiable. **BLOCKS EXECUTION.**

The cycle-4 edit added, in Task 2 step 2:

> **`04-08:355-360`** — "… `GetCharacterProfile` and `ListCharacterDirectory` … **check them in as a
> literal set** rather than leaving them as prose, because the partition now consumes them."

The acceptance criteria of the *same task* still carry the pre-cycle-4 line, untouched by `b97d049a1`:

> **`04-08:485`** — "The two public-audience RPC names appear in the file **only inside the comment**
> explaining their deliberate exclusion."

An executor cannot satisfy both. With `autonomous: true` this resolves one of two ways, and both are
bad: it declines to write the literal — which deletes the entire NEW-HIGH-3 fix — or it writes the
literal and reports the task's own acceptance criterion failed.

**Minimum edit:** rewrite `:485` to "The two public-audience RPC names appear in the public-audience
literal and its neighbouring comment, and in no other set literal."

### H2 — HIGH — The partition's universe filter fails OPEN; four handler shapes escape it. **BLOCKS 04-08 Task 2** (wave 6 only).

The filter, verbatim at `04-08:346-352`:

> collect every method on `*CharacterAccessServer` that is exported AND has the RPC-handler shape:
> **exactly two parameters, the first `context.Context`, and exactly two results of which the second
> is `error`**.

Anything not matching that shape is **silently outside the audit** rather than failing closed. Four
escapes, in descending order of realism:

1. **Server-streaming handler — a shape already shipped twice in the same package.**
   `internal/grpc/server.go:559` and `internal/grpc/subscribe_handler.go:272` are both
   `func (…) Subscribe(req *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[…]) error` —
   two params whose first is *not* `context.Context`, and **one** result. A
   `WatchMyCharacters` on the character facade in that shape references no gate, enters no derived
   set, and never enters the universe: union = 6, universe = 6, disjointness holds → **census GREEN
   with a live ungated streaming RPC on the facade.**
2. **Method promoted from an embedded type — live, and created by the cycle-4 NEW-HIGH-2 fix.**
   `04-02:105` embeds `playerGate` as a **bare embedded field**. An exported RPC-shaped method added
   to `playerGate` in `internal/grpc/player_gate.go` is promoted onto `*CharacterAccessServer`,
   satisfies the generated interface, and serves real data — but its `*ast.FuncDecl` receiver is
   `*playerGate`, which the now-pinned one-member receiver set excludes by construction. `04-08:247-249`
   actively celebrates promotion (D-78), so this is plausible drift, not exotica.
3. **Value receiver.** `04-08:108`/`:120` specify a **pointer** receiver only. The precedent being
   generalized accepts both: `test/meta/world_envelope_census_test.go:146-150` unwraps `*ast.StarExpr`
   *if present* and then matches the `*ast.Ident`. `func (s CharacterAccessServer) Foo(ctx, req) (resp, error)`
   is in `*CharacterAccessServer`'s method set and is registrable.
4. **Generated `Unimplemented…Server` promotion** — codex's case; inert, see above.

The plan's own in-tree precedent is **looser and better**: `test/meta/world_caller_census_test.go:38-42`
defines its universe as "an exported `*Service` method whose FIRST FLATTENED parameter's type
expression is `context.Context`" — no param-count, no result-shape — and its header states that this
predicate excludes lifecycle setters "**without growing an exemption list**". The tighter filter buys
nothing over the precedent and is what opens the hole. Relatedly, `world_caller_census_test.go:62-64`
records that **flattening is load-bearing** ("packs two parameters into one `*ast.Field`, so
`Params.List[1]` is not parameter 1"); `04-08` says "exactly two parameters" and never mentions it.

**Minimum edit — invert the default.** Universe = **every exported method** on the accepted receiver;
require each to land in owner ∪ public ∪ a checked-in `nonHandlerMethods` literal (empty for this
facade today; `WithSceneDEKAdder`-shaped setters would go there). Unknown shapes then fail **closed**.
If the shape filter is kept instead, it must at minimum admit `(req, stream) error`, accept value
receivers, use flattened param counting, and assert that `CharacterAccessServer`'s embedded-field set
equals a checked-in literal.

### H3 — HIGH — The public-audience literal is a working one-line bypass of the partition, and `04-08:33` asserts it is not. **BLOCKS, or must be carried as documented accepted risk.**

The partition compares `universe == owner ∪ public` and `owner ∩ public == ∅`. The **owner** literal
has a derived counterpart (methods whose bodies reference the guest-gate selector) and is compared by
set equality against it. The **public** literal has **no derived counterpart** — nothing in Task 2's
behaviors (`04-08:188-197`) derives public-audience membership from a handler body.

Consequence: a new exported RPC-shaped handler referencing neither gate goes RED in the partition's
extra group — and **the cheapest green is one line adding its name to `publicAudienceRPCs`**. After
that edit: guest-gate comparison 4 vs 4 GREEN, ownership 3 vs 3 GREEN, disjointness GREEN (it was
never in owner), partition 7 vs 7 GREEN. Nothing fires.

The plan asserts the opposite twice, and both statements are false for this case:

> **`04-08:33`** (a `must_haves` truth) — "The partition's public-audience literal is not [an excusal
> list] and cannot become one … adding a member to it does not silence a failure, it MOVES an RPC from
> one audited bucket to the other **and both buckets are compared by set equality**."
>
> **`04-08:414`** — "… it relocates the RPC into the other audited bucket and **immediately fails the
> guest-gate comparison it just left**."

Both are true only for **moving an existing owner-audience member** (which stays in the derived
guest-gate set and so breaks that comparison). They are false for a **new** handler that was never in
the guest-gate literal: it leaves nothing, so nothing fails. And "both buckets are compared by set
equality" is simply not the case — only the owner bucket is compared against a derived set; the public
bucket appears only inside the union.

There is a partial backstop: a new RPC *returning character data* is caught by Task 3's registry-derived
descriptor census unless its inventory row is added too. That covers ROADMAP criterion 1's actual scope
but not `04-08`'s broader claim, and it is the same "visible literal edit" shape (D-72) — so it narrows
the risk rather than eliminating it.

**Minimum edit (preferred — cheap and grounded):** give the public bucket its own derived predicate
using the *shipped* `bodyReferencesSelector`. Both public handlers reference `s.resolveViewerIdentity`
on their own receiver (`04-01:306`, `04-07:328`) and must NOT reference the guest gate (D-73).
Assert: every public-audience member references `resolveViewerIdentity` and references neither
`resolveAndGate` nor `ownedCharacter`. That converts the literal from an unaudited classification into
an audited bucket and makes `:33` and `:414` true as written.
**Minimum edit (fallback):** correct `:33` and `:414` and record the bypass as accepted risk.

### M1 — MEDIUM — `04-06`'s "Consumers that inherit the new rejection" paragraph is factually wrong, and the acceptance criterion built on it is vacuous. **ACCEPTABLE RISK** (does not block; the code change stays correct).

`04-06:407-420` claims Task 2 "tightens a **shipped** command with **two live in-tree consumers**
outside Phase 4, and both inherit the new `CHARACTER_INVALID` where the write previously succeeded."
Neither is live:

- **`internal/command/types.go:70`** is an interface *declaration* with **zero call sites**.
  `rg -n 'UpdateCharacterDescription' internal/command/` returns only `:69` (its doc comment) and
  `:70` (the declaration). The plan glosses it "i.e. the in-world `@desc`-style path" — there is no
  such command: `rg -ni '"desc"\|@desc\|"describe"' internal/command/` returns **no match**. An
  uncalled interface method cannot inherit a runtime rejection.
- **`internal/plugin/hostfunc/cap_property.go:140`** is the only non-test call site of
  `.UpdateCharacterDescription(` in the tree — but the capability that owns it is **never constructed
  in production**: `rg -n 'NewPropertyCapability'` returns `cap_property.go:47` (the declaration) and
  `cap_property_test.go:83`, `:95` (tests) and nothing else.

Two consequences:

1. **The added acceptance criterion is a non-gate.** `04-06:474` asserts
   "`task test -- ./internal/plugin/... ./internal/command/...` is green. This is **NOT redundant** …
   without this line a red test there would first surface at `task pr-prep`." It cannot go red:
   `internal/plugin/hostfunc`'s tests drive a `mockPropertyAccess` (`cap_property_test.go:68`) that
   never reaches `world.Service`, and `internal/command` never calls the method. A change to the
   command's *body* is unobservable in both packages. Same text at `:675`.
2. **`<reversibility>` at `04-06:488-493`** — the block cycle 4 rewrote "to be evidence-backed" —
   states "both are covered by this task's acceptance criteria, so the claim is **evidence-backed
   rather than assumed**." The evidence is wrong.

**This is not a re-raise of the settled signature-ABI decision.** The *same two citations* at
`04-06:544-550` justify rejecting option (b) (extending the signature), and that rejection remains
**sound**: those couplings are **compile-time** — `internal/command/types.go:110`
(`_ WorldService = (*world.Service)(nil)`) and `cap_property.go:34` (`PropertyAccess`) — and a
signature change breaks them regardless of call sites. The error is confined to Task 2's **runtime**
claim, which propagates only through actual calls.

Risk direction: the change is **safer** than the plan says, not riskier — live production blast
radius today is zero.

**Minimum edit:** at `:407-420`, replace "two live in-tree consumers" with "two compile-time-coupled
surfaces, neither reached in production today (`NewPropertyCapability` has no production constructor;
`types.go:70` has no caller)"; restate `:474`/`:675` as a **compile** check rather than a behaviour
gate; correct `:488-493`.

### L1 — LOW — `ValidateDescription` permits `\r`; `04-06` says "newline or tab" in seven places. **ACCEPTABLE RISK.**

`internal/world/validation.go:191`: `if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'`.
Carriage return **is** allowed. The plan says "control characters other than newline and tab" at
`04-06:28`, `:157`, `:272`, `:371`, `:381`, `:423`, `:519` — and `:371` is the `read_first` **summary
of that very function**, so it is a wrong source summary, not loose prose.

Two concrete failure modes: (a) Task 2's behaviour at `:381` ("a control character other than newline
or tab is rejected") — an executor choosing `\r` writes a test that fails and stays red; (b) `:28` and
`:272` promise the facade applies "the same rule" to the profile prose caps, so an implementer
transcribing from the prose builds a facade **stricter** than the domain, splitting a rule the plan
says is single-sourced.

**Minimum edit:** "newline, carriage return, or tab" in those seven places, or one parenthetical at `:371`.

### L2 — LOW — Two of `04-08`'s counting criteria lack the comment filter; one is false-GREEN. **ACCEPTABLE RISK.**

Cycle 4 added the filter to the three counts at `04-08:474-475`. Two more were missed:

- **`04-08:152`** — `rg -o 'func (serviceReceiverName\|bodyReferencesSelector)\(' … \| wc -l` returns 2,
  **unfiltered**. False-GREEN direction: if a helper were deleted and any comment mentioned it, the
  count still reads 2.
- **`04-08:477`** — `rg -o 'func bodyReferencesSelector\(' test/meta/ \| wc -l` returns 1, unfiltered.
  False-RED direction only (a doc comment quoting the signature inflates it).

### L3 — LOW — `04-08:481`'s banned-token grep is case-fragile. **ACCEPTABLE RISK.**

`exempt|Exempt|allowlist` misses `EXEMPT`, `Allowlist`, `AllowList`, `allow_list`. Use
`rg -i 'exempt|allow[-_ ]?list'`.

### L4 — LOW — `.planning/ROADMAP.md:484` says "**Plans**: 8 plans"; the Phase-4 block enumerates 9. **ACCEPTABLE RISK.**

`04-09` was promoted out of `04-06`'s objective (`04-06:74-80`) and the header count was never
updated. `04-03` already carries `.planning/ROADMAP.md` in `files_modified`, so the one-word fix is
in scope.

---

## Verification coverage

Drift-guard authority for this repo is `intel` (`gsd-tools drift-guard authority --raw`), under which
`AMBIGUOUS` → MEDIUM and `MISSING` → needs-acknowledgement — **neither hard-blocks**. **REFUTED** rows
are escalated above `AMBIGUOUS` deliberately: a plan asserting a mechanism that source contradicts is
a stronger failure than an unresolvable citation. Symbols under each plan's "Artifacts this phase
produces" are excluded from this table and are recorded UNCHECKABLE at the end.

| Claim / symbol | Cited at | Status | Evidence |
|---|---|---|---|
| `char.Description = description` is a bare, unvalidated assignment | `04-06:38`, `:366` | VERIFIED | `internal/world/service.go:862`; `rg -n 'char\.Description\s*=' internal/world/service.go` → exactly one match |
| Validation must sit after `checkAccess` and after the read (non-oracle) | `04-06:428-434` | VERIFIED | `checkAccess` `service.go:849`; repo read `:852`; assignment `:862` |
| `UpdateLocation` precedent: `checkAccess` → `Validate` → `oops.Code("LOCATION_INVALID").Wrap` | `04-06:372` | VERIFIED | `service.go:364`, `:367`, `:368` |
| `Character.SetDescription` is the shipped validating setter; doc comment says application code must use it | `04-06:369` | VERIFIED | `internal/world/character.go:95-108`; delegates to `ValidateDescription` at `:102` |
| `SetDescription` has zero production callers; `internal/property` hits are a different method | `04-06:369` | VERIFIED | Only non-test hit is `internal/property/definition_description.go:41`, whose interface is `entity_mutator.go:32` (6-param entity-mutator method) |
| `ValidateDescription`: empty early-return, UTF-8, byte-measured 4000 cap, control chars | `04-06:371` | VERIFIED | `internal/world/validation.go:96`, `:100`, `:103` (`len(desc)`), `:106`; `MaxDescriptionLength = 4000` at `:21` |
| …"control characters other than **newline and tab**" | `04-06:371` (+6 more) | **REFUTED (minor)** | `validation.go:191` also permits `'\r'` — L1 |
| ANSI escape sequences are the realistic rejection case | `04-06:418-424` | VERIFIED | ESC `U+001B` satisfies `unicode.IsControl` and is not `\n`/`\r`/`\t` |
| `CHARACTER_INVALID` is unused; `CHARACTER_INVALID_NAME` is a different auth-side code | `04-06:403-405` | VERIFIED | Bare code has no hits; `CHARACTER_INVALID_NAME` at `internal/auth/character_service.go:303`. `OBJECT_INVALID` analogue at `service.go:636`, `:643`, `:646` |
| `internal/plugin/hostfunc/cap_property.go:140` is a **live** consumer | `04-06:412`, `:474`, `:488`, `:675` | **REFUTED** | Real call site, but `NewPropertyCapability` has **no production constructor** (`cap_property.go:47` decl; `cap_property_test.go:83`, `:95` only) — M1 |
| `internal/command/types.go:70` is a **live** consumer / "the in-world `@desc`-style path" | `04-06:415-416`, `:474`, `:488`, `:675` | **REFUTED** | Declaration only; zero call sites in `internal/command/`; no `desc`/`@desc`/`describe` command exists — M1 |
| The same two surfaces make the **signature** load-bearing outside `world` | `04-06:544-550` | VERIFIED | Compile-time: `internal/command/types.go:110` `_ WorldService = (*world.Service)(nil)`; `cap_property.go:34` `PropertyAccess` — settled rejection of option (b) stands |
| Only two facade interfaces exist at wave 4; `04-07` is wave 5 and depends on `04-06` | `04-06:131`, `:166-172` | VERIFIED | `04-07` frontmatter `wave: 5`, `depends_on: ["04-01","04-04","04-05","04-06"]` |
| No new wave collision from the added task | `04-06:6` | VERIFIED | `internal/world/service.go` touched by 04-01 (W1), 04-09 (W2), 04-06 (W4) — strictly sequential, both declared in `depends_on`. `service_test.go` is 04-06's alone; 04-09 uses `service_profile_test.go` |
| ROADMAP criterion 4 requires over-cap rejection **server-side** | `04-06:398-400` | VERIFIED | `.planning/ROADMAP.md`, Phase 4 Success Criteria 4 |
| Accepted receiver sets: facade `{"CharacterAccessServer"}`, web `{"Handler"}` | `04-08:220-221` | VERIFIED | `internal/web` non-test receivers: `*Handler` (53), `*cookieWriter` (5), `*Server` (3 — `Start`/`Stop`/`Addr` at `internal/web/server.go:180,200,205`), `*reconnectDedup` (1). `var _ webv1connect.WebServiceHandler = (*Handler)(nil)` at `internal/web/handler.go:155`. No second server type carries handlers |
| Scene facade carries 24 `s.resolveAndGate(` / 21 `s.ownedCharacter(` | `04-08:180`, `:229-231` | VERIFIED | `rg -o … internal/grpc/sceneaccess_service.go \| wc -l` → 24 and 21 exactly |
| Census parses with `go/ast` and imports neither package | `04-08:432-436` | VERIFIED | `rg -n 'holomush/internal/grpc"' test/meta/` exits 1 |
| Gateway-boundary argument for the un-partitioned web half | `04-08:25`, `:376-381` | VERIFIED | `.claude/rules/gateway-boundary.md:19-27`; complete `internal/holomush` import set across non-test `internal/web/` is `gatewaymetrics`, `sessionlease`, `telemetry`, `ulidgen` — no store/world/access/auth. `Handler` fields (`handler.go:141-152`) are gRPC clients only |
| Derived universe is exactly six | `04-08:344-369` | VERIFIED | `GetCharacterProfile` (04-01), `ListMyCharacters`+`GetMyCharacter` (04-05), `UpdateCharacterProfile`+`UpdateCharacterDescription` (04-06), `ListCharacterDirectory` (04-07) = 4 owner + 2 public |
| "the partition proves the facade has **no RPC outside those two audiences**" | `04-08:492`, `:24` | **REFUTED** | Shape/receiver filter fails open — streaming (`internal/grpc/server.go:559`, `subscribe_handler.go:272`), embedded promotion (`04-02:105`), value receiver (`world_envelope_census_test.go:146-150`) — H2 |
| "the public-audience literal … cannot become [an excusal list]" | `04-08:33`, `:414` | **REFUTED** | Public bucket has no derived counterpart; a new handler added to it turns every assertion green — H3 |
| `:485` (names in comment only) vs `:356-360` (check in as a literal) | `04-08` | **REFUTED (self-contradiction)** | Mutually unsatisfiable; `:485` untouched by `b97d049a1` — H1 |
| `s\.ownedCharacter\(` cannot match `ownedCharacterForMutation(` | `04-08:474` | VERIFIED | Literal `(` after the name |
| Banned-token rg regexes are valid Rust-regex | `04-08:480`, `:481`, `:575` | VERIFIED | Bare `\|` alternation throughout; no `\\\|` |
| ROADMAP Phase-4 plan count | `.planning/ROADMAP.md:484` | **REFUTED** | Says 8; block enumerates 9 — L4 |
| Runtime behaviour of unwritten Phase-4 code | all plans | UNCHECKABLE | Phase-created artifacts; not treated as verified anywhere above |

## Risk Assessment

**HIGH**, and — unlike cycles 1–4 — **the residual is concentrated in one plan and one task**.

`04-06` is **execution-ready**. Its cycle-4 fix is correct, correctly positioned, and correctly
grounded in ROADMAP criterion 4; the only findings against it (M1, L1) are wrong-justification and
wrong-prose defects that make the change look *riskier and better-tested than it is*, in that order.
Neither blocks.

`04-08` **Task 2 is not ready.** H1 is an outright unsatisfiable instruction pair introduced by the
cycle-4 edit. H2 and H3 are the same root defect seen from two sides: the partition audits only the
shapes it recognises and only the bucket it derives, so both an unrecognised shape and an unaudited
bucket leave it green. Three independent reviewers found three different escapes from H2 in one cycle,
which is the strongest available signal that the filter should fail closed rather than be enumerated
tighter.

**Waves 1–5 are unblocked** (`04-01` … `04-07`, `04-09`). Every blocking finding lands in `04-08`,
which is wave 6 and the last plan in the phase — so execution may begin immediately and the three
HIGHs can be resolved while waves 1–5 run.

**Recommended disposition for the closing cycle:** fix H1 (one line, unambiguous), fix H3 by the
preferred edit (one derived predicate reusing a shipped helper), and fix H2 by inverting the universe
default (three sentences). All three are edits to a single task in a single plan and none changes the
phase's shape. Carry M1, L1, L2, L3, L4 as documented accepted risk if the loop must close now — none
of them can cause an incorrect implementation, only an over-stated one.
