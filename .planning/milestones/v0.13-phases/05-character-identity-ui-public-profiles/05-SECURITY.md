---
phase: 05
slug: character-identity-ui-public-profiles
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on (high). 0 = gate satisfied.
threats_open: 0
# Depth was SCALED, not uniform: L3 on bolded-high rows, L2 on plain-high, L1 on medium/low.
# This field records the DEEPEST level applied (which governs every blocking row). See "Audit Depth" below.
asvs_level: 3
created: 2026-08-13
---

# Phase 05 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

**Phase goal:** Web players get the whole identity surface — a structured creation card replacing the
name-only stub, one place to manage every alt, and a public profile page a logged-out visitor can
read — plus the media-schema proof with no uploader.

---

## Audit Depth

`workflow.security_asvs_level` is configured at **1**. This audit deliberately did **not** run at that
depth, and did not take the workflow's L1 short-circuit. Rationale, recorded so a future reader knows
this was a decision rather than a drift:

- The L1 short-circuit closes every row on the plans' own dispositions plus each SUMMARY's
  "Threat Flags: None" — that is the executors grading their own work. The maintainer declined this
  same short-circuit at Phase 04 on the grounds that no threat should rest solely on a self-report.
- `05-05-SUMMARY.md` carries **no `## Threat Flags` section at all** (all seven other plans do), so its
  seven rows — three of them bolded-high — had no self-report to lean on even in principle.

Depth actually applied, per row:

| Row class | Depth | What was required to close |
|---|---|---|
| bolded **high** (L3) | ASVS 3 | End-to-end trace from untrusted entry point to sink; property proven at the wire/DB boundary, not merely that a guard is called |
| plain `high` (L2) | ASVS 2 | Boundary placement — guard proven to run *before* the protected operation, on every path in, with no bypassing early return |
| medium / low (L1) | ASVS 1 | Cited symbol/literal confirmed present |

Verification was partitioned across three independent auditors (05-01…03, 05-04…05, 05-06…08), each
read-only, each required to cite `path:line` for every closure.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| anonymous internet → `/c/[id]` | The project's first unauthenticated read surface; the attacker controls the id and holds no credential | Character id (attacker-chosen); publicly-projected profile fields |
| cookie-less browser context → app | Exercised deliberately by the E2E suite; no credential crosses it | Anonymous reads only |
| browser → gateway (`internal/web`) | Untrusted request body crosses here; the session token is lifted from the `X-Session-Token` header, never the body | Create/mutate payloads, profile masks, prose values |
| gateway → facade (`internal/grpc`) | The gateway computes nothing and forwards the mask VERBATIM — a gateway that filtered would be a second allowlist that can drift | Every authorization decision is made past this line |
| facade → `charname.Gate` | The name-admission decision; the security-adjacent normalizer | Attacker-controlled text incl. invisible and confusable codepoints |
| facade → `auth.PlayerRepository` | The only write path to `players.default_character_id` | Character id, player id |
| facade → `CharacterGenesisService` / `world.Service` | Two separate transactions, each with its own gate | Character row, `entity_properties` rows |
| facade → domain (mask write) | The closed twelve-path allowlist and the per-field byte caps | Mask paths + values |
| policy corpus → compiled snapshot | The only channel by which a configuration change reaches a read decision | ABAC corpus rows |
| stored property rows → marshaled response | Where absence must be produced, and where the specs assert it | Profile field set |
| server response → rendered copy | Where an authored message is permitted and an internal error is not | Error text, confirmations |
| module store → status region | The create confirmation crosses a navigation in process memory, never in a URL | Confirmation name |
| test database → spec setup | Direct SQL used only to reach a lifecycle state no v0.13 UI can produce (retirement) | `characters.status` |

---

## Threat Register

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above `high` count toward `threats_open`*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*
*Depth: ASVS level actually applied when verifying this row.*

### 05-01 — `SetDefaultCharacter` RPC

| Threat ID | Category | Component | Sev | Disp | Depth | Verified control (`path:line`) | Status |
|---|---|---|---|---|---|---|---|
| T-05-01-01 | Spoofing | `WebSetDefaultCharacterRequest` | high | mitigate | L2 | `web.proto:1673-1678` — `character_id` is the only field; token lifted at `character_handlers.go:216`; set-equality gate `characteraccess_routing_census_test.go:601-620` | closed |
| T-05-01-02 | Elevation of privilege | `SetDefaultCharacter` handler | high | mitigate | L2 | Order confirmed positionally in body: `characteraccess_write.go:564` → `:569` → write `:584`; guest denial `player_gate.go:122-123` | closed |
| T-05-01-03 | Information disclosure | ownership refusal | medium | mitigate | L1 | `characteraccess_write.go:195-204` collapses all non-Internal to one literal; test `characteraccess_write_test.go:945-963` | closed |
| T-05-01-04 | Tampering | `players` row | high | mitigate | L2 | Narrow UPDATE `auth/postgres/player_repo.go:257-273`; **sole** production caller `characteraccess_write.go:584` (`rg 'playerRepo\.Update\('` over `internal/grpc/` = no match); integration reads password_hash/email/failed_attempts/locked_until before+after `character_write_test.go:618-630` | closed |
| T-05-01-05 | Tampering | retired-character default | medium | mitigate | L1 | `characteraccess_write.go:574-579` `world.Selectable` → `FailedPrecondition` | closed |
| T-05-01-06 | Information disclosure | error messages | medium | mitigate | L1 | `:579`, `:592` constants only; test `characteraccess_write_test.go:1038-1075` | closed |
| T-05-01-07 | Repudiation | default change (no audit event) | low | accept | L1 | AR-05-01 below | closed (accepted) |
| T-05-01-08 | Denial of service | roster read on every set | low | accept | L1 | AR-05-02 below | closed (accepted) |

### 05-02 — public profile `/c/[id]`

| Threat ID | Category | Component | Sev | Disp | Depth | Verified control (`path:line`) | Status |
|---|---|---|---|---|---|---|---|
| T-05-02-01 | Information disclosure | `/c/[id]` not-found render | **high** | mitigate | L3 | Both causes → same code + same literal: `characteraccess_service.go:428,438,446`, `mapDescriptionError:588` (ErrNotFound) / `:591` (ErrPermissionDenied), literal `:164`; one client predicate `errors.ts:24-26`, one flag `c/[id]/+page.svelte:47`; no id echo | closed |
| T-05-02-02 | Information disclosure | character-id enumeration | medium | accept | L1 | AR-05-03 below | closed (accepted) |
| T-05-02-03 | Information disclosure | which-profiles-are-populated oracle | **high** | mitigate | L3 | No sign-in markup in route or `PublicProfile.svelte`; `TopBar.svelte:142-144` anonymous pair keyed only on auth state, never on page or profile content | closed |
| T-05-02-04 | Information disclosure | client-side filtering | **high** | mitigate | L3 | Absence produced server-side `characteraccess_projection.go:80-82`; wire-byte assertion with paired positive control `characteraccess_profile_test.go:559-590`. **Register text inaccurate — see F-1** | closed |
| T-05-02-05 | Spoofing | anonymous read path | low | accept | L1 | AR-05-04 below | closed (accepted) |
| T-05-02-06 | Tampering | media `content_warning` bypass | medium | mitigate | L1 | `ProfileMedia.svelte:124,163` (warning is the reveal control's label), blur `:205`; test `ProfileMedia.svelte.test.ts:119` | closed |
| T-05-02-07 | Information disclosure | command palette on a public page | low | transfer | L1 | Transferred to **#4962** (verified OPEN, title matches the threat verbatim) | closed (transferred) |

### 05-03 — `CreateCharacter` reshape

| Threat ID | Category | Component | Sev | Disp | Depth | Verified control (`path:line`) | Status |
|---|---|---|---|---|---|---|---|
| T-05-03-01 | Spoofing | `WebCreateCharacterRequest` | high | mitigate | L2 | `web.proto:692-693` `reserved 1` + `reserved "player_token","character_name"`; census requires both conjuncts `…census_test.go:285-297` | closed |
| T-05-03-02 | Elevation of privilege | `CreateCharacter` handler | high | mitigate | L2 | `characteraccess_create.go:131` is the **first** statement; guest-gate set `…census_test.go:196` | closed |
| T-05-03-03 | Information disclosure | name-collision oracle | **high** | mitigate | L3 | `characteraccess_create.go:288` → `msgCharacterNameConfusable` (`auth_errors.go:46`); sink `:258` is `status.Errorf(code, "%s", message)`; test seeds a real collider into the oops context map **and** message, asserts neither reaches the wire `characteraccess_create_test.go:354-378` | closed |
| T-05-03-04 | Information disclosure | error-message leakage | **high** | mitigate | L3 | Every arm of `classifyCharacterCreateError:264-309` returns a package constant; deepest-code shadowing routes to the identical default pair; 13×13 negative matrix `characteraccess_create_test.go:543-576` incl. `NotContains(message, "refused:")` | closed |
| T-05-03-05 | Tampering | homoglyph / confusable squatting | high | mitigate | L2 | `charname/gate.go:146` NFKC, `:151` syntax, `:158` mixed-script, `:167` block list, `:177-184` skeleton — via `Admit` (`admission.go:83`) at `auth/character_service.go:167`, before any write; no client-side mirror in `web/src/` | closed |
| T-05-03-06 | Tampering | duplicate character under a race | high | mitigate | L2 | `migrations/000056_character_normalized_name_unique.sql:68`; 23505 handler `auth/character_service.go:235,251`; both producers → `CHARACTER_NAME_TAKEN` → `AlreadyExists` `characteraccess_create.go:275-280` | closed |
| T-05-03-07 | Denial of service | unbounded character creation | medium | mitigate | L1 | `auth/character_service.go:188-196` `CHARACTER_LIMIT_REACHED` vs `DefaultMaxCharacters` | closed |
| T-05-03-08 | Tampering | oversized profile values on create | medium | mitigate | L1 | `characteraccess_create.go:180` reads the **same** `updateCharacterProfileMaskablePaths` cap table; `TestEveryCreateProfileSeedPathIsAMaskablePath` pins the linkage | closed |
| T-05-03-09 | Repudiation | partial identity card | medium | accept | L1 | AR-05-05 below; checkpoint log implemented `characteraccess_create.go:206-208` | closed (accepted) |
| T-05-03-10 | Tampering | supply chain | low | accept | L1 | AR-05-06 below | closed (accepted) |

### 05-04 — sectioned edit surface `/characters/[id]`

| Threat ID | Category | Component | Sev | Disp | Depth | Verified control (`path:line`) | Status |
|---|---|---|---|---|---|---|---|
| T-05-04-01 | Elevation of privilege | mask-path smuggling | high | mitigate | L2 | `characteraccess_write.go:300-308` exact-string map lookup, `if !ok` → `InvalidArgument` (rejects, does not ignore), before the domain write at `:347`. Mask union **computed independently**: server 12 unique vs page 12 unique, diff empty | closed |
| T-05-04-02 | Tampering | lost update on concurrent edit | high | mitigate | L2 | One version cell `[id]/+page.svelte:175,237`, sent `:202,:223`; CAS `world/postgres/character_repo.go:151-155`; loser surfaced `Aborted` `characteraccess_write.go:419,508`; **no retry-on-conflict exists anywhere in the path** | closed |
| T-05-04-03 | Tampering | boundary-value rejection mismatch | medium | mitigate | L1 | `ByteCounter.svelte:34` `TextEncoder`; fixtures genuinely multi-byte — `THREE_BYTE.repeat(33)` = 99 bytes / 33 chars (a `.length` impl reports 33 and fails), astral case `:148` 84 bytes vs 82 UTF-16 — `ByteCounter.svelte.test.ts:64-89` | closed |
| T-05-04-04 | Information disclosure | error-code leakage to the player | medium | mitigate | L1 | `ProfileSection.svelte:94,122`; absence asserted `ProfileSection.svelte.test.ts:218-220` | closed |
| T-05-04-05 | Tampering | RP-preferences → settings column | medium | mitigate | L1 | `+page.svelte:128,214` mask path only; domain writes `entity_properties` `world/service.go:1170-1185` | closed |
| T-05-04-06 | Elevation of privilege | structural write via command parser | high | mitigate | L2 | `rg sendCommand` over route + both components → no match | closed |
| T-05-04-07 | Information disclosure | visibility editor shipping by accident | high | mitigate | L2 | `rg -i 'retire\|rename\|delete\|visibility\|who can see'` over route → no match | closed |
| T-05-04-08 | Repudiation | privacy edits are not retroactive | low | accept | L1 | AR-05-07 below; standing statement rendered above all sections `+page.svelte:63-64,259` | closed (accepted) |

### 05-05 — tier floor + media projection

| Threat ID | Category | Component | Sev | Disp | Depth | Verified control (`path:line`) | Status |
|---|---|---|---|---|---|---|---|
| T-05-05-01 | Information disclosure | a stamped (cached-onto-row) floor | **high** | mitigate | L3 | `character_readtime_floor_test.go:257-297`; direct-SQL row snapshots `:157-180` via `env.pool` prove the field set changes with the corpus alone | closed |
| T-05-05-02 | Information disclosure | unenumerated media name defaulting to permit | **high** | mitigate | L3 | Denial is **structural**: `characteraccess_projection.go:27-38` fixed 10-element array (no `.10`), both emission sites `:105,:213` iterate it; public denial `characteraccess_service.go:532`; owner denial `characteraccess_owner.go:180`; test `media_schema_test.go:280-285`. **See F-2** | closed |
| T-05-05-03 | Information disclosure | present-but-empty media field | medium | mitigate | L1 | `media_schema_test.go:241-249` non-vacuity control + `strings.Contains(raw, …)` on the marshaled body | closed |
| T-05-05-04 | Tampering | a disarmed control corpus | medium | mitigate | L1 | `character_profile_read_test.go:146-153` both guards present, single definition | closed |
| T-05-05-05 | Repudiation | a fabricated invariant binding | **high** | mitigate | L3 | INV-ACCESS-10 binding genuine, not partial: clauses (a)/(b) `character_readtime_floor_test.go:104`, clause (c) `profilevis_test.go:345-373` (`require.Error` + `ErrEvaluationFailed` + `assert.Nil`); clause→site table `invariants.yaml:2305-2320` matches what the tests assert; no `Skip` placeholder | closed |
| T-05-05-06 | Tampering | a green census bought by deleting a row | high | mitigate | L2 | `git show --stat 977347d35 bd253ee3b fb8a72e92` — no census file touched | closed |
| T-05-05-07 | Denial of service | specs holding a Postgres testcontainer | low | accept | L1 | AR-05-08 below; no `testcontainers` in either new spec, both reuse shared `env.pool` | closed (accepted) |

### 05-06 — create form `/characters/new`

| Threat ID | Category | Component | Sev | Disp | Depth | Verified control (`path:line`) | Status |
|---|---|---|---|---|---|---|---|
| T-05-06-01 | Spoofing | homoglyph / confusable name | high | mitigate | L2 | `auth/character_service.go:167` `gate.Admit` runs **before** the write at `:216`; `CreateCharacterForm.svelte` carries zero normalizer logic (prose only, `:55`) | closed |
| T-05-06-02 | Information disclosure | name-corpus enumeration via confusable message | **high** | mitigate | L3 | Server `charname/gate.go:188-194` names no character; `gate_test.go:105-107` three `NotContains` against a genuinely seeded collision. Client `CreateCharacterForm.svelte:100` reads **`rawMessage`**, `:101` unmodified; `…test.ts:198` `toBe(authored)` — nothing appended. **See F-3** | closed |
| T-05-06-03 | Information disclosure | internal error text reaching the player | medium | mitigate | L1 | `CreateCharacterForm.svelte:97-104` classify-by-code; absence assertions `…test.ts:200-201` (`invalid_argument`, `[`) and `:230-232` (`CHARACTER_CREATE_FAILED`, `pool exhausted`, `internal`) with the mock rejecting a real message | closed |
| T-05-06-04 | Spoofing | a forged create confirmation | low | mitigate | L1 | `createdNotice.ts:38-53` one-shot module value; neither route reads `searchParams` / `$page.url` | closed |
| T-05-06-05 | Tampering | duplicate character from a retried submit | medium | mitigate | L1 | `createFlow.ts:102-106` swallow+warn; `createFlow.test.ts:116-128` mocks `goto` rejection and asserts success | closed |
| T-05-06-06 | Elevation of privilege | creation through the command parser | high | mitigate | L2 | `client.ts:154` `client.webCreateCharacter`; `sendCommand` = **zero** matches across `lib/characters/`, `components/characters/`, `routes/(authed)/characters/` | closed |
| T-05-06-07 | Denial of service | unbounded submissions | low | accept | L1 | AR-05-09 below; `character_service.go:192-198` limit fires before write; absence of a client limit is real and deliberate | closed (accepted) |

### 05-07 — sectioned owner roster `/characters`

| Threat ID | Category | Component | Sev | Disp | Depth | Verified control (`path:line`) | Status |
|---|---|---|---|---|---|---|---|
| T-05-07-01 | Information disclosure | another player's roster | high | mitigate | L2 | `web.proto:745` `WebListCharactersRequest {}` and `:1582` `WebListMyCharactersRequest {}` — **no player-id field**; identity resolved from `req.Header()` token only, `character_handlers.go:81-88`, `auth_handlers.go:237-247` | closed |
| T-05-07-02 | Information disclosure | presence telemetry on a retired character | medium | mitigate | L1 | `RosterCard.svelte:62-64` template switch, `:95` gate; `RosterCard.svelte.test.ts:100-108` supplies `hasActiveSession: true` **deliberately** and asserts `not.toMatch(SESSION_WORDS)` — non-vacuous | closed |
| T-05-07-03 | Spoofing | a forged create confirmation | low | mitigate | L1 | No `searchParams` / `$page` / `url.` in the roster route | closed |
| T-05-07-04 | Elevation of privilege | default change via command parser | high | mitigate | L2 | `client.ts:121` `client.webSetDefaultCharacter`; `sendCommand` zero matches in `RosterCard.svelte`, `CharacterRoster.svelte`, `characters/+page.svelte` | closed |
| T-05-07-05 | Denial of service | roster load on every visit | low | accept | L1 | AR-05-10 below | closed (accepted) |
| T-05-07-06 | Tampering | stale default marker after a failed set | low | mitigate | L1 | `characters/+page.svelte:111-116` — assignment sits **inside** the `try`, after the await resolves; `catch` sets copy only | closed |

### 05-08 — Playwright E2E suite

| Threat ID | Category | Component | Sev | Disp | Depth | Verified control (`path:line`) | Status |
|---|---|---|---|---|---|---|---|
| T-05-08-01 | Information disclosure | a public page that quietly requires a session | **high** | mitigate | L3 | `public-profile.spec.ts:82/115/179` fresh `browser.newContext()`; `:89`, `:118` assert `cookies()` `toHaveLength(0)` — makes the reuse failure loud | closed |
| T-05-08-02 | Information disclosure | distinguishable not-found causes | **high** | mitigate | L3 | `public-profile.spec.ts:124-135` captured-vs-captured `expect(malformedText).toBe(absentText)`; `:140-141` neither id echoed; `:146-161` 12 reason regexes; `:167-187` positive control defeats the all-collapse case | closed |
| T-05-08-03 | Information disclosure | echoed name leaking the raw input | medium | mitigate | L1 | `characters-create.spec.ts:114` `not.toContainText(typed)` beside `:109` `toContainText(stored)`; also `:120` on the card | closed |
| T-05-08-04 | Information disclosure | presence telemetry on a retired character | medium | mitigate | L1 | `characters-roster.spec.ts:125-127`. **Conditionally vacuous at this tier — see F-4.** Property genuinely pinned one tier down by T-05-07-02 | closed |
| T-05-08-05 | Repudiation | a green suite bought by quarantine | medium | mitigate | L1 | Zero matches for `@quarantine`, `tag:`, `quarantinetest`, `test.skip/fixme` across all three specs (Playwright tag idiom checked specifically); `test/quarantine.yaml` untouched this phase | closed |
| T-05-08-06 | Denial of service | E2E lane contention | low | accept | L1 | AR-05-11 below; `Taskfile.yaml:315-319` refuses with `exit 1`, verdict read from `$E2E_EXIT` `:325-327`, never from a matched string | closed (accepted) |

**Totals — 59 threats: 47 mitigate, 11 accept, 1 transfer. 27 high, 19 medium, 13 low. Closed 59, open 0.**

---

## Accepted Risks Log

**Attribution matters here.** Only AR-05-05 traces to a maintainer ruling; the other ten are
**plan-time dispositions authored by the planner** and were not individually ratified by a human.
They are recorded as such so a future audit does not mistake a planner default for sign-off.
No accepted risk is `high` — every one is low or medium, so none affects `threats_open`.

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-05-01 | T-05-01-07 | v0.13 emits no audit event for a preference change on a `players` column; the change is player-scoped, self-service and immediately reversible by the same player | plan-time disposition (`05-01-PLAN.md:452`) | 2026-08-12 |
| AR-05-02 | T-05-01-08 | Response re-reads the caller's own roster, bounded by `DefaultMaxCharacters`, behind session auth; not anonymously reachable | plan-time disposition (`05-01-PLAN.md:453`) | 2026-08-12 |
| AR-05-03 | T-05-02-02 | A ULID is 128 bits of `crypto/rand` entropy, not guessable at scale; an enumerating client learns only the uniform not-found response. No rate limit added — its absence is what makes the uniform response the whole defence | plan-time disposition (`05-02-PLAN.md:365`) | 2026-08-12 |
| AR-05-04 | T-05-02-05 | The route authenticates nothing by design and degrades a stale cookie to the least-privileged rung — fail-closed by construction, since anonymous can only narrow what is returned | plan-time disposition (`05-02-PLAN.md:368`) | 2026-08-12 |
| AR-05-05 | T-05-03-09 | Q1's ratified two-transaction shape: a profile-write failure is logged with the character id and attempted paths, but the player is told the create succeeded. The player-visible consequence is handled separately by the `profileIncomplete` notice | **maintainer ruling** (Q1, pinned as a blocking checkpoint during 05-03) | 2026-08-12 |
| AR-05-06 | T-05-03-10 | The plan installs no package in either ecosystem (`05-RESEARCH.md` § Package Legitimacy Audit has no members), so no legitimacy checkpoint is owed | plan-time disposition (`05-03-PLAN.md:507`) | 2026-08-12 |
| AR-05-07 | T-05-04-08 | The system genuinely cannot recall what visitors already read; PROFILE-12's standing statement is a disclosure to the player rather than a technical control | plan-time disposition (`05-04-PLAN.md:334`) | 2026-08-12 |
| AR-05-08 | T-05-05-07 | Both specs reuse the package's existing shared fixture rather than starting a container of their own | plan-time disposition (`05-05-PLAN.md:378`) | 2026-08-12 |
| AR-05-09 | T-05-06-07 | The facade enforces `CHARACTER_LIMIT_REACHED` per player; no client-side rate limit is added and its absence is deliberate — the server-side limit is the control | plan-time disposition (`05-06-PLAN.md:376`) | 2026-08-12 |
| AR-05-10 | T-05-07-05 | Two bounded owner-scoped reads behind session auth; character count capped by `DefaultMaxCharacters` | plan-time disposition (`05-07-PLAN.md:332`) | 2026-08-12 |
| AR-05-11 | T-05-08-06 | `task test:e2e` refuses to start when a `holomush-e2e` compose project is already up; the refusal is read from the exit code, never from a matched string | plan-time disposition (`05-08-PLAN.md:297`) | 2026-08-12 |

### Transferred

| Ref | Threat | Transferred to | State |
|---|---|---|---|
| T-05-02-07 | `visibleSections()` offers `(authed)` destinations to an anonymous viewer on a public page. The `(authed)` redirect is the fail-safe working; a fix means widening `SectionVisibility` beyond `isGuest` inside a shared registry carrying ADR `holomush-stds8` | **#4962** | OPEN — verified 2026-08-13, title matches the threat verbatim |

---

## Findings — accurate properties, inaccurate claims

Every row below is **CLOSED**: the security property holds. What is recorded here is that the
register's *stated reason* was wrong or weaker than advertised. This distinction is the point — a
control that works for a reason nobody wrote down correctly is one refactor away from being removed
by someone who trusted the sentence.

**F-1 · T-05-02-04 — the renderer does hold a field list.**
The register says "*the renderer holds no expected-field list*". It does:
`PublicProfile.svelte:66-136` spells out twelve literal field names as `{#if 'profile.pronouns' in p}`
… `{#if 'profile.rp_preferences' in p}`. The property survives, because those are **presence tests over
server-produced absence**, not filters over received-then-hidden data, and the wire-byte test proves
the bytes never leave the server. But the supporting criterion (`05-02-PLAN.md:265`) only
negative-greps `Object.keys|Object.entries|#each .*profile` — the `in p` guard form sidesteps it
entirely, so **the criterion passes without testing the sentence it is cited for**.
*Suggested correction:* reword to "the renderer performs no filtering of received fields", and if the
stronger claim is wanted, widen the grep to `'profile\.[a-z_]+' in `.

**F-2 · T-05-05-02 — one gate, not two; and the evidence covers one direction.**
Two sub-findings. (a) The register cites only the guest-rung test, which exercises the **public** read;
the **owner** read is closed by a different mechanism (`isGovernedProfileName`,
`characteraccess_owner.go:180`) that this plan does not test. The property holds both ways; the
evidence covers one. (b) In `projectPublic`'s text-map loop
(`characteraccess_projection.go:78-93`) an unenumerated media name is *not* skipped, so if it ever
cleared a tier floor it would land in the prose `Profile` map. Denial rests **solely** on the ABAC
corpus. That is exactly §8.6's totality rule as the register claims — so the mitigation text is
accurate — but it is a single gate with no defence in depth. The code already cites **#4959** for the
adjacent pinned assumption.

**F-3 · T-05-06-02 — the collision test does not pin its code.**
`gate_test.go:91` asserts `require.Error` plus three `NotContains`, but never asserts the code is
`NAME_CONFUSABLE`. A future fixture change could silently retarget it at the mixed-script or blocklist
refusal and every absence assertion would still pass. The sibling at `gate_test.go:83` pins the code
for the same input class, so the property holds today; an `errutil.AssertErrorCode` here would close
the drift window. Not a threat gap.

**F-4 · T-05-08-04 — conditionally vacuous at the E2E tier.**
`characters-roster.spec.ts:125-127` asserts only the **absence** of `session-badge` and of
`/\b(active|offline)\b/i` on the retired card, with **no paired positive control** — nothing asserts a
session badge appears anywhere on the page. The roster route deliberately tolerates a failed session
read (`characters/+page.svelte:67` `Promise.allSettled`; `:76` populates the overlay only when
`fulfilled`), so if `webListCharacters` failed outright every row loses its overlay, the page still
draws, and the assertion passes for the wrong reason — it cannot distinguish "the template suppressed
it" from "no session data ever arrived". `05-08-SUMMARY.md:342` claims it runs "with session data
reaching the roster normally"; nothing in the spec establishes that. The PLAN row's own weaker wording
is literally true, which is why this is closed-as-written. **The property is genuinely pinned one tier
down** by T-05-07-02 (`RosterCard.svelte.test.ts:100-108`), which does supply session data deliberately.

**F-5 · process gap — `05-05-SUMMARY.md` has no `## Threat Flags` section.**
Every other plan in the phase recorded one. All seven of its rows were therefore verified from scratch
rather than against a self-report. Mitigating fact, grounded: the three 05-05 commits
(`977347d35`, `bd253ee3b`, `fb8a72e92`) changed **zero production files** — only `*_test.go` plus
`docs/architecture/invariants.{yaml,md}` — so the missing self-report could not have concealed new
production attack surface. The omission is a process defect, not an exposure.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Depth | Run By |
|------------|---------------|--------|------|-------|--------|
| 2026-08-13 | 59 | 59 | 0 | L3/L2/L1 scaled (config was L1; short-circuit declined) | `gsd-security-auditor` ×3 (partitioned 05-01…03 / 05-04…05 / 05-06…08), orchestrated by `/gsd-secure-phase 5` |

All three partitions returned `VERDICT: SECURED` independently. No partition reported an
`unregistered_flag` — no new network endpoint, auth path, file-access pattern or schema change was
found outside the declared registers.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer) — 47 / 11 / 1
- [x] Accepted risks documented in Accepted Risks Log — 11 entries, attribution recorded per entry
- [x] `threats_open: 0` confirmed — all 27 high rows closed at L2 or L3 depth with `path:line` evidence
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-08-13
