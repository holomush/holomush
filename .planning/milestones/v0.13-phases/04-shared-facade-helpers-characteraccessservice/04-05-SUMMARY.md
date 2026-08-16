---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 05
subsystem: internal/grpc
tags: [grpc, abac, privacy, ginkgo, invariants, authorization]
status: complete

requires:
  - phase: 02-abac-schema-vocabulary
    provides: "D-27's derived player-keyed peers (attribute.resolveDerivedPlayerPeers), the seed:viewer-property-* term-B twins, the grid-side seed:property-* family"
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: "plan 04-01's CharacterAccessServer + its two auth-repository fields and the D-83 viewer-identity seam; plan 04-02's playerGate (resolveAndGate, ownedCharacter, per-facade guest-denial message); plan 04-04's OwnCharacter proto surface and projectPublic"
provides:
  - "CharacterAccessServer.ListMyCharacters and .GetMyCharacter — the owner audience, live"
  - "CharacterAccessServer.ownedProfileAttributes — the owner-subject property enumeration"
  - "projectOwner — the sole constructor of OwnCharacter (01-SPEC §2.3)"
  - "CharacterAccessServer embeds playerGate; NewCharacterAccessServer takes a fifth auth.CharacterRepository argument"
  - "characterGuestDenialMessage — the character facade's own guest denial"
  - "internal/grpc/characteraccess_owner_test.go — 8 test functions over the owner audience"
  - "test/integration/access/viewer_alt_linkage_test.go — L1/L2/L3, the alt-linkage regression pin"
  - "Registry invariant INV-ACCESS-15 (bound); INV-PRIVACY-9 flipped to bound"
affects: [04-06, 04-07, 04-08, phase-05-character-identity-ui]

actuals:
  tokens: 14835
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Audience split as an authorization technique: two RPCs constructing two different principals, rather than one principal widened to cover both"
    - "Fail-on-call test double — a dependency whose methods t.Fatal, so 'this path never consults X' is enforced continuously by every spec in the file rather than by one spec"
    - "world.Caller asserted by VALUE equality: its fields are unexported but the struct is comparable, so a recorded caller pins WHICH principal was built"
    - "Regression pin with an explicit reverted-mutation RED, recorded verbatim, for a property that is green on day one"
    - "Two specs differing in exactly one fixture dimension, so the contrast isolates a derivation direction rather than asserting a bare denial"

key-files:
  created:
    - internal/grpc/characteraccess_owner.go
    - internal/grpc/characteraccess_owner_test.go
    - test/integration/access/viewer_alt_linkage_test.go
  modified:
    - internal/grpc/characteraccess_service.go
    - internal/grpc/characteraccess_projection.go
    - internal/grpc/characteraccess_profile_test.go
    - internal/grpc/characteraccess_viewer_test.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/harness.go
    - test/integration/access/character_profile_read_test.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md

key-decisions:
  - "The owner audience constructs NO viewer principal anywhere. GetMyCharacter enumerates with world.HumanCaller(access.CharacterSubject(...)) so the grid-side row-keyed policies decide — the same policies that have governed `look`-side reads since Phase 2 — rather than a second rule written for the web."
  - "Embedding playerGate did NOT unify the public read onto it. resolveAndGate denies EVERY guest (INV-SCENE-64); the public profile read must YIELD a guest rung. Both resolvers ship; comment-filtered occurrence counts prove neither moved."
  - "The two direct auth-repository fields were DELETED in favour of promotion through the bare embed, so no repository handle is stored twice on one struct. The compiler enforced it."
  - "ListMyCharacters carries no profile map, and that is the shape rather than a stub: §9.2 gives the roster and GetMyCharacter distinct jobs, and enumerating per character would make the detail read redundant and turn a roster into N+1 ABAC-filtered enumerations."
  - "INV-PRIVACY-10 left PENDING. Binding it to a test proving only its facade half would be a partial binding, and TestBoundInvariantsAreGenuinelyAsserted cannot detect a partial one."
  - "Three of this plan's acceptance greps were DEFECTIVE and were corrected rather than satisfied by reshaping the artifact — see 'Acceptance-criteria corrections'."

patterns-established:
  - "A `// Verifies:` annotation may coexist with the invariant id in a Ginkgo Describe title; the binding assertion must therefore target the annotation string, not the bare id."
  - "Constructor-widening ripple: a fifth argument on NewCharacterAccessServer forced five call sites, three of them tests. Strict mocks with no expectations are the right double at the sites that must never reach the new dependency."

requirements-completed: []

coverage:
  - id: D1
    description: "ListMyCharacters and GetMyCharacter resolve the caller through the shared playerGate and construct no viewer principal at any point"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_owner_test.go#TestGetMyCharacterCarriesTheOwnersFullPropertySliceAndDrivesNoViewerPrincipal (asserts the recorded world.Caller equals HumanCaller(CharacterSubject(id)) and is NOT either viewer-flavoured caller)"
        status: pass
      - kind: other
        ref: "rg -n 'ViewerSubject|resolveViewerTier|ViewerTier' internal/grpc/characteraccess_owner.go == 0 matches"
        status: pass
    human_judgment: false
  - id: D2
    description: "Both resolvers survive: the owner audience uses resolveAndGate, the public audience keeps resolveViewerIdentity, and embedding the gate did not unify them (D-83)"
    verification:
      - kind: other
        ref: "comment-filtered `resolveViewerIdentity` count in characteraccess_service.go is 2 at HEAD and 2 after the embed; comment-filtered `resolveAndGate` count in that file is 0"
        status: pass
      - kind: unit
        ref: "task test -- -run 'ViewerIdentity|ViewerTier|Profile' ./internal/grpc/ — 04-01's guest-rung behaviors still green after the embed"
        status: pass
    human_judgment: false
  - id: D3
    description: "GetMyCharacter returns the owner's full property slice including a row the public profile withholds at the anonymous rung"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_owner_test.go#TestGetMyCharacterCarriesTheOwnersFullPropertySliceAndDrivesNoViewerPrincipal — the owner reads profile.biography's VALUE, and the nested subtest drives the SAME seeds through the real seeded engine at the anonymous rung and gets nothing"
        status: pass
    human_judgment: false
  - id: D4
    description: "A non-owned character and a nonexistent id are wire-identical, asserted as an equality between the two messages rather than against a literal"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_owner_test.go#TestGetMyCharacterReturnsAnIdenticalNotFoundForANonOwnedAndAnUnknownCharacter"
        status: pass
    human_judgment: false
  - id: D5
    description: "GetCharacterProfile returns the identical projection whether or not the reader owns the character — no self-detection branch anywhere on the public read path"
    verification:
      - kind: other
        ref: "rg -n 'isSelf|viewerIsOwner|selfView|callerOwnsCharacter' internal/grpc/characteraccess_service.go == 0 matches; GetCharacterProfile untouched by this plan"
        status: pass
    human_judgment: false
  - id: D6
    description: "A multi-alt player reading through a viewer principal does not receive a private row owned by either of their own characters"
    verification:
      - kind: integration
        ref: "test/integration/access/viewer_alt_linkage_test.go#L1 (annotated // Verifies: INV-ACCESS-15), with L2 as the paired public-row positive control"
        status: pass
    human_judgment: false
  - id: D7
    description: "A single-alt player DOES receive that character's private row through their viewer principal, so the two-alt denial is a property of the derivation direction"
    verification:
      - kind: integration
        ref: "test/integration/access/viewer_alt_linkage_test.go#L3 — identical row shape to L1, differing only in roster size"
        status: pass
    human_judgment: false
  - id: D8
    description: "INV-ACCESS-15 is hand-registered with a .planning origin spec, because the orphan check walks only the superpowers specs directory"
    verification:
      - kind: other
        ref: "docs/architecture/invariants.yaml — one INV-ACCESS-15 entry, hand-registration note beside it; task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/ green"
        status: pass
    human_judgment: false
  - id: D9
    description: "INV-PRIVACY-10's configuration-side clause remains binding pending, because v0.13 ships no mechanism enforcing it and the meta-tests cannot detect a partial binding"
    verification:
      - kind: manual_procedural
        ref: "The entry carries binding: pending and no asserted_by; rg -n 'Verifies: INV-PRIVACY-10' internal/ test/ --type go returns no match. Reason recorded inline in the YAML citing 01-SPEC §8.8 and 04-VALIDATION.md's manual-only row."
        status: pass
    human_judgment: true

duration: 24min
completed: 2026-08-11
---

# Phase 04 Plan 05: The owner audience Summary

**`ListMyCharacters` and `GetMyCharacter` are live on the shared `playerGate`, and they reach the owner's own rows through the owning character's subject rather than through a `viewer:` principal — so D-27's ALL-direction owner peer, now pinned as `INV-ACCESS-15`, never governs a path anyone depends on.**

## Performance

- **Duration:** 24 min
- **Started:** 2026-08-11T15:53:00Z (approx.)
- **Completed:** 2026-08-11T16:17:26Z
- **Tasks:** 3 completed
- **Files:** 12 (3 created, 9 modified)

## Task Commits

1. **Task 1 (TDD): the owner audience on the shared playerGate** — `1b92444e1`
2. **Task 2 (TDD): pin the viewer path against alt-linkage widening** — `8d76615e1`
3. **Task 3: bring the invariant registry current** — `841c15894`

## RED demonstrated (TDD)

**Task 1** was written test-first. The whole test file was authored and run before any production change:

```text
=== FAIL: internal/grpc  (0.00s)
FAIL	github.com/holomush/holomush/internal/grpc [build failed]

=== Errors
internal/grpc/characteraccess_owner_test.go:137:107: too many arguments in call to NewCharacterAccessServer

	have (*ownerWorldReader, *failOnCallProfileVisibility, *MockPlayerSessionRepository, *MockPlayerRepository, *MockCharacterRepository)

	want (characterAccessWorldReader, characterAccessProfileVisibility, auth.PlayerSessionRepository, auth.PlayerRepository)

internal/grpc/characteraccess_owner_test.go:208:18: undefined: characterGuestDenialMessage
```

**Task 2 is a regression pin, not a red-first gate, and is presented as such.** Its three specs are green the day they land, because they assert a property Phase 2 already shipped. Its RED was demonstrated explicitly instead: `resolveDerivedPlayerPeers`' permit-side derivation was temporarily flipped from the ALL direction to the ANY direction — the exact one-line "simplification for symmetry" the invariant exists to prevent — and the spec was run:

```text
• [FAILED] [0.008 seconds]
INV-ACCESS-15: the viewer read path never widens a character-keyed grant to the player
  [It] L1: a two-alt player reading through their viewer principal does NOT receive a
  private row owned by one of their own characters
/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03/test/integration/access/viewer_alt_linkage_test.go:126

  [FAILED] the permit-side owner peer resolves in the ALL direction: twoAltPlayer holds a
  second character the row never named, so no player-keyed owner is emitted and the viewer
  twin cannot match
  Expected
      <types.Effect>: 1
  to equal
      <types.Effect>: 0
  In [It] at: .../viewer_alt_linkage_test.go:132 @ 08/11/26 12:06:11.042

Summarizing 1 Failure:
  [FAIL] INV-ACCESS-15: the viewer read path never widens a character-keyed grant to the player
  [It] L1: a two-alt player reading through their viewer principal does NOT receive a private
  row owned by one of their own characters

Ran 3 of 79 Specs in 2.112 seconds
FAIL! -- 2 Passed | 1 Failed | 0 Pending | 76 Skipped
```

The diff that produced it, in full:

```diff
-		all := true
+		all := false
 		for _, c := range chars {
-			if _, ok := member[c]; !ok {
-				all = false
+			if _, ok := member[c]; ok {
+				all = true
 				break
 			}
 		}
```

**That is the point of the invariant.** Six lines, reads as cleanup, removes an asymmetry a reviewer would plausibly call an oversight — and broadcasts alt linkage across every character a player owns. It was reverted before the commit; `git diff --stat -- internal/access/policy/attribute/property.go` is empty on the committed tree.

The run also proves the specs are non-vacuous in the other direction: **"Ran 3 of 79 Specs"** with **2 Passed** — L2 and L3 stayed green under the flip, so L1's failure is specific to the two-alt case rather than a broken harness.

## Accomplishments

### The audience split, and why the owner path is not "the viewer path with more permissions"

`entity_properties.owner` is a **scalar** column. D-27 resolved the permit-side player-keyed peer in the ALL direction, so "this player enters the row's permit" reduces to *"the owning character is that player's only character"*. A player with two characters therefore cannot read their own private row through a `viewer:` principal.

Phase 4 had two ways out. Widen the peer to ANY — which grants the human behind a named character everything granted to that character, reachable from every other character they own. Or split the audiences. D-69 chose the split, and this plan builds the half that makes the split cheap to keep:

| Audience | Principal | Gate | Guest |
| --- | --- | --- | --- |
| `public` (`GetCharacterProfile`) | `viewer:<tier>[:<player>]` | `resolveViewerIdentity` → tier floors | **yields a guest rung** |
| `owner` (`ListMyCharacters`, `GetMyCharacter`) | `character:<id>` | `resolveAndGate` → `ownedCharacter` | **denied** (INV-SCENE-64) |

`characteraccess_owner.go` states the prohibition at the top of the file, not in a commit message: **no handler in it may construct a `viewer:` principal** — not for the enumeration, not as a fallback, not as a convenience view of the caller's own public profile. The test file enforces it by asserting the **recorded `world.Caller` by value**: `world.Caller`'s fields are unexported, but the struct is comparable, so a spec can pin that the enumeration was driven with `HumanCaller(CharacterSubject(id))` and **not** with either viewer-flavoured caller. A spec asserting only "the read succeeded" would pass just as well if the owner path resolved a viewer rung.

### Embedding the gate did NOT unify the public read onto it

With `resolveAndGate` newly in scope, re-pointing `GetCharacterProfile` at it is a one-line change that looks like deduplication. It would delete the guest rung the entire profile tier model rests on: `resolveAndGate` returns `PermissionDenied` for **every** guest, and a guest reading a public profile is an ordinary web visitor.

Both resolvers ship, and the proof is a count that did not move:

| Property | HEAD (04-01/04-04) | After the embed |
| --- | --- | --- |
| `resolveViewerIdentity` in `characteraccess_service.go`, comment-filtered | 2 | **2** |
| `resolveAndGate` in `characteraccess_service.go`, comment-filtered | 0 | **0** |
| 04-01's guest-rung behaviors (`-run 'ViewerIdentity\|ViewerTier\|Profile'`) | green | **green** |

### One handle, one home

`playerGate` is embedded **bare**, and 04-01's two direct `playerSessionRepo` / `playerRepo` fields were **deleted**. `resolveViewerIdentity`'s body is unchanged — it reaches them by promotion. The struct body is now exactly:

```go
type CharacterAccessServer struct {
	characteraccessv1.UnimplementedCharacterAccessServiceServer

	playerGate

	world      characterAccessWorldReader
	profileVis characterAccessProfileVisibility
}
```

Two copies of one repository handle on one struct is a divergence waiting to happen; the compiler enforced the deletion, and the constructor grew by exactly **one** argument rather than three.

### The registry now records what is proven and what is not

| Entry | Before | After | Why |
| --- | --- | --- | --- |
| `INV-ACCESS-15` | — | **bound** | New. Hand-registered (the orphan check walks only `docs/superpowers/specs/`). `INV-ACCESS` because that scope's description names **attribute-provider invariants** explicitly, while `INV-PRIVACY`'s boundary forwards ABAC policy evaluation there by name. |
| `INV-PRIVACY-9` | pending | **bound** | Both preconditions verified first (below). |
| `INV-PRIVACY-10` | pending | **pending** | Deliberate. Recorded inline. |

**Highest allocated number in each scope before this change, enumerated rather than assumed:** `INV-ACCESS-14` and `INV-PRIVACY-11`. `rg -n 'INV-ACCESS-1[6-9]|INV-ACCESS-[2-9][0-9]'` returns no match, so 15 was genuinely the next free number in that scope.

**The `INV-PRIVACY-9` flip was gated on two checks, both run before the edit:**

1. `rg -c 'Verifies: INV-PRIVACY-9' test/integration/access/character_profile_read_test.go` → **1**, at line 286, immediately above spec P3. 04-01 Task 1 placed it; **this plan did not edit that file's annotation** (its only edit there is the constructor's fifth argument).
2. Every clause of the summary is genuinely asserted by P3: `status.Code` equality **and** `codes.NotFound`, `status.Convert(...).Message()` equality, `proto.Marshal` **body** equality, and an explicit assertion that the internal code string never reaches the wire message. Nothing in the summary is unasserted, so no coverage issue was filed.

**`INV-PRIVACY-10` stays pending, and the reason is now in the YAML rather than only in a plan.** Its statement has two clauses; v0.13 enforces only the first. The engine is deny-overrides, so an admin-authored `forbid` row beats the seeded permit and an operator *can* put `name` or `pronouns` out of reach of a viewer who reached the profile, with nothing objecting. Binding the entry to a test proving the facade half alone would be a **partial** binding — and `TestBoundInvariantsAreGenuinelyAsserted` detects a *Skip-only* binding, not a partial one, so there is nothing downstream to catch it. `rg -n 'Verifies: INV-PRIVACY-10' internal/ test/ --type go` returns no match.

## Acceptance-criteria corrections

Three of this plan's acceptance greps were defective as written. All three were **corrected rather than satisfied by reshaping the artifact** — the same class 04-01 and 04-04 each recorded, and the failure mode `.claude/rules/references/plan-review-learnings.md` names: the criterion goes red exactly when the plan is followed correctly.

| Criterion as written | Why it is defective | Corrected form | Result |
| --- | --- | --- | --- |
| Task 1: "`rg -o 'resolveViewerIdentity' internal/grpc/characteraccess_service.go \| wc -l` is unchanged from what 04-01 left" | The count is **3 → 5**, entirely because this plan's new doc comment on the embedded `playerGate` names the helper twice in order to explain *why* the public path does not use `resolveAndGate` — which is exactly what the sibling `resolveAndGate` criterion in the same bullet already anticipated and fixed with comment-filtering. Counting comment mentions makes the criterion fail on a correct, well-documented change. | `rg -v '^\s*//' … \| rg -o 'resolveViewerIdentity' \| wc -l`, the plan's own idiom — **2 at HEAD, 2 after** | **unchanged** |
| Task 1: "`rg -n -A 12 'type CharacterAccessServer struct' …` shows a bare embedded `playerGate` and no `playerSessionRepo`/`playerRepo` declarations" | The struct's doc comment is 18 lines, so an `-A 12` window ends **inside the comment** and never reaches the field list. The criterion's own parenthetical states the intended property correctly; only the window is wrong. | `awk '/^type CharacterAccessServer struct \{/,/^\}/' … \| rg -v '^\s*//'` — prints the whole body | bare `playerGate`; **no** `playerSessionRepo`/`playerRepo` |
| Task 2: "`rg -c 'INV-ACCESS-15' test/integration/access/viewer_alt_linkage_test.go` returns 1" | Two matching lines: the `// Verifies:` annotation **and** the Ginkgo `Describe` title, which names the invariant so the spec is discoverable. `-c` also counts matching *lines*, not occurrences. The criterion's stated intent is one annotation, correctly positioned. | `rg -c 'Verifies: INV-ACCESS-15' …` | **1**, at line 125, immediately above the L1 two-alt spec |
| Task 3: "`rg -n 'INV-PRIVACY-10' internal/ test/ --type go` returns no match — nothing is annotated against it" | One **pre-existing** match, `internal/access/policy/seed.go:697`, landed by Phase 2 (`2d9bdab52`) and untouched here — a prose comment explaining the §8.8 clause, not an annotation. The criterion can never return zero and would not detect a fabricated binding if it did. Same shape as 04-04's `SystemCaller` correction. | `rg -n 'Verifies: INV-PRIVACY-10' internal/ test/ --type go` | **0** |

Every other acceptance criterion passed as written: zero `ViewerSubject|resolveViewerTier|ViewerTier` in `characteraccess_owner.go`; exactly one `projectOwner` definition in `characteraccess_projection.go` with call sites only in the owner handlers; `OwnCharacter{` matching only inside `projectOwner`; zero `isSelf|viewerIsOwner|selfView|callerOwnsCharacter`; one `INV-ACCESS-15` entry with no `INV-ACCESS-16` or higher; `INV-PRIVACY-10` carrying `binding: pending` and no `asserted_by`; `INV-PRIVACY-9` listing the differential spec in `asserted_by`; the non-ownership opacity assertion phrased as an equality between the two messages; the visibility-evaluator double failing on call rather than returning a zero value; and an empty `git diff --stat` on `property.go`.

## Files Created/Modified

**Created**

- `internal/grpc/characteraccess_owner.go` — `ListMyCharacters`, `GetMyCharacter`, `ownedProfileAttributes`, and the file-level statement of the D-69 prohibition
- `internal/grpc/characteraccess_owner_test.go` — 8 test functions, every one of them wired to `failOnCallProfileVisibility`
- `test/integration/access/viewer_alt_linkage_test.go` — L1 (`// Verifies: INV-ACCESS-15`), L2, L3

**Modified**

- `internal/grpc/characteraccess_service.go` — bare embedded `playerGate`; the two direct auth-repository fields deleted; `characterGuestDenialMessage`; `NewCharacterAccessServer` + one argument
- `internal/grpc/characteraccess_projection.go` — `projectOwner`
- `internal/grpc/characteraccess_profile_test.go`, `internal/grpc/characteraccess_viewer_test.go` — constructor's fifth argument
- `cmd/holomush/sub_grpc.go`, `internal/testsupport/integrationtest/harness.go` — the two production-shaped call sites
- `test/integration/access/character_profile_read_test.go` — constructor's fifth argument
- `docs/architecture/invariants.yaml`, `docs/architecture/invariants.md` (generated)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `NewCharacterAccessServer` has FIVE call sites, not the two the plan names**

- **Found during:** Task 1
- **Issue:** The plan's `read_first` and `files_modified` name `cmd/holomush/sub_grpc.go` and `internal/testsupport/integrationtest/harness.go`. Three more exist and the package does not compile without them: `internal/grpc/characteraccess_viewer_test.go:99`, `internal/grpc/characteraccess_profile_test.go` (×2), and `test/integration/access/character_profile_read_test.go:121`.
- **Fix:** Each updated with the minimum the new signature forces. The two `internal/grpc` test sites pass `authmocks.NewMockCharacterRepository(t)` — a **strict** mock with no expectations, deliberately not `nil`, so a future regression routing a public read through the owner gate fails loudly instead of nil-panicking.
- **Files modified:** the three above
- **Commit:** `1b92444e1`

**2. [Rule 3 - Blocking] the access integration suite's `env.charRepo` does not satisfy `auth.CharacterRepository`**

- **Found during:** Task 1, first `task test:int`
- **Issue:** `env.charRepo` is `*worldpg.CharacterRepository`, which is missing `CountByPlayer` (and the rest of the auth-side surface). `*worldpg.CharacterRepository does not implement auth.CharacterRepository`.
- **Fix:** `setup.NewCharRepoAdapter(env.pool, env.charRepo)` — the **production** adapter, already imported in that file. No spec there reaches the owner audience, but wiring the real adapter rather than `nil` means a spec added later exercises the same shape `cmd/holomush` does.
- **Files modified:** `test/integration/access/character_profile_read_test.go`
- **Commit:** `1b92444e1`

**3. [Rule 3 - Blocking] `yamlfmt` reflowed the new registry entries**

- **Found during:** Task 3
- **Issue:** `task lint` exited 201 on `lint:yaml` after the hand-written YAML additions.
- **Fix:** `task fmt`, then `go run ./cmd/inv-render` again. The reflow touched **only** the 36 added lines (`git diff --stat` on the YAML: 36 insertions, 1 deletion, all this plan's); the summaries are folded scalars, so line-wrapping changed no value and the generated Markdown was unaffected.
- **Files modified:** `docs/architecture/invariants.yaml`
- **Commit:** `841c15894`

### Scope Decisions

**4. `ListMyCharacters` carries no profile map**

01-SPEC §9.2 gives the two owner reads distinct jobs: `ListMyCharacters` is "the owner's own roster", `GetMyCharacter` is "one owned character **in full, for the edit surfaces**". Enumerating every roster character's property rows would make `GetMyCharacter` redundant and turn a picker read into N+1 ABAC-filtered enumerations. The roster carries identity, lifecycle status and the concurrency token — what a picker needs — and the rows arrive when a character is opened. This is stated at the handler and in `projectOwner`'s doc (`a nil profile is the ROSTER shape, not a missing profile`), so it is **not** a stub: nothing is unwired, and no future plan is required to change it.

**5. `IDENT-02` deliberately NOT marked complete**

The plan frontmatter lists `requirements: [IDENT-02, PROFILE-03]`. IDENT-02 is *"a player can edit their character's prose fields … with server-enforced length caps"*. This plan ships **no mutation surface and no length cap** — it ships the two owner **reads**. It contributes the gate and the projection those writes will use; it does not deliver the capability, which lands in 04-06/04-07. This is the third consecutive plan in this phase to decline IDENT-02 (04-02 §3, 04-04 §5).

`PROFILE-03` was already marked Complete by 04-04 on both surfaces (checkbox line 152, traceability row line 381), and this plan's owner audience is a further contribution to the same requirement rather than a new claim. **`requirements mark-complete` was therefore not invoked at all**, which also avoids the known half-write (`table_unmatched` / `applied: false`) touching `REQUIREMENTS.md` for no gain. `PROFILE-04`'s pre-existing checkbox/row split was left alone as instructed.

**6. The owner enumeration inherits the grid-side colocation clause on public rows**

`ownedProfileAttributes` drives `world.HumanCaller(access.CharacterSubject(...))`, exactly as the plan directs, "so the grid-side row-keyed policies decide". A consequence worth naming for 04-06/04-07 and for phase verification: `seed:property-private-read` gates on `resource.property.owner == principal.character.id` and matches unconditionally for the owner, but `seed:property-public-read` also carries `principal.character.location == resource.property.parent_location`. A **public**-visibility row on a character with **no location** is therefore denied to that character's own subject. This is the shipped corpus answering, not a decision this plan made, and it is not papered over here — flagged for the phase gate rather than worked around.

---

**Total deviations:** 3 auto-fixed (all Rule 3) + 3 recorded scope decisions
**Impact on plan:** No scope creep. All three auto-fixes were compile- or lint-forced; the scope decisions record a shape, a declined requirement, and a corpus consequence.

## Verification

| Gate | Result |
| --- | --- |
| `task test -- -run 'MyCharacter\|Owner\|ViewerIdentity\|ViewerTier\|Profile' ./internal/grpc/` | green, 57 tests |
| `task test -- ./internal/grpc/ ./internal/web/ ./cmd/holomush/` | green, 1534 tests |
| `task test` (whole repo) | exit 0 |
| `task test:int` (whole repo, on the committed tree) | exit 0 |
| `task test:int -- ./test/integration/access/...` | green |
| `task test -- -run 'Invariant\|Provenance' ./test/meta/` | green, 10 tests |
| `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | green, 7 tests |
| `go run ./cmd/inv-render -check` | exit 0 — no drift |
| `task lint` | exit 0 |
| `task build` | exit 0 |

## Known Stubs

None. Two absences are deliberate and stated at their sites:

- `ListMyCharacters` emits no profile map — the roster shape, per §9.2's split of the two owner reads (deviation 4). Documented on the handler and in `projectOwner`.
- `primary_image` and `gallery` are routed but always empty, because v0.13 mints no media identifier (§9.7). Inherited unchanged from `projectPublic`; the routing is implemented, there is simply no row to route.

## Threat Flags

None. Every surface this plan adds is inside its own threat register: the audience split and the enumeration subject are **T-04-03**, ownership resolution is **T-04-11**, the not-found equality is **T-04-12**, the absent self-detection branch is **T-04-17**, and the registry honesty is **T-04-18**. No new network endpoint, auth path, file access pattern or schema change was introduced — `internal/access/` was touched only by a test, and `property.go`'s temporary RED mutation was reverted before any commit.

## Notes for the Phase Gate

- **`/holomush-dev:review-abac` remains mandatory for the phase** (04-01's seed additions trigger it), but this plan adds **no policy text**: `internal/access/` carries one new *test* file and nothing else. The temporary `resolveDerivedPlayerPeers` flip was reverted and is absent from all three commits.
- **Deviation 6 is the one open question for a reviewer**: an owner's *public*-visibility profile row is unreadable by the owning character's own subject when that character has no location, because the grid-side public-read permit is colocation-gated. Private rows are unaffected. 04-06/04-07 write through the same subject and should know this before choosing a visibility for the rows they create.
- Plans 04-06 and 04-07 add handlers over a constructor that now takes **five** arguments and a facade that now promotes `resolveAndGate`, `ownedCharacter`, `charRepo`, `playerRepo` and `playerSessionRepo`.

## Self-Check: PASSED

- `internal/grpc/characteraccess_owner.go` — FOUND
- `internal/grpc/characteraccess_owner_test.go` — FOUND
- `test/integration/access/viewer_alt_linkage_test.go` — FOUND
- `docs/architecture/invariants.yaml` — FOUND
- commit `1b92444e1` — FOUND in `git log --oneline --all`
- commit `8d76615e1` — FOUND
- commit `841c15894` — FOUND
