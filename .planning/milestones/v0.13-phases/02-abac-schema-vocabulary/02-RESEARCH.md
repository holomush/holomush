# Phase 2: ABAC & Schema Vocabulary — Research

**Researched:** 2026-08-03
**Domain:** Unicode identity normalization (UTS #39 / UAX #24), ABAC seed-policy vocabulary, PostgreSQL schema primitives under goose
**Confidence:** HIGH for repo grounding (every citation opened and read this session); HIGH for the P0 external survey (every module downloaded and its source read); MEDIUM for the P1 audit shape (query is grounded in verified DDL, but the *result* is unknown until run against real data)

**Scope note.** This research answers only what `02-CONTEXT.md` left open plus the repo grounding a planner would otherwise fabricate. The 26 locked decisions are treated as binding. Two of them rest on a factual premise this research found to be **wrong**; both are recorded in `## Concerns` rather than relitigated.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

Copied in substance from `.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md` (D-01 … D-26). The full text is normative; the planner MUST read it. Summarised here so this document is self-contained on the points research touches:

**Term B and the profile-visibility policy family**
- **D-01:** Term B is issued against **viewer-flavored read policies only**. The read-side subset of the shipped `seed:property-*` family gains `principal is viewer` twins evaluating the same `visibility` / `visible_to` / `excluded_from` row semantics. `seed:property-owner-write` gets **no twin** — a `viewer:` subject must never hold a write permit. *Reversibility: costly.*
- **D-02:** §01-SPEC §8.5.1.1's **option 2 is REJECTED** (term B against a co-located character subject, denying where none exists) — it violates §8.8 / INV-PRIVACY-10 for the `anonymous` rung. *Reversibility: one-way.*
- **D-03:** Tier-floor configuration ships as **three policies, one per floor rung** (`anonymous` / `guest` / `player`), each carrying an explicit literal list of the §8.6 attribute names at that floor, ANDed with §8.2.1's set-membership clearing test. Not one policy per attribute name; not a single name→floor map. Names matched as whole strings; a name in no list is denied, not defaulted.
- **D-04:** Phase 2 ships an **additive-permit regression test**: a `profile.*` row at `visibility='private'`, a viewer whose tier clears that name's floor, assert the attribute is **absent**. Goes RED if term B is ever dropped.
- **D-05:** §8.5.1.1 is **amended in this phase** to record option 2 rejected and D-01 settled, per the §14 amendment discipline. The finding is additionally **routed to `abac-reviewer`** before Phase 2 merges.

**Admin section registry and the D1 denial-code oracle**
- **D-06:** **Gate first, then distinguish.** `seed:admin-section-access` is evaluated **before** the registry lookup. A caller the gate denies always receives `DENY_ADMIN_SECTION`; only a caller the gate permits can ever receive `DENY_ADMIN_SECTION_UNREGISTERED`.
- **D-07:** Pinned by a **new `INV-PRIVACY` registry entry** (next free id in scope), bound by a test asserting a non-admin's refusal is byte-identical across a registered and an unregistered section id. **MUST be hand-registered** in `docs/architecture/invariants.yaml`. *Reversibility: costly.*
- **D-08:** §10.2's non-vacuous denial test runs at the **shared authorization helper** level in Phase 2 (seven assertions, one per registered section); the **endpoint-level** form lands in Phase 4.
- **D-09:** §10.2's "compile time or at boot" is carried by **boot validation plus a meta-test**: a boot validator refuses to start on a zero-valued or partially-zero authorization descriptor; a meta-test asserts every registry entry has a non-zero action and resource.

**`seed:profile-public-read` and the exposure audit**
- **D-10:** The widening permits off-location reads of **any** `parent_type='character'` row at `visibility='public'` — **not** scoped to §8.6's enumeration. Web exposure stays bounded because a name in no §8.6 row is denied by term A. *Reversibility: costly.*
- **D-11:** The grid-path consequence is **intended**. `public` means public on the grid as well as the web; the colocation restriction was the anomaly. **The fix for any row relying on colocation as de-facto privacy is to change the row's `visibility`, never to narrow the policy.**
- **D-12:** The audit is a **committed read-only query with its result recorded** in the phase artifacts — re-runnable; the recorded count is the evidence criterion 4 asks for.
- **D-13:** The in-world description **stays at the `anonymous` floor** (§8.11), decided in Phase 1, implemented unchanged.

**Character-name block list (IDENT-07 / §6.1.4)**
- **D-14:** The block list lives in the **settings game scope under a `core.*` key**, stored in `holomush_system_info`, seeded by migration with `ON CONFLICT DO NOTHING`. Reuses `settings.SetStringSlice`; no new table; no change to the namespace allowlist. *Reversibility: costly.*
- **D-15:** A pattern that fails to compile is a **hard startup failure** naming the offending entry. The whole list is validated and compiled at boot.
- **D-16:** The compiled list refreshes by **mirroring `policy.Cache` + `policy.Poller`** (atomic compiled snapshot + read barrier; version poll on a cheap indicator; 10s default). Poll `holomush_system_info`'s `updated_at` for the key. A pattern that fails to compile makes `Reload` fail, leaving the **last valid list in force**.
- **Note:** Go's `regexp` is RE2 — **no ReDoS risk**. The residual risk is a *wrong* (over-broad) pattern. Do not add backtracking-defense machinery.

**Pre-existing duplicate resolution (§6.3)**
- **D-17:** **Halt and report; no auto-resolution.** The job reports every collision set with ids, names, owners, `created_at`.
- **D-18:** The **operator supplies the replacement name**, validated through the full §6.1.1 pipeline and the block list before being written.
- **D-19:** Proven by a **synthetic-collision integration test** against real Postgres, **including an NFKC-only pair the live `LOWER(name)` check could never have caught**, then asserting detection and that the `UNIQUE` index applies cleanly afterwards.

**Migration framework and sequencing**
- **D-20:** **goose adoption is a separate phase inserted before this one** (Phase 01.1); Phase 2 execution is gated on it. *Reversibility: one-way.*
- **D-21:** Phase 2's DDL sequences as **three numbered migrations in one release**: (A) `status`, `last_active_at`, `normalized_name` (nullable), skeleton + its **non-unique** index, and the block-list settings seed; (B) a **Go migration** performing the backfill and duplicate detection; (C) `SET NOT NULL` on `normalized_name` **then** `CREATE UNIQUE INDEX`. C's ordering is load-bearing: a `UNIQUE` index over an unbackfilled nullable column succeeds and enforces nothing.
- **D-22:** A Go migration cannot pause for judgement, so on collision it **returns an error naming every collision set**; the transaction rolls back and startup aborts. The operator resolves via a **dedicated CLI command** routing the replacement through the full §6.1.1 pipeline and block list. Resolution **MUST NOT** be a hand-written SQL runbook.
- **D-23:** The skeleton's Unicode version is recorded in a **per-row column beside the skeleton**. *Reversibility: one-way.*

**Lifecycle and roster primitives**
- **D-24:** `characters.last_active_at` **lands in Phase 2** as a schema primitive; the **write seam is Phase 3's**, and it MUST NOT hook `RefreshConnection`. *Reversibility: one-way.*
- **D-25:** The column is **`BIGINT NOT NULL DEFAULT 0`**, `0` = Unix epoch = the never-active sentinel. No nulls. `0` needs a named constant; any "last active" rendering MUST special-case it. Side effect: `0` sorts last under most-recent-first without `NULLS LAST`.
- **D-26:** **Both §11.3 amendments land in this phase**, folded into the same amendment pass as D-05: add `last_active_at` as a permitted sort column (sketch A1), and add the joined `players.username` row (sketch A2). §11.3's existing `characters.player_id` row ("never an ordering") stays correct.

### Claude's Discretion

- **Unicode mechanism for UTS #39 confusables/skeleton.** Deliberately not decided in CONTEXT. `/gsd-plan-phase`'s researcher selects the mechanism — maintained third-party package, generated-into-repo table, or vendored data. **Binding constraint regardless of choice: the Unicode version MUST be pinnable and MUST be recorded per-row (D-23).** → **Answered in `## P0` below.**
- Exact policy ids/names for the three tier-floor policies and the viewer read-policy twins, so long as D-01 and D-03's shapes hold.
- Test-file placement and naming throughout, per `.claude/rules/testing.md`.

### Deferred Ideas (OUT OF SCOPE)

- Nothing was deferred out of scope. goose adoption was promoted to its own phase (D-20), not deferred.
- Follow-ups belonging to later phases: the `last_active_at` **write seam** (Phase 3, must not hook `RefreshConnection`); INV-ACCESS-10/11/12, INV-PRIVACY-9/10 and INV-WORLD-7 all bind in **Phase 4** — only INV-WORLD-5, INV-WORLD-6 and D-07's new entry are Phase 2's to bind; whether `viewer:` and `admin_section:` should be **registered** in `knownPrefixes`.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **IDENT-06** | Character names permit non-Latin scripts but are normalized with NFKC, `Cf` stripping, and a confusable/mixed-script rule | `## P0` selects the confusable mechanism and reports the **Script_Extensions gap** for the mixed-script half; `## Standard Stack`; `## Code Examples` §A/§B |
| **IDENT-07** | Configurable block/disallow list of regular expressions, server-side, at create and rename | `## P2.6` (settings surface + namespace allowlist verified); `## P2.7` (`policy.Cache`/`Poller` mirror); `## Common Pitfalls` P-5 (the `updated_at` poll indicator is **not** bumped by a direct-SQL edit) |
| **IDENT-08** | Player usernames remain ASCII-only — a regression guard pinning `^[a-zA-Z][a-zA-Z0-9_]*$` | `## P2.5` — exact location `internal/auth/player.go:31`, applied at `:167`; the rule is *pinned*, not re-implemented |
| **IDENT-09** | Unique index on a stored normalized character name lands before/with `Rename`; pre-existing duplicates resolved by a one-shot job first | `## P2.4` (three ExistsByName consumers — one more than the SPEC enumerates); `## P2.8` (the in-repo A→B→C fixture that already proves D-21's exact shape); `## Validation Architecture` (RED-first demonstration) |
| **PROFILE-11** | `seed:profile-public-read` permits off-location profile reads; ships only after an audit of existing public `parent_type='character'` rows and existing character descriptions | `## P1` — the runnable read-only SQL, verified against the real DDL, plus where the result is recorded |
| **EXT-07** | `seed:admin-section-access` covers all seven sections and every future section at zero additional policy cost | `## P2.3` — `resource is admin_section` is expressible today (`resource is character_directory` is the in-tree precedent for an underscore-bearing resource type); resource-**type** scoping needs no `when` clause |

</phase_requirements>

## Summary

Phase 2 lands vocabulary and schema, not behavior — but three of its five success criteria hinge on facts the CONTEXT could not settle, and this research settles two of them and reframes the third.

**The P0 question resolves against a third-party dependency and toward in-repo codegen.** No Go module — stdlib, `golang.org/x/text`, or any of the five surveyed third-party packages — is a defensible runtime dependency for a security gate here. The two candidates with a *correct* UTS #39 skeleton have 4 stars and 1 star respectively, one of them created and last pushed on a single day in February 2026. Meanwhile the algorithm itself is roughly twenty lines (NFD → confusable map → NFD), the data file is version-addressed at a stable URL, and the repo already runs seven `//go:generate` pipelines with `task generate`. Generating the table into the repo makes the Unicode version an exported Go constant — which is exactly D-23's binding constraint, satisfied by construction rather than by a test that watches a comment.

**A premise underneath the mixed-script half is wrong, and it is wrong in a way that changes the plan.** `02-CONTEXT.md` states that `golang.org/x/text` "covers NFKC (step 1) and script extensions for UTS #24 (Mechanism A)". It covers NFKC. It has no script package at all — `x/text@v0.40.0/unicode/` contains exactly `bidi`, `cldr`, `norm`, `rangetable`, `runenames`. Go's stdlib has `unicode.Scripts`, but that is the **Script** property, not **Script_Extensions**, and UTS #39's Moderately Restrictive profile is defined over Script_Extensions-derived augmented script sets. §6.1.2's table is therefore implementable as an approximation from stdlib, or faithfully only by generating a second table. This is a scoping fact the planner needs before it sizes Mechanism A.

**The migration gate is already clear, and the repo is further along than the CONTEXT knew.** D-20 gated Phase 2 execution on goose adoption. That work has landed: `internal/store/migrations/` holds 45 single-file goose migrations, `internal/store/migrate.go` wraps `goose.Provider`, and `internal/store/migrate_gointerleave_integration_test.go` ships a fixture chain that already demonstrates D-21's exact A→B→C sequence — nullable column, Go backfill, then `SET NOT NULL` before `CREATE UNIQUE INDEX` — with the load-bearing ordering documented in its own constants. The planner should treat that file as the worked example rather than deriving the shape again.

**Primary recommendation:** generate the UTS #39 confusables table into the repo from a pinned version-addressed `confusables.txt` (a new `cmd/internal/gen-confusables` wired into `task generate`, emitting an exported `UnicodeVersion` constant); implement §6.1.2's mixed-script table over Go stdlib `unicode.Scripts` and record the Script-vs-Script_Extensions approximation explicitly in the amendment pass; and model the three Phase-2 migrations directly on `migrate_gointerleave_integration_test.go`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| NFKC / `Cf` strip / case-fold pipeline | Core domain (`internal/world`) | — | §6.1.5 says Phase 2 **replaces** `NormalizeCharacterName` (`internal/world/validation.go:114-126`); the pipeline is world-model vocabulary, not gateway concern |
| Confusable skeleton computation | Core domain (new leaf package) | Generated data (repo codegen) | Must be dependency-free enough for `internal/world` and the migration package to both import; the data is generated, the algorithm is code |
| Mixed-script restriction | Core domain (same leaf package) | Go stdlib `unicode` | Pure function over a rune sequence; no I/O, no config |
| Block-list evaluation | Core domain | Settings store (`holomush_system_info`) + `Cache`/`Poller` | D-14/D-16: config is DB-backed, compiled snapshot lives in-process, refresh is poll-based because there is no in-process writer and the deployment is multi-replica |
| Uniqueness enforcement | **Database** (`UNIQUE` index) | Core domain (pre-check for a friendly error) | §6.1.3: "Uniqueness is a database constraint, not an application check." The app-level `ExistsByName` stays only as a UX affordance |
| Duplicate detection | Migration (Go migration, D-21 B) | CLI (`holomush` cobra command, D-22) | Detection must run in the release that adds the constraint; *resolution* needs a human and therefore cannot live in a migration |
| Tier-floor / viewer-twin / admin-section policies | ABAC engine (`internal/access/policy`) | — | §8.4/§10.4: the decision **MUST** be made by the default-deny engine, never in projection, facade, gateway, or a settings value |
| `admin_section:` / `viewer:` / `profile:` prefixes | `internal/access` (prefix family) | — | §8.4.1/§8.4.2/§10.4 all name `internal/access/prefix.go` explicitly |
| Lifecycle `status`, `last_active_at`, `normalized_name`, skeleton | **Database schema** | — | §7.1's column-vs-row rule: values the world model itself reads are columns |
| Web / gateway | **NONE — this phase ships no UI and no RPCs** | — | Phase boundary; `.claude/rules/gateway-boundary.md` forbids authorization decisions there regardless |

---

## P0 — The Unicode mechanism (the deliberately-deferred decision)

### What was actually verified, and how

Every claim in this section was checked **this session** against either the local Go module cache (downloaded from `proxy.golang.org` during this run) or the live Go module proxy / GitHub API. Nothing here is from training memory.

#### Finding 1 — `golang.org/x/text` has no confusables table. **Confirmed.**

```
$ ls $(go env GOMODCACHE)/golang.org/x/text@v0.40.0/unicode/
bidi  cldr  doc.go  norm  rangetable  runenames
$ ls $(go env GOMODCACHE)/golang.org/x/text@v0.40.0/secure/
bidirule  doc.go  precis
```

`[VERIFIED: local module cache, golang.org/x/text@v0.40.0, listed 2026-08-03]` — the module supplies NFKC/NFD (`unicode/norm`), rune filtering (`runes`), and PRECIS profiles (`secure/precis`). There is **no** confusables data and **no** UTS #39 support. `x/text v0.40.0` is already in `go.mod:178` as an **indirect** dependency; using it directly means promoting it, which is a one-line `go mod tidy` outcome and carries no new supply-chain surface.

#### Finding 2 — `golang.org/x/text` has no **script** package either. The CONTEXT's premise is wrong.

`02-CONTEXT.md` (Claude's Discretion) states x/text "covers NFKC (step 1) and script extensions for UTS #24 (Mechanism A)". The listing above shows no script package exists in x/text at all. `[VERIFIED: local module cache listing, as above]`

What *does* exist is Go's standard library:

```
unicode.Version = 15.0.0
len(unicode.Scripts) = 163
unicode.Scripts["Cyrillic"] present: true
```

`[VERIFIED: compiled and ran a 5-line program against the repo's Go toolchain (go.mod:3 → go 1.26.5), 2026-08-03]`

But `unicode.Scripts` is generated from **`Scripts.txt`** — the **Script (sc)** property — not from **`ScriptExtensions.txt`** (**scx**). `[CITED: go.googlesource.com/go src/unicode/maketables.go — "Scripts.txt has form: A673 ; Cyrillic # Po SLAVONIC ASTERISK ... installed := unicode.Scripts"]`

And UTS #39's Moderately Restrictive is defined over Script_Extensions:

> "The string qualifies as Highly Restrictive, or the string is covered by Latin and any one other Recommended script, except Cyrillic, Greek." … coverage is determined by **augmented script sets**, which incorporate Script_Extensions data with specific modifications for CJK writing systems.

`[CITED: https://www.unicode.org/reports/tr39/ §5.2, fetched 2026-08-03; the same fetch reports this revision corresponds to Unicode 17.0.0, released 2025-09-04]`

**No surveyed Go package exposes Script_Extensions.** `github.com/SCKelemen/unicode/v6/uax24` — the only candidate that advertises script support — generates from `Scripts.txt`:

```
$ head -2 .../SCKelemen/unicode/v6@v6.2.0/uax24/script_data.go
// Source: Unicode 17.0.0 Scripts.txt
$ rg -i 'scriptextensions|script_extensions|\bscx\b' .../uax24/
(no matches)
```

`[VERIFIED: local module cache, github.com/SCKelemen/unicode/v6@v6.2.0, inspected 2026-08-03]`

**What this means for §6.1.2 Mechanism A.** The SPEC's table is a closed enumeration of eight verdicts, not the full UTS #39 §5.2 algorithm. It is directly implementable from `unicode.Scripts` (`unicode.Is(unicode.Scripts["Cyrillic"], r)` etc.), treating `Common`/`Inherited` as script-neutral. Using **sc** instead of **scx** is an *approximation*, and the direction of the approximation is:

| Character shape | Under **scx** (faithful) | Under **sc** (stdlib) | Effect |
|---|---|---|---|
| sc=Common, scx={Hira Kana} — e.g. U+30FC prolonged sound mark | contributes {Hira, Kana} | neutral, excluded from the set | Fewer scripts in the set ⇒ **more permissive** |
| sc=Common, scx={Common} — ordinary punctuation | neutral | neutral | identical |
| sc = a real script | that script | that script | identical |

`[CITED: https://www.unicode.org/reports/tr24/ §2.9 and Table 2 — "30FC | Hira Kana | KATAKANA-HIRAGANA PROLONGED SOUND MARK"; "All code points not explicitly listed for Script_Extensions have as their value the corresponding Script property value"]`

So the sc approximation never *adds* a rejection the scx version would not make; it can only *miss* one. The missed case is narrow — a name whose only evidence of a second script is a Common-category character with a restricted scx set. Mechanism B (the skeleton) is the defense against whole-script confusables and is unaffected. **The residual risk is real but small, and it is closed-direction for the SPEC's three named rejections** (Latin+Cyrillic, Latin+Greek, Cyrillic+Greek), because Cyrillic and Greek letters carry real sc values.

Two viable dispositions, planner's call:
- **(a) Approximate.** Implement §6.1.2's table over `unicode.Scripts`, record the sc-vs-scx divergence in the §14 amendment pass alongside D-05/D-26. Zero new data, zero new dependency.
- **(b) Generate scx too.** Extend the confusables generator to also emit a Script_Extensions table from the same pinned Unicode version. Higher fidelity, one more generated file, and it removes the stdlib's Unicode-15-vs-data-17 version skew from Mechanism A.

**Recommendation: (a), with the divergence recorded.** (b) is a reasonable Phase-2 stretch if the generator is being built anyway — the marginal cost is one more parser over a file with the same format — but it should not be allowed to grow the phase.

#### Finding 3 — the third-party confusables survey

All five modules were downloaded into the module cache and their `Skeleton` implementations read. GitHub signals fetched via `gh api repos/<r>` on 2026-08-03.

| Module | Latest | Published | Stars | License | Unicode data | Skeleton correct? | Version at runtime? |
|---|---|---|---|---|---|---|---|
| `github.com/disciplinedware/go-confusables` | `v0.1.1` | 2026-02-18 | **1** | MIT | **17.0.0** (`data/confusables.json:2`) | **Yes** — NFD → map → NFD | **Yes** — `func (db *DB) UnicodeVersion() string` (`confusables.go:100-102`) |
| `github.com/eskriett/confusables` | `v0.0.0-20250910043846` | 2025-09-10 | 4 | MIT | **17.0.0** (`tables.go` header comment `// Version: 17.0.0`) | **Yes** — NFD → map → NFD (`confusables.go:299-313`) | **No** — comment only, no exported symbol |
| `github.com/mtibben/confusables` | `v0.0.0-20210201002637` | 2021-02-01 | 35 | BSD-3 | stale (≈Unicode 13 era) | Yes | No |
| `github.com/ergochat/confusables` | `v0.0.0-20201108231250` | 2020-11-08 | 1 | BSD-3 | stale | Yes, **plus `tweaks.go`** — deliberate deviations from the standard table (removes the `m → rn` mapping) | No |
| `github.com/SCKelemen/unicode/v6` (`/uts39`) | `v6.2.0` | 2026-05-20 | 6 | **NOASSERTION** (MIT text wrapped in emoji ASCII art, 37 lines) | 17.0.0 | **No** — `uts15.NFKD` → map → `strings.ToLower` → NFD-map fixed-point loop (`uts39/uts39.go:124-146`). That is not UTS #39 §4. `strings.ToLower` is not Unicode case folding. | No |

`[VERIFIED: proxy.golang.org @latest and @v/list for each module; module ZIPs downloaded and sources read; GitHub API repo metadata — all 2026-08-03]`

Three observations that decide the matter:

1. **`disciplinedware/go-confusables` is the only package that satisfies D-23's binding constraint out of the box** — and it is a repository **created 2026-02-18 and last pushed 2026-02-18**, with 1 star, 0 forks, one maintainer, and both of its releases cut the same day. Its code is correct; its provenance is not something a default-deny security repo should take a runtime dependency on for an identity gate. Per the package-legitimacy rule this is a **SUS** verdict — a `checkpoint:human-verify` would be mandatory before any install. (The `gsd-tools query package-legitimacy check` seam covers npm/PyPI/crates only, so this verdict is from the manual signals above, not from the seam.)
2. **`eskriett/confusables` is the best available third-party option** — MIT, correct algorithm, Unicode 17.0.0 data refreshed 2025-09-10, a `scripts/build-tables.go` regenerator, and a five-year history. But its Unicode version lives in a **comment** in `tables.go`, so recording it per-row means maintaining a repo-side constant whose agreement with the dependency is enforced only by a meta-test that greps a comment in a module-cache file. That is a fragile binding for a value stored in every character row.
3. **`SCKelemen/unicode` must be rejected outright** — wrong algorithm and a license file that `license-eye` (run by `task fmt` / `task license:check`) will not parse.

#### Finding 4 — what the UTS #39 skeleton actually is, at the current revision

> "For an input string X, define skeleton(X) = bidiSkeleton(LTR, X). The strings X and Y are confusable if and only if skeleton(X) = skeleton(Y)."

`[CITED: https://www.unicode.org/reports/tr39/ §4, fetched 2026-08-03]`

The classic core — NFD, then substitute each character's confusable prototype from `confusables.txt`, then NFD again — is what `eskriett` and `disciplinedware` both implement, and it is what `bidiSkeleton(LTR, ·)` reduces to for non-RTL input. **No surveyed Go package implements `bidiSkeleton`'s RTL handling.** For HoloMUSH this is an acceptable and honest gap: the comparison is always same-direction (stored skeleton vs. candidate skeleton), and RTL character names would need the bidi-aware form only to catch a reordering-based confusable. The planner should record this as a known limitation rather than claim full §4 conformance. `[ASSUMED: that same-direction comparison makes the LTR reduction adequate for this threat model — this is a judgement, not a spec statement]`

### Recommendation

**Generate the confusables table into the repo from a pinned, version-addressed `confusables.txt`.**

Shape:

| Piece | Where | Note |
|---|---|---|
| Generator | `cmd/internal/gen-confusables/main.go` | Follows the `cmd/internal/fsmdiagram` precedent (`internal/eventbus/crypto/dek/checkpoint_fsm.go:6` — `//go:generate go run github.com/holomush/holomush/cmd/internal/fsmdiagram`) |
| `//go:generate` directive | on the new leaf package's `doc.go` | Matches `internal/plugin/manifest.go:6`, `internal/access/policy/dsl/ast.go:8` |
| Taskfile entry | `generate:confusables`, wired beside `generate:schema` (`Taskfile.yaml:596-606`) and `generate:luabridge` (`:607`) | Those two already declare `sources:`/`generates:` for drift detection |
| Pinned URL | `https://www.unicode.org/Public/security/17.0.0/confusables.txt` | Version-addressed. `[CITED: the /latest/ form is what disciplinedware's `cmd/confusables-gen/main.go:19-20` uses; the versioned form `.../security/%s/confusables.txt` is its `versionedConfusablesURL` — use the versioned one]` |
| Emitted constant | `const UnicodeVersion = "17.0.0"` in the generated file | Parsed from the data file's own `# Version:` header — the same field `eskriett/confusables/scripts/build-tables.go:73` reads |
| Algorithm | ~20 lines, hand-written, using `x/text/unicode/norm` | See `## Code Examples` §B |

Why this over `eskriett`:

- **D-23 is satisfied structurally.** The stored per-row version comes from an exported constant in the same package that computes the skeleton. There is no gap between "the table we generated" and "the version we recorded" — they are emitted by one run of one generator.
- **No new runtime dependency for a security-critical gate.** The only import added is `golang.org/x/text/unicode/norm`, promoting an existing indirect dependency.
- **The upgrade story is a diff.** Bump the pinned version, `task generate`, the constant changes, and CI's generated-code drift check (`task lint` + the `sources:`/`generates:` declarations) makes the regeneration part of the same commit. D-23's per-row column then makes the stale subset a one-line query: `SELECT count(*) FROM characters WHERE name_skeleton_unicode_version <> '<new>'`.
- **It is the option the CONTEXT explicitly listed** ("generated-into-repo table") and it is the one the repo's existing codegen habits support best.

**Second choice, if the planner judges the generator too large for this phase:** `github.com/eskriett/confusables` pinned to `v0.0.0-20250910043846-220432c5bd73`, plus a repo-side `const ConfusablesUnicodeVersion = "17.0.0"` and a meta-test asserting that constant matches the `// Version:` line in the dependency's `tables.go`. Ship the D-23 column reading that constant. Accept that the binding is test-enforced rather than structural.

**Do not use:** `SCKelemen/unicode` (wrong algorithm, unparseable license), `ergochat/confusables` (stale data, deliberate table deviations), `mtibben/confusables` (stale). **Do not adopt `disciplinedware/go-confusables` without a `checkpoint:human-verify`** — its code is correct but its repository is one day old with one star.

### Migration/upgrade story when the Unicode version bumps

1. Generator's pinned version is edited; `task generate` rewrites the table and the `UnicodeVersion` constant.
2. Existing `characters` rows keep their old `name_skeleton` and old `name_skeleton_unicode_version`. **Nothing breaks**, because §6.1.2 requires the skeleton to be backed by a **non-unique** index and checked by query — not a constraint. A constraint whose meaning shifts under a dependency bump is the hazard the SPEC's non-unique rule exists to avoid.
3. The stale subset is queryable by the D-23 column. A recompute is a separate, resumable job — out of scope for Phase 2 but made possible by the column.
4. During the window where both versions coexist, a create/rename computes the *new* skeleton and compares it against a mix of old and new stored skeletons. That comparison is weaker than a fully-recomputed corpus, but it is never *wrong* in the fail-open direction for names whose skeleton did not change — which is the overwhelming majority. Worth one sentence in the phase's own documentation.

---

## P1 — The `seed:profile-public-read` exposure audit (success criterion 4)

### Verified schema

`entity_properties` (`internal/store/migrations/000001_baseline.sql:354-377`) — read this session, quoted verbatim:

```sql
CREATE TABLE entity_properties (
    id            TEXT PRIMARY KEY,
    parent_type   TEXT NOT NULL,
    parent_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    value         TEXT,
    owner         TEXT,
    visibility    TEXT NOT NULL DEFAULT 'public'
                  CHECK (visibility IN ('public', 'private', 'restricted', 'system', 'admin')),
    flags         JSONB DEFAULT '[]',
    visible_to    JSONB DEFAULT NULL,
    excluded_from JSONB DEFAULT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT entity_properties_parent_name_unique UNIQUE(parent_type, parent_id, name),
    ...
);
CREATE INDEX idx_entity_properties_parent ON entity_properties(parent_type, parent_id);
CREATE INDEX idx_properties_owner ON entity_properties(owner) WHERE owner IS NOT NULL;
```

`[VERIFIED: internal/store/migrations/000001_baseline.sql:354-377]`

`characters` (`internal/store/migrations/000001_baseline.sql:72-80`), verbatim:

```sql
CREATE TABLE characters (
    id          TEXT PRIMARY KEY,
    player_id   TEXT REFERENCES players(id),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    location_id TEXT,  -- nullable: character may not be in the world yet
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_characters_location ON characters(location_id);
```

`[VERIFIED: internal/store/migrations/000001_baseline.sql:72-80]` — plus three later additions, each read this session:
- `created_at` retyped to **BIGINT epoch-ns** by `internal/store/migrations/000042_world_timestamps_to_bigint.sql:29-31`
- `version INTEGER NOT NULL DEFAULT 1` added by `internal/store/migrations/000049_world_version_guard.sql:22`
- `preferences JSONB NOT NULL DEFAULT '{}'` added by `internal/store/migrations/000045_character_preferences.sql:7`

There is **no** `status`, **no** `last_active_at`, **no** normalized-name column, and **no** unique or `LOWER()` index on `name`. `[VERIFIED: the four files above, read 2026-08-03]`

> **Citation drift warning.** `01-SPEC.md` cites `000001_baseline.up.sql:67-76` for `characters` and `:350-371` for `entity_properties`. The goose adoption renamed every migration to a single `.sql` file and shifted line numbers. The current, correct citations are `000001_baseline.sql:72-80` and `:354-377`. **Every `internal/store/migrations/*.up.sql` citation in `01-SPEC.md` and in `02-CONTEXT.md` is stale in both filename and line number.** The planner MUST re-derive them rather than transcribe.

`parent_type` is a **bare string literal**, not a constant. `internal/world/property.go:18` documents the vocabulary inline:

```go
	ParentType   string // "character", "location", "object"
```

and `internal/world/postgres/parent_location_resolver.go:57` switches on `"location"` / `"character"` / `"object"`. There is no exported `ParentTypeCharacter`. `[VERIFIED: internal/world/property.go:18; internal/world/postgres/parent_location_resolver.go:33,57]`

### The audit query

Read-only. **Do not run this against a live database as part of planning** — it is an artifact the phase commits and an operator runs.

```sql
-- 02-AUDIT-profile-public-read.sql
-- PROFILE-11 / success criterion 4 exposure audit.
-- READ ONLY. Run against production before seed:profile-public-read merges.
-- Records what widening off-location character reads makes visible.

-- (1) Public character property rows, grouped by property name.
--     Every row here becomes readable off-location after the widening (D-10).
SELECT
    ep.name                              AS property_name,
    count(*)                             AS row_count,
    count(DISTINCT ep.parent_id)         AS distinct_characters,
    count(*) FILTER (WHERE ep.value IS NOT NULL AND ep.value <> '') AS nonempty_values
FROM entity_properties ep
WHERE ep.parent_type = 'character'
  AND ep.visibility  = 'public'
GROUP BY ep.name
ORDER BY row_count DESC, property_name;

-- (2) The same set, totalled — the single number criterion 4 records.
SELECT
    count(*)                     AS total_public_character_rows,
    count(DISTINCT ep.parent_id) AS characters_with_public_rows,
    count(DISTINCT ep.name)      AS distinct_property_names
FROM entity_properties ep
WHERE ep.parent_type = 'character'
  AND ep.visibility  = 'public';

-- (3) Which of those names are OUTSIDE 01-SPEC.md §8.6's enumeration.
--     D-10 widens to ALL public character rows; term A still denies any name
--     with no §8.6 floor, so this set is the "grid-widened but web-denied"
--     population. It is the set D-11's audit is really looking for.
SELECT
    ep.name                      AS unenumerated_property_name,
    count(*)                     AS row_count,
    count(DISTINCT ep.parent_id) AS distinct_characters
FROM entity_properties ep
WHERE ep.parent_type = 'character'
  AND ep.visibility  = 'public'
  AND ep.name NOT IN (
        'profile.pronouns', 'profile.rumors', 'profile.currently',
        'profile.rp_preferences', 'profile.timezone', 'profile.concept',
        'profile.species', 'profile.age', 'profile.faction',
        'profile.appearance', 'profile.personality', 'profile.biography',
        'profile.image.primary',
        'profile.image.gallery.00', 'profile.image.gallery.01',
        'profile.image.gallery.02', 'profile.image.gallery.03',
        'profile.image.gallery.04', 'profile.image.gallery.05',
        'profile.image.gallery.06', 'profile.image.gallery.07',
        'profile.image.gallery.08', 'profile.image.gallery.09'
      )
GROUP BY ep.name
ORDER BY row_count DESC, unenumerated_property_name;

-- (4) Character in-world descriptions. PROFILE-11's second half, and
--     01-SPEC.md §8.6 seeds this at the `anonymous` floor (D-13), so every
--     non-empty description below becomes anonymously readable on the web.
SELECT
    count(*)                                          AS total_characters,
    count(*) FILTER (WHERE c.description <> '')       AS nonempty_descriptions,
    max(length(c.description))                        AS longest_description_bytes
FROM characters c;

-- (5) Descriptions belonging to guest-provisioned characters, called out
--     because IDENT-09 flags the guest path as high-volume and automatic.
SELECT
    count(*) FILTER (WHERE p.is_guest AND c.description <> '') AS guest_nonempty_descriptions,
    count(*) FILTER (WHERE p.is_guest)                         AS guest_characters
FROM characters c
LEFT JOIN players p ON p.id = c.player_id;
```

Notes on the query, each grounded:

- `entity_properties` carries **no foreign key** to `characters`; the parent link is application-enforced. A test fixture in `test/integration/access/seed_policies_test.go:43-46` documents this explicitly ("entity_properties references characters, locations, and objects via parent_id (enforced at the application layer)"). So query (1) may legitimately count orphaned rows; that is information, not a bug. `[VERIFIED: test/integration/access/seed_policies_test.go:43-46]`
- Query (3)'s name list is transcribed from `01-SPEC.md:1548-1566` (§8.6's table). It excludes the two `characters`-column rows (`name`, in-world description) and *profile reachability*, which are not property names. **The planner MUST re-read §8.6 and diff this list** — a stale enumeration here silently under-reports.
- `players.is_guest` in query (5) comes from `internal/store/migrations/000002_player_is_guest.sql`. `[VERIFIED: file exists in internal/store/migrations/; column name not opened — treat as `[ASSUMED]` and confirm before running]`

### Where the result is recorded

Per D-12, the query is **committed** and its **result recorded** in the phase artifacts. Suggested placement:

| Artifact | Content |
|---|---|
| `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql` | The query above, verbatim and re-runnable |
| `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-RESULT.md` | Date run, database identifier, the five result sets pasted, and an explicit verdict line per D-11: for every unenumerated or surprising row, either "no change needed" or "row's `visibility` changed to `private` — never the policy narrowed" |

The recorded count is what discharges criterion 4. An unverifiable claim in a completion summary does not.

---

## P2 — Repo grounding

Every citation below was opened with `Read` or matched with `rg -n` **this session**, in the worktree at `/Volumes/Code/github.com/holomush/.worktrees/v013-milestone` on branch `fix/sandbox-restore-runbook-corrections`.

### P2.1 — The seed-policy mechanism: it is **Go code**, not migrations

Seed policies are Go struct literals returned by `SeedPolicies()` in `internal/access/policy/seed.go:36`. The struct (`seed.go:7-12`), verbatim:

```go
type SeedPolicy struct {
	Name        string
	Description string
	DSLText     string
	SeedVersion int
}
```

`[VERIFIED: internal/access/policy/seed.go:6-12,36]`

Installation is `policy.Bootstrap` (`internal/access/policy/bootstrap.go:30-57`), which loops the seeds and calls `bootstrapSeed`. The semantics matter to Phase 2:

| Situation | Behavior | Line |
|---|---|---|
| Policy absent | `createSeedPolicy` compiles the DSL, marshals the AST, inserts with `Source: "seed"`, `SeedVersion` | `bootstrap.go:75-77,104-141` |
| Policy exists with `Source != "seed"` | **Skip with a WARN** — an admin row wins | `bootstrap.go:80-88` |
| Policy exists as seed with a lower `SeedVersion` | `upgradeSeedPolicy` rewrites DSL + AST + effect and stamps a `ChangeNote` | `bootstrap.go:91-93,143-187` |
| Any error | **Fatal — aborts startup (ADR #92)** | `bootstrap.go:29,48-53` |

`[VERIFIED: internal/access/policy/bootstrap.go:29-102,104-141,143-187]`

**Adding a new seed policy is therefore an append to the `SeedPolicies()` slice — no migration.** Bumping an existing policy's behavior is an edit plus a `SeedVersion` increment. There is also `policy.UpdateSeed` (`bootstrap.go:189-251`) for "a migration-delivered fix" with compare-and-swap semantics that skips if an admin customized the DSL; `internal/store/migrations/000047_disable_unconditional_scene_write_seed.sql` and `000048_disable_unconditional_scene_read_seed.sql` are the in-tree precedent for disabling a shipped seed by migration.

**Analog to copy for a new policy:** the six `seed:property-*` entries at `internal/access/policy/seed.go:110-146`, and `seed:character-directory` at `:480-486` — the latter is the closest structural match for `seed:admin-section-access` because it is a **target-only** policy with no `when` clause. `[VERIFIED: internal/access/policy/seed.go:110-146,480-486]`

### P2.2 — `seed:player-character-colocation` and the two policies criterion 4 widens around

`internal/access/policy/seed.go:50-56`, verbatim:

```go
		{
			Name:        "seed:player-character-colocation",
			Description: "Characters can read co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is character) when { resource.character.location == principal.character.location };`,
			SeedVersion: 2,
		},
```

`internal/access/policy/seed.go:111-116`, verbatim:

```go
		{
			Name:        "seed:property-public-read",
			Description: "Public properties readable by co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "public" && principal.character.location == resource.property.parent_location };`,
			SeedVersion: 2,
		},
```

`[VERIFIED: internal/access/policy/seed.go:50-56,111-116]`

**These are two different resources, and PROFILE-11's two halves map one-to-one onto them:**

| PROFILE-11 half | Resource | Currently gated by |
|---|---|---|
| Existing public `entity_properties` rows | `property:<id>` | `seed:property-public-read` — colocation clause `principal.character.location == resource.property.parent_location` |
| `characters.description` (an intrinsic column, read via the character entity) | `character:<id>` | `seed:player-character-colocation` — colocation clause `resource.character.location == principal.character.location` |

Neither policy *denies*; both are permits that fail to match off-location, and the engine's default-deny (`combineDecisions` → `EffectDefaultDeny`, `internal/access/policy/engine.go:608-610`) supplies the denial. `[VERIFIED: internal/access/policy/engine.go:591-611]`

The full six-policy read family, for D-01's twinning:

| Policy | Line | Twin under D-01? |
|---|---|---|
| `seed:property-public-read` | `seed.go:111-116` | **Yes** (read) |
| `seed:property-private-read` | `:117-122` | **Yes** (read) |
| `seed:property-admin-read` | `:123-128` | **Yes** (read) |
| `seed:property-owner-write` | `:129-134` | **No** — D-01 forbids a viewer write permit |
| `seed:property-restricted-visible-to` | `:135-140` | **Yes** (read) |
| `seed:property-restricted-excluded` | `:141-146` | **Yes** — and note it is the family's only `forbid` |

`[VERIFIED: internal/access/policy/seed.go:110-146]`

> **Open question for the planner** (see `## Open Questions` Q1): is `seed:profile-public-read` one policy or two? D-11 says the **grid** path widens, which means the character-flavored side is touched; D-01 says term B needs **viewer**-flavored twins, which means the web side needs its own. The two paths have different principal types and different resources. The CONTEXT does not resolve this and it is a naming/shape decision, not a locked one.

### P2.3 — Making `admin_section:` real (EXT-07)

**The resource-prefix family**, `internal/access/prefix.go:21-34`, verbatim:

```go
// Resource prefix constants identify the type of entity being accessed.
const (
	ResourceCharacter = "character:"
	ResourceLocation  = "location:"
	ResourceObject    = "object:"
	ResourceCommand   = "command:"
	ResourceProperty  = "property:"
	ResourceStream    = "stream:"
	ResourceExit      = "exit:"
	ResourceScene     = "scene:"
	ResourceKV        = "kv:"
	// ResourceCharacterDirectory is the singleton character-directory resource (no instance id).
	ResourceCharacterDirectory = "character_directory:"
)
```

`knownPrefixes`, `internal/access/prefix.go:45-61`, verbatim:

```go
// knownPrefixes lists all valid entity reference prefixes for validation.
var knownPrefixes = []string{
	SubjectCharacter,
	SubjectPlugin,
	SubjectSession,
	SubjectPlayer,
	ResourceCharacter,
	ResourceLocation,
	ResourceObject,
	ResourceCommand,
	ResourceProperty,
	ResourceStream,
	ResourceExit,
	ResourceScene,
	ResourceKV,
	ResourceCharacterDirectory,
}
```

Subject prefixes, `internal/access/prefix.go:12-19`, verbatim:

```go
// Subject prefix constants identify the type of entity making a request.
const (
	SubjectCharacter = "character:"
	SubjectPlugin    = "plugin:"
	SubjectSystem    = "system"
	SubjectSession   = "session:"
	SubjectPlayer    = "player:"
)
```

`[VERIFIED: internal/access/prefix.go:12-19,21-34,45-61]`

Constructor precedent — `PlayerSubject` at `internal/access/prefix.go:89-94` panics on empty input, and the doc comment explains why: *"empty subject strings would silently bypass access control if returned as the bare prefix."* `CharacterResource` at `:102-107` is the resource-side analog. `[VERIFIED: internal/access/prefix.go:63-107]`

**Two facts that together decide EXT-07's cost:**

1. **`resource is admin_section` parses today, unregistered.** The DSL's `ResourceClause` (`internal/access/policy/dsl/ast.go:97-102`) is `'resource' ( ('is' @Ident) | (OpEq @String) )?` — an arbitrary identifier, matched by string equality against `parseEntityType(req.Resource)` (`internal/access/policy/engine.go:378-381,464,479,542-548`). The in-tree precedent for an **underscore-bearing resource type** is `seed:character-directory` (`internal/access/policy/seed.go:485`):

   ```go
			DSLText:     `permit(principal is character, action in ["list_character_directory"], resource is character_directory);`,
   ```

   `[VERIFIED: internal/access/policy/dsl/ast.go:97-102; internal/access/policy/engine.go:361-381,542-548; internal/access/policy/seed.go:485]`

2. **A policy may omit the `when` clause entirely and may leave `action`/`resource` unqualified.** `seed:admin-full-access` (`internal/access/policy/seed.go:105-110`) is `permit(principal is character, action, resource) when { "admin" in principal.character.roles };` — bare `action`, bare `resource`. `seed:character-directory` has no `when` at all. So `seed:admin-section-access` scoped by resource **type** (§10.4's EXT-07 requirement) is directly expressible and needs no per-id enumeration. `[VERIFIED: internal/access/policy/seed.go:105-110,480-486]`

**Phase 2's additions**, per §10.4 and §8.4.1/§8.4.2:
- `ResourceAdminSection = "admin_section:"` + `AdminSectionResource(id string) string` (panic on empty)
- `ResourceProfile = "profile:"` + `ProfileResource(...)` (§8.4.2's Phase-2 obligation)
- `SubjectViewer = "viewer:"` + `ViewerSubject(...)` (§8.4.1's Phase-2 obligation 2 and 3)
- All three appended to `knownPrefixes`, extending the known-prefix table test at `internal/access/prefix_test.go:588-600`

**None of `admin_section`, `viewer`, or `ViewerTier` exists in the tree today** — `rg -n 'admin_section|ViewerTier|viewer:' --type go -g '!*_test.go'` returns zero matches. `[VERIFIED: rg run 2026-08-03, empty result]`

**Where the seven section ids live.** §10.1 says the authoritative registry is **core-side** and the web nav is *derived* from it, mirroring the **shape** of `web/src/lib/nav/sections.ts:35-47`, not its location. The seven ids are enumerated in `01-SPEC.md:2109-2115`: `characters` (available), then `stats`, `players`, `moderation`, `audit`, `config`, `plugins` (all planned). `[CITED: .planning/phases/01-portal-spec/01-SPEC.md:2100-2118]`

### P2.4 — Characters, name validation, and the writers racing on it (IDENT-09, criterion 1 & 2)

**Today's normalization**, `internal/world/validation.go:107-126`. The doc comment and body, verbatim in substance: `strings.Fields` (trim + collapse) then per-word `ToLower` with the first rune `ToUpper`. `[VERIFIED: internal/world/validation.go:107-126]`

**Today's validation**, `internal/world/validation.go:69-105`, gating on UTF-8 validity, leading/trailing space, consecutive spaces, rune-count bounds, and:

```go
// characterNameRegex matches names with only Unicode letters and single spaces between words.
var characterNameRegex = regexp.MustCompile(`^[\p{L}]+( [\p{L}]+)*$`)
```

`[VERIFIED: internal/world/validation.go:59-60,69-105]`

**All five call sites of the two functions** (excluding tests):

| Site | Line | What it does |
|---|---|---|
| `internal/world/character.go:75` | — | `ValidateCharacterName(name)` inside character construction |
| `internal/world/character.go:105` | — | `ValidateCharacterName(c.Name)` inside `Validate()` |
| `internal/auth/character_service.go:105` | — | `world.NormalizeCharacterName(name)` |
| `internal/auth/character_service.go:108` | — | `world.ValidateCharacterName(normalizedName)` |

`[VERIFIED: rg -n 'NormalizeCharacterName|ValidateCharacterName|characterNameRegex' --type go, filtered to non-test files, 2026-08-03]`

**The check-then-insert race — three consumers, not two.** §6.1.3 and IDENT-09 both enumerate two writers. `rg -n 'ExistsByName' --type go` finds a **third** implementation:

| Participant | Location | Role |
|---|---|---|
| The shared query | `internal/bootstrap/setup/adapters.go:39-50` — `SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))` | Production read half |
| Writer 1 — player creation | `internal/auth/character_service.go:113` (`ExistsByName`), inside `createWithMaxAndBind` starting at `:103` | Normalizes first (`:105`), then checks |
| Writer 2 — guest provisioning | `internal/auth/guest_service.go:227`, inside `acquireUniqueName`'s retry loop (`:218-239`) | **Does NOT normalize** — checks `strings.ReplaceAll(name, "_", " ")` directly (`:226-227`) |
| Interface declarations | `internal/auth/character_service.go:23-24`, `internal/auth/guest_service.go:39` | Two separate interfaces, both with the same method |
| **Test-harness implementation** | `internal/testsupport/integrationtest/harness.go:1549` — `authCharRepoAdapter.ExistsByName`, with a note at `:1119` that "Production guest service relies on ExistsByName to retry-on-collision" | **A fourth site that must move in lockstep or integration tests will diverge from production** |

`[VERIFIED: internal/bootstrap/setup/adapters.go:38-50; internal/auth/character_service.go:23-24,103-122; internal/auth/guest_service.go:39,218-239; internal/testsupport/integrationtest/harness.go:1119,1549]`

Two consequences the planner must absorb:

1. **The guest path does not run the normalization pipeline.** Criterion 1 requires the confusable/block-list gate at *create*. If "create" means only `CharacterService`, the guest path is a hole. D-19 already calls the guest path out for the duplicate audit; the *gate* needs the same treatment.
2. **Replacing `ExistsByName` with a normalized-key lookup touches four sites**, one of them a test harness. A plan that lists two will not compile.

### P2.5 — The player-username rule (IDENT-08)

`internal/auth/player.go:28-31`, verbatim:

```go
// usernameRegex matches usernames that:
// - Start with a letter (a-z, A-Z)
// - Contain only letters, numbers, and underscores
var usernameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
```

Applied at `internal/auth/player.go:167` inside `ValidateUsername` (declared `:153`). Length bounds at `:24-25`:

```go
	MinUsernameLength = 3
	MaxUsernameLength = 30
```

`[VERIFIED: internal/auth/player.go:24-25,28-31,153,167]`

Uniqueness is a real database constraint — `username TEXT UNIQUE NOT NULL` at `internal/store/migrations/000001_baseline.sql:58`. `[VERIFIED: internal/store/migrations/000001_baseline.sql:56-68]`

§6.2 is explicit: **"Do not touch."** IDENT-08 is discharged by a regression guard, not by new validation. The test asserts the non-ASCII and leading-non-letter cases the regex rejects today are still rejected — so that a future "unification" of the two name policies fails loudly.

### P2.6 — Settings surface for the block list (IDENT-07 / D-14)

`RegisteredNamespaces`, `internal/settings/namespaces.go:15-20`, verbatim:

```go
var RegisteredNamespaces = []string{
	"core",
	"scenes",
	"channels",
	"auth",
}
```

`[VERIFIED: internal/settings/namespaces.go:11-20]` — `core` is already admitted, so D-14 needs **no** allowlist change. `ValidateNamespace` (`:32-50`) rejects a key with no dot, rejects the reserved `plugin` segment (`ReservedNamespace`, `:27`), and otherwise requires a registered first segment.

Write path — `internal/settings/game.go:147-159`, `SetStringSlice`, which namespace-validates then JSON-marshals into one `holomush_system_info` row. Read path — `StringSliceN` at `internal/settings/game.go:120`, which JSON-unmarshals and returns `(nil, false)` on a parse failure with a `DebugContext` log. `[VERIFIED: internal/settings/game.go:120,134,147]`

Storage — `internal/store/migrations/000001_baseline.sql:37-42`:

```sql
CREATE TABLE holomush_system_info (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

Both timestamp columns were retyped to **BIGINT epoch-ns** by `internal/store/migrations/000044_pregfo6_gap_timestamps_to_bigint.sql:74-85`. `[VERIFIED: internal/store/migrations/000001_baseline.sql:37-42; 000044_pregfo6_gap_timestamps_to_bigint.sql:69-85]`

The seed pattern D-14 reuses — `internal/store/migrations/000007_seed_scene_defaults.sql:6-18`, which `INSERT ... ON CONFLICT DO NOTHING` into `holomush_system_info` and deletes on Down. `[VERIFIED: internal/store/migrations/000007_seed_scene_defaults.sql:6-18]`

### P2.7 — The `Cache` + `Poller` pattern D-16 mirrors

`internal/access/policy/poller.go:19-37`, verbatim:

```go
// VersionQuerier queries the database for the latest policy version indicator.
// It returns both the latest timestamp and the total policy count so that
// deletions (which may not change MAX(updated_at)) are also detected.
type VersionQuerier interface {
	LatestPolicyVersion(ctx context.Context) (time.Time, int64, error)
}

// Reloadable is the subset of Cache that the poller needs.
type Reloadable interface {
	Reload(ctx context.Context) error
}

// PollerConfig configures the Poller.
type PollerConfig struct {
	Querier  VersionQuerier
	Reloader Reloadable
	Tracker  *lifecycle.HealthTracker
	Interval time.Duration // default: 10s
}
```

`NewPoller` requires all three collaborators and defaults `Interval` to `10 * time.Second` (`poller.go:96`). `Run` does an **immediate first poll** before entering the ticker loop (`poller.go:106`). `[VERIFIED: internal/access/policy/poller.go:19-37,85-116]`

Note the design detail in that first doc comment: the querier returns **timestamp AND count**, precisely because a deletion may not move `MAX(updated_at)`. The block list is a single row, so a count is degenerate — but the reason the pattern carries two signals is worth understanding before copying it (see `## Common Pitfalls` P-5).

### P2.8 — Migration mechanics: goose has landed, and D-21's shape is already proven in-tree

**D-20's gate is CLEAR.** `internal/store/migrations/` holds **45 single-file goose migrations**, `000001_baseline.sql` through `000053_sessions_location_index.sql`, plus a non-versioned `doc.go`. `internal/store/migrate.go:22-28` imports `github.com/pressly/goose/v3` and embeds with `//go:embed migrations/*.sql`. `[VERIFIED: ls internal/store/migrations/; internal/store/migrate.go:22-28]`

**The next free version is `000054`.**

**Go-migration wiring**, `internal/store/migrations/doc.go` — read in full this session. Three rules it fixes:

> "goose derives a Go migration's version from `runtime.Caller(1)`'s filename at the moment `goose.AddMigrationContext` runs. There is no other declaration and no override: a file named `000055_backfill_names.go` registers version 55 because of its name."

> "A `.go` migration is part of the chain only if its `init()` calls `goose.AddMigrationContext`. … A file that forgets its `init()` is therefore SILENTLY absent from the chain — every migration count is right, the schema matches the SQL-only corpus, and the suite is green."

> "`goose.AddMigrationNoTxContext` requires a `// goose-no-tx: <reason>` comment naming the specific statement that forbids a transaction … a backfill that must roll back and leave the schema at the prior version on collision (D-22) only does so inside a transaction."

`[VERIFIED: internal/store/migrations/doc.go, read 2026-08-03]`

The blank import that makes those `init()`s run is `internal/store/migrations_register.go:24`, guarded by `internal/store/migrations_register_test.go`. `[VERIFIED: internal/store/migrations_register.go]`

**D-21's A→B→C sequence already has a worked fixture.** `internal/store/migrate_gointerleave_integration_test.go` builds exactly it — and its constants document the ordering rationale in the same words D-21 uses. From `:70-88`, verbatim:

```go
// goInterleaveConstrainSQL is the fixture's migration 3: the D-21 (C) step.
//
// SET NOT NULL before CREATE UNIQUE INDEX is load-bearing, not stylistic.
// Postgres treats NULLs as distinct for uniqueness, so a unique index over an
// unbackfilled nullable column succeeds and enforces nothing — a green deploy
// with the guarantee silently absent. Putting SET NOT NULL first makes the Go
// backfill's execution a hard precondition of this migration: if version 2 had
// not run, every normalized_name would still be NULL and this ALTER would fail
// with "column contains null values" before the index was ever considered.
const goInterleaveConstrainSQL = `-- +goose Up
ALTER TABLE gointerleave_names ALTER COLUMN normalized_name SET NOT NULL;
CREATE UNIQUE INDEX gointerleave_names_normalized_key
    ON gointerleave_names (normalized_name);

-- +goose Down
DROP INDEX gointerleave_names_normalized_key;
ALTER TABLE gointerleave_names ALTER COLUMN normalized_name DROP NOT NULL;
`
```

And the (A) step at `:54-68` creates the column **nullable** and seeds rows that leave it NULL, so the backfill has real work and (C) has a precondition that can genuinely fail. The Go step at `:160-196` runs `Mode: goose.TransactionEnabled` with a `RunTx`, and its **down** returns `oops.Code("MIGRATION_FIXTURE_IRREVERSIBLE")` rather than silently succeeding. `[VERIFIED: internal/store/migrate_gointerleave_integration_test.go:39-68,70-88,160-210]`

One caution the fixture itself flags at `:206-208`: it passes `goose.WithDisableGlobalRegistry(true)` because it is a fixture. *"The production provider in internal/store/migrate.go MUST NOT set this: disabling the global registry there would make a Phase-2 Go migration invisible to the real chain."* `[VERIFIED: internal/store/migrate_gointerleave_integration_test.go:203-208]`

**One-shot job precedent (D-22's CLI command).** `cmd/holomush/` already carries a family of cobra subcommands built on this shape: `cmd_admin.go`, `cmd_admin_approve.go`, `cmd_admin_totp_reset.go`, `cmd_crypto_rekey.go`, `cmd_audit.go`, `cmd_plugin.go`, `cmd_plugin_validate.go`. `[VERIFIED: ls cmd/holomush/]` — note in particular that **`cmd_plugin.go` exists**, contradicting a stale claim in `.claude/rules/references/plan-review-learnings.md` that there is no `plugin` cobra group.

The sentinel-written-last bootstrap posture D-22 matches lives at `internal/bootstrap/setting.go` (cited in `02-CONTEXT.md` at `:103-112,156`) — **not opened this session; treat as `[ASSUMED]` and re-verify.**

### P2.9 — Invariants: D-07's entry is `INV-PRIVACY-11`

The SPEC's §13 invariants are **already registered** in `docs/architecture/invariants.yaml`, all `binding: pending` with `origin_spec: ".planning/phases/01-portal-spec/01-SPEC.md"`:

| Scope | Highest existing id | Phase-1-added |
|---|---|---|
| `INV-ACCESS` | **12** | 10, 11, 12 |
| `INV-PRIVACY` | **10** | 9, 10 |
| `INV-WORLD` | **7** | 5, 6, 7 |

`[VERIFIED: docs/architecture/invariants.yaml — id sweep, plus entries at :2156-2169 (INV-PRIVACY-9, -10) and :5065-5090 (INV-WORLD-5, -6, -7) read in full]`

So **D-07's new entry is `INV-PRIVACY-11`**, and it must be hand-registered: the orphan check in `test/meta/invariant_registry_test.go` walks only `docs/superpowers/specs/`, and a `.planning/` `origin_spec` is outside that walk root. `[CITED: .claude/rules/invariants.md — "A GSD milestone SPEC under `.planning/phases/**/*-SPEC.md` is likewise outside that walk root, so its `INV-<SCOPE>-N` ids are not auto-caught and MUST be hand-registered"]`

**Phase 2 binds exactly three:** INV-WORLD-5, INV-WORLD-6, and INV-PRIVACY-11. Everything else stays `pending` until Phase 4. Flipping `pending` → `bound` requires a `// Verifies: INV-<SCOPE>-N` annotation, an `asserted_by:` list, and `go run ./cmd/inv-render`.

### P2.10 — Project skills that apply

| Skill | Applies to |
|---|---|
| `.claude/skills/new-migration` | D-21's three migrations. Enforces six-digit numbering and warns that goose's own `create` uses **five** digits, which does not match this project |
| `.claude/skills/new-integration-test` | The real-Postgres criteria (2, 4, 5) — scaffolds `test/integration/<domain>/` with Ginkgo + testcontainers |
| `.claude/skills/holomush-dev` (`review-abac`) | D-05's routing obligation before merge |
| `.claude/skills/capture-adrs` | If the P0 mechanism choice is judged ADR-worthy |

`[VERIFIED: ls .claude/skills/; .claude/skills/new-migration/SKILL.md:1-30; .claude/skills/new-integration-test/SKILL.md:1-25]`

---

## Standard Stack

### Core

| Library | Version | Purpose | Why standard |
|---|---|---|---|
| `golang.org/x/text/unicode/norm` | `v0.40.0` (already in `go.mod:178` as **indirect**) | NFKC (§6.1.1 step 1); NFD for the skeleton | The only maintained Go NFKC/NFD implementation; already a transitive dependency, so promoting it adds no new supply-chain surface. `[VERIFIED: go.mod:178; module present in local cache]` |
| `golang.org/x/text/runes` + `golang.org/x/text/transform` | same module | `Cf` stripping (§6.1.1 step 2) via `runes.Remove(runes.In(unicode.Cf))` | Same module; composable with `norm` through `transform.Chain`. `[VERIFIED: package present in module cache listing]` |
| Go stdlib `unicode` | Go 1.26.5 → **Unicode 15.0.0** | `unicode.Cf` category; `unicode.Scripts` for §6.1.2 Mechanism A | No dependency. **Version skew against the 17.0.0 confusables data must be recorded.** `[VERIFIED: compiled `unicode.Version` = "15.0.0"]` |
| Go stdlib `strings.ToLower` — **NOT** for case folding | — | — | §6.1.1 step 4 says **Unicode full case folding**, which `strings.ToLower` is not. See `## Common Pitfalls` P-2. |
| `github.com/pressly/goose/v3` | as pinned in `go.mod` | Migrations, incl. the Go migration for D-21 (B) | Already adopted. `[VERIFIED: internal/store/migrate.go:22]` |
| Go stdlib `regexp` (RE2) | — | The IDENT-07 block list | CONTEXT records the rationale: linear time, no backtracking, **no ReDoS**. Do not add backtracking defenses. |

### Supporting — the confusables table

| Approach | Purpose | When to use |
|---|---|---|
| **Generate into repo** from `https://www.unicode.org/Public/security/17.0.0/confusables.txt` | UTS #39 skeleton data + an exported `UnicodeVersion` constant | **Recommended.** See `## P0`. |
| `github.com/eskriett/confusables` `v0.0.0-20250910043846-220432c5bd73` | Same, as a dependency | Fallback if the generator is judged too large. MIT, Unicode 17.0.0, correct algorithm; **no runtime version accessor** |

### Alternatives Considered

| Instead of | Could use | Tradeoff |
|---|---|---|
| Generated-in-repo table | `github.com/disciplinedware/go-confusables v0.1.1` | Nicest API and the only runtime `UnicodeVersion()`. **Repository created and last pushed 2026-02-18; 1 star, 0 forks, one maintainer, both releases same day → SUS.** Requires `checkpoint:human-verify` |
| Generated-in-repo table | `github.com/SCKelemen/unicode/v6/uts39` | Full UTS #39 surface (restriction levels, mixed-script). **Rejected:** skeleton is `NFKD → map → strings.ToLower → NFD-loop`, not UTS #39 §4; no exported version; license is MIT text inside 37 lines of emoji art (`NOASSERTION` per GitHub) which `license-eye` will not parse; `uax24` is `Scripts.txt` only, so it does not supply Script_Extensions either |
| Generated-in-repo table | `github.com/ergochat/confusables` | Battle-tested in an IRC daemon. **Rejected:** last push 2020-11-08, and `tweaks.go` applies deliberate deviations from the standard table (drops `m → rn`), so it is not the standard skeleton |
| stdlib `unicode.Scripts` for Mechanism A | Generate a Script_Extensions table | Higher fidelity to UTS #39 §5.2 and removes the Unicode-15-vs-17 skew, at the cost of a second generated file. See `## P0` Finding 2 disposition (b) |
| `golang.org/x/text/secure/precis` | — | PRECIS (RFC 8264/8265/8266) does NFC, width mapping, and the bidi rule. It answers a *different* question and does not supply confusables or script sets. Not applicable |

**Installation** (recommended path):

```bash
go get golang.org/x/text@v0.40.0   # promotes the existing indirect dep to direct
# No other module added. The confusables table is generated into the repo.
```

## Package Legitimacy Audit

Go modules. The `gsd-tools query package-legitimacy check` seam supports `npm|pypi|crates` only, so these verdicts come from manual signals: `proxy.golang.org` `@latest`/`@v/list`, the downloaded module source, and `gh api repos/<owner>/<repo>` — all 2026-08-03.

| Package | Registry | Age | Adoption | Source repo | Verdict | Disposition |
|---|---|---|---|---|---|---|
| `golang.org/x/text` | Go proxy | 2014→ | ubiquitous; already indirect in `go.mod:178` | go.googlesource.com/text | **OK** | Approved — promote to direct |
| `github.com/pressly/goose/v3` | Go proxy | long-lived | already a direct dependency | github.com/pressly/goose | **OK** | Approved — already in use |
| `github.com/eskriett/confusables` | Go proxy | repo created 2020-01-09; last push 2025-09-10 | **4 stars**, 5 forks, 0 open issues | github.com/eskriett/confusables (MIT) | **SUS** — very low adoption, single maintainer | **Fallback only.** If adopted, the planner MUST insert a `checkpoint:human-verify` before `go get` |
| `github.com/disciplinedware/go-confusables` | Go proxy | **repo created 2026-02-18, last push 2026-02-18** | **1 star**, 0 forks; v0.1.0 and v0.1.1 both cut the same day | github.com/disciplinedware/go-confusables (MIT) | **SUS** | **Not recommended.** Code is correct; provenance is not adequate for an identity gate. `checkpoint:human-verify` mandatory if used |
| `github.com/SCKelemen/unicode/v6` | Go proxy | created 2025-12-14, active | 6 stars, 4 open issues | github.com/SCKelemen/unicode (**NOASSERTION**) | **REMOVED** | **REMOVED** — wrong skeleton algorithm; license unparseable by `license-eye` |
| `github.com/ergochat/confusables` | Go proxy | 2019 → last push 2020-11-08 | 1 star | github.com/ergochat/confusables (BSD-3) | **REMOVED** | **REMOVED** — stale data, non-standard table tweaks |
| `github.com/mtibben/confusables` | Go proxy | 2013 → last push 2021-02-01 | 35 stars | github.com/mtibben/confusables (BSD-3) | **REMOVED** | **REMOVED** — superseded, stale Unicode data |

**Packages removed due to a REMOVED verdict:** `github.com/SCKelemen/unicode/v6`, `github.com/ergochat/confusables`, `github.com/mtibben/confusables`
**Packages flagged as suspicious [SUS]:** `github.com/eskriett/confusables`, `github.com/disciplinedware/go-confusables` — the planner MUST insert a `checkpoint:human-verify` before installing either. **The recommended path installs neither.**

## Architecture Patterns

### System Architecture Diagram

```text
┌──────────────── CREATE / RENAME (criterion 1) ────────────────────────────────┐
│                                                                               │
│  submitted name (any script)                                                  │
│        │                                                                      │
│        ▼                                                                      │
│  ┌──────────────────────── §6.1.1 pipeline (ORDER IS SPEC) ───────────────┐   │
│  │  1. NFKC (x/text/unicode/norm)                                         │   │
│  │  2. strip Cf (x/text/runes + unicode.Cf)                               │   │
│  │  3. whitespace canonicalize (trim, collapse runs → U+0020)             │   │
│  │        └─► DISPLAY NAME ─────────────────────────┐                     │   │
│  │  4. Unicode full case fold                       │                     │   │
│  │        └─► UNIQUENESS KEY (normalized_name)      │                     │   │
│  └──────────────────────────────────────────────────┼─────────────────────┘   │
│        │                                            │                         │
│        │  normalized form empty?  ──► REJECT        │                         │
│        ▼                                            │                         │
│  ┌── §6.1.4 block list ──┐   ┌── §6.1.2 A: mixed script ──┐                    │
│  │ compiled RE2 snapshot │   │ script set over unicode.   │                    │
│  │ (Cache + Poller,      │   │ Scripts, Common/Inherited  │                    │
│  │  holomush_system_info)│   │ excluded → 8-row verdict   │                    │
│  └───────────┬───────────┘   └────────────┬───────────────┘                    │
│              │ match → REJECT             │ reject-combo → REJECT              │
│              └──────────────┬─────────────┘                                    │
│                             ▼                                                  │
│              ┌── §6.1.2 B: skeleton ──────────────────────┐                    │
│              │ NFD → confusable map → NFD                 │                    │
│              │ SELECT ... WHERE name_skeleton = $1        │  non-unique idx    │
│              │   hit → REJECT (MUST NOT name the row)     │                    │
│              └────────────────────┬───────────────────────┘                    │
│                                   ▼                                            │
│              ┌── §6.1.3 uniqueness: THE DATABASE ─────────┐                    │
│              │ INSERT/UPDATE → UNIQUE(normalized_name)    │  ← the real gate   │
│              │ 23505 → NAME_TAKEN                          │  (criterion 2)     │
│              └─────────────────────────────────────────────┘                    │
└───────────────────────────────────────────────────────────────────────────────┘

┌──────────────── PROFILE READ (vocabulary only in Phase 2) ────────────────────┐
│  viewer:<rung>[:ulid]                                                         │
│        │                                                                      │
│        ▼  ViewerTierProvider.ResolveSubject → {tier, [player_id], has_player_id}
│  ┌── reachability (§8.4.2) ──┐   Evaluate(viewer, read, profile:<char>)        │
│  │  DENY → §8.7 not-found    │   ← evaluated FIRST, independently              │
│  └────────────┬──────────────┘                                                 │
│               ▼ PERMIT                                                         │
│  ┌── TERM A: tier floor ──┐          ┌── TERM B: row-keyed (D-01) ──┐          │
│  │ 3 policies, one per    │          │ viewer-flavored twins of the │          │
│  │ rung; literal name     │   AND    │ 5 read-side seed:property-*  │          │
│  │ list; set-membership   │  ◄────►  │ policies (NO owner-write twin)│         │
│  │ clearing (§8.2.1)      │  caller  │ visibility/visible_to/       │          │
│  │ name in no list = DENY │  ANDs    │ excluded_from semantics      │          │
│  └────────────────────────┘          └──────────────────────────────┘          │
│      Two Evaluate() calls. NEVER one evaluation with two permits —             │
│      combineDecisions OR-combines permits (engine.go:591-611).                 │
└───────────────────────────────────────────────────────────────────────────────┘

┌──────────────── ADMIN SECTION GATE (vocabulary only in Phase 2) ──────────────┐
│  caller ──► shared helper (AssertOperatorAdmin shape)                          │
│              │                                                                 │
│              ▼ 1. Evaluate(subject, read|write, admin_section:<id>)            │
│                  seed:admin-section-access — scoped by resource TYPE (EXT-07)  │
│              │                                                                 │
│         DENY ├──────────────────────────► DENY_ADMIN_SECTION  (always, D-06)   │
│              │                                                                 │
│       PERMIT ▼ 2. registry lookup (seven ids)                                  │
│                  missing ──► DENY_ADMIN_SECTION_UNREGISTERED                   │
│                  planned ──► NOT_IMPLEMENTED (§10.3, after the gate)           │
│                  available ► handler                                           │
│      Gate-then-distinguish closes the registry-enumeration oracle.             │
└───────────────────────────────────────────────────────────────────────────────┘

┌──────────────── D-21 MIGRATION CHAIN (one release) ───────────────────────────┐
│  000054 (SQL, A) ──► 000055 (Go, B) ──► 000056 (SQL, C)                        │
│  status/last_active_at/  backfill normalized_    SET NOT NULL                  │
│  normalized_name NULL/   name + skeleton;        ──THEN──                      │
│  skeleton + NON-UNIQUE   detect collisions →     CREATE UNIQUE INDEX           │
│  index; block-list seed  error, tx rollback      (order load-bearing)          │
│                          (D-22) → CLI resolve                                  │
└───────────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | File (new or modified) | Responsibility |
|---|---|---|
| Name pipeline | `internal/world/validation.go` (**replace** `NormalizeCharacterName` per §6.1.5) | NFKC → `Cf` strip → whitespace → case fold; returns display name **and** uniqueness key |
| Confusables data + skeleton | new leaf package + generated table | `Skeleton(string) string`, `const UnicodeVersion` |
| Mixed-script check | same leaf package | §6.1.2's eight-row verdict table over `unicode.Scripts` |
| Block list | new package mirroring `internal/access/policy/{cache,poller}.go` | Compiled RE2 snapshot; boot-fatal on a bad pattern; poll-refresh |
| Prefix vocabulary | `internal/access/prefix.go` | `+admin_section:`, `+profile:`, `+viewer:` and three constructors; all appended to `knownPrefixes` |
| Viewer attributes | `internal/access/policy/attribute/viewer.go` (new) | `Namespace() == "viewer"`; `{tier, [player_id], has_player_id}`; registered in `internal/access/setup/setup.go` beside the eight existing `resolver.RegisterProvider` calls (`:150,172,199,223,243,249,256,262`) |
| Seed policies | `internal/access/policy/seed.go` | Three tier-floor policies, five viewer read twins, `seed:profile-reachable`, `seed:admin-section-access`, `seed:profile-public-read` |
| Admin registry | new core-side package | Seven ids + mandatory authorization descriptor; boot validator (D-09) |
| Migrations | `internal/store/migrations/000054..000056` | D-21 A/B/C |
| Duplicate-resolution CLI | `cmd/holomush/cmd_*.go` | D-22 — routes a replacement through the full pipeline and block list |

### Pattern 1: seed-policy behavior test against the **real** engine

```go
// Source: internal/access/policy/seed_smoke_test.go:18-31 (verbatim)

// createSeedEngine builds an Engine loaded with ALL seed policies and the
// given attribute providers. This exercises the full evaluation pipeline:
// target matching → attribute resolution → condition evaluation → deny-overrides.
func createSeedEngine(t *testing.T, providers []attribute.AttributeProvider) *Engine {
	t.Helper()

	seeds := SeedPolicies()
	dslTexts := make([]string, len(seeds))
	for i, s := range seeds {
		dslTexts[i] = s.DSLText
	}

	return createTestEngineWithPolicies(t, dslTexts, providers)
}
```

**When to use:** every ABAC assertion in this phase — criterion 5, D-03's set-membership test, D-04's additive-permit regression, and the §8.2.1 fourth-rung test. Loading **all** seeds is what makes the paired positive control meaningful: a denial that survives the whole corpus is a real denial.

The provider shape to copy for `ViewerTierProvider`'s test double is `characterProvider` at `internal/access/policy/seed_smoke_test.go:35-52`, which supplies a `types.NamespaceSchema`.

### Pattern 2: goose Go migration with a rollback-on-collision failure (D-21 B / D-22)

```go
// Source: internal/store/migrate_gointerleave_integration_test.go:160-196
//         (fixture form — the real migration registers via init() instead)

backfill := goose.NewGoMigration(
	goInterleaveGoVersion,
	&goose.GoFunc{
		// D-10/D-22: transactional. The whole point of the Go step being
		// a migration is that a failure rolls back and leaves the schema
		// at the prior version.
		Mode: goose.TransactionEnabled,
		RunTx: func(ctx context.Context, tx *sql.Tx) error {
			rows, queryErr := tx.QueryContext(ctx, `SELECT id, name FROM gointerleave_names`)
			// ... scan, then per-row UPDATE inside the same tx ...
		},
	},
	&goose.GoFunc{
		Mode: goose.TransactionEnabled,
		RunTx: func(_ context.Context, _ *sql.Tx) error {
			return oops.Code(goInterleaveIrreversibleCode).
				Errorf("migration %d normalized names in place; the pre-normalization values are not recoverable",
					goInterleaveGoVersion)
		},
	},
)
```

**In the real chain**, the registration is an `init()` in `internal/store/migrations/000055_<name>.go` calling `goose.AddMigrationContext` — the filename *is* the version declaration, and a missing `init()` makes the migration silently vanish. `[VERIFIED: internal/store/migrations/doc.go]`

### Pattern 3: `ON CONFLICT DO NOTHING` settings seed (D-14)

`internal/store/migrations/000007_seed_scene_defaults.sql:6-18` is the template: an `INSERT INTO holomush_system_info (key, value) ... ON CONFLICT DO NOTHING` in the Up and a matching `DELETE FROM holomush_system_info WHERE key = ...` in the Down, so an operator override survives re-application.

### Anti-Patterns to Avoid

- **A tier-floor permit inside the same `Evaluate` as the row-keyed family.** `combineDecisions` (`internal/access/policy/engine.go:591-611`) returns the first satisfied forbid, else the first satisfied permit. Two permits in one evaluation are a **disjunction**. §8.5.1 requires a **conjunction**, and the caller must AND two separate `Evaluate` calls. Getting this wrong publishes `visibility='private'` rows to every viewer that clears the name's floor.
- **Ordinal comparison on the tier token.** `compareStrings` (`internal/access/policy/dsl/evaluator.go:185-201`) is Go byte order. The three v0.13 tokens happen to sort in ladder order; a fourth rung (`spectator`, `unverified`, `visitor` all sort above `player`) would silently receive the highest clearance. §8.2.1 forbids `>=` and forbids a numeric rank.
- **A glob or prefix over `profile.*` in the tier-floor family.** §8.6's totality rule: names matched as **whole strings**; a name in no row is **denied, not defaulted**.
- **Making the skeleton a `UNIQUE` constraint.** §6.1.2: the skeleton is backed by a **non-unique** index and checked by query, because the confusables table changes between Unicode releases and a constraint whose meaning shifts under a dependency bump retroactively invalidates existing rows.
- **Naming the colliding character in the confusable rejection message.** The roadmap sketch finding is explicit: the message **MUST NOT** name it. That would turn the gate into a name-enumeration oracle.
- **Resolving duplicates with hand-written SQL.** D-22: a direct `UPDATE` bypasses both the pipeline and the block list and can seat a name the index about to be created would reject.
- **Writing custom structure into `.planning/ROADMAP.md` or `STATE.md`.** `.claude/rules/planning-artifacts.md`: a version-bearing or ✅-bearing `###` heading truncates `extractCurrentMilestone`'s scope silently.

## Don't Hand-Roll

| Problem | Don't build | Use instead | Why |
|---|---|---|---|
| NFKC / NFD normalization | A composition table | `golang.org/x/text/unicode/norm` | Canonical ordering and Hangul composition are subtle; the module is already an indirect dependency |
| `Cf` stripping | A hand-listed codepoint set | `runes.Remove(runes.In(unicode.Cf))` | The `Cf` category is 160+ codepoints and grows; a hand list expires silently |
| Confusable mapping data | Hand-curated homoglyph pairs | Generated from Unicode's `confusables.txt` (~6,500 mappings) | A hand list is guaranteed incomplete and its gaps are exactly the attack surface |
| Uniqueness enforcement | An application-level `ExistsByName` check | A `UNIQUE` index | §6.1.3 is explicit; the current check-then-insert has three consumers already racing |
| DB-backed config refresh | A bespoke invalidation channel | Mirror `policy.Cache` + `policy.Poller` | Multi-replica with no in-process writer; in-process invalidation would only fix the replica that served the write |
| Compiled-regex snapshot swap | Ad-hoc `sync.Mutex` around a slice | The `Cache` read-barrier pattern | Already tested, including the reload-failure-does-not-partially-update property |
| Backfill inside a migration | A `DO $$ ... $$` block | A goose **Go** migration (D-21 B) | `.claude/rules/database-migrations.md` forbids long-running backfills in migrations and forbids functions/triggers |
| ReDoS defense on operator regexes | Timeouts, complexity analysis | Nothing — Go's `regexp` is RE2 | Linear time, no backtracking. The CONTEXT says explicitly: do not add this machinery |
| Ordering the tier ladder | A rank map or `>=` | Explicit set membership per floor | §8.2.1; a second source of truth for the ladder fails open |

**Key insight:** every one of these has a shipped in-repo precedent or a shipped upstream implementation. This phase's genuinely novel work is *exactly two things* — the confusables data pipeline and the `viewer:` subject namespace. Everything else is transposition.

## Common Pitfalls

### P-1: `01-SPEC.md`'s migration citations are stale in filename **and** line number
**What goes wrong:** the SPEC and CONTEXT cite `internal/store/migrations/000001_baseline.up.sql:67-76`. That file does not exist — goose adoption collapsed every `.up.sql`/`.down.sql` pair into one `.sql`, shifting every line.
**Why:** Phase 01.1 landed after the SPEC was written.
**How to avoid:** re-derive every `internal/store/migrations/*` citation. Correct values: `characters` at `000001_baseline.sql:72-80`; `entity_properties` at `:354-377`; `holomush_system_info` at `:37-42`; the enum-by-CHECK precedents at `:262` (`entity_properties.visibility`) and in the `access_policies` block.
**Warning signs:** a plan step that says "Edit `000001_baseline.up.sql`".

### P-2: `strings.ToLower` is not Unicode full case folding
**What goes wrong:** §6.1.1 step 4 says **Unicode full case folding** for the uniqueness key. `strings.ToLower` is lowercasing, not folding — it does not map `ß` → `ss` and mishandles the Turkish dotted/dotless `I`.
**Why:** the two look interchangeable in ASCII and diverge only in the cases that matter.
**How to avoid:** use `golang.org/x/text/cases.Fold()`. Note this is precisely the bug in `SCKelemen/unicode`'s skeleton (`uts39/uts39.go:130`).
**Warning signs:** a `strings.ToLower` call anywhere in the pipeline; a test corpus with no non-ASCII case pairs.

### P-3: The **guest** path never runs the normalization pipeline
**What goes wrong:** `internal/auth/guest_service.go:226-227` builds `charName` by `strings.ReplaceAll(name, "_", " ")` and calls `ExistsByName` directly — no `NormalizeCharacterName`, no `ValidateCharacterName`. A gate installed only in `CharacterService.createWithMaxAndBind` leaves this path open.
**Why:** guest names are machine-generated, so the pipeline looked unnecessary.
**How to avoid:** enumerate every character-creating path before writing the gate. There are two production writers plus a test-harness adapter at `internal/testsupport/integrationtest/harness.go:1549`.
**Warning signs:** a plan whose "create" task lists only `character_service.go`.

### P-4: A `UNIQUE` index over a nullable, unbackfilled column silently enforces nothing
**What goes wrong:** Postgres treats `NULL`s as distinct for uniqueness. `CREATE UNIQUE INDEX` over a column that is still all-NULL **succeeds**, and IDENT-09's guarantee is absent behind a green deploy.
**Why:** the failure produces no error at any point.
**How to avoid:** D-21 (C)'s ordering — `SET NOT NULL` **first**, which makes the Go backfill a hard precondition. The in-repo fixture documents this in its own words at `internal/store/migrate_gointerleave_integration_test.go:70-88`.
**Warning signs:** a migration whose Up is `CREATE UNIQUE INDEX` with no preceding `SET NOT NULL`.

### P-5: `holomush_system_info.updated_at` is **not** bumped by a direct-SQL edit
**What goes wrong:** D-16 polls `updated_at` for the block-list key. `PostgresEventStore.SetSystemInfo` (`internal/store/postgres.go:85-94`) does bump it:

```sql
INSERT INTO holomush_system_info (key, value) VALUES ($1, $2)
 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
```

But `.claude/rules/database-migrations.md` forbids triggers, so **nothing else** maintains the column. D-16 itself states that in v0.13 "there is no in-process writer to hook (§8.12 ships no editing surface, so edits are direct SQL)". A plain `UPDATE holomush_system_info SET value = '...' WHERE key = '...'` leaves `updated_at` unchanged, and the poller never notices.
**Why:** the write path that *does* maintain the column is the one v0.13 does not use for this key.
**How to avoid:** either (a) poll a **content hash** of the value rather than `updated_at`, (b) poll `(updated_at, md5(value))` — mirroring the poller's own `(timestamp, count)` two-signal design at `internal/access/policy/poller.go:19-24`, or (c) document that the operator must set `updated_at` and enforce it in the runbook. **(b) is the shape closest to the pattern being mirrored.**
**Warning signs:** a plan that says "poll `updated_at`" with no note about who writes it.
`[VERIFIED: internal/store/postgres.go:85-94; internal/access/policy/poller.go:19-24]`

### P-6: `policytest` engines are fakes and cannot prove a seed policy
**What goes wrong:** `policytest.GrantEngine`, `AllowAllEngine`, `DenyAllEngine` (`internal/access/policy/policytest/helpers.go:19,29,45`) return canned decisions. A criterion-5 test written against them asserts the fake's behavior, not the policy's.
**Why:** they are the right tool for testing *callers* of the engine, and the wrong tool for testing the *policies*.
**How to avoid:** use `createSeedEngine` (`internal/access/policy/seed_smoke_test.go:21-31`) for unit-tier policy behavior, and `test/integration/access/seed_policies_test.go` for the DB-backed tier.
**Warning signs:** `policytest` imported in a test whose name contains a `seed:` policy id.

### P-7: An unregistered attribute namespace default-denies **silently** in production while unit tests stay green
**What goes wrong:** §8.4.1's Phase-2 obligation 1 — if `ViewerTierProvider` is not registered in `BuildABACStack`, `principal.viewer.tier` is simply absent from the bag, every condition referencing it evaluates false, and the whole family default-denies. A unit test that stubs the bag never sees it.
**Why:** missing-attribute-is-false is the correct fail-closed semantic, and it makes the misconfiguration invisible.
**How to avoid:** register beside the eight existing `resolver.RegisterProvider` calls in `internal/access/setup/setup.go` (`:150,172,199,223,243,249,256,262`), and confirm `warnOnMissingSeedCoverage` (`internal/access/setup/seed_coverage.go:83`) does **not** WARN for `viewer`. There is an integration test for exactly this: `internal/access/setup/buildabacstack_seed_coverage_integ_test.go`.
**Warning signs:** a plan with a `ViewerTierProvider` task and no `setup.go` task.

### P-8: `ViewerTierProvider` must declare every key in `Schema()` or the key is dropped
**What goes wrong:** the resolver drops and counts (`abac_rejected_provider_attributes_total`) any attribute whose key is not in the namespace schema. An undeclared `tier` is silently absent rather than loudly wrong.
**How to avoid:** declare `tier`, `player_id`, `has_player_id` in `Schema()`, following `PropertyProvider.Schema()` (`internal/access/policy/attribute/property.go:197-211`).

### P-9: `player_id` must be **omitted** on the anonymous rung, never `""`
**What goes wrong:** an empty-string sentinel satisfies `"" == ""` against any other unresolved peer attribute, creating a fail-open match in a default-deny system.
**How to avoid:** `.claude/rules/abac-providers.md` / ADR holomush-ti1b. The in-tree reference is `PropertyProvider`'s `owner` handling (`internal/access/policy/attribute/property.go:104-118`), which sets `attrs["owner"]` only inside the non-nil branch and emits `has_owner` on **both** branches.

### P-10: A Go migration that forgets its `init()` vanishes from the chain with no error
**What goes wrong:** goose learns about a Go migration only when `goose.AddMigrationContext` executes, and that call lives in an `init()`. `//go:embed migrations/*.sql` globs `.sql` only, so goose's own unregistered-Go guard cannot fire. Every version count is correct, the schema matches the SQL-only corpus, and the suite is green — the Go step simply never happened.
**How to avoid:** `internal/store/migrations_register_test.go` guards the blank import; the doc.go rules are mandatory reading before writing 000055.

### P-11: `entity_properties` has no FK to `characters`
**What goes wrong:** the parent link is application-enforced. The audit query may count orphaned rows, and a test that deletes characters without deleting properties leaves orphans behind.
**How to avoid:** the cleanup ordering in `test/integration/access/seed_policies_test.go:34-50` is the reference — `player_character_bindings`, then `objects`, then `entity_properties`, then `characters`, then `players`, each for a documented reason.

### P-12: Unicode version skew between stdlib (15.0.0) and confusables data (17.0.0)
**What goes wrong:** Mechanism A runs on Unicode 15 script data; Mechanism B runs on Unicode 17 confusables data. A codepoint assigned in 16 or 17 has no script in `unicode.Scripts` and lands in the "no script" bucket.
**How to avoid:** decide and document the treatment of a rune with no script assignment — **reject** is the fail-closed choice and is consistent with `characterNameRegex`'s existing letters-only shape. Record the skew in the amendment pass.

## Code Examples

### §A — the §6.1.1 pipeline (illustrative; the planner owns the final shape)

```go
// Steps 1-3 produce the display name; step 4 additionally produces the
// uniqueness key. Order is part of the specification (01-SPEC.md §6.1.1).
//
// Imports: golang.org/x/text/unicode/norm, golang.org/x/text/runes,
//          golang.org/x/text/transform, golang.org/x/text/cases, unicode, strings

// stripCf removes every Unicode general-category Cf codepoint. These render as
// nothing, so they are pure padding for producing two distinct strings that
// look identical (01-SPEC.md §6.1.1 step 2).
var nameTransform = transform.Chain(
	norm.NFKC,                               // step 1
	runes.Remove(runes.In(unicode.Cf)),      // step 2
)

// step 3: strings.Fields + Join is what the CURRENT implementation already
// does for whitespace (internal/world/validation.go:114-126) — reuse that half
// and drop its per-word title-casing, which §6.1.5 says conflates the display
// name with the normalized form.
```

`[ASSUMED: this composition — it is a straightforward reading of §6.1.1 against the x/text API, but it was not compiled this session]`

### §B — the UTS #39 skeleton, matching the spec's core

```go
// Source shape: github.com/eskriett/confusables/confusables.go:299-313 and
//               github.com/disciplinedware/go-confusables/confusables.go:142-161
//               (both read from the local module cache 2026-08-03; both
//                implement the same three steps)
//
// UTS #39 §4 core: NFD → substitute each character's confusable prototype
// → NFD again.  `confusables` is the generated map[rune]string.

func Skeleton(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if c, ok := confusables[r]; ok {
			b.WriteString(c)
		} else {
			b.WriteRune(r)
		}
	}
	return norm.NFD.String(b.String())
}
```

Note that `disciplinedware` applies the final NFD (`confusables.go:159-160`) and `eskriett` does **not** (`confusables.go:312` returns the builder directly). The final NFD is required by the spec; include it.

### §C — the seed policy shape for `seed:admin-section-access` (EXT-07)

```go
// Structural analog: seed:character-directory (internal/access/policy/seed.go:480-486),
// which is likewise a target-only policy over an underscore-bearing resource type.
// Scoped by resource TYPE, not by enumerated id — an eighth section needs no
// policy edit (EXT-07, 01-SPEC.md §10.4).
{
	Name:        "seed:admin-section-access",
	Description: "Admins can read and write any admin section",
	DSLText:     `permit(principal is character, action in ["read", "write"], resource is admin_section) when { "admin" in principal.character.roles };`,
	SeedVersion: 1,
},
```

`[ASSUMED: the exact DSL text — the `principal is character` vs a player-flavored subject depends on §10.5's "per player, not per acting character" verdict, which this research did not fully trace. See `## Open Questions` Q2.]`

### §D — the §8.2.1 clearing test, verbatim from the SPEC

```text
anonymous floor:  principal.viewer.tier in ["anonymous", "guest", "player"]
guest floor:      principal.viewer.tier in ["guest", "player"]
player floor:     principal.viewer.tier in ["player"]
```

`[CITED: .planning/phases/01-portal-spec/01-SPEC.md:1243-1247]` — and the DSL's `in` operator is `evalInList` (`internal/access/policy/dsl/evaluator.go:317-336`), which is **false on an unresolved LHS**, so an absent `tier` denies.

### §E — `seed:profile-reachable`, verbatim from the SPEC

```text
permit(principal is viewer, action in ["read"], resource is profile)
  when { principal.viewer.tier in ["anonymous", "guest", "player"] };
```

`[CITED: .planning/phases/01-portal-spec/01-SPEC.md:1430-1432]`

## State of the Art

| Old approach | Current approach | When changed | Impact |
|---|---|---|---|
| golang-migrate with paired `.up.sql`/`.down.sql` | **goose**, one `.sql` per version with `-- +goose Up` / `-- +goose Down`, plus native Go migrations | Landed (Phase 01.1 / #4924) | **D-20's gate is clear.** 45 migrations; next free version `000054`; Go migrations supported and already fixture-proven |
| UTS #39 skeleton = `NFD → map → NFD` | `skeleton(X) = bidiSkeleton(LTR, X)` | Recent UTS #39 revision; current revision tracks Unicode 17.0.0 (2025-09-04) | No Go package implements `bidiSkeleton`. The classic core is what everyone ships; record the gap rather than claiming full §4 conformance |
| `characters.created_at TIMESTAMPTZ` | **BIGINT epoch-ns** | `000042_world_timestamps_to_bigint.sql:29-31` | D-25's `BIGINT` choice for `last_active_at` is consistent, and `task lint:no-timestamptz` (`Taskfile.yaml:216,885`) enforces it |
| `holomush_system_info.updated_at TIMESTAMPTZ` | **BIGINT epoch-ns** | `000044_pregfo6_gap_timestamps_to_bigint.sql:83-85` | D-16's poll indicator is an epoch-ns integer, not a timestamp |
| `oops.AsOops(err).Code()` asserts the top-level code | It resolves the **deepest** code in the chain | Phase 1 / issue #4902 | PORTAL-10 rule 5 was corrected to a **wire-level** assertion. Every denial assertion in this phase must follow the corrected form |

**Deprecated / outdated:**
- `internal/store/migrations/*.up.sql` / `*.down.sql` — gone. Every citation to them is stale.
- `.claude/rules/references/plan-review-learnings.md`'s "No `cmd/holomush/cmd_plugin.go`" — **stale**; the file exists.
- `NormalizeCharacterName`'s title-casing behavior — §6.1.5 says Phase 2 **replaces** the function, not extends it, specifically because it conflates the display name with the normalized form and denies players their own capitalization.

## Runtime State Inventory

Phase 2 is additive schema plus new vocabulary — not a rename or refactor — so most categories are empty. Recorded explicitly rather than left blank.

| Category | Items found | Action required |
|---|---|---|
| **Stored data** | `characters` rows: **every** row needs `normalized_name` and `name_skeleton` computed. This is the D-21 (B) backfill and is the phase's only data migration. `entity_properties` rows with `parent_type='character' AND visibility='public'`: **count unknown until the P1 audit runs** — no data change, but D-11 may require an operator to change individual rows' `visibility`. | Go migration (backfill) + operator action informed by the audit |
| **Live service config** | **None.** §8.12 ships no editing surface for the visibility configuration; policies live in `access_policies` seeded from Go code, and there is no external service holding this phase's vocabulary. Verified: `rg -n 'admin_section\|ViewerTier\|viewer:' --type go` returns zero non-test matches. | None |
| **OS-registered state** | **None.** No task-scheduler, launchd, systemd, or pm2 registration references character names, lifecycle status, or ABAC vocabulary. | None |
| **Secrets / env vars** | **None.** The block-list key is a `holomush_system_info` row, not a secret. No SOPS key, `.env` entry, or CI variable is renamed or introduced. | None |
| **Build artifacts / installed packages** | The **generated confusables table** becomes a build artifact under `task generate`. If the eskriett fallback is chosen instead, `go.mod`/`go.sum` change. Either way `task fmt` will add SPDX headers to new files — **commit those edits**, per CLAUDE.md's warning about uncommitted `fmt` output causing red CI. | `task generate` + `task fmt`, both committed in the same change |

**Related but out of scope:** the existing `access_policies` rows for `seed:property-*` will be **upgraded in place** by `upgradeSeedPolicy` if their `SeedVersion` is bumped (`internal/access/policy/bootstrap.go:91-93`) — but only if `Source == "seed"`. An admin-customized row is skipped with a WARN (`:80-88`). D-01's twins are *new* policies, so this path is not exercised unless an existing policy's DSL is edited.

## Validation Architecture

### Test Framework

| Property | Value |
|---|---|
| Unit framework | Go stdlib `testing` + `testify` (`assert` / `require`) |
| Integration framework | **Ginkgo/Gomega** BDD, build tag `//go:build integration`, testcontainers Postgres |
| Config file | `Taskfile.yaml` (no separate test config); test package list for `test:int` is hard-coded in the Taskfile |
| Quick run command | `task test -- ./internal/<pkg>/` |
| Full suite command | `task test` (untagged) then `task test:int` (integration, needs Docker) |
| Coverage | `task test:cover` |

`[VERIFIED: .claude/rules/testing.md; Taskfile.yaml:216,885; test/integration/access/seed_policies_test.go:1-22]`

**Critical:** `task test` does **not** compile `//go:build integration` files. Any refactor of shared types in this phase (the `ExistsByName` interfaces, `world.Character`) MUST be followed by `task test:int` or breakage is silent.

**Delegation rule:** per CLAUDE.md, `task test|lint|build|test:int|test:cover` MUST be dispatched to `local-check` rather than run inline in the parent — except the FINAL `task pr-prep` before a push, which runs inline.

### Phase Requirements → Test Map

| Req / criterion | Behavior | Type | Automated command | File exists? |
|---|---|---|---|---|
| IDENT-06 / crit. 1 | NFKC-collapsible pair rejected at create **and** rename | unit | `task test -- -run TestNormalize ./internal/world/` | ❌ Wave 0 |
| IDENT-06 / crit. 1 | `Cf`-padded name normalizes identically | unit | same | ❌ Wave 0 |
| IDENT-06 / crit. 1 | Latin+Cyrillic rejected; Latin+Han+Hiragana permitted (§6.1.2 all 8 rows) | unit | `task test -- -run TestMixedScript ./internal/world/` | ❌ Wave 0 |
| IDENT-06 / crit. 1 | Whole-Cyrillic homoglyph of an existing Latin name rejected by skeleton | integration | `task test:int` (needs a seeded existing character) | ❌ Wave 0 |
| IDENT-06 | Name normalizing to empty is rejected (§6.1.1) | unit | `task test -- -run TestNormalize ./internal/world/` | ❌ Wave 0 |
| IDENT-06 / D-23 | Stored skeleton carries the pinned `UnicodeVersion`; the constant equals the generated table's header | meta-test | `task test -- ./internal/<confusables-pkg>/` | ❌ Wave 0 |
| IDENT-07 / crit. 1 | Block-list pattern rejects at create **and** rename | unit | `task test -- -run TestBlockList ./internal/<pkg>/` | ❌ Wave 0 |
| IDENT-07 / D-15 | An uncompilable pattern is a **hard startup failure naming the entry** | unit | same | ❌ Wave 0 |
| IDENT-07 / D-16 | Reload failure leaves the **last valid list** in force | unit | same (precedent: `internal/access/policy/cache_test.go:166-188`) | ❌ Wave 0 |
| IDENT-08 / crit. 3 | Non-ASCII and leading-non-letter usernames still rejected — **pins** `^[a-zA-Z][a-zA-Z0-9_]*$` | unit | `task test -- -run TestValidateUsername ./internal/auth/` | ⚠️ extend existing |
| IDENT-09 / crit. 2 | Two concurrent claims of the same normalized name: **exactly one succeeds** | integration | `task test:int` | ❌ Wave 0 |
| IDENT-09 / crit. 2 | **Gate demonstrated RED against today's unindexed schema** | integration | see "RED-first" below | ❌ Wave 0 |
| IDENT-09 / D-19 | Synthetic collisions incl. an **NFKC-only pair** are detected; index applies cleanly after | integration | `task test:int` | ❌ Wave 0 |
| IDENT-09 / D-22 | Collision → migration error naming every set; transaction rolls back; schema at prior version | integration | `task test:int` | ❌ Wave 0 |
| PROFILE-11 / crit. 4 | Off-location viewer reads public properties and the in-world description | integration | `task test:int` (`test/integration/access/`) | ⚠️ extend `seed_policies_test.go` |
| PROFILE-11 / crit. 4 | **Paired positive control** — same fixture denied before the widening | integration | same | ❌ Wave 0 |
| PROFILE-11 / D-12 | The audit query is committed and its result recorded | artifact | manual — `02-AUDIT-RESULT.md` present and non-empty | ❌ Wave 0 |
| EXT-07 / crit. 5 | `seed:admin-section-access` permits admin, denies builder / plain player / guest, across **all seven ids** | unit | `task test -- -run TestSeed ./internal/access/policy/` via `createSeedEngine` | ⚠️ extend `seed_smoke_test.go` |
| EXT-07 / crit. 5 | **Paired positive control** per denial | unit | same | ❌ Wave 0 |
| EXT-07 / crit. 5 | Id list asserted by **set equality**, not membership | meta-test | `task test -- ./test/meta/` or the registry package | ❌ Wave 0 |
| EXT-07 / crit. 5 | An **eighth** section needs no new policy | unit | same — evaluate against an unregistered id | ❌ Wave 0 |
| D-06 / D-07 (INV-PRIVACY-11) | A non-admin's refusal is **byte-identical** across a registered and an unregistered id | unit | same | ❌ Wave 0 |
| D-09 | Boot validator refuses a zero-valued descriptor; meta-test asserts every entry non-zero | unit + meta | `task test -- ./internal/<registry-pkg>/` | ❌ Wave 0 |
| D-03 / §8.2.1 | Synthetic fourth tier `spectator` does **not** clear a `player` floor — **demonstrated RED against an ordinal implementation first** | unit | `task test -- -run TestTierFloor ./internal/access/policy/` | ❌ Wave 0 |
| D-04 | `visibility='private'` row + tier that clears the floor ⇒ attribute **absent** | unit | same | ❌ Wave 0 |
| §8.6 totality | A `profile.*` name in no §8.6 row is **denied, not defaulted** | unit | same | ❌ Wave 0 |
| INV-WORLD-5 | Character constructed **directly in `idle`** is excluded from selection; reads are exhaustive `switch` with a denying default | integration | `task test:int` | ❌ Wave 0 |
| INV-WORLD-6 | Retire does **not** release the name; the delete path **does** (paired) | integration | `task test:int` | ❌ Wave 0 |
| §8.4.1 obligation 1 | `warnOnMissingSeedCoverage` does **not** WARN for `viewer` | integration | `task test:int` — precedent `internal/access/setup/buildabacstack_seed_coverage_integ_test.go` | ⚠️ extend existing |
| §8.5 obligation | `PropertyProvider` still emits `name` | unit | `task test -- ./internal/access/policy/attribute/` | ⚠️ extend existing |

### Sampling rate

- **Per task commit:** `task test -- ./<touched package>/` plus `task lint`
- **Per wave merge:** `task test` (full untagged) — and `task test:int` for any wave that touched a shared type or interface
- **Phase gate:** `task pr-prep` green before `/gsd-verify-work`; `task test:int` green (the DB-backed criteria live there)

### RED-first demonstrations (PORTAL-10 rule 4)

Three gates in this phase must be **observed failing** before the fix lands. Each is the only assertion in the suite that can distinguish the correct implementation from the plausible wrong one:

| Gate | RED against | Method |
|---|---|---|
| Name-uniqueness (crit. 2) | **today's unindexed schema** | Write the concurrent-claim integration test, run it against the schema at version `000053` (before `000056`), record the failure, then land the index |
| Fourth-rung clearing (§8.2.1) | an **ordinal-comparison** implementation of the clearing test | Implement the clearing test with `>=` first, watch `spectator` clear a `player` floor, then replace with set membership |
| Additive-permit regression (D-04) | **term B removed** | Evaluate term A alone, watch the `private` row publish, then reinstate the conjunction |

The goose fixture already models how to run a test against a *staged* schema without polluting the real chain: a temp-dir migration set plus `goose.WithDisableGlobalRegistry(true)` (`internal/store/migrate_gointerleave_integration_test.go:134-208`). The RED-first uniqueness demonstration can use the same technique instead of hand-reverting the corpus.

### Wave 0 gaps

- [ ] `internal/world/<name-pipeline>_test.go` — IDENT-06 pipeline, mixed script, empty-normal-form
- [ ] `internal/<confusables-pkg>/skeleton_test.go` + a generated-table drift meta-test — IDENT-06, D-23
- [ ] `internal/<blocklist-pkg>/*_test.go` — IDENT-07, D-15, D-16
- [ ] `internal/access/policy/<tier-floor>_test.go` — D-03, D-04, §8.2.1 fourth rung, §8.6 totality
- [ ] `internal/access/policy/<admin-section>_test.go` — EXT-07 crit. 5, D-06, D-07/INV-PRIVACY-11, D-09
- [ ] `test/integration/world/<lifecycle>_test.go` — INV-WORLD-5, INV-WORLD-6
- [ ] `test/integration/<domain>/<name-uniqueness>_test.go` — crit. 2 concurrency, D-19 synthetic collisions, D-22 rollback
- [ ] Extend `test/integration/access/seed_policies_test.go` — crit. 4 with its paired positive control
- [ ] Extend `internal/access/policy/seed_smoke_test.go` — a `viewerProvider` test double beside `characterProvider` (`:35-52`)
- [ ] Extend `internal/auth/player_test.go` — IDENT-08 regression pin
- [ ] `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql` + `02-AUDIT-RESULT.md` — D-12
- [ ] Framework install: **none needed** — Ginkgo, Gomega, testify, testcontainers all present

## Security Domain

`security_enforcement` is not set to `false` in `.planning/config.json`, so this section applies. This phase is *almost entirely* a security phase.

### Applicable ASVS categories

| ASVS category | Applies | Standard control in this phase |
|---|---|---|
| V2 Authentication | Indirect | IDENT-08 pins the username rule; **do not touch** the auth path (§6.2) |
| V3 Session Management | Indirect | §8.4.1: the viewer tier **MUST** be derived from server-side session state, **never** from a client header, query param, cookie value, or request field |
| **V4 Access Control** | **Yes — the core of the phase** | Default-deny ABAC engine (`internal/access/policy`); seed policies; `combineDecisions` deny-overrides; the caller-ANDed conjunction of §8.5.1; gate-then-distinguish (D-06) |
| **V5 Input Validation** | **Yes** | The §6.1.1 pipeline; §6.1.2 confusable/mixed-script; §6.1.4 block list — **server-side, at both create and rename**. "Client-side evaluation is not evaluation" |
| V6 Cryptography | No | Nothing in this phase touches `internal/eventbus/crypto/`; `crypto-reviewer` is not triggered |
| V7 Error Handling / Logging | Yes | §8.7's not-found-equivalent; D-06's identical denial code; `.claude/rules/grpc-errors.md` (never leak inner errors past a trust boundary) |
| V12 Files / Resources | No | No upload surface in v0.13 |

### Known threat patterns for this stack

| Pattern | STRIDE | Mitigation |
|---|---|---|
| Homograph impersonation (whole-script Cyrillic clone of a Latin name) | **Spoofing** | §6.1.2 Mechanism B — the skeleton check |
| Cross-script splicing (`раypal` — Cyrillic `а`+`р` in a Latin word) | **Spoofing** | §6.1.2 Mechanism A — the mixed-script restriction |
| Invisible-codepoint padding (ZWJ/ZWNJ producing two distinct-but-identical strings) | **Spoofing** | §6.1.1 step 2 — `Cf` stripping. Note `characterNameRegex` already rejects these today, but §6.1.5 explains why stripping is still required: "Defense that depends on an unrelated rule staying strict is defense that expires without notice" |
| Check-then-insert TOCTOU on name claim | **Tampering / Spoofing** | §6.1.3 — a database `UNIQUE` index, not an application check. Three consumers race today; `Rename` would be a fourth |
| Registry-enumeration oracle via distinguishable denial codes | **Information disclosure** | **D-06 gate-then-distinguish**, pinned by INV-PRIVACY-11 with a byte-identical-refusal test |
| Profile-existence disclosure via a "this profile is private" response | **Information disclosure** | §8.7 not-found-equivalent; INV-PRIVACY-9 (binds Phase 4) |
| Additive-permit exposure of `visibility='private'` rows to the open web | **Information disclosure** | §8.5.1's conjunction; D-04's regression test is the mechanical guard |
| Fail-open on a newly appended tier token | **Elevation of privilege** | §8.2.1 set membership, never ordinal comparison; the `spectator` RED-first test |
| Fail-open on an unenumerated `profile.*` name | **Information disclosure** | §8.6 totality rule — denied, not defaulted; no glob or prefix |
| Empty-string attribute sentinel creating a fail-open match | **Elevation of privilege** | `.claude/rules/abac-providers.md` / ADR holomush-ti1b — omit, don't sentinel |
| Confusable-rejection message naming the colliding character | **Information disclosure** | The roadmap sketch finding: the message MUST NOT name it |
| Over-broad operator-supplied block-list regex (denial of legitimate names) | **Denial of service** | Not ReDoS (RE2 is linear). D-15's boot validation surfaces a *malformed* pattern; an over-*broad* pattern is an operator concern, and D-16's "last valid list stays in force" limits blast radius |

**Domain gate:** `abac-reviewer` (`/holomush-dev:review-abac`) is **mandatory** before this phase merges — D-05 routes the §8.5.1.1 finding to it explicitly, and the diff touches `internal/access/`. `crypto-reviewer` is **not** triggered (no `internal/eventbus/crypto/`, `codec/`, `history/dispatcher.go`, `cold_postgres.go`, `event_emitter.go::Emit`, `audit/projection.go`, `crypto.emits`, or `crypto_keys`/`events_audit` migration).

## Environment Availability

| Dependency | Required by | Available | Version | Fallback |
|---|---|---|---|---|
| Go toolchain | everything | ✓ | 1.26.5 (`go.mod:3`) | — |
| `golang.org/x/text` | NFKC / NFD / `Cf` / case folding | ✓ | v0.40.0, indirect (`go.mod:178`) | — |
| `github.com/pressly/goose/v3` | migrations | ✓ | in `go.mod`, used at `internal/store/migrate.go:22` | — |
| Docker | `task test:int`, testcontainers Postgres | **UNVERIFIED** — not probed this session | — | Integration criteria (2, 4, 5, D-19) cannot run without it. `task pr-prep` fast lane does not need it |
| Network access to `unicode.org` | **generating** the confusables table | **UNVERIFIED** | — | Generation is a one-time developer action, not a build step. The generated file is committed, so CI never fetches. If unreachable, download `confusables.txt` out of band and run the generator with a `-input` flag (the pattern `disciplinedware/go-confusables/cmd/confusables-gen/main.go:49` uses: `input := fs.String("input", "", "Path to local confusables.txt (offline mode)")`) |
| `license-eye` (via `task fmt`) | SPDX headers on new files | ✓ (in `task fmt`) | — | — |
| `gh` CLI | issue filing | ✓ (used this session) | — | — |

**Missing with no fallback:** none identified.
**Missing with fallback:** network access to `unicode.org` — offline generation via a local `confusables.txt`.
**Unverified:** Docker availability. The planner should confirm before sequencing the integration-tier waves.

## Concerns

Recorded rather than planned around, per the brief. Two locked decisions rest on premises this research found to be inaccurate, and one CONTEXT statement is now out of date. None of these change the *shape* of a decision — they change its cost.

1. **The CONTEXT's Unicode premise is wrong in the mixed-script half.** "Claude's Discretion" states `golang.org/x/text` "covers NFKC (step 1) and script extensions for UTS #24 (Mechanism A)". x/text has **no script package at all** (`unicode/` = `bidi`, `cldr`, `norm`, `rangetable`, `runenames`). Go's stdlib has `unicode.Scripts`, but that is the **Script** property at **Unicode 15.0.0**, not **Script_Extensions**, which is what UTS #39's Moderately Restrictive is defined over. Mechanism A therefore also needs a decision (approximate from stdlib, or generate a second table), and it was scoped as free. See `## P0` Finding 2.

2. **`02-CONTEXT.md` D-20 describes goose adoption as an inserted, not-yet-executed phase. It has landed.** 45 single-file goose migrations, `goose.Provider` wired at `internal/store/migrate.go`, Go migrations supported, and `internal/store/migrate_gointerleave_integration_test.go` already proving D-21's A→B→C sequence. Phase 2's execution gate is clear, and the planner should treat the fixture as the worked example. This is good news, but a planner reading only the CONTEXT would sequence around a blocker that no longer exists.

3. **D-16's poll indicator does not do what the decision assumes.** `holomush_system_info.updated_at` is bumped by `SetSystemInfo`'s `ON CONFLICT` clause — but D-16 itself says the only v0.13 edit path is direct SQL, and a direct `UPDATE ... SET value = ...` leaves `updated_at` untouched. There are no triggers (migrations forbid them). The *decision* — mirror `Cache` + `Poller` — is right; the *indicator* needs to be `(updated_at, hash(value))` or a content hash alone. See `## Common Pitfalls` P-5.

4. **IDENT-09's writer enumeration is short by one, and the missing one is a test harness.** The SPEC (§6.1.3) and the CONTEXT both name two writers. `internal/testsupport/integrationtest/harness.go:1549` is a third `ExistsByName` implementation whose own comment at `:1119` says the production guest service depends on the behavior. A plan that migrates two sites will leave the integration harness diverging from production, which is the worst place for a divergence in a phase whose criteria are integration-tier.

5. **`01-SPEC.md`'s migration citations are stale post-goose.** Filenames and line numbers both. Correct values are given in `## P1`. This is not a decision problem, but transcribing the SPEC's citations into a plan will produce Edit-tool failures.

## Assumptions Log

| # | Claim | Section | Risk if wrong |
|---|---|---|---|
| A1 | The §6.1.1 pipeline composes cleanly as `transform.Chain(norm.NFKC, runes.Remove(runes.In(unicode.Cf)))` followed by whitespace canonicalization | Code Examples §A | Low — the API shapes are documented; the composition was reasoned, not compiled. A planner task should compile a spike |
| A2 | LTR-only skeleton (omitting `bidiSkeleton`'s RTL handling) is adequate for this threat model | P0 Finding 4 | Medium — an RTL-script character name could evade the skeleton check via reordering. No Go package implements the bidi form, so the alternative is writing it |
| A3 | `players.is_guest` is the column name used in audit query (5) | P1 | Low — `000002_player_is_guest.sql` exists but was not opened. Confirm before running |
| A4 | `seed:admin-section-access` uses `principal is character` with a `roles` check | Code Examples §C | **Medium-high** — §10.5 says the gate is evaluated **per player**, which may imply a `player:` subject and `player.grants`. This research did not trace §10.5's full verdict. See Q2 |
| A5 | `internal/bootstrap/setting.go:103-112,156` carries the sentinel-written-last bootstrap pattern D-22 matches | P2.8 | Low — cited from CONTEXT, not opened this session |
| A6 | The sc-vs-scx approximation is closed-direction for the SPEC's three named rejections | P0 Finding 2 | Medium — reasoned from UAX #24 §3.3's default rule ("code points not listed have as their Script_Extensions value the corresponding Script value"). Cyrillic and Greek letters carry real sc values, so the named pairs are caught either way. Not exhaustively verified across the repertoire |
| A7 | Docker is available for `task test:int` | Environment Availability | Medium — not probed. Blocks four of the five success criteria if absent |

## Open Questions

1. **Is `seed:profile-public-read` one policy or two?**
   - *What we know:* PROFILE-11's scope covers **both** public `entity_properties` rows **and** `characters.description`. Those are two different ABAC resources (`property:` and `character:`), currently gated by two different colocation-conditioned seeds (`seed:property-public-read` at `seed.go:111-116`; `seed:player-character-colocation` at `:50-56`). D-11 says the **grid** path widens, implying a character-flavored change; D-01 says term B needs **viewer**-flavored twins, implying a separate web-side policy.
   - *What's unclear:* whether the phase ships one policy name covering both resources (possible — the DSL permits a bare `resource`), two policies under one conceptual name, or a character-flavored widening plus viewer twins that happen to subsume the web case.
   - *Recommendation:* the planner should settle this in the plan and route it to `abac-reviewer` with D-05's §8.5.1.1 amendment, since it is the same reviewer and the same conjunction. A bare-`resource` policy is the most dangerous of the three options (it matches `location:`, `object:`, `stream:` too) and should be rejected unless explicitly argued.

2. **What subject does `seed:admin-section-access` take?**
   - *What we know:* §10.5's verdict is *"the admin gate is evaluated PER PLAYER."* Every shipped seed is `principal is character`, `principal is plugin`, or `principal is system` (`internal/access/policy/seed.go`). `SubjectPlayer = "player:"` exists (`internal/access/prefix.go:18`) and `PlayerSubject` is its constructor (`:89-94`), with a `PlayerAttributeProvider` registered at `internal/access/setup/setup.go:243`. `PlayerHasRole` (`internal/store/role_store.go`) returns true iff *any* character of the player holds the role — deliberate, documented, and tracked in #4899.
   - *What's unclear:* whether `principal is player` policies are viable today (does `PlayerAttributeProvider` supply a roles-equivalent?), and whether §10.5 intends the *subject* to be player-flavored or only the *semantics*.
   - *Recommendation:* read §10.5 and §10.5.1 in full (`01-SPEC.md:2237-2303`) and `internal/access/policy/attribute/player.go` before writing the DSL. This is the single highest-risk unresolved detail for criterion 5 — a wrong principal type makes the policy match nothing and the criterion pass vacuously via default-deny.

3. **Should `viewer:` and `admin_section:` be registered in `knownPrefixes`?**
   - *What we know:* the CONTEXT flags this as an open hygiene question. §8.4.1's Phase-2 obligation 2 and §8.4.2's Phase-2 obligation say **yes, explicitly**, extending the known-prefix table test. The CONTEXT's `## Established Patterns` observes `ParseEntityRef`/`knownPrefixes` has zero production callers.
   - *Recommendation:* register them. The SPEC mandates it, the cost is three lines plus test rows, and "parses today because nothing validates" is not a property to build on.

4. **Does the `unicode.Scripts` approximation need an explicit "unassigned script" rule?**
   - *What we know:* stdlib is Unicode 15.0.0; the confusables data is 17.0.0. A codepoint assigned in 16 or 17 is in no `unicode.Scripts` range.
   - *Recommendation:* **reject** such a name (fail-closed), consistent with `characterNameRegex`'s existing letters-only shape which would already reject most of them. Document the rule; do not leave it emergent.

## Sources

### Primary (HIGH confidence — opened and read this session)

| Source | What was checked |
|---|---|
| `.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md` | All 26 decisions, canonical refs, code insights (full file) |
| `.planning/phases/01-portal-spec/01-SPEC.md` | §4.1–4.5, §6.1.1–6.3, §7.1, §8.1–8.12, §10.1–10.4, §11.3–11.4, §13 |
| `.planning/REQUIREMENTS.md` | IDENT-06..09, PROFILE-11, EXT-07, PORTAL-10, traceability (full file) |
| `.planning/STATE.md` | Current position, milestone shape, pre-existing hazards (lines 29-83) |
| `internal/access/policy/seed.go` | `SeedPolicy` struct :6-12; `SeedPolicies` :36; colocation :50-56; admin-full-access :105-110; property family :110-146; character-directory :480-486 |
| `internal/access/policy/bootstrap.go` | Full file — seed install/upgrade/collision semantics, `UpdateSeed` |
| `internal/access/policy/engine.go` | :361-381, :464, :479, :542-548 (`parseEntityType`), :550-573 (`validateRequest`), :591-611 (`combineDecisions`) |
| `internal/access/policy/dsl/ast.go` | :75-102 — `Target`, `PrincipalClause`, `ResourceClause` grammar |
| `internal/access/prefix.go` | :12-19, :21-34, :45-61, :63-107 |
| `internal/access/policy/attribute/property.go` | :38-45, :61-140, :197-211 |
| `internal/access/policy/poller.go` | :19-37, :39-52, :85-116 |
| `internal/access/policy/policytest/helpers.go` | :19, :29, :45-137 |
| `internal/access/policy/seed_smoke_test.go` | :1-90 — `createSeedEngine`, provider doubles |
| `internal/access/setup/setup.go` | `RegisterProvider` call sites :150,172,199,223,243,249,256,262; :276 seed-coverage warn |
| `internal/access/setup/seed_coverage.go` | :43, :83 |
| `internal/world/validation.go` | :50-130 |
| `internal/world/property.go` | :18 |
| `internal/world/postgres/parent_location_resolver.go` | :25-58 |
| `internal/auth/player.go` | :18-40, :150-175 |
| `internal/auth/character_service.go` | :23-24, :100-130 |
| `internal/auth/guest_service.go` | :39, :215-240 |
| `internal/bootstrap/setup/adapters.go` | :25-60 |
| `internal/settings/namespaces.go` | :1-50 |
| `internal/settings/game.go` | :45-75, :120-165 |
| `internal/store/postgres.go` | :84-94 |
| `internal/store/migrate.go` | :1-80 |
| `internal/store/migrations_register.go` | full file |
| `internal/store/migrations/doc.go` | full file |
| `internal/store/migrations/000001_baseline.sql` | :37-42, :50-90, :352-378 |
| `internal/store/migrations/000042_world_timestamps_to_bigint.sql` | :23-32, :200-211 |
| `internal/store/migrations/000044_pregfo6_gap_timestamps_to_bigint.sql` | :69-90, :121-138 |
| `internal/store/migrations/000045_character_preferences.sql` | :7-11 |
| `internal/store/migrations/000049_world_version_guard.sql` | :1-35 |
| `internal/store/migrate_gointerleave_integration_test.go` | :1-210 |
| `test/integration/access/seed_policies_test.go` | :1-50, heading sweep |
| `docs/architecture/invariants.yaml` | id sweep; :2156-2169; :5065-5090 |
| `.planning/config.json` | `workflow.nyquist_validation: true` |
| `.claude/skills/new-migration/SKILL.md`, `new-integration-test/SKILL.md` | headers + steps |
| Go toolchain | compiled and ran a probe: `unicode.Version` = `15.0.0`, `len(unicode.Scripts)` = 163 |
| Local Go module cache | `golang.org/x/text@v0.40.0` tree listing; five confusables modules downloaded and their `Skeleton` bodies, licenses, and data headers read |

### Secondary (MEDIUM confidence — external, verified against a primary artifact)

- `https://www.unicode.org/reports/tr39/` — §4 skeleton definition (`skeleton(X) = bidiSkeleton(LTR, X)`), §5.2 Moderately Restrictive, revision tracks Unicode 17.0.0. Fetched 2026-08-03. Cross-checked against the two candidate implementations' code.
- `https://www.unicode.org/reports/tr24/` — §2.9 and §3.3, Script vs Script_Extensions, Table 2 (U+30FC example), the default rule for unlisted code points. Fetched 2026-08-03.
- `https://proxy.golang.org/.../@latest` and `/@v/list` — authoritative version and publication timestamps for all five candidate modules, 2026-08-03.
- `gh api repos/<owner>/<repo>` — stars, forks, creation date, last push, license SPDX for all five candidates, 2026-08-03.
- `go.googlesource.com/go` `src/unicode/maketables.go` — confirms `unicode.Scripts` is generated from `Scripts.txt`.

### Tertiary (LOW confidence — flagged, not relied upon)

- pkg.go.dev summary text for `SCKelemen/unicode/v6/uts39` claiming `skeleton(X) = toNFD(toCaseFold(toNFKD(X)))`. **Contradicted by the module's own source** (`uts39/uts39.go:124-146`), and both differ from UTS #39 §4. Used only as evidence that the package should be rejected.

## Metadata

**Confidence breakdown:**

| Area | Level | Reason |
|---|---|---|
| Repo grounding (P2) | **HIGH** | Every `path:line` was opened with `Read` or matched with `rg -n` this session; discrete values are quoted verbatim |
| P0 confusables survey | **HIGH** | All five modules downloaded into the local cache; skeleton bodies, licenses, and data headers read directly; versions from the Go module proxy; repo signals from the GitHub API |
| P0 Script_Extensions gap | **HIGH** | Established by exhaustive listing of `x/text@v0.40.0/unicode/`, a compiled `unicode.Version` probe, a null `rg` for `scriptextensions` in the only candidate that claims script support, and the UTS #39 / UAX #24 texts |
| P0 recommendation | **MEDIUM-HIGH** | The facts are verified; the *judgement* (generate over depend) weighs supply-chain risk against generator cost and is arguable |
| P1 audit query | **MEDIUM** | The SQL is grounded in DDL read this session, but the §8.6 name list must be re-diffed against the SPEC, `players.is_guest` is `[ASSUMED]`, and the *result* is unknown until run |
| Migration mechanics | **HIGH** | goose adoption is landed and the D-21 fixture reads verbatim in-tree |
| Pitfalls | **HIGH** | Each is derived from a file read this session, a SPEC section, or a documented in-repo rule |
| Admin-section policy DSL | **MEDIUM** | Expressibility is verified against the grammar and an in-tree precedent; the correct *principal type* is Open Question 2 |

**Research date:** 2026-08-03
**Valid until:** 2026-09-02 for the repo grounding (30 days — the tree moves, and Phase 01.1's landing already invalidated the SPEC's citations once). **7 days** for the third-party module survey — `disciplinedware/go-confusables` is a week-scale-young repository and its signals will change.
