---
phase: 5
slug: character-identity-ui-public-profiles
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-12
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
| **Full suite command** | `task pr-prep` (bats → schema → luabridge → ebnf → license → plugin build → lint → fmt:check → test → test:int → test:e2e) |
| **Estimated runtime** | ~30s scoped unit · ~5–15 min `task pr-prep` |

> `task test:cover` does **not** interpolate `CLI_ARGS` — it always runs whole-repo (`Taskfile.yaml:250-258`).

---

## Sampling Rate

- **After every task commit:** Run `task test -- <touched packages>` plus `task lint` (the CLAUDE.md floor). Dispatch via the `local-check` agent, not inline.
- **After every plan wave:** Run `task test` (whole repo, `-race`) and `task test:int -- ./test/integration/access/ ./internal/grpc/ ./internal/auth/postgres/`.
- **Before `/gsd-verify-work`:** `task pr-prep` green, run **inline in the parent** (the final gate is never delegated).
- **Max feedback latency:** ~60 seconds for the scoped per-task lane.
- **Web unit tests:** `cd web && pnpm test:unit` is run **manually per web task** — no Taskfile target and no CI job invokes vitest or `svelte-check`. This is stated plainly rather than claimed as a gate.

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|

*Rows are seeded from PLAN.md task IDs by `/gsd-validate-phase` after plans exist. The requirement→test map below is the source those rows are derived from.*

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

- [ ] **Reloadable corpus harness** — `newCorpusEngine` discards the `*policy.Cache` and the `*profileCorpusStore` (`test/integration/access/character_profile_read_test.go:112-132`), so it cannot be reloaded twice. Criterion 4's spec needs a sibling that returns them, or the existing helper widened. **Keep its two guards** (the differs-in-one-direction refusal and the `removed` count) — they are what stop a disarmed control.
- [ ] **`internal/grpc` facade fixtures** — `characteraccess_write_test.go` and `characteraccess_owner_test.go` carry the fixtures; both need the new constructor arguments for `CreateCharacter` / `SetDefaultCharacter` regardless.
- [ ] **E2E logged-out-visit spec** — no existing spec exercises a genuinely logged-out visit to an authenticated app path. The *pattern* exists (`landing.spec.ts` visits `/` anonymously); the *combination* (create while logged in, then read the profile from a fresh cookie-less context) does not. `web/e2e/helpers/db.ts` already exposes `getCharacterByName` / `getCharactersByPlayerId` / `getPlayerByCharacterId` and `fixtures.ts` exposes `registerAndEnterTerminal` — buildable with no new helper.
- [ ] **UI-SPEC backstop #1 — media renderer** (`populated + error | /c/[id] media`): primary replaces the portrait, `alt_text` becomes `alt`, non-empty `content_warning` blurs behind a reveal, zero rows render nothing, and a failed `<img>` load falls back to the identical initial-letter placeholder. Unreachable by running v0.13 (no row can exist) → needs a **fixture-driven `*.svelte.test.ts`**.
- [ ] **UI-SPEC backstop #2 — byte counter**: a held-out test with a multi-byte value at 99 / 100 / 101 bytes proving the client counter and the server agree.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Every web unit / component assertion | IDENT-01, PROFILE-02, EXT-08, both UI-SPEC backstops | `web/package.json` declares `test:unit`, but **no Taskfile target and no CI job** invokes vitest or `svelte-check` — verified by absence in both `Taskfile.yaml` and `.github/workflows/ci.yaml` | `cd web && pnpm test:unit` after each web task, and **paste vitest's own summary lines (`Test Files ... passed` / `Tests ... passed`) into that task's commit body** — an ungated runner's result is evidence only if its output is carried forward. Adding a `web:test` task + `pr-prep` step is a **real gate over a surface no gate covers** (not a duplicate under rule `7zy1161fh1`) but is scope this phase did not ask for. **The deferral is assigned, not hoped for: plan 05-05 Task 3 files it as its own GitHub issue** (a second `gh issue create`, separate from the four-amendment register), and the SUMMARY records the number. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s (scoped lane)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
