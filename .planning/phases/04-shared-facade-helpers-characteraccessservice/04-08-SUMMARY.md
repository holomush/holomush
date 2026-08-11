---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 08
subsystem: test/meta
tags: [meta-test, census, set-equality, go-ast, proto-descriptors, tdd]
status: complete

requires:
  - phase: 01-portal-spec
    provides: "§2.6 (the census as the sole mandated gate, the exact fully-qualified key, the symmetric-difference diff), §3.1–3.5 (the read-surface inventory and the name-reachable class), §9.2/§9.3 (the Phase-4 surface tables)"
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: "04-02's playerGate (resolveAndGate, ownedCharacter); 04-05's two owner reads; 04-06's ownedCharacterForMutation and the two owner writes; 04-07's ListCharacterDirectory and the WebListAllCharacters retirement; 04-04's Web* proxy shape"
provides:
  - "test/meta/characteraccess_routing_census_test.go — criterion 1's binding: three set-equality comparisons, a verified ownership indirection, a fail-closed audience partition and a derived-public counterpart"
  - "test/meta/character_rpc_census_test.go — §2.6's mandated descriptor census over the 39-member read-surface inventory"
  - "facadeReceiverName — a receiver predicate over a SET of accepted type names"
  - "bodyReferencesIdent — a bare-identifier body predicate that does not match a same-named field selector"
  - "setSymmetricDifference — the §2.6 symmetric-difference diff with stable ordering"
affects: [05-character-identity-ui, 06-admin-surface]

actuals:
  tokens: 18000
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A fail-closed classification universe: every member is positively classified or fails BY NAME, so an unrecognized shape is a finding rather than a silent exclusion"
    - "A checked-in literal given a DERIVED counterpart compared by set equality, so the literal cannot become a place to park an unclassified member"
    - "An accepted indirection whose own body is asserted to reach the thing it indirects, positioned BEFORE the comparison that consumes it"
    - "RED demonstrated per ASSERTION rather than per test file, because one demonstration proves only the assertion it moved"

key-files:
  created:
    - test/meta/characteraccess_routing_census_test.go
    - test/meta/character_rpc_census_test.go
  modified:
    - test/meta/meta_helpers_test.go

key-decisions:
  - "The web half's universe is scoped by the facade-client selector (h.characterAccess), not by the proxy-name prefix. internal/web's *Handler carries every domain's proxies, and all of them read the session-token header, so neither the receiver nor the header conjunct separates the character surface."
  - "WorldQueryService.QueryCharacter is a NAME-REACHABLE census member, not a type-reachable one. Its response declares id/player_id/name/description/location_id as inline bare scalars, so no typed message exists for the descriptor predicate to find. §3.3's own row says so in its message column; its Notes column just does not use the words."
  - "The expected descriptor set carries only §9 rows that are DECLARED today. The Admin* surfaces, CreateCharacter's reshape and retire/un-retire are named in §9's tables but are not in the tree, so each arrives with its own inventory row under D-72 rather than sitting permanently in the missing group."
  - "No second gate was added for criterion 5. The narrow-interface compile fence is the decided mechanism and this plan left it alone."
  - "Three of this plan's acceptance criteria were DEFECTIVE and were corrected rather than satisfied by reshaping the artifact — see 'Acceptance-criteria corrections'."

patterns-established:
  - "Prove the RED for a shared HELPER by inverting its own implementation, when helper and test necessarily share a file and an ordering-based RED is unavailable"
  - "Demonstrate a two-step bypass as two REDs on one probe: the ungated handler, then the one-line 'fix' for it, with the second capture proving the fix is not one"

requirements-completed: []

coverage:
  - id: D1
    description: "Every owner-audience facade RPC reaches the ONE shared guest gate, proven by set equality in both directions with a symmetric-difference diff"
    verification:
      - kind: unit
        ref: "test/meta/characteraccess_routing_census_test.go#TestCharacterAccessRoutingCensusGuestGate (4-member literal; RED (a) captured)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Every owner-audience facade RPC naming a character id reaches the ONE shared ownership resolution — the two write RPCs through a single checked-in indirection whose own body is verified first"
    verification:
      - kind: unit
        ref: "#TestCharacterAccessRoutingCensusOwnership (3-member literal; wrapper integrity precedes the comparison; REDs (b) and (c) captured)"
        status: pass
    human_judgment: false
  - id: D3
    description: "The facade has NO exported method outside its audience classification — the case the three gate comparisons are structurally blind to"
    verification:
      - kind: unit
        ref: "#TestCharacterAccessRoutingCensusAudiencePartition (unary RPCs == owner ∪ public, literals disjoint; RED (d) captured)"
        status: pass
    human_judgment: false
  - id: D4
    description: "The public-audience literal cannot absorb an unclassified handler: its membership is recomputed from the code"
    verification:
      - kind: unit
        ref: "#TestCharacterAccessRoutingCensusDerivedPublicAudience (RED (e) captured — the one-line bypass turns the partition green and this comparison red)"
        status: pass
    human_judgment: false
  - id: D5
    description: "The classification universe fails CLOSED: a streaming signature, an unlisted setter shape, a value receiver and an unnamed receiver all stay in the universe and fail by name"
    verification:
      - kind: unit
        ref: "#TestCharacterAccessRoutingCensusClassifierFailsClosed (driven from parsed fixtures, not from production code)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Method promotion cannot place an exported method on the facade surface that the declaration walk never sees"
    verification:
      - kind: unit
        ref: "#TestCharacterAccessRoutingCensusPromotionGuard (embedded-field literal + every-method-unexported over playerGate)"
        status: pass
    human_judgment: false
  - id: D7
    description: "Every owner-audience web proxy forwards the session-token header AND names its paired facade method, proven by set equality"
    verification:
      - kind: unit
        ref: "#TestCharacterAccessRoutingCensusWebProxies (4-member literal, universe scoped by the facade-client selector)"
        status: pass
    human_judgment: false
  - id: D8
    description: "The census scope is character-only; admitting the scene facade fails loudly rather than being repaired by widening the expected literals"
    verification:
      - kind: unit
        ref: "#TestCharacterAccessRoutingCensusIsCharacterScoped"
        status: pass
    human_judgment: false
  - id: D9
    description: "The set of RPCs whose response carries character data equals the checked-in §3 inventory, keyed on the exact fully-qualified proto method name"
    verification:
      - kind: unit
        ref: "test/meta/character_rpc_census_test.go#TestCharacterReturningRPCCensusMatchesTheReadSurfaceInventory (39 members; RED captured in BOTH directions)"
        status: pass
    human_judgment: false
  - id: D10
    description: "Plan 04-07's RPC retirement is complete at the DESCRIPTOR level, not only in source"
    verification:
      - kind: unit
        ref: "#TestCharacterReturningRPCCensusRemovedWebDirectoryRPCIsGone"
        status: pass
    human_judgment: false
  - id: D11
    description: "The name-reachable enumeration's membership is exact in both directions, so dropping a member is a visible edit"
    verification:
      - kind: unit
        ref: "#TestCharacterReturningRPCCensusNameReachableMembershipIsExact"
        status: pass
    human_judgment: false
  - id: D12
    description: "The comparison key is an exact string; the substring collapse §2.6 forbids is derived from the inventory and shown to be live"
    verification:
      - kind: unit
        ref: "#TestCharacterReturningRPCCensusKeyIsAnExactString (17 live method-name collapses; the exact key keeps each member its own)"
        status: pass
    human_judgment: false
  - id: D13
    description: "Neither census can pass vacuously: every discovered universe is asserted non-empty before its comparison"
    verification:
      - kind: other
        ref: "characterFacadeUniverse, characterWebProxyUniverse, the unary-shape set, the registry service count, the method set, the character-shaped message set, the playerGate method count, the struct-declaration count"
        status: pass
    human_judgment: false

duration: 37min
completed: 2026-08-11
---

# Phase 04 Plan 08: The two censuses Summary

**Criterion 1 is bound by set equality rather than by a per-endpoint suite — and the assertion that actually earns the guarantee is the one none of the three set comparisons could make: an audience partition, fail-closed over every exported method on the facade, whose public side is recomputed from the code so the obvious one-line escape from it is itself a RED.**

## Performance

- **Duration:** 37 min
- **Started:** 2026-08-11T17:50:00Z
- **Completed:** 2026-08-11T18:27:16Z
- **Tasks:** 3 completed
- **Files:** 3 (2 created, 1 modified)

## Task Commits

1. **Task 1 — the three shared census helpers** — `607fc6b8e`
2. **Task 2 — the routing census** — `ce0089a6f`
3. **Task 3 — the descriptor census** — `e25a4b828`

## RED demonstrated

### Task 1 — three helper contracts, by inversion

Helpers and their tests necessarily share `meta_helpers_test.go` (the plan permits "the same file or a sibling test file", and `files_modified` names only that file), so an ordering-based compile RED was unavailable. Each contract was instead **inverted in place** and the failure captured — stronger evidence than `undefined: facadeReceiverName`, because it proves the assertion is load-bearing rather than merely that the symbol was absent:

```text
=== FAIL: test/meta TestFacadeReceiverNameAcceptsOnlyAcceptedNamedReceivers (0.00s)
    Messages: a receiver with no variable name is rejected — there is no name to build a selector against

=== FAIL: test/meta TestBodyReferencesIdentMatchesABareIdentifierAndNotASameNamedFieldSelector (0.00s)
    Messages: a same-named FIELD SELECTOR on an unrelated expression is NOT a match — reading some struct's field of that name is not reading the package constant

=== FAIL: test/meta TestSetSymmetricDifferenceReportsBothDirectionsAndRendersStably (0.00s)
    Error: Not equal:
        expected: "extra (derived but not expected): [alpha mu zeta]; missing (expected but not derived): []"
        actual  : "extra (derived but not expected): [zeta alpha mu]; missing (expected but not derived): []"
    Messages: the rendered shape is exact, so a census failure message is reviewable rather than incidental

DONE 3 tests, 3 failures in 0.335s
```

The three inversions were: dropping the receiver-must-be-named check, replacing the selector-skipping walk with a naive `ast.Inspect`, and removing the two sorts.

### Task 2 — five REDs, one per assertion

**(a) Guest-gate predicate.** `s.resolveAndGate(` removed from `ListMyCharacters`:

```text
=== FAIL: test/meta TestCharacterAccessRoutingCensusGuestGate (0.04s)
    Messages: the facade methods reaching the shared guest gate (s.resolveAndGate) do not equal the
    checked-in owner-audience set. … extra (derived but not expected): []; missing (expected but not
    derived): [ListMyCharacters]
```

**(b) Ownership predicate.** `s.ownedCharacterForMutation(` removed from `UpdateCharacterProfile` — the assertion 04-06's handoff exists to make possible, and the one that would never have been exercised had the predicate known only the base name:

```text
=== FAIL: test/meta TestCharacterAccessRoutingCensusOwnership (0.09s)
    Messages: the facade methods reaching the shared ownership resolution (s.ownedCharacter, or a
    checked-in wrapper) do not equal the checked-in set. extra (derived but not expected): [];
    missing (expected but not derived): [UpdateCharacterProfile]
```

**(c) Wrapper integrity.** `s.ownedCharacter(` removed from `ownedCharacterForMutation`'s own body:

```text
=== FAIL: test/meta TestCharacterAccessRoutingCensusOwnership (0.08s)
    Messages: the accepted ownership indirection "ownedCharacterForMutation" no longer references
    s.ownedCharacter in its OWN body. Both mutations still name the wrapper, so the set comparison
    below stays green — this assertion is the only thing standing between a gutted wrapper and a
    silently ungated write surface
```

**The asymmetry the plan predicted was confirmed, not assumed.** The integrity assertion uses `require`, which short-circuits, so a first run could not show what the set comparison did. It was temporarily softened to `assert` and re-run against the same gutted wrapper: the test then executed both, and **only the integrity assertion failed** — the ownership set comparison at the line below it stayed GREEN, because both mutations still reference the wrapper and so remain members. That is exactly why part 2 exists. The softening was reverted with the probe.

**(d) Audience partition.** An exported, RPC-shaped `TemporaryRedProbeUngatedHandler` added to `*CharacterAccessServer`, referencing neither gate name:

```text
=== FAIL: test/meta TestCharacterAccessRoutingCensusAudiencePartition (0.04s)
    Messages: the facade's unary-RPC-shaped exported methods do not equal owner-audience ∪
    public-audience. An EXTRA member is an RPC carrying NO audience classification at all — the case
    the three gate comparisons cannot see, because it joins no derived set and no expected set and
    leaves their symmetric difference empty. … extra (derived but not expected):
    [TemporaryRedProbeUngatedHandler]; missing (expected but not derived): []

DONE 8 tests, 1 failure in 0.600s
```

**`DONE 8 tests, 1 failure` is the load-bearing line.** (a), (b) and (c) all stayed GREEN through (d) — the partition is the sole assertion that moved, which is the whole demonstration: before it existed, this exact edit left the census entirely green.

**(e) The one-line bypass, closed.** With (d)'s stub still in place, its name was added to the public-audience literal — the exact "fix" a hurried author reaches for. The partition went GREEN, and the census still failed:

```text
=== FAIL: test/meta TestCharacterAccessRoutingCensusDerivedPublicAudience (0.04s)
    Messages: the facade's gate-free, s.resolveViewerIdentity-resolving unary RPCs do not equal the
    checked-in public-audience set. A MISSING member is a name in that literal with no public read
    pipeline behind it — the one-line move a hurried author reaches for when the audience partition
    goes RED against a wholly ungated handler. … missing (expected but not derived):
    [TemporaryRedProbeUngatedHandler]
```

Both edits were then reverted together.

### Task 3 — RED in both directions

**Extra.** A real RPC was added to `characteraccess.proto` and `task proto` run, so the descriptor walk had to genuinely discover it — not simulated by deleting an inventory row:

```text
=== FAIL: test/meta TestCharacterReturningRPCCensusMatchesTheReadSurfaceInventory (0.00s)
    Messages: … An EXTRA member is a character-returning endpoint that shipped without an inventory
    row and therefore without an audience verdict or a projection … extra (derived but not expected):
    [holomush.characteraccess.v1.CharacterAccessService.TemporaryRedProbeGetCharacter]; missing: []
```

**Missing.** An inventory row for an RPC that does not exist:

```text
=== FAIL: test/meta TestCharacterReturningRPCCensusMatchesTheReadSurfaceInventory (0.00s)
    Messages: … A MISSING member is an inventory row whose RPC no longer exists, which quietly
    shrinks what this census covers. extra (derived but not expected): []; missing (expected but not
    derived): [holomush.web.v1.WebService.WebTemporaryRedProbeListCharacters]
```

The proto was restored and `task proto` re-run; `git status --short -- api/ pkg/proto/` is empty, and `rg -n 'TemporaryRedProbeGetCharacter' api/ internal/ pkg/ test/meta/` and `rg -n 'WebTemporaryRedProbeListCharacters' test/meta/` both return no match.

## Accomplishments

### The handoff 04-06 wrote was load-bearing, and RED (b) is the proof

Both write RPCs reference `s.ownedCharacterForMutation`, never `s.ownedCharacter`. A predicate knowing only the base name would have been permanently RED, and the only cheap repair — dropping the two write RPCs from the ownership literal — would have left this phase's highest-risk surface outside criterion 1's proof while the census reported green. The census teaches **both** names, and RED (b) is the demonstration that the second one is genuinely exercised rather than decorative.

The indirection is closed, verified and additive:

| Property | Mechanism |
| --- | --- |
| closed | a one-member checked-in set; a future `ownedCharacterForX` is a non-member and goes RED |
| verified | the wrapper's own body is asserted to reach `s.ownedCharacter` **before** the comparison consumes it — RED (c) |
| additive | it adds an accepted call shape to the DERIVED side only; an ungated handler names neither and lands in the missing group unchanged |

### The partition is what makes the must-haves' third truth true rather than asserted

The three set comparisons derive membership from what a body **references**. A handler referencing neither gate name joins no derived set and no expected set, so the symmetric difference is empty and the census is green. RED (d) shows that concretely: the ungated stub left (a), (b) and (c) all passing.

The universe is **fail-closed**, which is the part a filter built for unary shapes gets wrong:

| Shape | Outcome |
| --- | --- |
| unary RPC (`ctx` first, `error` second) | enters the audience partition |
| a checked-in non-handler | classified, and additionally asserted **not** to be unary-shaped |
| server-streaming, or anything else | **fails by name** |
| value receiver, unnamed receiver | stays **in** the universe (the collector deliberately does not use `facadeReceiverName`) and fails downstream by name |

The non-handler literal ships **empty** — this facade declares no such method today — and both failing shapes are driven from parsed **fixtures** rather than by adding them to production code.

Method promotion is the one vector the `FuncDecl` walk cannot see, so both embeds are pinned: the embedded-field list is compared by set equality, and every method on the in-repo `playerGate` is asserted unexported. The generated `Unimplemented` embed's residue is inert by construction (`codes.Unimplemented`, no grant) and is audited at the descriptor level by Task 3.

### The public literal has a derived counterpart, so it is not an escape hatch

The literal sits on the classified side of the partition, and its membership is **recomputed from the code**: a member must be unary-shaped, must reference `s.resolveViewerIdentity`, and must reference no gate name. RED (e) is the demonstration that this closes the bypass — parking the ungated stub's name in the literal satisfied the partition and failed the derived-public comparison instead.

Three constructs in that file could be mistaken for a way out of scrutiny and none is: the wrapper set names a **call shape** on the derived side, the public literal has the derived counterpart above, and the non-handler literal carries a not-RPC-shaped guard over any future member. The file contains none of the erosion vocabulary (`rg -in 'exempt|allow[-_ ]?list'` → no match).

### The descriptor census found a real inventory gap on its first run

`holomush.plugin.host.v1.WorldQueryService.QueryCharacter` came back **missing**. It is not a census defect and not a SPEC error — §3.3's own "character-shaped message returned" column for that row is the inline field list `id/player_id/name/description/location_id`, and `QueryCharacterResponse` (`api/proto/holomush/plugin/host/v1/world.proto:96-108`) declares every one of them as a bare `string`. There is no typed message for a descriptor-graph predicate to reach, so it belongs to §3.2's **name-reachable** class exactly as `SelectCharacter` and `CreateCharacter` do; §3.3's Notes column simply does not say the words for that row. It was added to the name-reachable enumeration with the reason recorded at its site.

That is the census working: the first thing it did was disagree with a hand-transcribed table, and the disagreement was real.

Final derived set: **39 members**, equal in both directions.

### The SPEC's own substring example does not hold, and the trap it describes is still live

§2.6 justifies the exact-string key with *"`…ListCharacters` is a substring of `…ListAllCharacters`"*. It is not — `ListCharacters` does not occur inside `ListAllCharacters`. My first fixture repeated a variant of the same mistake (`WebListCharacters` in `WebListCharacterDirectory`) and the test caught it immediately:

```text
Error: "holomush.web.v1.WebService.WebListCharacterDirectory" does not contain
       "holomush.web.v1.WebService.WebListCharacters"
```

Rather than delete the assertion, the collapse was **derived from the inventory**. No fully-qualified name is a substring of another — the package-and-service prefixes differ — but **17 pairs of METHOD names collapse**, `GetCharacter` into `GetCharacterProfile` among them. That is precisely why §2.6 fixes the key at the fully-qualified name **and** forbids a Go handler identifier, which is method-name-shaped. The test now scans the inventory for live collapses, requires at least two distinct names to be foldable, and pins one named real pair; the file header was corrected to say this instead of repeating the SPEC's example.

### Reuse before building

`world_envelope_census_test.go` and `world_caller_census_test.go` are **untouched**. The routing census calls their shipped leaf helpers directly — `bodyReferencesSelector`, `flattenedParamTypes`, `isContextContext`, `isIdentNamed` — rather than copying them: `rg -v '^\s*//' test/meta/ | rg -o 'func bodyReferencesSelector\(' | wc -l` is **1** across the whole directory, and `world_envelope_census_test.go` still declares its own `serviceReceiverName` and `bodyReferencesSelector` (comment-filtered count **2**).

One predicate genuinely had no precedent — "does this body name method X on *any* expression", which a web proxy needs because it calls its paired facade method on a client field. It is declared **local to the routing census**, not promoted to the shared helper file, so Task 1's contract (three helpers) is unchanged.

## Acceptance-criteria corrections

Three of this plan's criteria were defective as written. All were **corrected rather than satisfied by reshaping the artifact**, per the phase's standing protocol.

| Criterion as written | Why it is defective | Corrected form | Result |
| --- | --- | --- | --- |
| Task 2: "`rg -v '^\s*//' internal/grpc/characteraccess_write.go \| rg -o 's\.ownedCharacterForMutation\(' \| wc -l` returns 2 (one per mutation handler)" | Returns **3**, and did so at HEAD before this plan touched anything. There is a **third** call site the plan does not account for: `ownerMutationResponse` (`characteraccess_write.go:326`), the shared post-write response helper 04-06 introduced so both mutations re-resolve the bumped version. The criterion's premise ("one per mutation handler") was simply incomplete. | count all three sites, and verify the reverts by the live working tree instead: `git diff --stat -- internal/grpc` | **empty** — every probe fully reverted. Counts: `s.resolveAndGate(` = **4** ✓, `s.ownedCharacterForMutation(` = **3** (not 2), `s.ownedCharacter(` = **1** ✓ |
| Task 2: "`rg -n 'os.ReadDir' … matches at least twice, once per directory`" | Assumes the directory walk is inlined once per directory. It is extracted into one shared `parseGoDir` helper called twice — which is the reuse-before-building rule this phase applies everywhere else. The criterion's INTENT ("the file walks a directory rather than naming a single Go file") is met. | `rg -o 'parseGoDir\(t, "internal", "(grpc\|web)"\)' … \| sort -u` | **2** distinct directory walks (`internal/grpc`, `internal/web`), one `os.ReadDir` behind them |
| Task 3: "`rg -n 'strings.Contains\|strings.HasPrefix\|strings.HasSuffix' … returns no match` on the comparison path; the SUMMARY notes any match found elsewhere" | Not defective — its escape clause is exercised. | as written | `strings.Contains` appears at **:402 only**, inside `TestCharacterReturningRPCCensusKeyIsAnExactString`, in the scan that DERIVES the method-name collapses in order to prove the forbidden key would be wrong. `strings.LastIndex` / `strings.SplitN` appear in the same test for name-splitting. The comparison path uses map lookup on the exact fully-qualified string and nothing else. |

Every other criterion passed as written: no erosion vocabulary, no loop over the expected entries, the one-member receiver literals, `SceneAccessServer` only inside explaining comments, the disjointness assertion, both write RPCs present in the guest-gate and ownership literals, the two public RPC names only in the public literal and the header, `bodyReferencesSelector` declared exactly once across `test/meta/`, and no trailing or block comment in either `internal/grpc` file carrying a counted token (`rg -n '\S\s+//.*(s\.resolveAndGate\(|…)|/\*'` → no match, so the three counts are honest rather than accidentally green).

## Deviations from Plan

### Scope / method decisions

**1. Task 1's RED was captured by contract inversion, not by test-before-implementation**

The plan directs the helpers and their tests into `meta_helpers_test.go` (the only Task 1 entry in `files_modified`), so the two land in one file and an ordering-based compile RED would have been a bare `undefined:` message. Each contract was inverted in place instead, which proves the assertion is load-bearing — a strictly stronger property than symbol absence. All three captures are recorded above.

**2. The web half's universe is scoped by the facade-client selector, not by the proxy-name prefix**

The plan says a proxy is a member when it "references the session-token header identifier **and** references its paired facade method name as a selector", over a universe of exported `*Handler` methods. Taken literally that universe is every web proxy in the product — scenes, auth, content — because `*Handler` is shared and **every** proxy reads `headerInjectSessionToken`, including the two public character ones. The derived set would be dozens against a four-member literal: permanently RED.

The universe is therefore scoped to methods whose body references `<recv>.characterAccess`, and the two public-audience proxies are excluded through the same checked-in public literal the facade half already audits by derivation. Both directions survive: a new owner-audience proxy is EXTRA, a proxy that drops either conjunct is MISSING, and a new proxy whose facade half gets parked in the public literal fails the facade-side derived-public comparison.

**3. `QueryCharacter` was added to the name-reachable enumeration** — recorded above under "found a real inventory gap".

**4. §9's undelivered rows are deliberately not in the expected set**

§9.2 and §9.3 name `AdminListCharacters`, `AdminSearchCharacters`, `AdminGetCharacter`, `AdminUpdateCharacter`, `AdminRetireCharacter`, `AdminUnretireCharacter`, `RetireCharacter`, `UnretireCharacter` and `CreateCharacter`'s reshape. None is declared in the tree, so none is a descriptor. Including them would put nine permanent members in the missing group, and the only cheap repair for a permanently-red census is to delete rows — the erosion §3.1 rule 3 forbids. D-72 already fixes the correct behavior: each arrives with its own inventory row in the change that declares it. The file header says so.

**5. No requirement newly claimed**

The plan declares `requirements: [IDENT-02, PROFILE-03]`. Both are already **Complete** on both surfaces — IDENT-02 from 04-06, PROFILE-03 from 04-04 — with matching traceability rows. This plan *binds the criteria that prove them*; it does not deliver either for the first time. `requirements mark-complete` was not run and `REQUIREMENTS.md` is unchanged, following 04-07's precedent. PROFILE-04's pre-existing checkbox/row split was left alone as instructed.

**6. No second gate for criterion 5**

The narrow-interface compile fence (`characterAccessWorldReader` / `characterAccessWorldMutator` / `characterAccessProfileVisibility`) is the decided mechanism, and this phase explicitly rejected a `gorules` rule or meta-test alongside it. Nothing here touches it. `internal/grpc/characteraccess_service.go:35-36` states the same thing at the site.

---

**Total deviations:** 0 auto-fixed + 6 recorded decisions. No production code was changed by this plan; every edit to `internal/grpc` and `api/proto` was a reverted RED probe.

## Verification

| Gate | Result |
| --- | --- |
| `task test -- -run 'RoutingCensus' ./test/meta/` | green, **8 tests** |
| `task test -- -run 'CharacterReturningRPCCensus' ./test/meta/` | green, **4 tests** |
| `task test -- ./test/meta/` | green, **186 tests** (174 before this plan) |
| `task test` (whole repo) | **exit 0** |
| `task lint` | **exit 0** |
| `task build` | **exit 0** |
| `task proto` idempotency | `git status --short -- api/ pkg/proto/` **empty** after the probe was reverted and regenerated |
| `task test:int` (whole repo) | **exit 0**, 12020 tests, 7 skipped, **zero failures** |

**The known #4955 rate-limiter flake did not fire, and nothing had to be excused.** One intermediate `task test:int` invocation returned go-task's collapsed 201 while a sibling run of the same task was still holding testcontainer resources in the same shell session; the clean serialized re-run completed in 161s with exit 0 and no `=== Failed` block at all. The verdict is taken from the exit code of the isolated run, not from a grep of its output.

## Known Stubs

None. Both censuses are wired, both are demonstrated non-vacuous by their own REDs, no test is skipped, and every `<verify>` was run.

Two absences are deliberate and stated at their sites:

- **The web half carries no audience partition.** `internal/web`'s `*Handler` is shared by every domain in the product, so "every exported method on the accepted receiver" is not the character surface there. The facade is the enforcement point — an ungated proxy still lands on a gated facade RPC — so the partition is asserted exactly where bypassing it would grant access.
- **The name-reachable class is self-certifying.** §3.2 specifies the union that makes it so and states the cost; the "no more" half does not hold over that class, and §3.2's standing review obligation is what covers it. The file header repeats this rather than leaving it to be discovered under a red test.

## Issues Encountered

- **`require` short-circuits, which hides the very asymmetry a two-part assertion exists to demonstrate.** RED (c)'s first run showed only the integrity failure; proving the set comparison stayed green needed a temporary `require` → `assert` softening. Worth remembering when a test's claim is *"this assertion moves and that one does not"*.
- **A `task test:int` run launched while another was still finishing returns go-task's collapsed 201 with no failing test in its output.** Read the exit code of an isolated run; the log tail from a contended one says nothing.
- **The formatter strips unused imports on write**, so adding an import ahead of the code that uses it silently reverts. Write the consuming code first.

## Threat Flags

None. Every register entry in this plan's threat model is closed:

| Threat | Disposition | Where |
| --- | --- | --- |
| T-04-08 a future owner RPC bypassing the guest gate | mitigate | set equality over three named expected sets covering both halves of every proxy pair; REDs (a), (b), (c), (d), (e) |
| T-04-32 ownership indirection hiding an ungated mutation | mitigate | one checked-in wrapper name and no other, its own body asserted to reach `s.ownedCharacter` **before** the comparison — RED (c), with the set comparison shown to stay green |
| T-04-26 a future character-returning RPC with no audience verdict | mitigate | descriptor census keyed on the exact fully-qualified name, both directions, 39 members; RED captured in each direction |
| T-04-27 a vacuous census | mitigate | eight anti-vacuity guards across the two files, each before its comparison; no construct that removes a member from scrutiny; no loop-over-expected |
| T-04-34 a new facade RPC referencing NEITHER gate name | mitigate | the audience partition, plus the derived-public counterpart that closes the one-line bypass — REDs (d) and (e), with (a)–(c) shown green throughout |
| T-04-36 a handler shape outside the unary filter | mitigate | fail-closed classification (unclassifiable fails by name), fixtures for the streaming and setter shapes, value and unnamed receivers kept in the universe, embedded-field literal + `playerGate` every-method-unexported |
| T-04-35 census scope silently widened to the scene facade | mitigate | one-member receiver literals with the count consequence stated; `TestCharacterAccessRoutingCensusIsCharacterScoped`; `SceneAccessServer` appears only inside explaining comments |
| T-04-25 an incomplete RPC removal surviving at the descriptor level | mitigate | `TestCharacterReturningRPCCensusRemovedWebDirectoryRPCIsGone` asserts absence from the registered method set, not from source |
| T-04-SC package installs | accept | none performed; no dependency added |

## Notes for the Phase Gate

- **`/holomush-dev:review-abac` MUST still run before push.** This plan adds no ABAC surface, but 04-01 and 04-07 each added a seed policy and neither executor ran the reviewer. It remains a phase-gate obligation.
- **A phase that adds a character-returning RPC now has two censuses to satisfy.** The descriptor one wants an inventory row in `characterReadSurfaceInventory`; the routing one wants an audience classification — the shared gate plus an owner-audience entry, or a demonstrable public read pipeline plus a public-audience entry. Neither is satisfiable by adding a name alone.
- **Phase 6's admin surface will make the descriptor census RED on the commit that declares the RPCs and green again on the commit that adds the rows.** `AdminCharacter` also needs adding to `characterShapedMessages`. That is D-72 working as designed; it is not a census defect.
- **A first server-streaming RPC on `CharacterAccessService` will fail the classifier by name.** That is deliberate — it must arrive with a reviewable edit teaching the classifier the shape, not slide past a filter written for unary handlers.
- **01-SPEC §2.6's substring example is wrong and the file header records the correction.** If §2.6 is ever revised, the true statement is that method names collapse in 17 places while fully-qualified names do not.
- **`NewCharacterAccessServer` still has nine call sites.** Nothing here changed that; enumerate with `codegraph_callers` rather than a plan's count.

## User Setup Required

None — no external service configuration required.

## Self-Check: PASSED

- `test/meta/characteraccess_routing_census_test.go` — FOUND
- `test/meta/character_rpc_census_test.go` — FOUND
- `test/meta/meta_helpers_test.go` — FOUND
- commit `607fc6b8e` — FOUND in `git log --oneline --all`
- commit `ce0089a6f` — FOUND
- commit `e25a4b828` — FOUND
- `task test`, `task test:int`, `task lint`, `task build` all exit 0 on the committed tree
