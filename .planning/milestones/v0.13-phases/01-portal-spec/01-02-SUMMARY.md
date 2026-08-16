---
phase: 01-portal-spec
plan: 02
subsystem: spec
tags: [spec, proto, audience-matrix, census, privacy, invariant-registry]

requires:
  - phase: 01-portal-spec (plan 01-01)
    provides: 01-SPEC.md with the 16-section skeleton, section 13 opened, and proof that a .planning/ origin_spec passes the registry meta-tests
provides:
  - "SPEC section 2 (Audience Matrix and Message Shape) authored to completion — three audiences, three proto messages, three projection functions, the WebListAllCharacters split, the breaking-change posture, and the census as sole gate"
  - "SPEC section 3 (Read-Surface Inventory) authored to completion — 29 character-returning RPCs enumerated from the proto tree, each with exactly one audience verdict; this table IS the Phase-4 census expected set"
  - "A mechanical census predicate: seven named type-reachable character messages plus an explicitly-enumerated name-reachable class"
  - "INV-ACCESS-12 (character read-surface census) declared in section 13 and registered, binding: pending"
affects: [01-03, 01-04, 01-05, 01-06, phase-4-facade, phase-5-profile-ui, phase-6-admin]

actuals:
  tokens: 8636
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Per-audience proto message types as a type-system absence guarantee, replacing per-field optional-scalar discipline"
    - "Census-by-set-equality over a checked-in SPEC table as the binding gate for read-surface completeness"

key-files:
  created:
    - .planning/phases/01-portal-spec/01-02-SUMMARY.md
  modified:
    - .planning/phases/01-portal-spec/01-SPEC.md
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md

key-decisions:
  - "The audience verdict names the audience of the character data carried in the response, not the caller's relationship to the RPC — a player reading their own roster is owner, the same player reading the directory is public"
  - "The census predicate is split into type-reachable and name-reachable members; the name-reachable class is enumerated explicitly because a type-driven predicate cannot see a bare string or rendered bytes"
  - "ParticipantInfo, PublishedSceneEntry.speaker and CharacterSceneInfo are deliberately outside the census predicate — they are name capture, governed by section 5 — with the public export surfaces cross-listed in both tables"
  - "A fourth public export surface (WebListPublishedScenes) was found in the tree beyond the three research named, and inventoried"
  - "INV-ACCESS-12 lands in ACCESS, not PRIVACY, following the 01-01 checkpoint split: the census is an evaluation-coverage guarantee, not a disclosure-shape one"

patterns-established:
  - "Membership-rule preamble before an inventory table, so a downstream planner reads the rules before the rows"
  - "Recording a census predicate's own blind spot (type-reachability) as normative text rather than discovering it at implementation time"

requirements-completed: [PORTAL-01, PORTAL-02]

duration: 35min
completed: 2026-08-01
status: complete
---

# Phase 1 Plan 02: Audience Matrix and Read-Surface Inventory Summary

**Absence stops being a discipline and becomes a type-system property: three audiences, three proto messages, three projection functions, and a 29-row inventory that is literally the expected set the Phase-4 census compares against by set equality.**

## Performance

- **Duration:** ~35 min
- **Tasks:** 2/2
- **Commits:** 2

## Accomplishments

1. **Authored §2 (Audience Matrix and Message Shape)** — fixed exactly three audiences; mandated `PublicCharacter` / `OwnCharacter` / `AdminCharacter` as distinct proto messages (D-01) with the rejected single-message alternative recorded by its actual failure mechanism (a proto3 scalar nobody marked `optional` marshals `""`, which is *present*); mandated `projectPublic` / `projectOwner` / `projectAdmin` as the only construction sites; specified the `WebListAllCharacters` split naming the four telemetry fields the public shape drops (D-03); stated the breaking-change posture (D-02); and fixed the census as the sole mandated gate with its exact comparison semantics (D-04), including a `Notably absent` clause recording the struct-literal lint as considered-and-deferred.
2. **Authored §3 (Read-Surface Inventory)** — enumerated **29 character-returning RPCs** across four services by reading the proto tree, each with exactly one audience verdict, plus three normative membership rules and a mechanically-stated census predicate.
3. **Declared and registered `INV-ACCESS-12`** (character read-surface census), `binding: pending`, no `asserted_by`, and regenerated `docs/architecture/invariants.md` via `cmd/inv-render`.

## What the proto-tree enumeration found that the plan's minimum list did not

The plan's `read_first` named a minimum inventory. Reading the tree produced **more**, and two of the additions are the point of doing the enumeration rather than transcribing the list.

| Finding | Why it matters |
|---|---|
| **A fourth public export surface.** Research named three (`WebExportScene`, `WebGetPublicSceneArchive`, `WebDownloadPublicSceneArchive`). `WebListPublishedScenes` (`api/proto/holomush/web/v1/web.proto:339`) returns `repeated PublicSceneArchive` (`web.proto:1176`) whose `participants_snapshot` (`scene.proto:1053`) carries the same frozen participant names — **in bulk**, in list form. | A census seeded from the three-item list would have been RED-by-omission on the exact surface that publishes the most names per call. |
| **Eight auth/session RPCs carry the leaky roster message.** `AuthenticatePlayer`, `CreatePlayer`, `CreateGuest`, `CheckPlayerSession` and their four web twins each carry `repeated CharacterSummary` — the message holding all four presence-telemetry fields. | `CharacterSummary` is load-bearing on the **auth path**, not just the directory path. Narrowing it is not a directory-local change; Phase 4 must reshape these responses in the same breath. This is recorded in the `AuthenticatePlayer` row's Notes. |
| **`WorldService.GetCharacter` returns `player_id`** (`api/proto/holomush/world/v1/world.proto:81`) to any ABAC-permitted reader. | An OOC player↔character linkage on a `public`-audience surface. Flagged in its row as a Phase-4 decision rather than silently carried into `PublicCharacter`. |

## The census predicate's own blind spot, recorded normatively

The most consequential thing §3 says is what the census **cannot** see.

A predicate that walks generated descriptors looking for character-shaped *message types* finds `CharacterInfo`, `CharacterSummary`, `CharacterDirectoryEntry`, `PresenceEntry`, `WebPresenceEntry` — seven types, all named in §3.2. It finds **none** of:

- `GameEvent.actor`, documented in the tree as *"the DISPLAY NAME of the acting character, extracted from the event payload"* (`web.proto:427-429`) — a bare `string`.
- `SelectCharacterResponse.character_name` and its three siblings — bare `string`s.
- `WebExportSceneResponse.content` / `WebDownloadPublicSceneArchiveResponse.content` — opaque `bytes` containing rendered speaker labels.

Six of the 29 rows are reachable **only** by explicit enumeration. §3.2 therefore splits the predicate into a type-reachable set and a name-reachable set and states that the census MUST seed its expected set from **both**. Left undiscovered until Phase 4, this would have produced a census that passes while missing every unauthenticated export surface — a gate reporting green over exactly the leak it exists to catch.

## Deviations from Plan

### [Authored beyond the literal action text] The scene-metadata family needed an explicit in/out verdict

- **Found during:** Task 2, while making the census predicate mechanical.
- **Issue:** `SceneInfo.participants` carries `ParticipantInfo.character_name` (`scene.proto:330`), and roughly forty scene and sceneaccess RPCs return `SceneInfo`. A predicate reading "response transitively contains a character display name" makes all forty census members; a predicate reading "contains a character-projection message" makes none of them. The plan's action text did not settle which, and either reading silently produces a different expected set — so a Phase-4 implementer would have guessed, and the census would then be RED against §3 on its first run.
- **Fix:** Settled it in §3.2 with a rationale rather than a ruling. `ParticipantInfo`, `PublishedSceneEntry.speaker` and `CharacterSceneInfo` are **name capture**, not character projection: the question they raise is "was this name frozen at emit time and is it therefore unreachable by a later privacy change", which is §5's question, not §3's. The public export surfaces are the exception and are cross-listed in **both** tables — which is exactly what threat `T-01-12` asks for.
- **Files:** `.planning/phases/01-portal-spec/01-SPEC.md`

### [Authored beyond the literal action text] §3.4 states the union rule rather than pre-listing §9's RPCs

- **Found during:** Task 2.
- **Issue:** §3 "IS the expected set", but §9's new v0.13 RPCs are authored by plan 01-04 and are also census members. Pre-listing invented names here would produce two tables that can disagree — the master-vs-sibling drift `.claude/rules/references/design-review-learnings.md` catalogues.
- **Fix:** §3.4 states the composition rule instead: the Phase-4 expected set is the **union** of §3.3 and §9's character-returning rows, minus the rows §2.4 deletes. The obligation on 01-04 is stated (every character-returning RPC in §9 carries an audience verdict); the names are not.

### [Rule 3 — Blocking] `task lint:yaml` failed after the registry edit

- **Found during:** Task 2, before commit. Same failure mode plan 01-01 recorded.
- **Issue:** `yamlfmt` (`max_line_length: 120`) wanted its own wrapping of the new folded `summary:` scalar.
- **Fix:** Ran the sanctioned `task fmt:yaml`; confirmed by `git diff` that it rewrapped **only** the five lines of the new `INV-ACCESS-12` entry and touched no other file. Re-ran `go run ./cmd/inv-render` afterwards so the generated markdown matches the reformatted YAML.
- **Files:** `docs/architecture/invariants.yaml`
- **Commit:** `47b95bccc`

## Verification

| Gate | Result |
|---|---|
| `go run ./cmd/inv-render -check` | exit 0 |
| `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestRegistryBindingChecks\|TestProvenanceGuard\|TestRegistrySchemaParsesOwnershipFields\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | exit 0 — 18 tests |
| `task lint:yaml` | exit 0 (exit code read directly, not grepped from output) |
| `task lint:markdown` | exit 0 — 746 + 84 files. (`.planning` is excluded at `Taskfile.yaml:764`; the gate covers the regenerated `invariants.md`.) |
| Task 1 automated verify (`PublicCharacter` present, `set equality` present, `field_visibility` absent) | PASS — `field_visibility` count is 0 in the §2 body |
| §2 required tokens | all three message types, all three projection functions, `order-independent`, `Notably absent`, both replacement RPC names, `deprecation window` — present |
| §3 audience cells | 29 data rows: 14 `public`, 14 `owner`, 1 `admin`. No fourth token. |
| §3 `path:line` citations | 77 distinct citations extracted and resolved against the tree; every one shows the construct named. The two `plugin/host/v1/world.proto` short-form citations were missed by the extraction regex (`v1` contains a digit) and were resolved by hand — both correct. |
| §13 ids vs registry | set-equal: `INV-ACCESS-10/11/12`, `INV-PRIVACY-9/10` |
| `INV-ACCESS-12` shape | `binding: pending`, no `asserted_by`, `origin_spec` = the planning SPEC path |
| `INV-ACCESS` scope `origin_specs` | `.planning/phases/01-portal-spec/01-SPEC.md` present exactly once (added by 01-01; no duplicate added) |
| Section headings | still 16, in order; zero `01-02` placeholders remain |
| Deletions in either commit | none (`git diff --diff-filter=D` empty) |
| Untracked files | none |

## Known Stubs

None. `01-SPEC.md` retains nine intentional placeholders for §4–§6 and §9–§12, §14–§16 minus the sections wave 1 and this wave filled — each naming the plan that authors it. §2 and §3 carry no placeholder line.

Two forward references are deliberate and named, not stubs: §3.3's three replacement rows cite "Phase 4" as their proto location because the RPCs do not exist yet, and §3.4 defers §9's rows to plan 01-04 by rule rather than by silence.

## Threat Flags

None new. This plan authored a document and one registry entry; it introduced no endpoint, auth path, file access, or schema change. The threats the plan's own register named (T-01-08 through T-01-13) are each discharged in text: T-01-08 by §2.2, T-01-09 by §3.3's tree-derived enumeration, T-01-10 by §2.6's exact-string key, T-01-11 by §2.7, T-01-12 by §3.2's cross-listing rule, and T-01-13 remains `accept` as the plan specified.

## Notes for the orchestrator

- **STATE.md and ROADMAP.md were not touched.** `git diff 69edc8034..HEAD --name-only` returns exactly three files.
- **Amendment count is still FIVE**, unchanged by this plan. Plan 01-05 carries the four from `01-CONTEXT.md:197-202` plus the `INV-PRIVACY` boundary amendment 01-01 landed. This plan added none.
- **Actuals scale:** `tokens: 8636` is `chars/4` over the realized diff (`git diff 69edc8034..HEAD | wc -c` = 34,545), per the ADR-2629 contract. The plan's `estimate.tokens: 60000` is on the whole-run context scale, so the two are not directly comparable; recorded honestly rather than adjusted.
- **One item for 01-04:** §3.4 places an obligation on §9 — every new character-returning RPC needs an audience verdict drawn from §2.1's three, or the Phase-4 census cannot be written from the SPEC alone.

## Self-Check: PASSED

- Files: `01-SPEC.md`, `invariants.yaml`, `invariants.md`, `01-02-SUMMARY.md` — all FOUND.
- Commits: `82d4d9165`, `47b95bccc` — both FOUND in `git log`.
</content>
</invoke>
