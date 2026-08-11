---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 07
subsystem: internal/grpc
tags: [grpc, abac, protobuf, directory, privacy, ginkgo, tdd, rpc-removal]
status: complete

requires:
  - phase: 01-portal-spec
    provides: "§2.4 (the directory split and the public/admin surfaces), §2.5 (no compatibility shim), §3.3 (the inventory row assigning the deletion to Phase 4), §8.7 (the membership rule), §8.10 (an outage is not a policy answer), §9.2 (the tier floor ON the directory resource)"
  - phase: 02-abac-schema-vocabulary
    provides: "internal/access/profilevis (Reachable, PolicyEvaluator, the three-outcome evaluate), the seed:viewer-* twinning pattern, seed:profile-reachable"
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: "04-01's CharacterAccessServer, its narrow dependency interfaces, the D-83 viewer-identity seam and projectPublic; 04-04's public projection family and the Web* proxy shape; 04-06's mutate seam and the six-argument constructor"
provides:
  - "CharacterAccessServer.ListCharacterDirectory — the public directory, gated once then filtered per character"
  - "CharacterAccessServer.evaluateGate — the facade's three-outcome ABAC collapse, including the infra-failure-DENY-with-nil-error branch"
  - "characterAccessPolicyEvaluator — the one-method raw-ABAC seam (Evaluate); satisfied by *policy.Engine and by the harness engine, no new production type"
  - "characterAccessDirectoryReader — the one-method enumeration seam (ListAll); satisfied by bootstrapsetup.CharRepoAdapter"
  - "seed:viewer-directory-list-characters — §9.2's viewer-tier floor on character_directory"
  - "projectPublicSummary — the sole constructor of PublicCharacterSummary"
  - "Proto: PublicCharacterSummary, ListCharacterDirectory{Request,Response}, the RPC, and the WebListCharacterDirectory pair"
  - "Handler.WebListCharacterDirectory and the grpcclient/GRPCClient leg"
  - "internal/grpc/characteraccess_directory_test.go — 13 unit specs; test/integration/access/character_directory_test.go — 8 Ginkgo specs"
removes:
  - "Proto RPC WebService.WebListAllCharacters and its request/response messages"
  - "Go method Handler.WebListAllCharacters and its tests"
  - "CoreClient.ListAllCharacters from internal/web's narrow consumer interface"
  - "The generated TypeScript client method and directoryClient's characterId argument"
affects: [04-08, 05-character-identity-ui]

actuals:
  tokens: 203000
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Two separable authorization layers on one RPC: a bulk gate on a singleton resource, then a per-row membership rule — each provably deniable while the other permits"
    - "A call-count assertion as the ONLY way to prove a gate precedes enumeration, because a denial and a post-hoc filter produce identical bodies"
    - "A shipped test engine (policytest.NewInfraFailureEngine) used DIRECTLY as a narrow seam's double, because its method signature already is the seam"
    - "Fail-on-call doubles for the two new seams passed by every non-directory spec, so 'only the directory enumerates' is enforced continuously"
    - "An RPC retirement whose totality is asserted over live source with generated output excluded, and the historical mention kept as a doc comment"

key-files:
  created:
    - internal/grpc/characteraccess_directory.go
    - internal/grpc/characteraccess_directory_test.go
    - test/integration/access/character_directory_test.go
  modified:
    - api/proto/holomush/characteraccess/v1/characteraccess.proto
    - api/proto/holomush/web/v1/web.proto
    - internal/access/policy/seed.go
    - internal/access/policy/seed_test.go
    - internal/grpc/characteraccess_service.go
    - internal/grpc/characteraccess_projection.go
    - internal/grpc/characteraccess_owner_test.go
    - internal/grpc/characteraccess_profile_test.go
    - internal/grpc/characteraccess_viewer_test.go
    - internal/grpc/characteraccess_write_test.go
    - internal/web/auth_handlers.go
    - internal/web/auth_handlers_test.go
    - internal/web/character_handlers.go
    - internal/web/handler.go
    - internal/web/handler_test.go
    - internal/grpcclient/client.go
    - cmd/holomush/deps.go
    - cmd/holomush/deps_test.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/harness.go
    - test/integration/access/character_profile_read_test.go
    - test/integration/access/character_write_test.go
    - web/src/lib/scenes/directoryClient.ts
    - web/src/lib/components/scenes/CharacterMultiSelect.svelte
    - web/src/lib/components/scenes/CharacterMultiSelect.svelte.test.ts
    - web/src/lib/components/scenes/SceneContextRail.svelte
    - pkg/proto/holomush/{characteraccess,web}/v1/**
    - web/src/lib/connect/holomush/{characteraccess,web}/v1/**
    - site/src/content/docs/reference/grpc-api.md

key-decisions:
  - "A directory-gate denial is codes.PermissionDenied, NOT the §8.7 not-found-equivalent. §8.7's indistinguishability rule is about whether a PARTICULAR CHARACTER exists; this denial is about a server-wide singleton resource whose existence is a product fact. It discloses no character and no count."
  - "The gate and the membership rule are proven separable in BOTH directions, at both tiers: a closed gate with reachability permitting everything (unit + integration D7), and a raised reachability floor with the gate open (integration D5/D6)."
  - "The task order executed was 1 → 3 → 2, not 1 → 2 → 3, so no commit in the sequence leaves the tree unbuildable."
  - "internal/web's CoreClient loses ListAllCharacters because no caller survives there; grpcclient.Client.ListAllCharacters, cmd/holomush's GRPCClient declaration and CoreService.ListAllCharacters are all untouched."
  - "CharacterMultiSelect loses its characterId PROP, not just the argument: the listing is viewer-scoped, so an acting-alt input the facade never reads would be a lie in the component's own signature."
  - "Two of this plan's acceptance greps were DEFECTIVE and were corrected rather than satisfied by reshaping the artifact — see 'Acceptance-criteria corrections'."

patterns-established:
  - "Sort the response by id rather than trusting the repository's documented order: the contract promises name-ascending, but two characters may share a name and tie-breaking is unspecified, so only the unique key gives a total order two identical calls agree on"
  - "A membership-absence assertion compares WHOLE responses (proto.Equal) between a withheld corpus and a corpus without the row, so a tombstone, a null entry or a subtractable count all fail it"

requirements-completed: []

coverage:
  - id: D1
    description: "The §9.2 gate is ONE ABAC decision on character_directory:all, made before any enumeration; a denied viewer learns nothing, not even the corpus size"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_directory_test.go#TestListCharacterDirectoryDeniesABelowFloorViewerBeforeEnumerating (gate.calls == 1, reader.listCalls == 0)"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_directory_test.go#D7 — seed:viewer-directory-list-characters removed from a control corpus, PermissionDenied against the REAL engine"
        status: pass
    human_judgment: false
  - id: D2
    description: "The gate is independent of reachability: it denies while profilevis.Reachable would have permitted every character, so a game can move the two floors separately"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "#TestListCharacterDirectoryGateIsIndependentOfReachability (reach.calls == 0 under a permitting reachability double)"
        status: pass
      - kind: integration
        ref: "character_directory_test.go#D7 paired with #D8 — only the directory floor differs between the two corpora"
        status: pass
    human_judgment: false
  - id: D3
    description: "A gate decision that could not be EVALUATED is neither a permit nor a deny — both the non-nil-error shape and the DENY-with-infra:-id-and-nil-error shape return Internal and leave ListAll uninvoked"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "#TestListCharacterDirectoryReportsAGateEvaluationFailureAsInternal, #TestListCharacterDirectoryTreatsAnInfraFailureDenyAsAnEvaluationFailure (the latter driven by the shipped policytest.NewInfraFailureEngine)"
        status: pass
    human_judgment: false
  - id: D4
    description: "The §8.7 membership rule: an unreachable character is absent and its absence is indistinguishable from it not existing, with a paired positive control at a clearing rung"
    requirement: "PROFILE-04"
    verification:
      - kind: unit
        ref: "#TestListCharacterDirectoryOmitsAnUnreachableCharacterIndistinguishably (whole-response proto.Equal), #TestListCharacterDirectoryIncludesTheSameCharacterAtAClearingRung"
        status: pass
      - kind: integration
        ref: "character_directory_test.go#D5 (reachability floor raised to guest+player; anonymous listing empty and proto-equal to the no-rows response) and #D6 (the guest rung lists both)"
        status: pass
    human_judgment: false
  - id: D5
    description: "The directory serves identity only: PublicCharacterSummary has two fields, so the four presence-telemetry values the retired roster carried have nowhere to land"
    requirement: "PROFILE-03"
    verification:
      - kind: other
        ref: "rg -n 'has_active_session|session_status|last_location|last_played_at' api/proto/holomush/characteraccess/v1/characteraccess.proto == no match; PublicCharacterSummary declares exactly id and name"
        status: pass
      - kind: integration
        ref: "character_directory_test.go#D2 — the marshaled bytes contain the NAME (control) and contain neither the description nor the location label"
        status: pass
      - kind: unit
        ref: "failOnCallDirectoryWorldReader fails on BOTH world reads, so every directory spec proves the path enumerates no property row and reads no description"
        status: pass
    human_judgment: false
  - id: D6
    description: "A reachability-evaluation failure aborts the whole call rather than silently shortening the list (§8.10)"
    requirement: "PROFILE-04"
    verification:
      - kind: unit
        ref: "#TestListCharacterDirectoryReportsAReachabilityFailureAsInternal (asserts the response is nil, not partially populated)"
        status: pass
    human_judgment: false
  - id: D7
    description: "The listing is deterministically ordered and an empty directory is a success"
    requirement: "PROFILE-03"
    verification:
      - kind: unit
        ref: "#TestListCharacterDirectoryIsDeterministicAcrossIdenticalCalls (fixture ordered id-DESCENDING so a pass-through would differ; byte equality across two calls), #TestListCharacterDirectoryReturnsAnEmptyListForAnEmptyDirectory"
        status: pass
      - kind: integration
        ref: "character_directory_test.go#D3, #D4"
        status: pass
    human_judgment: false
  - id: D8
    description: "WebListAllCharacters no longer exists as a proto RPC, a Go handler, a generated TypeScript client method or a thin forwarder; CoreService.ListAllCharacters is untouched"
    verification:
      - kind: other
        ref: "rg over api/ internal/ cmd/ pkg/ web/src excluding generated output, comment-filtered == no match; grpc-api.md == no match; rpc ListAllCharacters still in core.proto; git diff --stat over core.proto empty; task build + task lint green"
        status: pass
    human_judgment: false
  - id: D9
    description: "The facade reaches the enumeration and the raw ABAC decision through two ONE-METHOD seams satisfied at HEAD, adding no production type and no third construction site; the D-79 property-repository compile fence is intact"
    verification:
      - kind: other
        ref: "characterAccessDirectoryReader declares only ListAll; characterAccessPolicyEvaluator declares only Evaluate; task build green proves *policy.Engine, the harness types.AccessPolicyEngine and bootstrapsetup.CharRepoAdapter all satisfy them unchanged; rg 'ListByParent' internal/grpc (code lines) == no match"
        status: pass
    human_judgment: false

duration: 39min
completed: 2026-08-11
---

# Phase 04 Plan 07: The public character directory Summary

**The web directory is now two separable decisions — one ABAC gate on `character_directory:all` that runs before a single row is read, then a per-character reachability filter — and it publishes id and name only, because the message it returns has no other field; `WebListAllCharacters` is gone from the proto, the Go tree, the tests and the browser, with nothing forwarding in its place.**

## Performance

- **Duration:** 39 min
- **Started:** 2026-08-11T17:06:14Z
- **Completed:** 2026-08-11T17:46:06Z
- **Tasks:** 3 completed
- **Files:** 38 (3 created, 26 modified by hand, 9 regenerated)

## Task Commits

1. **Task 1 — declare the directory surface, remove the RPC it replaces** — `b4690ce6e`
2. **Task 3 — retire every consumer of the removed RPC** — `cce2b1fdf`
3. **Task 2 — the seed, the two seams, the handler, 21 specs** — `84ac21429`

**The order is 1 → 3 → 2, deliberately.** The plan says in as many words that "the build will be red until Task 2 lands the handler and Task 3 updates the consumers", which would have put a broken tree in the middle of the sequence and made `git bisect` useless across it. Task 3 does not depend on Task 2: the `WebListCharacterDirectory` proxy forwards to a facade method that answers `Unimplemented` through the embedded `UnimplementedCharacterAccessServiceServer` until the handler lands — exactly the arrangement 04-04 shipped for its four proxies. So running 3 before 2 leaves every commit buildable, at no cost to what any task contains.

## RED demonstrated (TDD)

The whole 13-spec unit file was written and run **before any production change**, against the real constructor:

```text
=== FAIL: internal/grpc  (0.00s)
FAIL	github.com/holomush/holomush/internal/grpc [build failed]

=== Errors
internal/grpc/characteraccess_directory_test.go:136:13: undefined: characterAccessPolicyEvaluator

internal/grpc/characteraccess_directory_test.go:189:4: too many arguments in call to NewCharacterAccessServer

	have (*failOnCallDirectoryWorldReader, *recordingWorldMutator, characterAccessProfileVisibility,
	      unknown type, *recordingDirectoryReader, auth.PlayerSessionRepository,
	      auth.PlayerRepository, *mocks.MockCharacterRepository)
	want (characterAccessWorldReader, characterAccessWorldMutator, characterAccessProfileVisibility,
	      auth.PlayerSessionRepository, auth.PlayerRepository, auth.CharacterRepository)

DONE 0 tests, 1 failure, 2 errors in 0.000s
```

**The Ginkgo half was proven non-vacuous by an accidental failure rather than a probe.** The first integration run failed on `D3` with `Gomega's BeNumerically matcher requires a number` and reported `Ran 99 of 100 Specs — 98 Passed | 1 Failed | 1 Skipped`. The suite carried 92 specs at the end of 04-06 (90 passed + 1 failed + 1 skipped in that plan's own probe run), so 100 − 92 = **exactly the 8 specs this file adds**, and 99 of them ran. No deliberate probe was needed.

## Accomplishments

### The §9.2 gate is a real decision, and it is provably not the reachability filter

Two layers, evaluated in order, neither derived from the other:

| Layer | Mechanism | Resource | What a denial means |
| --- | --- | --- | --- |
| §9.2 **gate** | `evaluateGate` → `characterAccessPolicyEvaluator.Evaluate` | `character_directory:all` (singleton) | the directory is closed to this viewer; nothing is read |
| §8.7 **membership** | `profilevis.Reachable`, per row | `profile:<character-id>` | this character is not listed, indistinguishably from not existing |

**The separability is asserted in both directions, at both tiers**, because that is the property the withdrawn design (substituting per-character reachability for the gate) could not express at all:

- gate closed / reachability wide open → `PermissionDenied`, and the reachability double records **zero** calls (unit) — and against the **real engine** with only `seed:viewer-directory-list-characters` removed from the corpus (integration D7, paired with D8 on the full corpus);
- gate open / reachability floor raised to guest+player → the anonymous listing is empty and `proto.Equal` to the response for a corpus with no characters at all, while the guest rung on the identical corpus lists both (integration D5/D6).

**The `ListAll` call count is the whole proof that the gate precedes enumeration.** A denial that still enumerated and then filtered everything out returns a byte-identical body; no assertion about the response can tell those apart. Every gate spec therefore asserts `reader.listCalls == 0`.

### The infra-failure branch, which is the one a hand-written gate gets wrong

`evaluateGate` collapses the engine's answer into three outcomes, modelled line for line on `profilevis.evaluate`. The subtle one is a **DENY decision carrying an `infra:` policy id and a NIL error** — the shape the engine's degraded-mode and session-resolution paths return. Collapsing it into an ordinary denial tells the caller "you may not list the directory" when in fact **nothing was evaluated**, which is §8.10's forbidden masking.

The double is the **shipped** `policytest.NewInfraFailureEngine` used *directly* as the seam, not a fixture invented here — its `Evaluate` already has exactly the seam's signature and returns exactly that decision. (It also declares `CanPerformAction`, which the seam ignores.)

The duplication of `profilevis.evaluate` is deliberate and bounded, and the file says why: that helper is unexported and its package is scoped by its own doc to profile *visibility*; exporting it, or hanging a directory-gate method off `profilevis.Evaluator`, would make that package the home of §9.x decisions it has nothing to do with.

### Two one-method seams, zero new production types

| Seam | Method | Production satisfier | Test satisfier |
| --- | --- | --- | --- |
| `characterAccessPolicyEvaluator` | `Evaluate` | `*policy.Engine` — the value already in scope as `policyEngine`, the same one the `profilevis.Evaluator` two lines away is built over | the harness's `accessEngine` (`types.AccessPolicyEngine`) |
| `characterAccessDirectoryReader` | `ListAll` | `bootstrapsetup.CharRepoAdapter` — the same `authCharRepo` already passed for the owner audience | the harness's `charRepo` |

`task build` green is the proof neither needed new code. **The D-79 compile fence is untouched**: the fence is the *absence* of `PropertyReader.ListByParent` / `PropertyRepository.ListByParent` from every interface the facade holds, so a third and a fourth interface naming one method each cannot weaken it.

### The message shape is the protection

`PublicCharacterSummary` has **two fields**. The retired roster shape carried four presence-telemetry values — an active-session flag, a session status string, a last-location label and a last-played timestamp — and none of them has anywhere to land here. That is a descriptor property, not a handler discipline: no future edit to the handler can leak them.

Reinforced two more ways: `failOnCallDirectoryWorldReader` fails the test if the directory path reaches *either* world read, so "no property row, no description" is enforced by every one of the 13 unit specs; and integration D2 asserts over the marshaled bytes that the name is present (the control) while the description text and the location label are not.

### The retirement is total

| Surface | State |
| --- | --- |
| `api/proto/.../web.proto` | RPC + both messages deleted; the now-unused `holomush/core/v1/core.proto` import removed with them |
| `internal/web/auth_handlers.go` | handler deleted, no forwarder, no alias, no deprecation stub |
| `internal/web/handler.go` | `CoreClient.ListAllCharacters` removed — no caller survives in that package |
| `internal/web/{auth_handlers,handler}_test.go` | test and the three mock fields deleted |
| `web/src/lib/scenes/directoryClient.ts` | calls `webListCharacterDirectory({})`; the `characterId` parameter is gone |
| `CharacterMultiSelect.svelte` (+ test) and `SceneContextRail.svelte` | the `characterId` **prop** removed, not merely unused |
| `site/.../grpc-api.md` | regenerated; no longer advertises the RPC |
| `CoreService.ListAllCharacters`, `grpcclient.Client.ListAllCharacters`, `cmd/holomush`'s `GRPCClient` declaration | **untouched** — `git diff --stat` over `core.proto` is empty |

The component lost the whole prop rather than just the argument: the listing is viewer-scoped, so an acting-alt input the facade never reads would be a lie in the component's own signature. Its test now asserts `toHaveBeenCalledWith()` — the empty argument list is what pins that the scope comes from the session cookie and not from a client-supplied id.

### The seed is a twin, not a widening

`seed:viewer-directory-list-characters` is the `viewer`-flavored twin of the shipped `principal is character`-scoped `seed:directory-list-characters` (D-76's pattern, D-01's precedent), at `SeedVersion: 1` — per-policy, and a brand-new policy has no prior row to upgrade from.

The plan's premise checked out at HEAD and the doc comments say so accurately: `character_directory`, `access.CharacterDirectoryResource()` and `seed:directory-list-characters` **all exist**; what v0.13 lacked was a viewer-tier floor on that resource. No production comment claims otherwise.

Both seed meta-tests the plan flagged were satisfied **by the policy's name and scope**, with no edit to `seed_profile_visibility_test.go`: the new name does not carry the `seed:viewer-property-` prefix `viewerTwins` scopes on (that set still holds exactly its five HEAD entries), and `character_directory` is a different resource *type* from `character`.

## Acceptance-criteria corrections

Two of this plan's own greps were defective as written — the same false-red class this phase has now hit ten times. Both were **corrected rather than satisfied by reshaping the artifact**.

| Criterion as written | Why it is defective | Corrected form | Result |
| --- | --- | --- | --- |
| Task 2: "`rg -n 'CharacterDirectoryResource' internal/grpc/characteraccess_directory.go` returns exactly one match" | Returns **3**. Two are doc-comment lines that name the constructor in order to *explain* the gate — the same rationale-comment vector the plan itself anticipated for `Reachable` (and explicitly downgraded to a read-check for), simply not applied to this neighbouring bullet. | `rg -n 'CharacterDirectoryResource' internal/grpc/characteraccess_directory.go \| rg -v ':\s*//'` | **1**, at line 102, and the `s.directory.ListAll(` call is at line 110 — the gate precedes enumeration |
| Task 3: "`rg -n 'WebListAllCharacters\|webListAllCharacters' api/ internal/ cmd/ pkg/ web/src/ --glob …` returns no match" | Returns **1**: `internal/web/character_handlers.go:199`, a doc comment on the replacement stating what it replaces and why no forwarder was left. The `<action>`'s intent is that no live **consumer** survives; a sentence recording the retirement is the opposite of a consumer. | the same command piped through `rg -v ':\s*(//\|\*)'` | **no CODE line matches** |

The criterion the plan already marked "diagnostic, not a gate" (`rg -n 'ListByParent' internal/grpc/`) also has comment-only hits and no code-line hits; it was read as intended.

Every other criterion passed as written: `PublicCharacterSummary{` constructed only inside `projectPublicSummary` (1), `IsInfraFailure()` exactly 1, `seed:viewer-directory-list-characters` exactly 1 in `seed.go`, the `viewerTwins` five-entry count still 5, both new interfaces declaring exactly one method, no presence-telemetry token in the characteraccess proto, `rpc ListAllCharacters` still in `core.proto`, and an empty `git status --porcelain` over the three generated trees after a fresh `task proto && task web:generate && task docs:proto`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Removing `WebListAllCharactersResponse` orphaned `web.proto`'s `core.proto` import**

- **Found during:** Task 1
- **Issue:** That response message held the only `holomush.core.v1.` reference in the whole file. `buf lint` failed with `Import "holomush/core/v1/core.proto" is unused.`
- **Fix:** Removed the import in the same change. `core.proto` itself is untouched.
- **Commit:** `b4690ce6e`

**2. [Rule 3 - Blocking] `NewCharacterAccessServer` now has NINE call sites, not the two the plan names**

- **Issue:** The plan says "the same two call sites this task already edits … No third call site exists; `rg -o 'NewCharacterAccessServer\(' cmd/ internal/ \| wc -l` is the check." That command returns 8 in `cmd/`+`internal/` alone (two of which are the declaration and its doc line), and the real construction sites are nine: `cmd/holomush/sub_grpc.go`, `internal/testsupport/integrationtest/harness.go`, `internal/grpc/characteraccess_{profile×2,write,owner,viewer}_test.go`, and `test/integration/access/character_{profile_read,write}_test.go`. This is the third consecutive plan in this phase to undercount them (04-05 deviation 1, 04-06 deviation 1).
- **Fix:** All nine updated. The two production-shaped sites pass the engine already in scope and the character-repository adapter already built there. **The five `internal/grpc` unit sites pass `&failOnCallDirectoryGate{}` and `&failOnCallDirectoryReader{}`** — doubles that fail the test on call, so "only the directory listing makes a raw ABAC decision or enumerates every character" is now enforced continuously by every profile, owner, viewer and write spec rather than by one test. The two integration sites pass the production values with a comment saying no spec there reaches them yet.
- **Commit:** `84ac21429`

**3. [Rule 3 - Blocking] The `CharacterAccessClient` widening ripples to `grpcclient` and `cmd/holomush`**

- **Found during:** Task 3
- **Issue:** The plan's file list for Task 3 names only the `internal/web` files and the TypeScript. Adding `ListCharacterDirectory` to `CharacterAccessClient` leaves `*grpcclient.Client` no longer satisfying it and `mockGRPCClient` incomplete. Identical to 04-04's deviation 1 and 04-01's deviation 2; the interface is simply wider now.
- **Fix:** The forwarding method on `grpcclient.Client` in its `RPC_FAILED` sibling shape, the signature on `cmd/holomush.GRPCClient`, and the nil stub on `mockGRPCClient`.
- **Commit:** `cce2b1fdf`

**4. [Rule 3 - Blocking] The seed inventory ratchet in `seed_test.go`**

- **Issue:** `TestSeedPoliciesCount`, `TestSeedPoliciesEffectDistribution` and `TestSeedPoliciesExpectedNames` are strict-equality inventories that go red on any seed addition (63 → 64, 53 → 54 permits, and the per-name list).
- **Fix:** All three updated with the provenance sentence the file's existing entries carry. **This is `seed_test.go`, a deliberately different file from `seed_profile_visibility_test.go`** — which the plan forbids editing and which was **not** touched (`git diff --stat` over it is empty across all three commits).
- **Commit:** `84ac21429`

### Scope / design decisions

**5. Task order 1 → 3 → 2** — see "Task Commits" above. No commit in the sequence leaves the tree unbuildable, which the plan's stated order would not have achieved.

**6. A gate denial is `PermissionDenied`, not the §8.7 not-found-equivalent**

The plan says "a deny ends the call" without naming a code. §8.7's indistinguishability rule exists so a viewer cannot learn **which character ids exist**; a denial on the server-wide singleton `character_directory:all` names no character, carries no count, and is about a resource whose existence is a product fact rather than a secret. The alternative — returning an empty list — would be strictly worse: it is a lie that renders identically to a genuinely empty game. The message (`the character directory is not available`) names no policy, no subsystem and no reason beyond the one that applies. **In the shipped corpus this branch is unreachable**, because the new seed clears all three rungs; it is the fail-closed floor under a game that raises that floor, and the source says so at the site.

**7. `CharacterMultiSelect` lost the prop, not just the argument** — recorded under "The retirement is total". Following the change through to `SceneContextRail` and the component test is what the plan's "follow the change through to every caller of this module" asks for.

**8. No `internal/web` test for the new proxy**

`internal/web` has **no** test double for `CharacterAccessClient` at all (`rg -n 'CharacterAccessClient|characterAccess' internal/web/*_test.go` returns nothing), so the five character proxies 04-01 and 04-04 shipped are likewise untested there. `WebListCharacterDirectory` matches that shape; building the first double for this facade is a gap that belongs to whichever plan decides to close it for all six, not a silent omission by this one. The behavior the proxy forwards is covered at the facade tier by 21 specs.

**9. No requirement newly claimed**

The plan declares `requirements: [PROFILE-03, PROFILE-04]`. **PROFILE-03 is already Complete on both surfaces** (04-04 claimed it) and **PROFILE-04's checkbox/row split is pre-existing** (checkbox `[x]` from 04-01, traceability row `Pending`) and was left alone exactly as instructed. This plan *consumes* both — reachability as the membership rule, per-field visibility not at all — rather than delivering either for the first time, so `requirements mark-complete` was not run and `REQUIREMENTS.md` is unchanged by this plan.

---

**Total deviations:** 4 auto-fixed (all Rule 3) + 5 recorded decisions. No scope creep; nothing was added beyond what the plan's `<action>` blocks require plus the compile-driven ripples.

## Verification

| Gate | Result |
| --- | --- |
| `task lint:proto` | exit 0 |
| `task test -- -run 'Directory' ./internal/grpc/` | green, **13 tests** |
| `task test -- ./internal/access/policy/...` | green, 1267 tests |
| `task test -- ./internal/grpc/` | green, 659 tests |
| `task test -- ./internal/web/ ./internal/grpcclient/ ./cmd/holomush/` | green, 963 tests |
| `task test` (whole repo) | **exit 0**, 11535 tests, 4 skipped |
| `task test:int -- ./test/integration/access/...` | **exit 0** (99 of 100 specs; 8 new) |
| `task test:int` (whole repo) | **exit 0**, 12005 tests, 7 skipped |
| `task lint` | exit 0 |
| `task build` | exit 0 |
| `cd web && pnpm check` | exit 0 (0 errors; 6 pre-existing warnings, none in touched files) |
| `pnpm vitest run CharacterMultiSelect + SceneContextRail` | green, 37 tests |
| generated-output idempotency | `git status --porcelain pkg/proto web/src/lib/connect site/src/content/docs/reference` **empty** after a fresh `task proto && task web:generate && task docs:proto` |

**The known #4955 rate-limiter flake did not fire.** One `task test:int` invocation was interrupted by a harness timeout at 601s and left a truncated log reporting "2 failures"; the clean re-run completed in 172s with **exit 0 and zero failures**, so nothing had to be excused.

## Known Stubs

None. Every surface this plan declares is wired and exercised; no test is skipped and every `<verify>` was run.

Two absences are deliberate and stated at their sites:

- **No `internal/web` proxy test** — deviation 8 above; the package has no double for this client at all, and the forwarded behavior is covered at the facade tier.
- **No unit-tier spec driving a real below-floor viewer through the seeded corpus** — impossible by construction: `seed:viewer-directory-list-characters` clears all three rungs, and the unit tier's `abactest.NewSeedEngine` compiles the full corpus with no narrowing hook. The denial branch is driven by an ordinary-DENY double at the unit tier and by a **narrowed real corpus** at the integration tier (D7), which is where the shipped-policy claim actually belongs.

## Issues Encountered

- **A defective assertion in my own first integration draft, caught by the run.** `Expect(id).To(BeNumerically("<", otherID))` on ULID *strings* fails — Gomega's matcher requires a number. Replaced with a plain Go comparison inside `BeTrue()`. The failure is what supplied the non-vacuity evidence above, so it is recorded rather than quietly fixed.
- **A positional assertion on freshly minted ULIDs.** The first draft of `TestListCharacterDirectoryIncludesTheSameCharacterAtAClearingRung` asserted `characters[1].id == bram.id`, which is a coin flip once the handler sorts by id. Rewritten as a membership assertion; which of two ULIDs sorts first is not that spec's subject.
- **The session-start `git status` snapshot named a stale HEAD.** `dcc3b10bd` was reported as HEAD at spawn, but plans 04-01…04-06 had landed 30 commits on this branch by the time this plan ran, so any metric computed against that baseline is wrong by an order of magnitude. The `actuals` below are measured against `b4690ce6e~1`, the true parent of this plan's first commit.

## Metrics note (for estimate calibration)

`actuals.tokens: 203000` is `chars/4` over the **full current content of the 29 hand-written files this plan touched** (811,451 chars), which is the method that reproduces 04-04's and 04-06's figures. It is well over the plan's `estimate.tokens: 88000` (confidence: `low`), and the reason is file size rather than task count: this plan edits `internal/access/policy/seed.go`, `internal/testsupport/integrationtest/harness.go` and `cmd/holomush/sub_grpc.go` — three of the largest files in the tree — by a handful of lines each, plus nine more files than the estimate's 20. For reference the **realized diff** is 91,190 chars (≈22,800 tokens) across 1,406 inserted and 101 deleted lines. Neither number is rounded toward the estimate.

## Threat Flags

None. Every surface this plan adds is inside its own threat register, and every register entry is closed:

| Threat | Disposition | Where |
| --- | --- | --- |
| T-04-23 presence telemetry on the directory response | mitigate | `PublicCharacterSummary` has two fields; the exclusion is a descriptor property. Integration D2 asserts it over the bytes |
| T-04-01 directory membership disclosure | mitigate | whole-response `proto.Equal` between a withheld corpus and a corpus without the row, at both tiers |
| T-04-26 bulk enumeration with no controlling decision | mitigate | one ABAC decision on `character_directory:all` before any read, backed by the new seed, made through the narrow seam and **never** approximated by `Reachable`; `listCalls == 0` on every denial path |
| T-04-29 `evaluateGate`'s infra-failure branch | mitigate | DENY + `infra:` id + nil error → Internal, driven by the shipped `policytest.NewInfraFailureEngine` |
| T-04-04 reachability-pass failure | mitigate | aborts with Internal; the response is asserted nil rather than partial |
| T-04-24 nondeterministic enumeration order | mitigate | sorted by id; the fixture is ordered id-descending so a pass-through would differ, and two calls are compared byte for byte |
| T-04-25 a surviving forwarder | mitigate | comment-filtered sweep over live source returns no code line; `grpc-api.md` regenerated |
| T-04-SC package installs | accept | none performed; no dependency added |

## Notes for the Phase Gate

- **`/holomush-dev:review-abac` MUST run before push.** This plan adds an ABAC seed policy (`seed:viewer-directory-list-characters`) and touches `internal/access/policy/`, which is the documented trigger — the same consequence D-76 records for 04-01. It has **not** been run by this executor.
- **04-08's descriptor census will find `WebListAllCharacters` gone** from the proto, the generated descriptors and `grpc-api.md`, and will find six `Web*` character proxies where 04-04 left five.
- **`NewCharacterAccessServer` now takes EIGHT arguments across NINE call sites.** Any later plan touching it should enumerate with `codegraph_callers` rather than trusting a plan's count — three consecutive plans have now undercounted them.
- **A future admin directory (Phase 6) inherits the gate, not the message.** `PublicCharacterSummary` is structurally incapable of carrying presence telemetry, so the admin half needs its own message; reusing this one is the audience-collapse §2.2 forbids.
- **`seed_profile_visibility_test.go` was not modified.** Its two sweeping tests pass on the new policy's name and scope alone.

## User Setup Required

None — no external service configuration required.

## Self-Check: PASSED

- `internal/grpc/characteraccess_directory.go` — FOUND
- `internal/grpc/characteraccess_directory_test.go` — FOUND
- `test/integration/access/character_directory_test.go` — FOUND
- commit `b4690ce6e` — FOUND in `git log --oneline --all`
- commit `cce2b1fdf` — FOUND
- commit `84ac21429` — FOUND
- `task build`, `task lint`, `task lint:proto`, `task test`, `task test:int` all exit 0 on the committed tree
