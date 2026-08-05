---
phase: 02-abac-schema-vocabulary
reviewed: 2026-08-05T00:00:00Z
depth: standard
files_reviewed: 26
files_reviewed_list:
  - cmd/holomush/cmd_character_name.go
  - cmd/holomush/core.go
  - cmd/holomush/root.go
  - cmd/holomush/sub_grpc.go
  - cmd/internal/gen-confusables/main.go
  - internal/access/prefix.go
  - internal/access/profilevis/profilevis.go
  - internal/auth/character_genesis.go
  - internal/auth/character_service.go
  - internal/auth/guest_service.go
  - internal/bootstrap/setup/adapters.go
  - internal/bootstrap/setup/subsystem.go
  - internal/charname/admission.go
  - internal/charname/blocklist/blocklist.go
  - internal/charname/blocklist/cache.go
  - internal/charname/blocklist/poller.go
  - internal/charname/blocklist/subsystem.go
  - internal/charname/doc.go
  - internal/charname/gate.go
  - internal/charname/mixedscript.go
  - internal/charname/pipeline.go
  - internal/charname/skeleton.go
  - internal/charname/syntax/syntax.go
  - internal/grpc/auth_handlers.go
  - internal/lifecycle/subsystem.go
  - internal/store/go_migration_census.go
  - internal/store/migrate.go
  - internal/store/migrations/000054_character_identity_and_lifecycle.sql
  - internal/store/migrations/000055_backfill_character_normalized_names.go
  - internal/store/migrations/000056_character_normalized_name_unique.sql
  - internal/store/postgres.go
  - internal/store/role_store.go
  - internal/world/character.go
  - internal/world/lifecycle.go
  - internal/world/postgres/character_owner_resolver.go
  - internal/world/postgres/character_repo.go
  - internal/world/postgres/identity_backfill.go
  - internal/world/postgres/skeleton_guard.go
  - internal/world/repository.go
  - internal/world/validation.go
  - internal/testsupport/integrationtest/harness.go
  - Taskfile.yaml
findings:
  critical: 1
  warning: 8
  info: 5
  total: 14
status: issues_found
---

# Phase 02: Code Review Report

**Reviewed:** 2026-08-05
**Depth:** standard (config requested `deep`; workflow downgraded at 151 files)
**Files Reviewed:** 42 read closely (list above); remainder triaged
**Status:** issues_found

## Summary

Scope triage, stated plainly so the coverage of this review is legible:

**Read line-by-line (production code):** the whole of `internal/charname/` (pipeline,
skeleton, mixedscript, gate, admission, syntax, doc) and `internal/charname/blocklist/`
(blocklist, cache, poller, subsystem); `internal/world/postgres/{skeleton_guard,
character_repo, identity_backfill, character_owner_resolver}.go`; all three migrations
plus `internal/store/{migrate,go_migration_census,postgres,role_store}.go`;
`cmd/holomush/cmd_character_name.go` and the `core.go`/`sub_grpc.go`/`root.go` wiring
diffs; `internal/auth/{character_service,guest_service,character_genesis}.go`;
`internal/bootstrap/setup/{adapters,subsystem}.go`; `internal/world/{character,
lifecycle,validation,repository}.go`; `internal/grpc/auth_handlers.go` diff;
`internal/access/{prefix.go,profilevis/profilevis.go}`; `cmd/internal/gen-confusables/
main.go`; `Taskfile.yaml` diff.

**Deliberately triaged past (already covered by the `abac-reviewer` READY pass, whose 8
findings are filed as #4933–#4936):** `internal/access/policy/attribute/{player,
property,viewer}.go`, `internal/access/policy/seed.go`, `internal/access/setup/*`,
`internal/admin/section/*`. I read `prefix.go` and `profilevis.go` anyway looking for
non-ABAC defects and found none beyond what is already filed.

**Read as evidence, not style-reviewed:** the test corpus. Two observations from it are
reported below (WR-05, WR-07) because they are claims about production wiring that the
tests do not in fact prove.

The design work here is unusually careful — the advisory-lock skeleton guard, the
compile-then-swap block-list cache, the three-migration ordering and the `Admitted`
token fence are all correctly built and correctly argued. The defects below are almost
all at the *edges* the doc comments assert are covered but are not: a hardcoded game id
in the new CLI writer, a generated security table with no drift gate despite a comment
claiming one, and a swallowed error in the guest retry loop.

## Critical Issues

### CR-01: Operator rename writes its world-feed envelope under a phantom game id

**File:** `cmd/holomush/cmd_character_name.go:367`
**Issue:** `characterRenameIntent` hardcodes `GameID: "main"`. Nothing in this system uses
`"main"` as a game id. The live value is `cfg.GameID` when explicitly configured, and
otherwise a fresh ULID generated and stored in `holomush_system_info.game_id`
(`internal/store/postgres.go:132-147`, resolved through `gameIDProvider` at
`cmd/holomush/core.go:302-307`). Every other envelope producer threads that value
(`internal/world/service.go:352,918,1076`, `internal/auth/character_genesis.go:233`,
`internal/world/postgres/genesis_store.go:143`).

Three consequences, all silent:

1. The outbox row is never relayed. `OutboxLease` reads
   `WHERE game_id = $1 AND published_at IS NULL`
   (`internal/world/postgres/outbox_lease.go:138`) with the relay's real game id, so a
   `game_id='main'` row is invisible to the publisher forever.
2. The row is never pruned — `DELETE FROM outbox WHERE game_id = $1 AND published_at IS
   NOT NULL` (`outbox_lease.go:224`) has the same filter — so the table accumulates one
   unreachable row per operator rename.
3. `FeedCounter.Allocate` creates a stray `world_feed_counter` row for the phantom game
   (`internal/world/postgres/feed_counter.go:82-88`), because the INSERT is
   `ON CONFLICT DO NOTHING` and takes any string.

The character row itself commits correctly, so the rename *appears* to succeed and prints
`renamed <id> to "<name>"`. The world feed simply never sees it. That is exactly the
failure `CharacterRepository.Rename`'s doc comment
(`internal/world/postgres/character_repo.go:184-192`) says writing the envelope inside the
repository transaction eliminates — INV-WORLD-4 is satisfied structurally (a row *is*
written) and violated in effect (no consumer can ever read it). The unit test drives a
fake `characterRenamer` and the integration test does not assert on `outbox.game_id`, so
nothing catches it.

**Fix:** resolve the game id from the database the command already has open, exactly as
the server does, and fail loudly rather than defaulting:

```go
// in defaultCharacterNameEnvFactory, after `settings` is constructed:
gameID, err := settings.GetSystemInfo(ctx, "game_id")
if err != nil {
    teardown()
    return nil, nil, oops.Code("CHARACTER_NAME_CLI_GAME_ID_FAILED").
        With("command", "character name").Wrap(err)
}
// carry gameID on characterNameEnv and thread it through:

func characterRenameIntent(gameID string, characterID ulid.ULID) wmodel.EnvelopeIntent {
    return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
        GameID: gameID,
        // ... unchanged
    })
}
```

Do **not** call `InitGameID` here — that would *generate* an id against a database whose
server has not booted. A missing `game_id` row must be an error, not a mint.

Add an assertion to `cmd/holomush/cmd_character_name_integration_test.go` that the outbox
row written by `character name set` carries the same `game_id` the harness seeded.

## Warnings

### WR-01: Guest creation reports a gate/database outage as name exhaustion, silently

**File:** `internal/auth/guest_service.go:250-260`
**Issue:** `acquireUniqueName` discards `admitErr` unconditionally and `continue`s. The
comment justifies this for the two *policy* refusals (block-list hit, skeleton collision),
but `Gate.Admit` also returns:

- `NAME_SKELETON_LOOKUP_FAILED` — the corpus query failed (DB down, pool exhausted)
- `NAME_SKELETON_UNVERIFIABLE` — a `name_skeleton IS NULL` row exists, the documented
  fail-closed state between migrations 000054 and 000055 (`internal/charname/gate.go:183`)

Under either, *every* one of the `maxGuestNameRetries` iterations fails identically and
the caller receives `GUEST_NAME_EXHAUSTED` — "unable to find unique guest name after N
attempts". No log line is emitted at any level. An operator debugging "guests cannot log
in" is pointed at the name generator, which is healthy. The sibling
`ExistsByNormalizedName` failure two lines below is correctly surfaced, which makes the
asymmetry look accidental rather than considered.

**Fix:** log every refusal, and distinguish infrastructure from policy:

```go
token, admitErr := s.gate.Admit(ctx, charName)
if admitErr != nil {
    s.namer.ReleaseGuest(name)
    if code, ok := oops.AsOops(admitErr); ok {
        if c, _ := code.Code().(string); c == "NAME_SKELETON_LOOKUP_FAILED" || c == "NAME_SKELETON_UNVERIFIABLE" {
            return "", charname.Admitted{}, oops.Code("GUEST_CREATE_FAILED").
                With("name", name).Wrap(admitErr)
        }
    }
    slog.WarnContext(ctx, "guest name candidate refused by the name gate",
        "name", charName, "error", admitErr)
    continue
}
```

### WR-02: The generated confusables table has no drift gate, contrary to two doc comments

**Files:** `internal/charname/doc.go:26-29`, `cmd/internal/gen-confusables/main.go:40-42`,
`Taskfile.yaml:607-619`
**Issue:** Both comments assert CI protection that does not exist:

- `doc.go:26-29` — "`task generate` plus the Taskfile's sources:/generates: declaration
  make a stale or edited table a visible diff in CI." There is no `generate` task.
  `rg '^  generate' Taskfile.yaml` yields only `generate:schema`, `generate:confusables`,
  `generate:luabridge`, `generate:ebnf`, `generate:ebnf:check` — no umbrella.
- `main.go:41-42` — "Taskfile's sources:/generates: declaration and CI's drift check make
  that a single change or a red build." `pr-prep:fast:run` hashes and regenerates exactly
  two artifacts (`Taskfile.yaml:1118-1133`: the plugin schema and the Lua bindings);
  `.github/workflows/ci.yaml:80` runs only `task generate:schema`. `generate:confusables`
  is invoked by nothing.

go-task's `sources:`/`generates:` provide *up-to-date skipping*, not verification — they
never fail a build. The only committed check is `internal/charname/version_test.go`, which
compares the two version *strings*; it cannot see a hand-edited or deleted mapping in the
table body. The doc itself calls this table "a security gate (character-name
impersonation)" (`main.go:17`), which is precisely the artifact that needs the gate the
comments promise.

**Fix:** add a hash-compare step to `pr-prep:fast:run` mirroring the schema block. Because
the generator downloads by default, use the offline path against a checked-in fixture, or
gate on network availability:

```yaml
      - echo "▸ Verifying the confusables table is current..."
      - cmd: |
          TABLE=internal/charname/confusables_table_gen.go
          BEFORE=$(sha256sum "$TABLE" | cut -d' ' -f1)
          go generate ./internal/charname/
          AFTER=$(sha256sum "$TABLE" | cut -d' ' -f1)
          if [ "$BEFORE" != "$AFTER" ]; then
            echo "ERROR: confusables table out of sync. Run 'task generate:confusables' and commit."
            exit 1
          fi
```

If a network dependency in `pr-prep` is unacceptable, correct both doc comments to say so
rather than leaving them asserting a gate that is absent.

### WR-03: `characters.name_skeleton` stays nullable forever, so one NULL row disables character creation server-wide

**Files:** `internal/store/migrations/000056_character_normalized_name_unique.sql:66-69`,
`internal/world/postgres/skeleton_guard.go:102,110-116`,
`internal/charname/gate.go:183-187`
**Issue:** Migration 000056 constrains `normalized_name` (`SET NOT NULL` + unique index)
but leaves `name_skeleton` unconstrained. Both the gate's corpus lookup
(`skeleton_guard.go:165`) and the in-transaction guard (`skeleton_guard.go:102`) evaluate
`EXISTS(SELECT 1 FROM characters WHERE name_skeleton IS NULL)` over the *whole table* and
fail closed with `NAME_SKELETON_UNVERIFIABLE` on any hit. So a single row acquiring a NULL
skeleton — a direct-SQL touch, a hand-written data fix, an interrupted future Unicode
recompute, or a restore from a partially-backfilled dump — refuses **every** character
create and **every** rename for the entire deployment, with an error message
("try again shortly") that never resolves.

The design deliberately keeps the *index* non-unique (D-30 part 1, correct), but that
argument does not apply to nullability. After 000055 backfills, every row is non-null and
the only writers (`Create`, `Rename`) always populate it. The migration comment even names
this as an operational hazard (`000056:57-64`) without closing it.

**Fix:** add `ALTER TABLE characters ALTER COLUMN name_skeleton SET NOT NULL;` alongside
the `normalized_name` constraint in 000056 (with the matching `DROP NOT NULL` in its Down).
The `EXISTS(... IS NULL)` guard then becomes belt-to-braces rather than a live global
kill-switch. If nullability must be retained for a future recompute strategy, say so in
the migration and scope the unverifiable check to the skeleton under adjudication rather
than the whole corpus.

### WR-04: `blocklist.Subsystem.Prepare` documents idempotence but panics on a second call

**File:** `internal/charname/blocklist/subsystem.go:120-134`
**Issue:** The doc says "Prepare is idempotent: re-running it simply recompiles the current
value." It also calls `s.cfg.Registry.Register(...)` unconditionally at line 128, and
`ReadinessRegistry.Register` panics on a duplicate id
(`internal/lifecycle/registry.go:33-35`). A second `Prepare` without an intervening `Stop`
therefore crashes the process. The `Stop` method's own comment (lines 194-196) shows the
author was aware of the panic and solved it only for the Stop-then-Prepare path.

**Fix:** make the registration itself idempotent, or make the doc honest:

```go
	if s.cfg.Registry != nil && !s.registered {
		s.cfg.Registry.Register(lifecycle.SubsystemCharacterNameBlockList, s.tracker)
		s.registered = true
	}
```

(and clear `s.registered` in `Stop` beside the existing `Unregister`).

### WR-05: The integration harness bypasses `NewCharacterNameGate`, so no test proves the block list is wired

**File:** `internal/testsupport/integrationtest/harness.go:459,1783`
**Issue:** The harness builds `&charname.Gate{Skeletons: worldpg.NewSkeletonLookup(pool)}`
directly — twice — with a nil `BlockList`. The comment at `harness.go:455-458` claims the
harness "runs the same admission decision production runs"; it does not. The block-list
term is absent, so the highest-fidelity Go tier cannot observe a regression in which
production stops passing the matcher.

This matters more than a normal test-fidelity gap because the entire argument for
`NewCharacterNameGate` existing (`internal/bootstrap/setup/adapters.go:22-38`) is "there is
exactly ONE of these, and that is the point". The property is enforced by convention at
three production roots and broken by two harness call sites; nothing structural prevents a
fourth root from copying the harness shape and shipping a decorative block list.

**Fix:** have the harness stand up a real `blocklist.Subsystem` over its pool and call
`bootstrapsetup.NewCharacterNameGate(pool, blockListSub)`. That also gives the whole-system
tier a place to prove IDENT-07 end to end. If a nil block list is genuinely wanted for
speed, correct the comment to say the harness runs a *subset* of the production decision.

### WR-06: An empty block-list pattern compiles to a match-everything regex

**File:** `internal/charname/blocklist/blocklist.go:60-74`
**Issue:** `Compile` passes each element straight to `regexp.Compile`. `regexp.Compile("")`
succeeds and the resulting regexp matches every string. Since v0.13's only edit path for
this key is direct SQL (stated at `blocklist.go:15-19`), the realistic accidents —
`'["", "^admin$"]'`, a trailing comma producing an empty element, an `UPDATE` that writes
`'[""]'` — all install a list that rejects **every** character name with `NAME_BLOCKED`
("that character name is not available"), including the bootstrap admin character.

The package doc argues at length (lines 21-29) that no execution-time defense is warranted
because RE2 is linear, and that "boot validation surfaces a malformed pattern to the
operator who wrote it, and that is the whole of the defense". An empty pattern is not
malformed to `regexp.Compile`, so boot validation passes it.

**Fix:** reject the empty pattern in `Compile`, where the operator-facing index is already
in hand:

```go
	for i, p := range patterns {
		if p == "" {
			return nil, oops.Code("BLOCKLIST_PATTERN_INVALID").
				With("index", i).
				Errorf("block-list entry %d is the empty pattern, which matches every name", i)
		}
		re, err := regexp.Compile(p)
```

### WR-07: `describeCollisionKind` is duplicated with divergent operator-facing text

**Files:** `internal/store/migrations/000055_backfill_character_normalized_names.go:141-151`,
`cmd/holomush/cmd_character_name.go:266-275`
**Issue:** Two independent implementations of the same operator-facing label, with
different strings for the same kind. The migration prints
`"NORMALIZED-NAME (same §6.1.1 uniqueness key; migration 56's UNIQUE index would reject these)"`;
the CLI prints `"NORMALIZED-NAME"`. The migration's own halt message tells the operator to
run `holomush character name duplicates` — so the operator reads both renderings of the
same data, five minutes apart, and they do not match. The surrounding comments in both
files argue at length that the two kinds must be labelled distinctly *because* an
undifferentiated report invites dismissal; that argument applies equally to two divergent
reports of the same set.

**Fix:** export one renderer from `internal/world/postgres` (which already owns
`IdentityCollisionKind`) and have both call sites use it. The whole report body —
`formatCollisionReport` and `printDuplicateReport` differ only in their header — is a
better extraction boundary.

### WR-08: Bare `slog.Warn` in a path whose caller holds a context

**File:** `cmd/holomush/cmd_character_name.go:260`
**Issue:** `printDuplicateReport` logs with bare `slog.Warn`. The rule in
`.claude/rules/logging.md` is a MUST for context-carrying variants "whenever a
`context.Context` is in scope", with the carve-out reserved for cases where one "cannot
reasonably be plumbed". The only caller (`cmd_character_name.go:207`) has `ctx` two lines
above and passes it to `reportCharacterNameDuplicates` already. `sloglint`'s
`context: scope` check will not fire because the function signature omits `ctx`, which
makes this a rule violation the linter structurally cannot catch — the signature is the
bug.

**Fix:** `func printDuplicateReport(ctx context.Context, w io.Writer, sets []...)` and
`slog.WarnContext(ctx, ...)`. The sibling `slog.WarnContext` at line 349 in the same file
is the correct shape.

## Info

### IN-01: The confusables parser never checks the mapping-type field

**File:** `cmd/internal/gen-confusables/main.go:231-243`
**Issue:** `parseConfusables` accepts any line with three or more `;`-separated fields and
ignores `fields[2]`, which is the UTS #39 mapping type. The generated table's doc comment
and the emitter both call this "the UTS #39 MA (multi-script any-case) mapping"
(`main.go:307-308`), a claim the parser does not enforce. Harmless today because the input
is SHA-256 pinned and current `confusables.txt` revisions carry only `MA` rows, but a
future pin refresh to a revision that reintroduces `SL`/`ML`/`SA` rows would silently widen
the table.
**Fix:** `if strings.TrimSpace(fields[2]) != "MA" { continue }`, and say so in the doc.

### IN-02: `blocklist.NewSubsystem` accepts a nil `Source` and defers the panic to Prepare

**File:** `internal/charname/blocklist/subsystem.go:74-87`
**Issue:** `cfg.Source` is captured in a closure and never nil-checked. A misconfigured
root panics inside `Cache.Reload` at `cache.go:153` (`c.raw()` on a nil func) rather than
failing at construction with a named code, which every sibling constructor in this package
does (`NewPoller` returns `BLOCKLIST_POLLER_MISCONFIGURED` for each nil dependency).
**Fix:** mirror `NewPoller` — return `(*Subsystem, error)` and refuse a nil `Source`.

### IN-03: `charname.Gate` has exported fields and no nil guard on `Skeletons`

**File:** `internal/charname/gate.go:93-104,179`
**Issue:** `Gate.Check` dereferences `g.Skeletons` unconditionally. `BlockList` is
correctly nil-guarded at line 162; `Skeletons` is not. `NewCharacterNameGate` always
populates it, but the struct is exported with exported fields and the test harness already
builds it by literal (WR-05), so `&charname.Gate{}` is a reachable shape that nil-panics
rather than returning a coded error.
**Fix:** guard it in `Check` with a `CHARACTER_NAME_GATE_MISCONFIGURED` refusal, or
unexport the fields behind a constructor.

### IN-04: A legacy name with no normal form aborts migration 000055 with an unactionable message

**Files:** `internal/world/postgres/identity_backfill.go:122-128`,
`internal/store/migrations/000055_backfill_character_normalized_names.go:83-95`
**Issue:** If `charname.Normalize` rejects any existing row, the migration aborts with
`CHARACTER_IDENTITY_BACKFILL_NORMALIZE_FAILED`. The row id is attached as oops *context*,
which `oops.OopsError.Error()` does not render — cobra prints only the message, so the
operator sees "please enter a character name" with no id. The remediation command the
migration recommends (`holomush character name duplicates`) calls the same function and
fails identically, listing nothing. Practically unreachable today because
`world.ValidateCharacterName` has always required `\p{L}` and spaces, so no stored name can
normalize to empty — hence Info rather than Warning.
**Fix:** put the id in the message, not only the context:
`Errorf("character %s has a name with no normal form: %v", id, nErr)`.

### IN-05: `collectIdentityCollisions` builds SQL by string concatenation

**File:** `internal/world/postgres/identity_backfill.go:206-232`
**Issue:** The column name is interpolated into the query text. It is genuinely safe — the
`switch` above admits only two package constants and the `default` arm errors — and the
comment says so. Flagged only because the shape is the one a future edit copies without
the guarding switch.
**Fix:** none required; consider two literal query constants instead of one interpolated
template, which removes the pattern entirely.

---

_Reviewed: 2026-08-05_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
