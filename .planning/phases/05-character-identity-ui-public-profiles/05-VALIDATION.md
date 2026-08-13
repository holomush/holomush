---
phase: 5
slug: character-identity-ui-public-profiles
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-08-12
validated: 2026-08-13
---

# Phase 5 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `05-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go unit: `go test` via `gotestsum` + `testify` (ACE naming) · Go integration: Ginkgo/Gomega under `//go:build integration` · Web unit/component: `vitest 4.1.10` + `jsdom 29.1.1` (components mounted with Svelte's own `mount`/`unmount` — no `@testing-library/svelte`) · E2E: Playwright `1.61.1` on the Docker compose stack |
| **Config file** | Go: none (`Taskfile.yaml:185-200`, `265-289`) · Web: `web/vite.config.ts:6-34` + `web/src/test-setup.ts` |
| **Quick run command** | `task test -- ./internal/grpc/ ./test/meta/` (scoped; `test` and `test:int` both interpolate `{{.CLI_ARGS \| default "./..."}}`) |
| **Full suite command** | `task pr-prep` (bats → schema → luabridge → ebnf → license → plugin build → lint → fmt:check → test → **web:test** → test:int → test:e2e) — `web:test` added 2026-08-13 by this audit; it is in both the fast (`pr-prep:fast:run`) and full (`pr-prep:run`) lanes |
| **Estimated runtime** | ~30s scoped unit · ~12s `task web:test` · ~5–15 min `task pr-prep` |

> `task test:cover` does **not** interpolate `CLI_ARGS` — it always runs whole-repo (`Taskfile.yaml:250-258`).

---

## Sampling Rate

- **After every task commit:** Run `task test -- <touched packages>` plus `task lint` (the CLAUDE.md floor). Dispatch via the `local-check` agent, not inline.
- **After every plan wave:** Run `task test` (whole repo, `-race`) and `task test:int -- ./test/integration/access/ ./internal/grpc/ ./internal/auth/postgres/`.
- **Before `/gsd-verify-work`:** `task pr-prep` green, run **inline in the parent** (the final gate is never delegated).
- **Max feedback latency:** ~60 seconds for the scoped per-task lane.
- **Web unit tests:** ~~run manually per web task~~ — **superseded 2026-08-13 by this audit.** `task web:test` (vitest + `svelte-check`) now exists and is wired into `pr-prep:fast:run`, `pr-prep:run`, and the CI **Build** job (an already-required check). `cd web && pnpm test:unit` remains valid for a tight local loop, but it is no longer the *only* runner. Closes holomush#4964.

---

## Per-Task Verification Map

Threat refs cite the **plan's** STRIDE register range (threats are registered per plan, not per task);
all 59 are dispositioned in `05-SECURITY.md` (verdict SECURED).
Every row's Status was re-derived by running the lane, not inherited from the seeded column.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 05-01 T1 | 01 | 1 | IDENT-05 | T-05-01-01..08 | The narrow `UpdateDefaultCharacter` writes only that column (never `password_hash`) | integration + meta | `task test -- ./test/meta/ ./internal/auth/... ./internal/grpc/ ./internal/web/ && task test:int -- ./test/integration/access/ && task lint:proto` | ✅ `player_repo_test.go:418`, census gates | COVERED |
| 05-01 T2 | 01 | 1 | IDENT-05 | T-05-01-01..08 | Guest denied; unparseable and not-owned collapse onto one outcome | unit | `task test -- -run 'TestSetDefaultCharacter\|TestWebSetDefaultCharacter' ./internal/grpc/ ./internal/web/` | ✅ `characteraccess_write_test.go:906-1038` | COVERED |
| 05-01 T3 | 01 | 1 | IDENT-05 | T-05-01-01..08 | The `Make default` control moves the badge without a reload | web component | `task web:test` | ✅ `characters/client.test.ts` | COVERED |
| 05-02 T1 | 02 | 2 | EXT-08, PROFILE-02 | T-05-02-01..07 | Media renderer: `alt_text`→`alt`, `content_warning` blur, `<img>` failure falls back to the identical placeholder | web component | `task web:test` | ✅ `ProfileMedia.svelte.test.ts`, `CharacterPortrait.svelte.test.ts` | COVERED |
| 05-02 T2 | 02 | 2 | PROFILE-01, PROFILE-02 | T-05-02-01..07 | PublicProfile is props-only and enforces the absence contract (no key ⇒ no element) | web component | `task web:test` | ✅ `PublicProfile.svelte.test.ts` | COVERED |
| 05-02 T3 | 02 | 2 | PROFILE-01, PROFILE-10a | T-05-02-01..07 | Unreachable profile is byte-identical to nonexistent (INV-PRIVACY-9) | integration + E2E | `task test:int -- ./test/integration/access/` · `task test:e2e -- e2e/public-profile.spec.ts` | ✅ `character_profile_read_test.go`, `public-profile.spec.ts` | COVERED |
| 05-03 T1 | 03 | 2 | IDENT-01 | T-05-03-01..10 | Every create-path code maps to its pinned status and authored message; no internal code string on the wire | unit + meta | `task test -- ./test/meta/ ./internal/grpc/ && task test:int -- ./test/integration/access/ && task lint:proto && task build` | ✅ `characteraccess_create_test.go:182-583` (11 funcs) | COVERED |
| 05-03 T2 | 03 | 2 | IDENT-01 | T-05-03-01..10 | `WebCreateCharacter` forwards all six submitted values and passes facade errors through as-is | unit + meta | `task test -- ./test/meta/ ./internal/web/ && task build && task lint:proto` | ✅ `character_handlers_test.go:157-226` | COVERED |
| 05-03 T3 | 03 | 2 | IDENT-01 | T-05-03-01..10 | The `createCharacter` client wrapper reads `rawMessage`, not the `[code]`-prefixed `message` | web unit | `task web:test` | ✅ `characters/client.test.ts`, `connect/errors.test.ts` | COVERED |
| 05-04 T1 | 04 | 3 | PROFILE-06/07/08/09 | T-05-04-01..08 | The client counter measures **bytes** and agrees with the server at 99/100/101 on multi-byte input | web component | `task web:test` | ✅ `ByteCounter.svelte.test.ts` | COVERED |
| 05-04 T2 | 04 | 3 | PROFILE-06/07/08/09 | T-05-04-01..08 | Per-section dirty/save/status/error scoping (D-93): a failing section does not disable its siblings | web component | `task web:test` | ✅ `ProfileSection.svelte.test.ts` | COVERED |
| 05-04 T3 | 04 | 3 | PROFILE-12 | T-05-04-01..08 | The not-retroactive notice appears on `/characters/[id]` and on no other surface | web component + E2E | `task web:test` · `task test:e2e -- e2e/characters-roster.spec.ts` | ✅ `characters-roster.spec.ts:221` | COVERED |
| 05-05 T1 | 05 | 3 | PROFILE-01 | T-05-05-01..07 | Corpus mutation + `Reload()` changes the same anonymous viewer's answer with **no write** to the character or its rows | integration | `task test:int -- ./test/integration/access/` | ✅ `character_readtime_floor_test.go:105` (spec R1) | COVERED |
| 05-05 T2 | 05 | 3 | EXT-05 | T-05-05-01..07 | 1 primary + 10 gallery rows read back through the viewer-filtered path; the unenumerated 11th does not | integration | `task test:int -- ./test/integration/access/` | ✅ `media_schema_test.go:95` (specs M1, M2) | COVERED |
| 05-05 T3 | 05 | 3 | PROFILE-01, EXT-05 | T-05-05-01..07 | INV-ACCESS-10 bound clause-by-clause; the registry render is drift-free | meta | `go run ./cmd/inv-render && git diff --exit-code docs/architecture/invariants.md && task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted\|TestInvariantRegistryRender' ./test/meta/` | ✅ `invariants.yaml:2321-2330` | COVERED |
| 05-06 T1 | 06 | 4 | IDENT-01 | T-05-06-01..07 | The RPC is authoritative; profile-seeding failure returns SUCCESS with keys absent + a `profileIncomplete` notice | web unit | `task web:test` | ✅ `characters/createFlow.test.ts` | COVERED |
| 05-06 T2 | 06 | 4 | IDENT-01 | T-05-06-01..07 | A rejected submit preserves all six entered fields and focuses Name | web component | `task web:test` | ✅ `CreateCharacterForm.svelte.test.ts` | COVERED |
| 05-06 T3 | 06 | 4 | IDENT-01 | T-05-06-01..07 | `/characters/new` echoes the **server-folded** stored name, not what was typed | web component + E2E | `task web:test` · `task test:e2e -- e2e/characters-create.spec.ts` | ✅ `characters-create.spec.ts` | COVERED |
| 05-07 T1 | 07 | 5 | IDENT-05, IDENT-01 | T-05-07-01..06 | Badge matrix + suppression: a retired card shows the retired badge and no session word | web component | `task web:test` | ✅ `RosterCard.svelte.test.ts` | COVERED |
| 05-07 T2 | 07 | 5 | IDENT-05 | T-05-07-01..06 | Sections, the collapse chip (removes from DOM, not `display:none`), and the empty states | web component | `task web:test` | ✅ `CharacterRoster.svelte.test.ts` | COVERED |
| 05-07 T3 | 07 | 5 | IDENT-05, IDENT-01 | T-05-07-01..06 | Two calls joined on character id; status only on `OwnCharacter`, badge only on `CharacterSummary` | web component + E2E | `task web:test` · `task test:e2e -- e2e/characters-roster.spec.ts` | ✅ `characters-roster.spec.ts` | COVERED |
| 05-08 T1 | 08 | 6 | PROFILE-01 | T-05-08-01..06 | A genuinely logged-out visit to `/c/<id>` renders name + pronouns + description | E2E | `task test:e2e -- e2e/public-profile.spec.ts` | ✅ `public-profile.spec.ts` (0 skip markers) | COVERED |
| 05-08 T2 | 08 | 6 | IDENT-01 | T-05-08-01..06 | The structured card and its rejection path; the rune counter's 80%-of-cap display gate with a positive control | E2E | `task test:e2e -- e2e/characters-create.spec.ts` | ✅ `characters-create.spec.ts:92-100` | COVERED |
| 05-08 T3 | 08 | 6 | IDENT-05, PROFILE-12 | T-05-08-01..06 | Sections, badges, default, and the not-retroactive notice | E2E | `task test:e2e -- e2e/characters-roster.spec.ts` | ✅ `characters-roster.spec.ts` | COVERED |

### Requirement → test map (from RESEARCH.md)

| Req ID | Behavior | Test Type | Automated Command | File Exists |
|--------|----------|-----------|-------------------|-------------|
| IDENT-01 | `CreateCharacter` proto declared, census rows present | meta (set equality) | `task test -- ./test/meta/` | ✅ (rows are the edit) |
| IDENT-01 | `CreateCharacter` reaches the shared guest gate; is **not** an ownership member | meta (routing census) | `task test -- ./test/meta/` | ✅ |
| IDENT-01 | Name-rejection codes map to `CHARACTER_NAME_INVALID` / `CHARACTER_NAME_TAKEN` on the **wire** | unit | `task test -- -run TestCreateCharacter ./internal/grpc/` | ❌ W0 |
| IDENT-01 | Create returns the display name the server stored (the D-88 echo) | unit | same | ❌ W0 |
| IDENT-01 | A rejected submit preserves every entered field | web component | `cd web && pnpm test:unit` | ❌ W0 — **not a gate** |
| IDENT-01 | Structured creation end to end | E2E | `task test:e2e -- e2e/characters-create.spec.ts` | ❌ W0 |
| IDENT-05 | `SetDefaultCharacter` sets `players.default_character_id` and returns the roster | integration | `task test:int -- ./internal/auth/postgres/ ./test/integration/access/` | ❌ W0 |
| IDENT-05 | Guest denied; non-owner takes the uniform not-owned outcome (paired positive control) | unit | `task test -- -run TestSetDefaultCharacter ./internal/grpc/` | ❌ W0 |
| PROFILE-01 | Anonymous rung resolves and the public projection is returned | integration | `task test:int -- ./test/integration/access/` | ✅ `character_profile_read_test.go` |
| PROFILE-01 | A logged-out browser loads `/c/<id>` and sees name + pronouns + description | E2E | `task test:e2e -- e2e/public-profile.spec.ts` | ❌ W0 |
| PROFILE-01 | Blank field absent from marshaled bytes (PORTAL-10 rule 3) | integration | existing sentinel spec | ✅ `integrationBiographySentinel` |
| PROFILE-02 / EXT-08 | Named empty sheet + web-DM slot render as non-interactive labelled slots | web component | `cd web && pnpm test:unit` | ❌ W0 — **not a gate** |
| PROFILE-06/07/08/09 | The four fields round-trip through the mask | unit | `task test -- ./internal/grpc/` | ✅ `characteraccess_write_test.go` |
| PROFILE-08 | `profile.rp_preferences` is never written to `characters.preferences` | structural | mask maps to `entity_properties` names only | ✅ by construction |
| PROFILE-10a | `characters.description` renders on the public profile | integration | existing | ✅ |
| PROFILE-12 | Notice copy present on `/characters/[id]` | E2E | `task test:e2e` | ❌ W0 |
| **Criterion 4** | Corpus mutation + `Reload()` changes what the **same anonymous viewer** sees, with **no write** to the character or its rows | integration (**NEW #1**) | `task test:int -- ./test/integration/access/` | ❌ W0 |
| **EXT-05 / Criterion 5** | 1 primary + 10 gallery rows insert through the real schema and read back **through the viewer-filtered path** | integration (**NEW #2**) | `task test:int -- ./test/integration/access/` | ❌ W0 |
| EXT-05 | An 11th primary is rejected by `UNIQUE(parent_type,parent_id,name)` | — | **CITE, do not reprove** | ✅ `property_repo_test.go:430` |

### Existing coverage that MUST be cited, not reproved

| Clause | Proven by | Location |
|---|---|---|
| Clearing a floor is set membership, never ordinal | `TestNoTierFloorPolicyUsesAnOrdinalTierComparison` | `seed_profile_visibility_test.go:260` |
| A synthetic 4th rung clears neither shipped floor | `TestASyntheticFourthRungClearsNeitherShippedTierFloor` | `tierfloor_test.go:173` |
| The floor is evaluated per read, twice per attribute, separated by the action token | `TestAttributeVisibleIssuesExactlyTwoEvaluationsSeparatedByTheActionToken` | `profilevis_test.go:113` |
| `UNIQUE(parent_type,parent_id,name)` rejects a duplicate (`PROPERTY_DUPLICATE_NAME`) | `TestPropertyRepository_ParentNameUniqueness` | `property_repo_test.go:430-450` |
| The policy enumerates exactly eleven media names, not twelve | `TestTheElevenMediaNamesAreEnumeratedAndTheTwelfthIsNot` | `seed_profile_visibility_test.go:393` |
| Unreachable profile is byte-identical to nonexistent | `INV-PRIVACY-9` (`binding: bound`) | `invariants.yaml:2164-2172` → `character_profile_read_test.go` |
| Never stamped onto a row | structural — `entity_properties` carries no tier column and no migration writes one | — |

---

## Wave 0 Requirements

All five closed during execution; re-derived against the tree 2026-08-13 (not inherited from this file's seeded column).

- [x] **Reloadable corpus harness** — closed by `test/integration/access/character_readtime_floor_test.go:105` (spec R1); runs green in `task test:int -- ./test/integration/access/` (108/109 specs, the 1 skip unrelated).
- [x] **`internal/grpc` facade fixtures** — closed; `characteraccess_create_test.go` (11 funcs) and `characteraccess_write_test.go:906-1038` carry the new constructor arguments.
- [x] **E2E logged-out-visit spec** — closed by `web/e2e/public-profile.spec.ts` (0 skip/quarantine markers), gated by the required `E2E Test` check.
- [x] **UI-SPEC backstop #1 — media renderer** — closed by `ProfileMedia.svelte.test.ts`; **now gated** via `task web:test`. Still unreachable at runtime in v0.13 (no media column in any migration), so the fixture-driven component test is and remains its only possible proof — that is now a *gated* proof rather than an ungated one.
- [x] **UI-SPEC backstop #2 — byte counter** — closed by `ByteCounter.svelte.test.ts`; **now gated** via `task web:test`. Note the gated E2E assertion at `characters-create.spec.ts:92` covers the **rune** counter (`26 / 32`), a different unit system — it is not a substitute (see `g650arqbgn`).

<details>
<summary>Original Wave 0 text as seeded at plan time (2026-08-12)</summary>

- [ ] **Reloadable corpus harness** — `newCorpusEngine` discards the `*policy.Cache` and the `*profileCorpusStore` (`test/integration/access/character_profile_read_test.go:112-132`), so it cannot be reloaded twice. Criterion 4's spec needs a sibling that returns them, or the existing helper widened. **Keep its two guards** (the differs-in-one-direction refusal and the `removed` count) — they are what stop a disarmed control.
- [ ] **`internal/grpc` facade fixtures** — `characteraccess_write_test.go` and `characteraccess_owner_test.go` carry the fixtures; both need the new constructor arguments for `CreateCharacter` / `SetDefaultCharacter` regardless.
- [ ] **E2E logged-out-visit spec** — no existing spec exercises a genuinely logged-out visit to an authenticated app path. The *pattern* exists (`landing.spec.ts` visits `/` anonymously); the *combination* (create while logged in, then read the profile from a fresh cookie-less context) does not. `web/e2e/helpers/db.ts` already exposes `getCharacterByName` / `getCharactersByPlayerId` / `getPlayerByCharacterId` and `fixtures.ts` exposes `registerAndEnterTerminal` — buildable with no new helper.
- [ ] **UI-SPEC backstop #1 — media renderer** (`populated + error | /c/[id] media`): primary replaces the portrait, `alt_text` becomes `alt`, non-empty `content_warning` blurs behind a reveal, zero rows render nothing, and a failed `<img>` load falls back to the identical initial-letter placeholder. Unreachable by running v0.13 (no row can exist) → needs a **fixture-driven `*.svelte.test.ts`**.
- [ ] **UI-SPEC backstop #2 — byte counter**: a held-out test with a multi-byte value at 99 / 100 / 101 bytes proving the client counter and the server agree.

</details>

---

## Manual-Only Verifications

**None.** The single entry this table carried — "every web unit / component assertion" — was resolved by this audit rather than restated: `task web:test` now gates that surface.

<details>
<summary>Resolved 2026-08-13 — the former sole entry</summary>

| Behavior | Requirement | Why it *was* manual | Resolution |
|----------|-------------|------------|-------------------|
| Every web unit / component assertion | IDENT-01, PROFILE-02, EXT-08, both UI-SPEC backstops | `web/package.json` declared `test:unit`, but **no Taskfile target and no CI job** invoked vitest or `svelte-check` — verified by absence in both `Taskfile.yaml` and `.github/workflows/ci.yaml`. Its own text argued a `web:test` task was "a real gate over a surface no gate covers (not a duplicate under rule `7zy1161fh1`) but scope this phase did not ask for", and assigned the deferral to a GitHub issue. | That issue was filed as **holomush#4964** and is closed by this audit. `task web:test` was added and wired into `pr-prep:fast:run`, `pr-prep:run`, and the CI **Build** job. Build was chosen over a new job because it already carries the Node/pnpm/cache setup *and* is already a required check — a new job name would gate nothing until an operator added it to ruleset `11923801`. |

</details>

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies — 24/24 tasks carry an `<automated>` command; all five Wave 0 items closed
- [x] Sampling continuity: no 3 consecutive tasks without automated verify — no gap anywhere in the 24
- [x] Wave 0 covers all MISSING references — zero MISSING; the residual gap was *gating*, not authorship
- [x] No watch-mode flags — `pnpm test:unit` is `vitest run` (single-shot), not `vitest` watch
- [x] Feedback latency < 60s (scoped lane) — `task web:test` ≈ 12s; scoped Go unit ≈ 30s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated 2026-08-13 by `/gsd-validate-phase 5`.

---

## Validation Audit 2026-08-13

| Metric | Count |
|--------|-------|
| Gaps found | 2 |
| Resolved | 2 |
| Escalated | 0 |

**What the audit actually found.** This file was still `status: draft` with an **empty** Per-Task Verification Map — it was seeded by `plan-phase` and never updated, and the `verify:post` nyquist hook (`produces: VALIDATION.md`) was satisfied by the filename alone. Memory `yd9j0fwj2c` therefore recorded "nyquist SATISFIED", which was true of the hook and false of the coverage.

No requirement was MISSING. Every one had a test that exists and runs green. The two gaps were **PROFILE-02** and **EXT-08**, whose only automated proof ran on a runner nothing invoked:

| Gap | Behavior | Why no gated counterpart existed |
|---|---|---|
| G1 | ProfileMedia renderer | Zero E2E matches. Unreachable at runtime in v0.13 — no media column exists in any migration, so no E2E *can* reach it. |
| G2 | ByteCounter byte-semantics at 99/100/101 | The gated E2E assertion covers the **rune** counter (32 runes); bytes and runes are different unit systems (`g650arqbgn`). The comment at `characters-create.spec.ts:90-91` calls it "the same gate as the five ByteCounter siblings" — an *analogy*, not coverage. |

**Evidence re-derived, not inherited.** Every lane was run:

| Lane | Result | Gate |
|---|---|---|
| Go unit | ✓ exit 0, 1651 tests | CI `Test` |
| Go integration | ✓ exit 0, 108/109 specs (1 unrelated skip) | CI `Integration Test` (required) |
| E2E | ✓ 3 Phase-5 specs, 0 skip/quarantine markers | CI `E2E Test` (required) |
| Web unit | ✓ exit 0, 57 files / 567 tests | **`task web:test` — new** |

**The new gate was proven RED in both halves** before being claimed as a gate:

| Probe | Result |
|---|---|
| Failing vitest assertion | `task web:test` → **exit 201**, `Tests 1 failed \| 567 passed` |
| `svelte-check` type error (vitest passing) | `task web:test` → **exit 201**, `COMPLETED 5602 FILES 1 ERRORS` |

Both probe files were removed; `git status` carries no probe artifacts.
