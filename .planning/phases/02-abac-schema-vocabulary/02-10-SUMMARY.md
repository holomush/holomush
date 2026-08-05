---
phase: 02-abac-schema-vocabulary
plan: 10
subsystem: access-control
tags: [abac, profile-visibility, exposure-audit, privacy, profile-11, d-10, d-11, d-12, d-13, criterion-4, sanitized-ledger]

requires:
  - phase: 02-abac-schema-vocabulary
    provides: "02-07's seed:profile-public-read-property widening — the permit this plan audits and gates"
provides:
  - "02-AUDIT-profile-public-read.sql — a committed, re-runnable, read-only seven-part audit: five aggregates plus two per-row ledgers that emit ids, lengths and md5 digests but no player text"
  - "02-AUDIT-detail-operator-only.sql — the adjudication companion; the FILE is committed, its OUTPUT never is"
  - "02-AUDIT-RESULT.md — the sanitized ledger discharging ROADMAP Phase 2 success criterion 4, from a query that reached a real database"
  - "test/integration/access/profile_public_read_test.go — six specs proving the widening widens, against a control corpus where it does not"
  - "accessTestEnv.resolver / .compiler — a second engine over the SAME real provider stack but a DIFFERENT policy corpus, for paired positive controls"
  - "The viewer namespace registered in test/integration/access, so §8.4.2 reachability is assertable there"
affects: [02-11, phase-04-character-access-service]

actuals:
  tokens: 12708
  tasks: 6
  commits: 6

tech-stack:
  added: []
  patterns:
    - "Split the audit's output rather than the audit: a committed sanitized ledger (stable row id + content digest + length + verdict) alongside an operator-only detail query whose output is read outside the repository and deleted — so the evidence file cannot itself become the exposure"
    - "A content digest as a STABLE HANDLE rather than as secrecy: it lets a verdict name a row and lets a later re-run prove whether that row changed, without either run recording what the row said"
    - "A PolicyStore decorator overriding only ListEnabled — the single method Cache.Reload calls — to build a control corpus excluding one policy BY NAME, with a removed-count the spec pins so the control cannot silently disarm"
    - "Counting matches with `rg -o … | wc -l` and a numeric `-eq` instead of `rg -c`, which exits 1 and prints nothing on the zero matches that mean success"
    - "Auditing a restored corpus AS-IS at its own schema level, after verifying every column the query reads predates the migrations under development"

key-files:
  created:
    - .planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql
    - .planning/phases/02-abac-schema-vocabulary/02-AUDIT-detail-operator-only.sql
    - .planning/phases/02-abac-schema-vocabulary/02-AUDIT-RESULT.md
    - test/integration/access/profile_public_read_test.go
  modified:
    - test/integration/access/access_suite_test.go

key-decisions:
  - "Task 2 (checkpoint:decision, blocking) resolved to `sanitized-ledger` — MAINTAINER-selected, not auto-selected. Committed evidence is ids, lengths, digests and verdicts; no player-authored text enters the repository in any form, including truncated or representative excerpts."
  - "Task 4 (checkpoint:decision, blocking) resolved to `no-remediation-required` — MAINTAINER-selected. Zero rows in scope, zero enumerated row ids, no prior-value capture or rollback needed because no row is written. Reflects the produced ledger, not an assumption about it."
  - "Schema path: the restored corpus was audited AS-IS at goose level 53, with NO migrations applied, because every column the audit reads predates 000054. Verified against the restored DDL before running. Consequence knowingly accepted: 000055/000056 were NOT rehearsed against real data."
  - "02-REMEDIATION.sql was never created — on a no-remediation verdict Task 4b performs no write, and there is no after-ledger because no row moved."

patterns-established:
  - "Do not write a gate's own trigger string into prose that explains the gate. A substring gate is disarmed by any mention anywhere in the document, including a sentence asserting the section is absent."
  - "Prove a control corpus is non-vacuous by asserting it still PERMITS something (here the co-located read), not only that it denies the thing under test — a control that denies everything is indistinguishable from a broken engine."

requirements-completed: []

coverage:
  - id: A1
    description: "The audit query is committed, re-runnable, read-only by construction, and emits no player-authored text"
    requirement: PROFILE-11
    verification:
      - kind: gate
        ref: "rg -v '^\\s*--' <both files> | rg -o 'INSERT|UPDATE|DELETE|ALTER|DROP|CREATE|TRUNCATE' | wc -l == 0; md5( == 2; ep.value|c.description == 0"
        status: pass
    human_judgment: false
  - id: A2
    description: "The ledger's §8.6 name list is re-derived from the SPEC, not transcribed from research"
    requirement: PROFILE-11
    verification:
      - kind: gate
        ref: "23 distinct quoted profile.* literals; set-equal by diff against the 23 names in internal/access/policy/seed.go"
        status: pass
    human_judgment: false
  - id: A3
    description: "Result set 6 enumerates EVERY public parent_type='character' row with an in_spec_86 flag, not only the unenumerated ones (B-14)"
    requirement: PROFILE-11
    verification:
      - kind: gate
        ref: "set 6 WHERE clause carries no §8.6-name exclusion; in_spec_86 present"
        status: pass
    human_judgment: false
    rationale: "The strictly-greater fixture check the plan asks for was not exercisable: on a corpus where set 3 and set 6 are both 0, >= holds but strict > cannot. See Deviations."
  - id: A4
    description: "The audit ran against a REACHED database and its result is recorded with a date, a database identifier and a per-row verdict"
    requirement: PROFILE-11
    verification:
      - kind: manual
        ref: "02-AUDIT-RESULT.md — kopia snapshot 7e48a9b592c2e0d302a5da3cf0171835 restored to a throwaway PostgreSQL 18.4 container; both queries exit 0 with zero SQL errors"
        status: pass
    human_judgment: true
  - id: A5
    description: "No player-authored text was committed, and the operator-only detail output is absent from the working tree"
    requirement: PROFILE-11
    verification:
      - kind: manual
        ref: "detail output written to $TMPDIR outside the repo, chmod 600, read, deleted; git status clean; both its result sets returned zero rows so there was none to leak"
        status: pass
    human_judgment: true
  - id: A6
    description: "The widening permits the off-location read the colocation policy denied, and the SAME call is denied against a corpus excluding it"
    requirement: PROFILE-11
    verification:
      - kind: integration
        ref: "test/integration/access/profile_public_read_test.go#W1 (RED demonstrated: control expectation flipped to Allow -> exit 201, effect 0 vs 1)"
        status: pass
    human_judgment: false
  - id: A7
    description: "The widening's boundary is pinned on both sides: private rows and non-character parents stay denied, each paired with a permit on the same fixture"
    requirement: PROFILE-11
    verification:
      - kind: integration
        ref: "profile_public_read_test.go#W4 (visibility guard) / #W5 (parent_type guard)"
        status: pass
    human_judgment: false
  - id: A8
    description: "The widening is additive — the co-located read that already worked still works"
    requirement: PROFILE-11
    verification:
      - kind: integration
        ref: "profile_public_read_test.go#W3, plus #W2 proving the control corpus still permits it"
        status: pass
    human_judgment: false
  - id: A9
    description: "A character with zero entity_properties rows still resolves its profile — reachability is independent of any per-field result"
    requirement: PROFILE-11
    verification:
      - kind: integration
        ref: "profile_public_read_test.go#W6, at all three viewer rungs, with the zero-row fixture asserted rather than assumed"
        status: pass
    human_judgment: false
  - id: A10
    description: "No remediate verdict is left unperformed — the result file carries a recorded verdict with an approver and a date (B-13)"
    requirement: PROFILE-11
    verification:
      - kind: gate
        ref: "rg -o '^## Remediation verdict$' 02-AUDIT-RESULT.md | wc -l == 1, line-anchored"
        status: pass
    human_judgment: false

duration: 63min
completed: 2026-08-05
status: complete
---

# Phase 02 Plan 10: The `seed:profile-public-read` Exposure Audit Summary

**The gate on 02-07's widening is discharged: both committed read-only queries ran against a real restored corpus, the sanitized ledger records a measured total of `0` exposed rows with no player-authored text, and six integration specs prove the widening widens against a control corpus where the identical call is denied.**

## Performance

- **Duration:** 63 min across three sessions (blocked mid-plan on credential access)
- **Tasks:** 6 of 6 — three execute, two blocking checkpoints, one TDD-shaped spec
- **Files:** 5 (4 created, 1 modified)

## Task Commits

| Task | Name | Commit | Key files |
| --- | --- | --- | --- |
| 1 | The ledger query and the operator-only detail query | `1cc97a105` | `02-AUDIT-profile-public-read.sql`, `02-AUDIT-detail-operator-only.sql` |
| 2 | `checkpoint:decision` — evidence-recording scheme | `86f7d2b0f` | `.planning/STATE.md` (decision record) |
| 3 | Run the audit read-only, record the ledger | `31202d461` | `02-AUDIT-RESULT.md` |
| 3 | Fix: the result file vacuously satisfied Task 4b's gate | `3b6e398a4` | `02-AUDIT-RESULT.md` |
| 4 | `checkpoint:decision` — remediation approval | (no commit — decision only) | — |
| 4b | Record the verdict; no write performed | `e117b2620` | `02-AUDIT-RESULT.md` |
| 5 | The widening proved, with its paired control | `c2c88e937` | `test/integration/access/profile_public_read_test.go`, `access_suite_test.go` |

## The audit — what actually ran, and what it found

**Target.** kopia snapshot **`7e48a9b592c2e0d302a5da3cf0171835`** (start `2026-08-05T03:00:00Z`), the most recent of 36 — which needed a sort by `startTime`, because `kopia snapshot list --all` groups by ephemeral container host and its listing order is not chronological. Restored into a throwaway `postgres:18` container with no published ports, destroyed after the run. The live sandbox was never contacted.

**Schema path: audited AS-IS at goose level 53, no migrations applied.** Verified against the restored DDL *before* running, not assumed: the audit reads only `entity_properties.{id,parent_type,parent_id,name,value,visibility}` and `characters.{id,description,player_id}` (all `000001`) plus `players.is_guest` (`000002`). Nothing from `000054`+.

| Result set | Value |
| --- | --- |
| (2) **total_public_character_rows** | **`0`** — the number criterion 4 records |
| (1) public rows by property name | 0 rows |
| (3) names outside §8.6 | 0 rows |
| (4) characters / non-empty descriptions / longest | 3 / **0** / 0 |
| (5) total / guest / guest-with-description | 3 / **2** / 0 |
| (6) property ledger | **0 rows** (0 `in_spec_86=true`, 0 `false`) |
| (7) description ledger | **0 rows** |

`entity_properties` is empty **in its entirety** — not merely empty of public character rows. Both detail result sets also returned zero rows, so there was no player-authored text to adjudicate and none to leak.

**This is a legitimate zero from a REACHED database, which is the distinction the plan's whole gate design turns on.** The earlier plan shape would have accepted an unreachable database as a discharging result; T-02-98 made that a blocking escalation, and that escalation actually fired — the plan stopped mid-flight for a session until credential access was available. The gate did its job by refusing to be satisfied by the condition it exists to detect.

**How far the zero reaches, recorded in the artifact so nobody over-reads it:** the sandbox is a demo environment. This is evidence that the widening exposes nothing *in the audited corpus*; it is not and cannot be evidence about a future populated one. That is precisely why D-12 wanted the query committed and re-runnable, and why the ledger carries content digests.

## Task 2 and Task 4 — both checkpoints, both maintainer-answered

Neither was auto-selected, and both are recorded as such. Auto mode was active in the session; the coordinator overrode it for Task 4 specifically.

- **Task 2 → `sanitized-ledger`.** Committed evidence is ids, property names, lengths, digests and verdicts. The explicit confirmation the resume-signal asks for was given: **no player-authored text is to be committed**, including truncated or "representative" excerpts.
- **Task 4 → `no-remediation-required`.** Zero rows in scope, zero enumerated row ids, no capture and no rollback because no row is written. The option's one attached condition — that it reflect the ledger rather than an assumption about it — is satisfied by a measured zero.

Task 4b consequently performed **no write**: no `visibility` changed, no text edited, no `02-REMEDIATION.sql` created, no database contacted after the read-only audit. There is no after-ledger because no row moved; it would be byte-identical to the before-ledger and would prove nothing the recorded verdict does not.

## Deviations from Plan

### 1. [Rule 1 — Bug, self-inflicted] The result file vacuously satisfied Task 4b's completion gate

**Found during:** the post-Task-5 verification sweep. **Fixed in `3b6e398a4`.**

Task 4b's gate is `rg -o '## After ledger|## Remediation verdict' 02-AUDIT-RESULT.md | wc -l -ge 1`. While Task 4 was still unanswered, I wrote a sentence explaining that the recorded-verdict section was deliberately absent — **and quoted that section's heading verbatim to do it.** The gate is a substring match over the whole document, so the prose alone satisfied it. The plan could then have completed with the remediation decision outstanding, which is exactly the fail-open B-13 added the gate to prevent.

**This is a plan-authoring defect class, not a one-off, and 02-11 should carry it forward.** A gate that matches a bare substring anywhere in a document is disarmed by any mention of its own trigger — including, perversely, a mention whose purpose is to state that the guarded thing is missing. **Recommendation: anchor to line start**, `rg -o '^## After ledger$|^## Remediation verdict$' … | wc -l`, which matches only a real heading. Both forms now count 1 on the final file and they agree, which is itself the evidence that no prose contamination remains.

The same shape already bit this phase once: 02-07 had to order a comment so a `-B 6` grep window would not catch a justification it was forbidden to contain.

### 2. [Rule 3 — Blocking] Two suite-file changes beyond `files_modified`

`test/integration/access/access_suite_test.go` is not in the plan's `files_modified`, but Task 5's required assertions are unwritable without two additions to it:

| Change | Why it was blocking |
| --- | --- |
| `resolver` + `compiler` exposed on `accessTestEnv` | The paired positive control needs a SECOND engine over the **same** real provider stack but a different policy corpus. Rebuilding the ~40-line provider wiring inside the spec would let the control drift from the engine under test — and a control that drifts is worse than none. |
| `attribute.NewViewerTierProvider()` registered | Without it `principal.viewer.tier` is unresolvable, so W6's reachability read default-denies **for a reason unrelated to reachability** — which reads exactly like a correct denial and is actually an absent provider. This is the failure mode 02-07's attribute-coverage gate exists to catch. No DB dependency; the tier is carried in the subject string. |

No existing spec's outcome changes: none used a `viewer:` subject before. Full `task test:int` is green.

### 3. [Plan defect] Task 5's `<behavior>` block contradicts its own `<action>` and `<deferral>`

Behavior bullet 2 asks for an assertion that "A is permitted to read B's character entity — the read that carries `characters.description`". The `<action>`, the `<deferral>` and an acceptance criterion all **forbid** it: D-29 defers the character-resource-type permit to Phase 4, so such an assertion would be satisfied by default-deny and would read as a correct denial while actually being an absent feature.

I followed the corrective text and asserted `entity_properties` reads only. **02-11 should reconcile the two.**

### 4. [Plan defect] Task 1's `ep.value` / `c.description` criterion is unsatisfiable with the obvious aliases

The criterion pairs `rg -o '\bep\.value\b|\bc\.description\b' … -eq 0` with "every text column it touches is reached through `length(...)` or `md5(...)`". Both cannot hold if the tables carry the aliases `ep` and `c`, because `length(ep.value)` *contains* `ep.value`. 02-RESEARCH's drafted query uses exactly those aliases, so this was live rather than hypothetical.

Resolved by using **no alias** on the single-table statements (`length(value)`, `md5(description)`) and `ch`/`pl` only on set 5's join. The gate reads 0 and the query genuinely emits no text — **but the gate is weaker than it reads**, since it catches a future `SELECT ep.value` only if the editor happens to introduce that alias. The real guard is the file header's stated contract plus review.

### 5. [Plan defect] Task 1's B-14 "strictly greater" fixture check is not exercisable on this corpus

The criterion asks that set 6's row count be `>=` set 3's "with at least one fixture making it strictly greater". On a corpus where both are 0, `>=` holds and strict `>` cannot. The structural half of B-14 **is** satisfied and verified — set 6's `WHERE` carries no §8.6-name exclusion and does emit `in_spec_86` — but the mechanical count proof is vacuous here. Re-running the audit against a populated corpus would discharge it.

### 6. `000055` / `000056` were NOT rehearsed against real data

The migration path was the alternative that would have bought this, and it was not taken because the audit needs no column from it. **The maintainer accepted the trade knowingly.** On a 3-character corpus, D-22's duplicate detection would have been a weak rehearsal in any case — but it is a real thing this run did not buy, and it is worth doing deliberately rather than assuming it happened here.

## Requirements bookkeeping — PROFILE-11

`gsd-tools query requirements.mark-complete PROFILE-11` returned, exactly as it has for every other plan in this phase:

```json
{"updated": false, "marked_complete": [], "table_unmatched": ["PROFILE-11"], "write_set_complete": false}
```

The checkbox at `REQUIREMENTS.md:173` was already `[x]` (flipped by `02-03` in wave 2 — seven plans in this phase claim this ID and the verb has no partial-credit model), while the traceability-table row at `:369` still reads `Pending`. **The two halves of the artifact disagree and the verb cannot reconcile the table.** `.planning/REQUIREMENTS.md` is a tool-owned parsed artifact, so it was **not** hand-edited (`.claude/rules/planning-artifacts.md`); reporting the gap is the sanctioned path. This is a tool-behaviour gap for the phase audit, not a state to repair locally.

**This plan's genuine share:** PROFILE-11's `entity_properties` half is now fully discharged — the permit exists (02-07), it is proven to widen (Task 5), and its real-data exposure is audited and recorded (Tasks 3/4b). The **`characters.description` half remains undischarged in Phase 2** by D-29 and moves to Phase 4 with the projection narrowing that makes it safe.

## `<verification_integrity>` — gates demonstrated RED

Rule 4 requires the widening spec be *observed* failing against the pre-`02-07` corpus. It was, twice, each mutation reverted and the suite re-run green:

| Gate | Mutation | RED exit | Observed |
| --- | --- | --- | --- |
| W1's paired control | control expectation flipped `DefaultDeny` → `Allow` | `201` | `6 Passed \| 1 Failed`; effect `0` (default-deny) vs `1` (allow). That corpus **is** the tree as it stood before 02-07, so this is the required RED demonstration, not a hypothetical. |
| Control non-vacuity guard | excluded policy name misspelled | `201` | All 6 specs fail in `BeforeEach` on `controlStore.removed == 1` — proving the control cannot silently become a copy of the full corpus if the name ever stops matching |

The second is the D-29 trap made mechanical: 02-07 already showed that excluding a name which does not exist either fails loudly or silently no-ops depending on how the filter is written. This one fails loudly, and that is asserted rather than hoped for.

Rules 1, 3, 5 and 6 do not apply here and are recorded as such in the plan: no census, no marshaled-response assertion, no opacity contract, and no registry invariant pinned — `INV-ACCESS-10`/`INV-ACCESS-11` bind in Phase 4 against the read path, and annotating an engine-decision spec would bind them to the wrong thing.

## Restore-runbook defects found (NOT edited — out of plan scope)

This branch is the runbook-corrections branch, so these are recorded for whoever owns `site/src/content/docs/operating/how-to/sandbox/sandbox-restore.md`:

1. **The dump is PostgreSQL 18.4 and uses the `\restrict` psql meta-command.** A pre-18 `psql` cannot load it. The runbook names no version floor.
2. **The roles `holomush_plugin_core_channels` and `holomush_plugin_core_scenes` must be pre-created**, or every `OWNER TO` in the dump fails. `POSTGRES_USER=holomush` covers only the primary role.

Both are in addition to the already-known directory trap (`kopia snapshot restore <id> ./backup.sql` produces a **directory**; the dump lands at `backup.sql/holomush-holomush.sql`), which reproduced exactly. The `sfo3` vs `nyc3` endpoint error in `scripts/sandbox.env.example` also still stands.

## Threat mitigations applied

| Threat | Disposition | Where it landed |
| --- | --- | --- |
| T-02-60 (rows relying on colocation as de-facto privacy) | mitigate | Audit run against real data before merge; per-row verdict vocabulary fixed in advance, remedy is the row's `visibility`. Zero such rows found. |
| T-02-61 (descriptions at the anonymous floor) | accept | Result sets 4/5 quantify it: 3 characters, **0** non-empty descriptions. The §8.11 acceptance is informed rather than notional. |
| T-02-62 (`parent_type` guard omitted) | mitigate | W5 pins it; 02-07's S3b demonstrated it RED. |
| T-02-63 (private rows swept in) | mitigate | W4 pins it, paired with a permit on the same fixture. |
| T-02-64 (stale §8.6 list under-reporting) | mitigate | List re-derived from the SPEC in Task 1 and proven **set-equal** by `diff` against `seed.go`'s 23 names — not merely equal in count. |
| T-02-65 (credentials in the recorded result) | mitigate | Environment identified by kopia snapshot id only; no connection string, no credential. Verified by grep. |
| T-02-66 (unverifiable completion claim) | mitigate | The committed query plus the recorded ledger are the artifact, not a summary sentence. |
| T-02-98 (audit discharged without an audit) | mitigate | **This one actually fired.** The plan halted for a session rather than accept an unreachable database, and resumed only against a real restore. |
| T-02-99 (unremediable description exposure) | mitigate | Separate three-option description vocabulary; no description verdict names a `visibility` column `characters` does not have. |
| T-02-106 (player text committed to git history) | mitigate | Ledger emits ids/names/lengths/digests only; detail output written outside the repo and deleted. Both detail sets returned zero rows, so there was none to leak. |
| T-02-107 (production data edited under a read-only task's cover) | mitigate | Task 3's diff touches only the result file; Task 4b performed no write at all. |
| T-02-107 (privacy gate failing open on its own remedy path) | mitigate | The recorded verdict carries an approver and a date, gate-verified line-anchored. See Deviation 1 — this gate very nearly failed open, for a different reason than B-13 anticipated. |
| T-02-108 (adjudicating the published population by count alone) | mitigate | Set 6 covers every public character row with `in_spec_86`; the detail query covers the same population. Vacuous on a 0-row corpus (Deviation 5). |
| T-02-109 (remediation over-reaching the approved set) | n/a | No remediation performed. |

## Known Stubs

None. Every artifact this plan ships has a real body and was exercised.

One thing is **shipped but not fully exercised**, and it is honest to name it: the ledger's per-row verdict machinery — the `in_spec_86` split, the two verdict vocabularies, the digest-based re-check — has never adjudicated a real row, because the corpus had none. Its structure is verified; its behaviour on a populated corpus is not. The re-run is cheap and the artifact is designed for it.

## Verification

| Gate | Command | Result |
| --- | --- | --- |
| Plan `<verification>` | `task test:int -- ./test/integration/access/` | exit **0** |
| Whole-repo unit | `task test` | exit **0** — 11,016 tests, 4 skipped |
| Whole-repo integration | `task test:int` | exit **0** — 11,469 tests, 7 skipped |
| Project rule | `task lint` | exit **0** |
| Project rule | `task fmt` then `task fmt:check` | exit **0**, no formatter drift |
| AC (read-only, both SQL files) | `rg -v '^\s*--' … \| rg -o 'INSERT\|UPDATE\|DELETE\|ALTER\|DROP\|CREATE\|TRUNCATE' \| wc -l` | `0` |
| AC (both ledger sets hash) | `… \| rg -o 'md5\(' \| wc -l` | `2` |
| AC (no text selected in ledger) | `… \| rg -o '\bep\.value\b\|\bc\.description\b' \| wc -l` | `0` |
| AC (seven result sets) | `rg -o '^SELECT$' … \| wc -l` | `7` |
| AC (§8.6 list re-derived) | distinct `'profile.*'` literals | `23`, set-equal to `seed.go` by `diff` |
| AC (§8.6 count reconciliation) | SPEC literal names vs expanded | `15` literal → `23` expanded (the gallery range is elided as "each name a row"); **diff 0** |
| AC (gallery.10 excluded) | `rg -o 'profile\.image\.gallery\.10' … \| wc -l` | `0` |
| AC (detail banner in first 3 lines) | `sed -n '1,3p'` | operator-only / never committed / written outside the repo |
| AC (no could-not-run discharge) | `rg -io 'could not be run\|unavailable\|connection refused' … \| wc -l` | `0` |
| AC (no policy narrowing) | `rg -io 'narrow(ed)? the policy' … \| wc -l` | `0` |
| AC (recorded verdict present) | `rg -o '^## Remediation verdict$' … \| wc -l` | `1` (line-anchored) |
| AC (no `policytest` in the spec) | `rg -o 'policytest' … \| wc -l` | `0` |
| AC (D-29 name absent from the spec) | `rg -o 'seed:profile-public-read-character' … \| wc -l` | `0` |
| AC (no description-permit assertion) | `rg -o 'resource is character\|character:.*description' … \| wc -l` | `0` |
| AC (detail output not in the tree) | `git status --short` | clean |

Every gate is counted with `rg -o … | wc -l` and compared numerically with `-eq`/`-ge`, never `rg -c` — which prints nothing and exits 1 on exactly the zero matches that mean success. That was cycle-2 defect C2-9, and this plan's gates sit on a privacy artifact and a production database, where the cheapest apparent repair for a gate that fails on its own success path is deleting the gate.

## Next Phase Readiness

- **`02-11`** owes, on top of what 02-03 / 02-07 / 02-13 recorded: reconciling Task 5's `<behavior>`-vs-`<action>` contradiction (Deviation 3); the §8.6 description-floor row if it is ever raised to `player`; and the substring-gate defect class from Deviation 1, which is a plan-authoring lesson rather than a code fix. `02-RESEARCH.md`'s schema section is stale and should be corrected: it states `characters` has no `status`/`last_active_at`/normalized-name columns, false since `000054`–`000056` landed on this branch.
- **The merge gate 02-07 declared is now DISCHARGED.** `02-AUDIT-RESULT.md` exists, is non-empty, and records a query that reached a real database. The phase may merge on this axis.
- **Phase 4** inherits `seed:profile-public-read-character`, the projection narrowing that must land with it, and the `characters.description` half of PROFILE-11 — plus `INV-ACCESS-10`/`INV-ACCESS-11`, which bind there against the read path rather than here against an engine decision.
- **Re-run the audit** against any environment that later holds real player data, before the widening reaches it. The query is committed for exactly that, and the ledger's digests make a later run able to prove whether any row changed.
- **`abac-reviewer`** (`/holomush-dev:review-abac`) MUST still see this phase's diff before merge (D-05), now including this plan's control-corpus decorator.

## Self-Check: PASSED

All four created files verified present on disk; the one modified file present. All six commits (`1cc97a105`, `86f7d2b0f`, `31202d461`, `c2c88e937`, `3b6e398a4`, `e117b2620`) resolve via `git cat-file -e`. `02-REMEDIATION.sql` verified **absent**, as a no-remediation verdict requires. The operator-only detail output verified absent from the working tree. Working tree clean at the time of writing.

---
*Phase: 02-abac-schema-vocabulary*
*Completed: 2026-08-05*
