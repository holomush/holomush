---
phase: 02-abac-schema-vocabulary
plan: 12
subsystem: character-identity
tags: [migration, goose, unique-index, backfill, uts39, cli, cobra, postgres]
status: complete

requires:
  - migration 000054 identity columns (02-01)
  - charname.Gate + charname.Admitted + guardSkeleton (02-01, 02-06)
  - worldpostgres.BackfillCharacterIdentity + CharacterRepository.Rename (02-06)
  - setup.NewCharacterNameGate + the block-list transport (02-05, 02-06)
provides:
  - migration 000055 — the Go backfill + duplicate detection (D-21 step B)
  - migration 000056 — SET NOT NULL then CREATE UNIQUE INDEX on normalized_name (D-21 step C)
  - worldpostgres.ClearCharacterIdentity — 000055's real Down
  - internal/testsupport/chartest — the one shared fixture identity helper
  - holomush character name set / duplicates — the operator resolution CLI
  - internal/store/go_migration_census.go — Go migrations in the version helpers
  - the RED-first uniqueness proof, the write-window specs, the duplicates loop
affects:
  - every direct-INSERT character fixture in the tree (NOT NULL + UNIQUE)
  - internal/store version + row-count constants at four sites
  - cmd/holomush (a new top-level command group)
  - INV-WORLD-4's writer count (amendment owned by 02-11)

tech-stack:
  added: []
  patterns:
    - registered goose Go migration as a thin caller of the writer boundary
    - literal census + set-equality meta-test for a view the embed glob cannot see
    - schema staging by withholding one .sql file with the global registry enabled
    - consumer-side narrow write interface on a CLI so a refusal is observable

key-files:
  created:
    - internal/store/migrations/000055_backfill_character_normalized_names.go
    - internal/store/migrations/000056_character_normalized_name_unique.sql
    - internal/store/go_migration_census.go
    - internal/testsupport/chartest/chartest.go
    - cmd/holomush/cmd_character_name.go
    - cmd/holomush/cmd_character_name_test.go
    - cmd/holomush/cmd_character_name_integration_test.go
    - cmd/holomush/root_test.go
    - test/integration/charname/name_uniqueness_test.go
    - test/integration/charname/name_write_window_test.go
    - test/integration/charname/name_duplicates_test.go
  modified:
    - internal/world/postgres/identity_backfill.go
    - internal/store/migrate.go
    - internal/store/migrate_test.go
    - internal/store/migrate_embed_test.go
    - internal/store/migrate_integration_test.go
    - internal/store/migrations_register_test.go
    - internal/store/role_store_integration_test.go
    - internal/bootstrap/setup/adapters.go
    - internal/auth/guest_service.go
    - internal/testsupport/integrationtest/harness.go
    - internal/testsupport/holomushtest/server.go
    - internal/settings/character_store_integration_test.go
    - internal/auth/postgres/player_repo_test.go
    - internal/access/setup/setup_viewer_registration_integ_test.go
    - internal/world/postgres/{postgres,parent_location_resolver,scene_repo,location_repo,binding_repo,cascade_delete,object_repo,character_owner_resolver,identity_backfill}_test.go
    - cmd/holomush/root.go
    - cmd/holomush/gateway_imports_test.go
    - cmd/holomush/admin_authenticate_e2e_test.go
    - test/integration/access/{access_suite,evaluation,seed_policies}_test.go
    - test/integration/auth/{auth_suite,character_create_gate}_test.go
    - test/integration/charname/name_confusable_test.go
    - test/integration/world/world_suite_test.go
    - test/meta/world_import_graph_test.go

decisions:
  - 000055's Down is a REAL revert (ClearCharacterIdentity), not the error-returning stub the plan specified — an erroring Down makes every version below 55 unreachable, including 000054's own Down, and wedged five in-tree specs
  - internal/store/migrations was added to the world-postgres composition allowlist; the alternative is a characters mutation in a Go migration, which the world-SQL fence cannot exempt
  - go_migration_census.go: the version helpers read a .sql-only embed, so the adopt gate seeded a goose ledger with a hole at 55
  - the index is isolated with direct INSERTs, not through Create — the pre-check and the advisory lock both sit above it and are present in either schema
  - the CLI surfaces the gate's own refusal code rather than remapping it (oops resolves the deepest chain code, #4902)
  - the CLI opens its own pool; D-22 aborts startup on collision, so the server is not running when the command is needed
  - the D-30 wave-ordering constraint is discharged by fail-closed NAME_SKELETON_UNVERIFIABLE, not by a wave reorder

metrics:
  duration: ~200min
  completed: 2026-08-05

actuals:
  tokens: 128000
  tasks: 3
  commits: 3
---

# Phase 02 Plan 12: The Database Decides Whether a Name Is Free Summary

`characters.normalized_name` is `NOT NULL` and `UNIQUE` as of migration
`000056`, the backfill and duplicate detection that make that possible ship as
the registered Go migration `000055`, and an operator can report and resolve a
pre-existing collision with `holomush character name` while the server is
refusing to start.

## What was built

**The A→B→C chain.** `000055` is a registered Go migration whose `init()` calls
`goose.AddMigrationContext`. It carries **no SQL of its own** — it calls
`worldpostgres.BackfillCharacterIdentity`, because
`test/meta/world_sql_fence_test.go`'s Go scan does not skip `internal/store` and
its allow marker exists only for `.sql` files. It halts on **both** collision
kinds, labelling each so an operator can tell a normalized-name set (which
`000056` would reject) from a skeleton set (which `000056` will happily let
coexist — D-30 part 3). `000056` does `SET NOT NULL` **then**
`CREATE UNIQUE INDEX`, in that order, with the deployment-precondition block in
its header.

**The fixture repair.** `internal/testsupport/chartest` computes the identity
triple through the same §6.1.1 pipeline the gated writer uses, and every
direct-INSERT character fixture in the tree routes through it. The `UNIQUE`
index had a second consequence the plan anticipated: fixture rows sharing a
literal display name now collide. Names are suffixed from the character's ULID —
from its **tail**, because a ULID's first ten characters are a timestamp and are
identical for every id minted in the same millisecond, which is exactly what a
fixture does (this cost one integration run to discover).

**The operator CLI.** `holomush character name duplicates` runs the migration's
own detection in a transaction it always rolls back;
`holomush character name set <id> <name>` routes the operator's replacement
through the same `charname.Gate` the create path uses and writes through
`CharacterRepository.Rename`, so the write is envelope-atomic and the command
holds no `characters` SQL. Both are registered on `NewRootCmd`, with a
`root_test.go` tree traversal that fails if the `AddCommand` line is removed.

**The transitional branch is gone.** 02-06's
`normalized_name IS NULL AND LOWER(name) = LOWER($1)` predicate is removed at
all its sites, in the same atomic unit as the migration that retires it. There
were **six**, not five: the plan named `adapters.go`, both auth interface
declarations, `harness.go` and `auth_suite_test.go`, and
`test/integration/auth/character_create_gate_test.go` carries a sixth in its own
`gateGuestCharRepo`.

## Gates demonstrated RED (PORTAL-10 rule 4)

This plan carries the milestone's named rule-4 instance. Both halves were
**observed**, not asserted.

### 1. The uniqueness gate against the schema staged at `000055`

The full-chain assertion — a second row holding one uniqueness key is refused by
the database — was run against the schema staged at `000055` (every migration
except `000056`) by pointing the spec's `newCreatePath` at
`stageSchemaWithoutUniqueIndex` instead of `FreshDatabase`.

```
task test:int -- ./test/integration/charname/ \
  -ginkgo.focus='rejects a duplicate uniqueness key at the database'
```

**Exit 201.** The observed verdict is specific and diagnostic:

```
[FAILED] the database itself must refuse a second row holding one uniqueness key
Expected an error to have occurred.  Got:
    <nil>: nil
Ran 1 of 24 Specs — 0 Passed | 1 Failed
```

The duplicate **succeeded** — which is the pre-index state, not a column-missing
error. Restored and green immediately after. That inverted case ships
permanently as the paired negative control ("ACCEPTS the very same duplicate
against the schema staged at 000055").

### 2. The staging precondition can tell a mis-staging from the demonstration

A deliberate mis-staging — `goose.WithDisableGlobalRegistry(true)` added to the
provider, which is precisely the option that removes the **registered** Go
migration 55 — was run once:

```
task test:int -- ./test/integration/charname/ \
  -ginkgo.focus='ACCEPTS the very same duplicate'
```

**Exit 201**, failing as a **SETUP error** rather than as the demonstration:

```
[FAILED] the provider's collected sources must contain the REGISTERED Go
migration 55. If this fails, goose's global registry was disabled when the
provider was built, or internal/store was not imported, and the staged schema
is 54 rather than 55
Expected <bool>: false to be true
```

Restored and green immediately after. **Observed staged values on a correct
run:** the provider's collected sources contain `go:55`, and
`provider.GetDBVersion` reports `55`.

## Verification

| Check | Result |
|---|---|
| `task test` (full untagged suite) | **exit 0** — 11,004 tests, 4 skipped |
| `task test:int` (full) | **exit 0** — 11,467 tests, 7 skipped, 154.9s |
| `task lint` | **exit 0** (includes `lint:no-timestamptz` and `lint:access-migration`) |
| `task fmt:check` | **exit 0** |
| `task build` | **exit 0** |

Docker was available, so the integration half ran in full.

### Mechanical acceptance criteria

| Criterion | Result |
|---|---|
| `AddMigrationNoTxContext` in `000055` | **0** — the step is transactional |
| `TransactionEnabled\|RunTx\|NewGoMigration` in `000055` | **0** — no fixture API |
| fenced `characters` DML in `000055` (comments stripped) | **0** |
| fenced `characters` DML in `cmd_character_name.go` | **0** |
| `ValidateCharacterName` in the CLI's live code | **0** — `Admit` is the whole check |
| `skeleton` in `000056` (comments stripped) | **0** — D-30 part 1 |
| `TIMESTAMPTZ` in `000056` | **0** |
| `-- +goose Up` / `-- +goose Down` in `000056` | **1 each** |
| `SET NOT NULL` line (68) < `CREATE UNIQUE INDEX` line (73) | **yes** |
| `REMOVE with migration 000056` in the tree | **0** |
| `000053` in the uniqueness spec (comments stripped) | **0** |
| `WithDisableGlobalRegistry` in the uniqueness spec (comments stripped) | **0** |
| `NewRootCmd()` in the CLI integration spec | **3** |
| `migrations_register.go` modified | **no** — verified, not touched |
| `test/meta/world_sql_fence_test.go` modified | **no**; `TestNoRawWorldSQLOutsideWriterBoundary` green |
| `latestMigrationVersion` / `expectedAppliedMigrationRows` | **56 / 47**, re-derived from the tree |
| every `INSERT INTO characters` file also names `normalized_name` | all but two, both deliberate (below) |

Two files legitimately omit the identity columns. `test/meta/world_sql_fence_test.go`
holds synthetic parser fixtures that never reach a database (the plan names it as
the sole exclusion). `internal/store/role_store_integration_test.go` stages the
schema at **version 20**, before `000054` adds the columns — a comment at the top
of the file records why it is the one deliberate exception to the `chartest` rule.

## Deviations from Plan

### Deliberate departures from the plan text

**A. `000055`'s Down is a REAL revert, not an error-returning stub.**

The plan specified a Down returning `oops.Code("MIGRATION_IRREVERSIBLE")`. That
was written first and **observed failing 15 in-tree specs** on the first
`task test:int` run: goose rolls migrations down in version order, so a Down that
refuses makes **every version below 55 unreachable** — including `000054`'s own
Down, which drops these very columns. `migrate_clamp_integration_test.go` (five
INV-STORE-9 entries) stages schemas at versions 37–45 by rolling a
fully-migrated database DOWN, and every one of them wedged.

It is also wrong on the merits. `.claude/rules/database-migrations.md` requires
an error **when the up's effect cannot be reverted**; this one can. `characters.name`
is never read or written by the backfill and the three columns are pure functions
of it, so clearing them restores exactly the version-54 shape and a later Up
recomputes byte-identical values. `worldpostgres.ClearCharacterIdentity` is the
inverse, and it lives behind the writer boundary for the same reason the backfill
does. A rollback an operator cannot perform is not a safety property.

Consequence: `MIGRATION_IRREVERSIBLE` does **not** appear in `000055`, and that
acceptance criterion is not met as written. The Down still returns an error on
failure (`CHARACTER_IDENTITY_REVERT_FAILED`); it simply does not refuse on
principle.

**B. The index is isolated with direct INSERTs, not through `CharacterService.Create`.**

The plan's RED design was "two concurrent claims through the real create path;
against `000055` **both succeed**". Two layers falsify that, and both were
observed:

1. **02-06's `guardSkeleton`** takes a transaction-scoped advisory lock keyed on
   the skeleton and re-checks inside the write transaction. Two claims of one
   name share a skeleton, so the loser is refused `NAME_CONFUSABLE` — in **both**
   schemas, index or no index.
2. **The `ExistsByNormalizedName` pre-check** catches a non-concurrent duplicate
   and returns `CHARACTER_NAME_TAKEN` — again in both schemas. The first draft of
   the negative control failed exactly this way (`CHARACTER_NAME_TAKEN` where
   "no error" was expected at `000055`).

A negative control routed through `Create` therefore goes red at `000055` for the
**wrong reason** — refused by a check present in both schemas — which is the same
class of non-diagnostic failure the plan corrected the `000053` staging for. The
shipped pair goes straight at the table: the identical INSERT is refused with
23505 on `characters_normalized_name_key` with the index present and **accepted**
without it. That isolates the index and nothing else.

The concurrent-claim spec still ships, asserting exactly one commit and exactly
one row, and accepts either `NAME_CONFUSABLE` or `CHARACTER_NAME_TAKEN` from the
loser — pinning one would make it a test of which guard won the race rather than
of the guarantee. A third spec pins the caller-visible `CHARACTER_NAME_TAKEN`
contract on the pre-check path, using a decoupled skeleton so the gate's
confusable step (which runs first) is blind to the seeded row.

**C. The duplicates spec DOES call `CharacterRepository.Rename`.**

The plan's action text requires resolving each pair through `Rename` with a
gate-minted token, and an acceptance criterion requires `000056` to apply after
resolution. A separate criterion asserts `rg -o '\.Rename\('` counts **0** in that
file. The two cannot both hold; the action and the resolve-then-apply criterion
win, and the count is **1**. The criterion's real intent — that the spec must not
stand in for the CLI — is honoured: the file's doc comment states it explicitly
and names `cmd/holomush/cmd_character_name_integration_test.go` as where the CLI
surface is exercised.

**D. 02-06's NULL-visibility test does not exist, so nothing was deleted.**

02-06's Known Stubs table names "the test asserting a NULL-normalized row IS
reported as existing" as this plan's to delete. A whole-tree scan
(`rg 'normalized_name IS NULL' --type go`, plus the `ExistsByNormalizedName`
declaration and test sites) finds no such test: 02-06 shipped the transitional
predicate and its five-site parity check but not that assertion. Nothing was
removed, and the criterion asserting a whole-tree count of zero for
`normalized_name IS NULL` is **not met and cannot be** — the production backfill
scan at `internal/world/postgres/identity_backfill.go:102` uses that exact
predicate as its row filter and must.

### Auto-fixed issues

**1. [Rule 3 — Blocking] The world-postgres composition allowlist rejected the migration**

- **Found during:** Task 1, first `task test -- ./test/meta/`
- **Issue:** `TestWorldPostgresCompositionAllowlist` (INV-WORLD-4's import-graph
  half) failed: `internal/store/migrations` imports `internal/world/postgres` and
  was not allow-listed.
- **Fix:** added the entry with the reason — the alternative is a
  `UPDATE characters` inside a Go migration, which `world_sql_fence_test.go`
  structurally **cannot** exempt (its allow marker globs `*.sql`). The migration
  constructs no repository; it holds one free-function call.
- **Files:** `test/meta/world_import_graph_test.go`
- **Commit:** `7c71a8903`

**2. [Rule 2 — Missing critical functionality] Go migrations were invisible to the version helpers, and the adopt gate seeded a ledger with a hole**

- **Found during:** Task 1, first full `task test:int` (9 adopt specs red)
- **Issue:** `loadMigrationVersions` and `loadMigrationNames` read the embedded
  FS, and `//go:embed migrations/*.sql` globs `.sql` only. Before the first Go
  migration those helpers happened to describe the whole chain; they no longer
  did. The adopt gate seeds one goose ledger row per version it knows about, so
  version 55 was recorded as **not applied** while 56 **was**, and the next Up
  refused with `detected 1 missing (out-of-order) migration lower than database
  version (56): version 55`.
- **Fix:** `internal/store/go_migration_census.go` — a literal census merged into
  both helpers, held to the on-disk corpus in both directions by
  `TestGoMigrationCensusMatchesTheMigrationsDirectory`. A named import of the
  migrations package was NOT an option:
  `TestExactlyOneBlankImportWiresTheMigrationsPackageIntoStore` fails any
  non-blank import of it from `package store`.
- **Files:** `internal/store/go_migration_census.go`, `internal/store/migrate.go`,
  `internal/store/migrations_register_test.go`, `internal/store/migrate_test.go`
- **Commit:** `7c71a8903`

**3. [Rule 1 — Bug] Fixture names built from a ULID PREFIX collide by construction**

- **Found during:** Task 1, second `task test:int`
- **Issue:** `"TestChar_"+charID.String()[:8]` and the `[:6]` suffixes added to
  several other fixtures produced `duplicate key value violates unique constraint
  "characters_normalized_name_key"`. A ULID string is 10 characters of timestamp
  followed by 16 of randomness, so a prefix slice is **identical** for every id
  minted in the same millisecond.
- **Fix:** every fixture-name suffix takes `id.String()[20:]` — six characters of
  real entropy — with the reason recorded at each helper.
- **Files:** the `world/postgres`, `access`, `settings`, `auth/postgres` and
  `test/integration/world` fixtures
- **Commit:** `7c71a8903`

**4. [Rule 1 — Bug] `internal/world/postgres`'s backfill specs could not stage their own subject**

- **Found during:** Task 1, second `task test:int`
- **Issue:** `identity_backfill_test.go` seeds rows with NULL identity columns and
  duplicate keys — the pre-backfill shape, un-insertable once `000056` applies.
- **Fix:** `stagePreConstraintSchema` relaxes the constraint for one test and
  restores it in a `t.Cleanup` registered FIRST, so it runs LAST (after every
  fixture row is deleted) and the index can be recreated. The restore reports a
  leftover row as a test failure rather than swallowing it.
- **Files:** `internal/world/postgres/identity_backfill_test.go`
- **Commit:** `7c71a8903`

**5. [Rule 1 — Bug] 02-01's "a stock database is not a verifiable corpus" spec inverted**

- **Found during:** Task 1
- **Issue:** that spec asserts `count(*) WHERE name_skeleton IS NULL > 0` on a
  freshly migrated database — true only because `000001_baseline.sql:397` seeds a
  bootstrap character with no skeleton and nothing backfilled it. `000055` now
  does, as part of the chain. Leaving the spec would have made this plan's central
  deliverable read as a regression.
- **Fix:** the spec's verdict is inverted and renamed — a stock database now
  carries **zero** NULL skeletons and admits an ordinary name with no fixture
  repair — with a paired control that the gate is still adjudicating. The
  fail-closed behaviour it used to demonstrate is not lost: it lives in the
  neighbouring "newly inserted unbackfilled row" spec, which is the durable
  hazard (an interrupted post-Unicode-upgrade recompute) rather than the transient
  repo-state artifact.
- **Files:** `test/integration/charname/name_confusable_test.go`
- **Commit:** `7c71a8903`

**6. [Rule 3 — Blocking] The CLI tripped the gateway import fence**

- **Found during:** Task 2
- **Issue:** `TestGatewayImportsAreOnlyProtocolTranslation` (INV-EVENTBUS-1)
  flagged `internal/store`, `internal/world/postgres` and `internal/world/wmodel`.
- **Fix:** the three `cmd_character_name*` files were added to `coreOnlyFiles`
  with the reason — a host-shell operator tool, same precedent as `migrate.go`
  and `cmd_admin.go`. The forbidden-package policy list was NOT widened.
- **Files:** `cmd/holomush/gateway_imports_test.go`
- **Commit:** `1e1cef78b`

**7. [Rule 1 — Bug] Nine lint findings on first-pass code**

Three staticcheck `QF1012` (`WriteString(fmt.Sprintf(...))` → `fmt.Fprintf`),
five errcheck on the report's `fmt.Fprint*` calls (fixed by rendering into a
buffer and doing one checked write, so a half-printed collision report is not a
reachable state), and one revive `error-return` on a helper returning
`(error, string)`. `.golangci.yaml` was never widened.
**Commits:** `7c71a8903`, `1e1cef78b`.

## Deployment precondition (recorded, per Task 1)

`000054`, `000055` and `000056` are **one release**. Character writers MUST be
quiesced across the `000055` → `000056` boundary; goose's advisory lock
serializes migrators, not application writers. Three acceptable ways to satisfy
it: drain all character writers before `000055`; run the pair with one replica
up; or rely on the single-replica `compose.prod.yaml` topology, where migrations
complete before `orch.StartAll` and the window is empty by construction. It is
**not** empty for `compose.cluster.yaml`.

A violation fails **loudly** — "column contains null values" on the ALTER, or a
duplicate-key error on the index — so the exposure is a repeatedly-failing
rollout, not corruption. `name_write_window_test.go` observes both, and observes
that the schema is left at `000055` rather than half-constrained.

A deployment that applies `000054` and stops refuses **every** character create
and rename with `NAME_SKELETON_UNVERIFIABLE` until the chain completes — the
correct direction, closed by construction when the three ship together, but it
presents as "character creation is broken".

**D-30 sequencing (recorded as a decision).** The gate goes live in wave 4 and
the backfill lands in wave 5, so on the face of it the gate precedes the
backfill. It is discharged by a stronger mechanism than a reorder: 02-01's
`SkeletonLookup` sets `unverifiable` whenever any row has `name_skeleton IS NULL`,
and both `Gate.Check` and `guardSkeleton` fail closed. The gate never returns a
false "no collision" — it refuses. A reorder would close only the repo-state
artifact, not the durable hazard (an interrupted recompute), and would cost
splitting 02-06 into three plans across two new waves. In deployment the gap does
not exist at all.

## Human-check: the duplicate report from a real database

Not run — there is no production database in this worktree, and the phase has not
shipped. The `<human-check>` on Task 2 is an **operator obligation at deploy
time**, and it is recorded in `000056`'s header rather than only here: before
`000056` is applied to any database holding real characters, run
`holomush character name duplicates` and resolve every reported set. `000055`'s
own halt is the backstop if that step is skipped — the migration aborts startup
rather than proceeding (D-22).

## Requirements

Frontmatter names **IDENT-09**, and it is discharged here: uniqueness is decided
by a database `UNIQUE` index over `characters.normalized_name`, proven against
real Postgres and demonstrated RED against the unindexed schema first.

`.planning/REQUIREMENTS.md` was NOT hand-edited.

## Known Stubs

None. Two seams remain, both with named owners and neither a placeholder:

| Seam | Owner |
|---|---|
| `INV-WORLD-4`'s "exactly **TWO** sanctioned out-of-world writers" is now false — the operator name-resolution command is a **third** | plan `02-11` (the phase's amendment pass, wave 6, already depends on this plan) |
| The `backfillCharacterSkeletons` fixture stand-ins in four test trees are now dead (000055 does the work) but were left in place | harmless no-ops; removing them is churn with no behavioural change |

`docs/architecture/invariants.yaml` is deliberately absent from this plan's
`files_modified`: plan `02-09` edits it in the same wave, so an edit here would
be a same-wave collision. **The mechanism that makes the third writer conformant
already exists** — 02-06 made `Rename` envelope-atomic inside its own transaction
— and `cmd_character_name_integration_test.go` asserts the property by observing
the outbox row after a CLI rename. Only the registry TEXT is outstanding. A
reader of this plan alone must not conclude the count is still two.

## Threat Flags

None. Every security-relevant surface this plan introduces is enumerated in its
threat register (T-02-29, T-02-32/33/34/35/36, T-02-78/79, T-02-100/101/102/103/104/105).
`worldpostgres.ClearCharacterIdentity` is new and outside it: it writes only the
three DERIVED columns, never `characters.name`, is reachable only from `000055`'s
Down, and introduces no new trust boundary — the same shape as the backfill it
inverts.

## Invariants

This plan pins no registry invariant and writes no `// Verifies:` annotation.
It works WITHIN `INV-WORLD-4`'s writer-boundary fence:
`test/meta/world_sql_fence_test.go` is **unmodified** and
`TestNoRawWorldSQLOutsideWriterBoundary` is green. Its import-graph half
(`world_import_graph_test.go`) gained one allowlist entry, documented above. No
ad-hoc `INV-NAME-*` family was minted.

## Post-landing fix: a production concurrency defect the spec caught

An independent re-run of `task test:int -- ./test/integration/charname/...`
failed roughly 2 runs in 4 on this plan's concurrent-claim spec, in two
different ways: `Expect(chars[i]).NotTo(BeNil())` firing inside the
`errs[i] == nil` branch, and BOTH claims refused with the SYNTACTIC rule
(`must contain letters and spaces only`) on the literal constant `"Brenna"`.

**Root cause — a real production bug, not a test defect.**
`internal/charname/pipeline.go` held two **stateful** `golang.org/x/text`
transformers in package-level vars: `transform.Chain(norm.NFKC,
runes.Remove(runes.In(unicode.Cf)))` and `cases.Fold()`. `cases.Caser`'s own doc
states "A Caser may be stateful and should therefore not be shared between
goroutines", and `transform.Chain` returns a `*chain` carrying mutable `link`,
`err` and `errStart` fields plus a read/write buffer per link. `Normalize` is
called by **every** character create, so two players creating at the same moment
interleave one call's `Reset` with another's `Transform`.

Both observed manifestations follow directly:

- **the syntactic refusal** is a truncated or garbled `display` reaching
  `syntax.ValidateName`. The input the validator saw was not the string the
  caller passed — exactly the possibility the coordinator flagged.
- **the "nil character, nil error"** was never returned by `Create` at all. The
  goroutine **panicked** inside `Normalize` (`slice bounds out of range` in
  `transform.String`), so `defer wg.Done()` ran, the assignment never happened,
  and the main goroutine read a zero-valued slot: `errs[i] == nil` counted as a
  win and `chars[i]` was nil.

**Made deterministic before fixing.**
`internal/charname/pipeline_concurrency_test.go` runs `Normalize` from 1,000
goroutines over five fixtures and asserts **equality with the sequential
result** — not merely that nothing panicked, since a corrupted transformer
usually returns a wrong string rather than crashing.

```
task test -- -run 'TestNormalizeIsSafeForConcurrentUse|TestSkeletonIsSafeForConcurrentUse' ./internal/charname/
```

**Exit 201**, three concurrent panics with the stack pointing straight at the
shared chain:

```
panic: runtime error: slice bounds out of range [36:6]
  golang.org/x/text/transform.String(...)  transform.go:650
  charname.Normalize(...)                  pipeline.go:66
panic: runtime error: slice bounds out of range [36:18]
  golang.org/x/text/transform.(*link).src(...)      transform.go:370
  golang.org/x/text/transform.(*chain).Transform(...) transform.go:422
```

`TestSkeletonIsSafeForConcurrentUse` passed in the same RED run — the paired
control proving the defect is about **shared state**, not about x/text being
unusable concurrently (`norm.Form` is a `type Form int` whose `String` allocates
its own buffer).

**Fix.** The pipeline now uses only stateless forms: `norm.NFKC.String`, a pure
`strings.Map` for the Cf strip, and a `Caser` constructed per call and never
escaping. The doc comment says why, and says not to hoist them back for the
allocation. The transform-error branch became unreachable and was removed; the
empty-normal-form check still catches everything it caught, and the package's
existing behavioural table is unchanged and green (173 tests).

**Spec hardening.** The concurrent-claim spec now records that `Create`
RETURNED, so a future panic in the create path reports itself instead of
masquerading as a nil winner. That misdirection is what made the original
diagnosis expensive.

**Proof.**

| Run | Result |
|---|---|
| `internal/charname` concurrency test, BEFORE the fix | **exit 201** — three panics |
| `internal/charname` concurrency test, AFTER | **exit 0** |
| `task test:int -- ./test/integration/charname/` ×5 | **0, 0, 0, 0, 0** |
| the concurrent-claim spec focused, ×10 | **0 ×10** |
| `task test` | **exit 0** — 11,016 tests |
| `task test:int` (full) | **exit 0** — 11,469 tests, 157.0s |
| `task lint` / `task fmt:check` | **exit 0** |

Not quarantined, and it should not have been: the cause is a shared-mutable-state
race with a deterministic reproduction, which is precisely the case
`.claude/rules/testing.md` excludes from quarantine.

**Commit:** `8e98127d3`.

## Self-Check: PASSED

All eleven created artifacts exist on disk. All three commits (`7c71a8903`,
`1e1cef78b`, `0a531d2ac`) resolve in `git log`. Working tree clean before this
document.
