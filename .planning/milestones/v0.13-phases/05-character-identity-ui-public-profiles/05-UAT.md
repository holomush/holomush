---
status: complete
phase: 05-character-identity-ui-public-profiles
source: [05-VERIFICATION.md]
started: 2026-08-13T12:21:26Z
updated: 2026-08-13T12:55:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Live create at `/characters/new` with a full-width-Latin name (e.g. `Ｋａｅｌ`), then observe the roster confirmation
expected: The confirmation names the SERVER-folded ASCII form, not the string typed. A rejected submit preserves all six entered values and moves focus to the name field.
why_human: WINDOWS.md row 19 — 05-06 Task 3 human-check recorded UNRUN. Needs a live stack; the E2E covers the happy path but the plan's own walkthrough was never executed.
result: pass
source: live-stack (docker compose, gateway localhost:8080, Playwright-driven)
observed: |
  Typed `Ｋａｅｌ` into Name plus all five profile fields. Confirmation rendered
  "Created Kael." — the server-folded ASCII form, NOT the typed string. The roster card
  also rendered `Kael`, so both echo sites carry the server name.
  Rejection half: re-submitted the now-taken name `Kael` with all six fields populated.
  All six values survived (Kael, she/her, Rooftop courier, Tiefling, 24, Gutter Post),
  the Name field took focus (`[active]`), and the error read "That name is taken. Try another."
  — authored player copy with no `[code]` prefix and no server token, i.e. the client reads
  `rawMessage`, not `ConnectError.message`.

### 2. Load `/characters` with a mixed roster (at least one active, one retired, one default) and exercise the sectioned grid
expected: Playable section first with the create link; Not playable second, expanded, its count chip collapsing the grid out of the flow; the retired card shows `Retired` and no session word; both echo sites render the server name.
why_human: WINDOWS.md row 20 — 05-07 Task 3 human-check recorded UNRUN. Requires a live grid.
result: pass
source: live-stack (roster seeded: Kael=default+active, Toma Reyes=active, Vessa Dunmore=retired)
observed: |
  Playable first (Kael, Toma Reyes) with the "Create a character / Names are reserved once
  taken." tile. Not playable second, expanded by default, chip reading "Hide 1 character".
  Collapsing is literal removal from the DOM, not `display:none` — after the click
  `document.body.innerText` no longer contained "Vessa Dunmore" or "Retired", so the grid
  genuinely leaves the flow. Retired card showed `Retired` and NO session word (the two
  playable cards showed `Offline`), and carried no "Make default" control. Kael showed
  `Default` + `Offline` and no "Make default". All names rendered server-side forms.
  NOTE: a retired character is not reachable through any UI in v0.13 — `world.Service.RetireCharacter`
  (internal/world/service.go:1308) has no RPC/command/admin caller — so the retired row was
  seeded with `UPDATE characters SET status='retired'`, the same direct write
  web/e2e/characters-roster.spec.ts:31-45 uses.

### 3. Open `/characters/[id]` in two tabs, save a section in tab A, then save a DIFFERENT section in tab B
expected: Tab B's failing section shows the concurrent-edit copy and keeps its typed text; the other four sections are untouched; focus moves to the failed section's first field.
why_human: 05-04's two-tab conflict walkthrough (D7) recorded unrun. The per-section conflict scoping (D-93) cannot be exercised without two live clients against one row.
result: pass
source: live-stack (two browser tabs on the same character row, both loaded at the same version)
observed: |
  Both tabs loaded Kael at the same version. Tab A edited Concept and clicked "Save identity"
  → "Saved.", button returned to disabled, row version advanced. Tab B then clicked
  "Save appearance & history" against its now-stale version. Result, all four clauses:
    - failing section carried "This character changed somewhere else. Reload to get the
      latest, then re-apply your edits." plus a Reload button (authored copy, no server token);
    - its three typed values all survived (appearance / personality / biography);
    - the other four sections carried NO alert and kept their Save buttons disabled — the
      failure did not bleed outside the section that failed (D-93 per-section scoping holds);
    - focus landed on `appearance`, the FIRST field of the failing section.
  Screenshot: uat-test3-conflict.png

### 4. Confirm the roster ordering expectation for `/characters`
expected: Two consecutive roster loads with no intervening write render the Playable cards in the same order.
why_human: Plan 05-01 carried this truth as `verification: backstop` and it is NOT automated-verified. `charRepo.ListByPlayer` has no `ORDER BY` (WINDOWS.md row 22, issue #4965), so the property holds only by heap-scan accident.
result: pass
source: live-stack + source inspection
observed: |
  PASSES, AND THE STATED why_human IS FACTUALLY WRONG AGAINST THIS TREE.
  `charRepo.ListByPlayer` DOES declare an ordering:
    internal/world/postgres/character_repo.go:343
    `SELECT ... FROM characters WHERE player_id = $1 ORDER BY name`
  It has done so since commit 7ff05af3c (PR #4816, 2026-07-13) — a month before this phase
  opened — so the clause was never absent during v0.13 Phase 5.
  The order is TOTAL, not merely stable: `characters_normalized_name_key` is a UNIQUE index
  on normalized_name, so no two rows share a name and `ORDER BY name` fully determines the
  sequence. Five consecutive reads through the real predicate returned
  `Kael | Toma Reyes | Vessa Dunmore` identically, and the rendered grid matched.
  CONSEQUENCE: issue #4965 ("ListByPlayer has no ORDER BY") is invalid as written, and the
  same false premise is repeated in web/e2e/characters-roster.spec.ts:11-17 (which declines
  to assert ordering because of it) and in 05-VERIFICATION.md's why_human above. All three
  want correcting; none is a defect in shipped behaviour.

### 5. Confirm the two UI-SPEC `backstop` truths whose SOLE gate is the ungated web suite — the media renderer and the byte counter
expected: The `ProfileMedia` renderer and `ByteCounter` behave as `05-UI-SPEC.md` describes under a live page, independent of the ungated vitest runner.
why_human: Both truths are `verification: backstop` and are NOT automated-verified. Their only executing gate is the 566-test web suite, which has no Taskfile target and no CI job (#4964) — an unwired runner is not a gate, so these remain human items until #4964 lands.
result: pass
scope: PARTIAL BY CONSTRUCTION — ByteCounter verified live; ProfileMedia is unreachable at runtime in v0.13.
source: live-stack (ByteCounter) + source inspection (ProfileMedia)
observed: |
  ByteCounter — VERIFIED LIVE, on the discriminating case the ungated unit suite's ASCII
  fixtures cannot distinguish:
    - 27 × `三` = 27 UTF-16 code units but 81 UTF-8 bytes → counter rendered "81 / 100".
      It measures BYTES, not `.length`. (A `.length` spelling would have shown "27 / 100".)
    - hidden below the display gate (`shown = bytes >= maxBytes * 0.8`): absent at 79 bytes,
      present from 80 — matches 05-UI-SPEC's within-20%-of-cap rule.
    - exactly at cap: 100 bytes → `data-over="false"`, mirroring the server's strict `>`
      (internal/grpc/characteraccess_write.go:213-224). One byte past: 101 → `data-over="true"`.
    - Save stayed ENABLED when over — no clamping, no truncation, no disabling. The server
      owns the refusal, as the component documents.
  ProfileMedia — NOT OBSERVABLE UNDER A LIVE PAGE IN v0.13, by construction rather than by
  omission. No migration defines a media column, and the component states it directly
  (web/src/lib/components/characters/ProfileMedia.svelte:28-32): "NOTHING IN v0.13 REACHES IT.
  01-SPEC §7.3 ships the media model with zero upload behaviour and nothing mints a media
  identifier, so projectPublic emits no image row in this release." The checkpoint's own
  wording ("under a live page") therefore describes a state this release cannot produce.
  That half remains gated solely by the ungated web suite and stays with #4964.

## Summary

total: 5
passed: 5
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none — no checkpoint failed]

## Follow-Ups (not gaps; no fix plan owed)

- issue: 4965
  finding: "Invalid as written. `ListByPlayer` has carried `ORDER BY name` since 7ff05af3c (PR #4816, 2026-07-13). Recommend closing as invalid and correcting the two in-tree comments that repeat the premise (web/e2e/characters-roster.spec.ts:11-17, 05-VERIFICATION.md human_verification[3].why_human)."
  raised_at: 2026-08-13
- issue: 4964
  finding: "Still open and still load-bearing for the ProfileMedia half of checkpoint 5, which no live page in v0.13 can exercise. ByteCounter no longer depends on it — this UAT verified it live against a multi-byte case."
  raised_at: 2026-08-13
