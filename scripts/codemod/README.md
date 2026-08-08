<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# World Caller Codemod Rules

`ast-grep` rules that mechanise the `world.Service` bare-subject-string →
`world.Caller` migration (phase 02.1), plus a read-only census probe that answers
"is any bare subject-string parameter left on a world command?".

## Prerequisite

- **ast-grep 0.45.1 or later** (`ast-grep --version`).

`ast-grep` is **not** installed by `task setup` and is **not** present in CI.
These rules are a **local developer step, never a CI gate**. Deliberately so: a
`task` target would fail on a fresh clone and in CI, where the tool is absent, so
a documented prerequisite is the lower-blast-radius landing spot. Adding
`ast-grep` to `task setup`'s brew list is a possible follow-up; it was considered
and declined here.

Spell the tool **`ast-grep`** everywhere. The `sg` alias is deprecated and prints
a deprecation warning on stderr.

## Files

| File | Rewrites? | What |
|---|---|---|
| `world-caller-arg2.yml` | yes | Call argument 2 → `world.HumanCaller($SUBJ)`, in packages that qualify `world`. |
| `world-caller-arg2-internal.yml` | yes | Same, emitting the unqualified `HumanCaller($SUBJ)`, in files declaring `package world`. |
| `world-caller-decl.yml` | yes | Parameter declaration `subjectID string` → `subjectID world.Caller`, qualified packages. |
| `world-caller-decl-internal.yml` | yes | Same, emitting `subjectID Caller`, in `package world`. |
| `probe-subject-param.yml` | **no** | Read-only census. **MUST NOT be run with `-U`.** |

## Running

```bash
# Preview (dry run)
ast-grep scan -r scripts/codemod/<rule>.yml .

# Apply in place
ast-grep scan -r scripts/codemod/<rule>.yml -U .
```

Run the four rewrite rules in this order — declarations first, so the call-site
rules see the post-flip types:

1. `world-caller-decl.yml`
2. `world-caller-decl-internal.yml`
3. `world-caller-arg2.yml`
4. `world-caller-arg2-internal.yml`

### Two counting idioms — do not mix them

A rule that declares a rewrite prints a **unified diff**; a rule that does not
prints the rich **`┌─` frame**. So:

```bash
# The four REWRITE rules — count hunks:
ast-grep scan -r scripts/codemod/world-caller-arg2.yml . | rg -c '│\+'

# probe-subject-param.yml (no rewrite) — count sites:
ast-grep scan -r scripts/codemod/probe-subject-param.yml . | rg -c '┌─ '
```

`rg -c '┌─ '` on a rewrite rule prints nothing and exits 1. Note also that on
**no match** `rg -c` prints nothing and exits 1 generally — so never write an
acceptance criterion as "`rg -c … returns 0`". Use `| wc -l` returns 0, or
`! rg -q …`.

## The re-run review contract

**Re-running the four rewrite rules against the post-flip tree MUST leave
`git diff --exit-code` clean.** That empty diff is the mechanism by which a
reviewer trusts several hundred unread hunks without reading each one. It only
holds because of the three-clause `SUBJ` guard below.

**Standing remedy:** if a post-flip re-run produces ANY hunk, add that file to
the rule's `ignores:` or spell the constructor inline at the call site. Never
accept the double-wrap, and never hand-revert the hunk.

## The three load-bearing constraints

### 1. `SUBJ` idempotency guard — three clauses, not one

`$SUBJ` binds one node, whatever its shape. Post-flip, argument 2 is a
`world.Caller`, which can reach that slot in exactly three syntactic shapes —
each of which a naive rule happily re-wraps:

| Shape | Example | Guarded by |
|---|---|---|
| constructor call | `svc.GetLocation(ctx, world.HumanCaller(subjectID), id)` | `not: regex: '(HumanCaller\|SystemCaller)\('` |
| composite literal | `svc.GetObject(ctx, world.Caller{…}, id)` | `not: kind: composite_literal` |
| bare identifier | `svc.GetExitsByLocation(ctx, c, id)` | positive `regex:` allowlist + per-rule `ignores:` |

A **single-clause** guard (only the constructor check) is insufficient and was
measured to rewrite the composite literal to
`world.HumanCaller(world.Caller{})` and the bare local to `world.HumanCaller(c)`,
on both variants.

**The positive allowlist's future-shape failure mode — it fails in the dangerous
direction.** Clause 1 is `regex: '(?i)subj|^"'`. On the pre-flip tree its
complement is empty: all 355 true arg-2 sites spell argument 2 with one of just
13 distinct expressions, every one either mentioning `subj` case-insensitively or
beginning with a `"`. But a call site added later whose argument 2 is spelled
neither `subj`-ish nor a string literal — say `s.actorID` or `req.GetPlayerId()`
— matches neither pattern and is **silently skipped**: no hunk, no warning, and a
clean re-run diff, which is exactly the signal the re-run contract reads as
"nothing left to do".

The durable backstops are **the compiler** (an unmigrated site fails to build
once the signature is `world.Caller`) and **`probe-subject-param.yml`**, whose
post-flip count must be 8. The allowlist is a convenience for the mechanical
bulk, never the completeness proof.

**Retention cost: zero.** With all three clauses the Basis-A counts are identical
to the single-clause guard (351 external + 4 `package world` = 355). If either
number drops, the allowlist is excluding a real site — **widen the regex, do not
delete the clause**.

The declaration rules keep the parameter *name*, so post-flip a forwarded caller
is frequently still spelled `subjectID` and still matches clause 1. The
bare-identifier shape is therefore closed by path `ignores:` on the forwarding
surfaces, not by the constraint.

### 2. `RECV` false-positive blocklist — `ignores:` cannot express these

`not: regex: '(\.EXPECT\(\)|^querier|^adapter|^repo|characterWriter)$'`

`ignores:` is a **path glob**; three of these surfaces sit in files that **also
hold genuine migration targets**, so a path ignore would silently drop real work.
The constraint is node-scoped and can distinguish `hostcap/world.go:143` from
`:227` inside one file.

The 20 excluded sites, on the whole-tree basis (5 `EXPECT()` + 15 receiver-shape;
14 of the 15 are external, the 15th is internal):

- `\.EXPECT\(\)` — mockery expectation builders:
  `internal/world/service_test.go:4016, :4054, :4208, :5065`,
  `internal/world/movement_hook_test.go:83`.
- `^querier` — `internal/plugin/hostcap/world.go:143`,
  `internal/plugin/hostfunc/world.go:119`.
- `^adapter` — `internal/plugin/hostfunc/adapter_test.go:230, :241, :254, :266, :277, :414`.
- `^repo` — `internal/world/postgres/character_repo_test.go:543, :571, :582, :825, :841, :849`.
- `characterWriter` — `internal/world/mutator.go:231` (this file is `package world`,
  so it lands in the `-internal` variant's exclusion set, not the external one).

**Retention proof — the constraint must not over-suppress.** A dry run still
produces hunks at all four **real** `internal/plugin/hostcap/world.go` sites
(`:227, :288, :330, :385`) and all three **real**
`internal/world/postgres/cascade_delete_test.go` sites (`:338, :377, :416`). A
path-glob ignore of either file would have destroyed those.

> **Measurement note (2026-08-08).** On the current tree the `SUBJ` allowlist and
> the `RECV` blocklist each independently exclude the same 20 sites — argument 2
> at every one of them is spelled `charID` / `locID` / `characterID`, none of
> which is `subj`-ish. The whole-tree progression 375 → 370 → 355 reproduces only
> with the `SUBJ` guard **removed**; with it present all three stages read 355.
> Both guards are retained as defense in depth: the `SUBJ` allowlist is the one
> whose complement may grow, and the `RECV` blocklist is the one that stays true
> if the allowlist is ever widened.

### 3. Package-clause scoping — why there are two variants of each rule

`internal/world/` mixes 22 `package world` files with 11 `package world_test`
files and **no path glob separates them**, so `ignores:` cannot express the
split. It is done structurally instead: an `inside:` block of
`kind: source_file`, `stopBy: end`, `has: {kind: package_clause, regex: '^package world$'}`.
The `-internal` variants assert it; the external variants wrap the identical
block in `not:`.

Emitting the `world.`-qualified spelling inside `package world` produces the
doubled-package-name failure this repo's plan-review notes catalogue.

Reporting wrinkle: a rule whose top-level `rule:` carries
`inside: {kind: source_file}` makes ast-grep print the enclosing file frame, so
the output is verbose. The counting idioms above still work.

### Schema gotcha

`constraints:` is a **top-level sibling of `rule:`**, never a child of it. A rule
nesting `constraints` inside `rule` fails to load with
`unknown field 'constraints', expected one of pattern, kind, regex, nthChild,
range, inside, has, precedes, follows, all, any, not, matches`.

Related trap: `pattern: $NAME string` parses as an ERROR node.
`ast-grep run -p '$NAME string' --lang go` warns "Pattern contains an ERROR node"
on stderr yet **still exits 0**, while `ast-grep scan -r` on the same rule exits
**8** with "Rule must specify a set of AST kinds to match". Neither failure mode
is caught by reading a match count of zero as success — always check the count
against the pinned baseline below.

## Per-rule `ignores:`

`ignores:` is applied **per rule**, never uniformly, and never to a surface the
constraints already handle.

| Rule | `ignores:` | Why |
|---|---|---|
| `world-caller-arg2.yml` | `internal/world/caller_test.go`, `internal/grpc/location_follow.go`, `internal/property/**`, `internal/plugin/hostfunc/world_write.go`, `internal/plugin/hostfunc/cap_property*.go`, `internal/plugin/hostfunc/cap_world_query*.go` | Whole-file surfaces where a blind wrap is wrong: `location_follow.go` must become `SystemCaller()`; `internal/property/**` threads the caller unwrapped; `world_write.go`'s callbacks receive an already-typed caller from `withMutatorContext`; the two `cap_*` façades are out of scope for 02.1. |
| `world-caller-arg2-internal.yml` | `internal/world/caller_test.go` | **Inert, retained as belt-and-braces.** Plan 01 landed that file as `package world_test` (see the note below), so this rule's package-clause scope already excludes it. Kept so a future move back into `package world` cannot silently reintroduce the double-wrap. |
| `world-caller-decl.yml` | `internal/plugin/hostfunc/cap_property*.go`, `internal/plugin/hostfunc/cap_world_query*.go` | **Only** the out-of-scope façades. |
| `world-caller-decl-internal.yml` | (none) | The package-clause scope already restricts it. |
| `probe-subject-param.yml` | (none) | Its residual `cap_*` output is the point. |

> **Why `caller_test.go` is ignored on the EXTERNAL rule.** It was planned as
> `package world` so its criterion-2 proof could build an attribute-carrying
> `Caller` by same-package composite literal. That is impossible: `policy.NewEngine`
> takes a concrete `*attribute.Resolver`, and `internal/access/policy/attribute`
> imports `internal/world`, so an in-package test file importing it is
> "import cycle not allowed in test". The file is therefore `package world_test`
> (like `service_test.go`, which is exempt from the cycle) and reaches Caller's
> unexported state through `internal/world/export_test.go`. It is consequently
> scanned by `world-caller-arg2.yml`, which is where the live ignore sits.

**Deliberately NOT ignored on the declaration rules**, because each holds real
migration targets:

- `internal/property/**` — `entity_mutator_test.go`'s `fakeVersionMutator`
  methods (`UpdateLocation:44`, `UpdateObject:53`) are required targets. Its
  `fakeVersionQuerier` siblings are correctly left alone.
- `internal/world/mutator.go` — one of the four migrated interfaces.
- `internal/world/postgres/**` — 3 of its 9 arg-2 matches are real
  `world.Service` calls (`cascade_delete_test.go:338, :377, :416`).

## Grouped declarations are hand-migrated

A `parameter_declaration` carrying two names and one type — `subjectID, name string`
— matches a naive `has: {field: name}` rule, and the rewrite **drops the second
name**: `FindLocationByName(ctx context.Context, subjectID, name string)` was
observed rewriting to `FindLocationByName(ctx context.Context, subjectID world.Caller)`,
silently deleting a parameter. Both declaration rules therefore carry
`not: {regex: ','}`.

Enumerate the residual set (**21 sites repo-wide** on the pre-flip tree):

```bash
cat > /tmp/grouped.yml <<'EOF'
id: grouped-subject-decl
language: go
severity: hint
rule:
  kind: parameter_declaration
  regex: 'subjectID, '
EOF
ast-grep scan -r /tmp/grouped.yml . | rg -c '┌─ '
```

## Hand-migrated surfaces

### Unnamed parameters — invisible to every rule here

An **unnamed** parameter is a `parameter_declaration` with **no `name` field**,
so `has: {field: name}` cannot match it. A double declared
`GetLocation(context.Context, string, ulid.ULID)` is invisible to both
declaration rules **and** to `probe-subject-param.yml`. Eight are known:

| File | Lines |
|---|---|
| `internal/plugin/lua/corebuilding_brokered_command_test.go` | `:52` `GetLocation`, `:56` `GetCharacter`, `:60` `GetCharactersByLocation`, `:64` `GetObject`, `:89` `UpdateLocation`, `:93` `UpdateObject`, `:97` `FindLocationByName` |
| `internal/plugin/objects_integration_test.go` | `:63` |

Re-derive rather than trusting the line numbers:

```bash
rg -n '^func \(m \*recordingWorldMutator\)' internal/plugin/lua/corebuilding_brokered_command_test.go
```

`corebuilding_brokered_command_test.go` matters most: its
`var _ hostfunc.WorldMutator = (*recordingWorldMutator)(nil)` assertion at `:50`
makes the double a full `world.Mutator` implementer, and Go requires **every
implementer of a migrated method to move in the same commit**. Missing it is the
single way the flip commit fails to compile.

### Other hand-migrated surfaces

- `internal/grpc/location_follow.go` — both world calls become
  `world.SystemCaller()` with a plain `ctx`; the ambient
  `access.WithSystemSubject` marker and the `systemSubjectID` constant are
  deleted. **Never** rewrite these to `HumanCaller(systemSubjectID)`: a bare
  `system` subject without the marker is a hard `SYSTEM_SUBJECT_REJECTED`.
- Grouped declarations (21, above).

## The census probe

```bash
ast-grep scan -r scripts/codemod/probe-subject-param.yml . | rg -c '┌─ '
```

| When | Count |
|---|---|
| pre-flip | **185** sites across **24** files |
| post-flip | **exactly 8** |

All eight post-flip sites must be the deliberately out-of-scope Lua-facing
façades: `internal/plugin/hostfunc/cap_property.go:28,34`,
`cap_property_test.go:54,68`, `cap_world_query.go:38,41`,
`cap_world_query_test.go:43,48`. The probe carries no `ignores:` for these on
purpose — their visibility is what proves they were left alone rather than
silently swept.

**The probe MUST NOT be run with `-U`.** It declares no rewrite.

**8 is necessary but not sufficient** — the probe shares the unnamed-parameter
blind spot above. The compiler and `task test:int` are what close that gap.

## Measured baselines

Take these on a **pre-flip** tree. Each is labelled with its basis; **mixing
bases is what makes the arithmetic irreproducible**.

### Call sites (arg 2)

| Basis | External | `package world` | Total |
|---|---|---|---|
| **Whole-tree progression** (no package split, no `ignores:`, `SUBJ` guard removed): 375 naive → 370 after the `EXPECT()` guard → 355 after the receiver guard | — | — | **355** |
| **Basis A — true arg-2 sites** (rule CONSTRAINTS only, `ignores:` NOT applied). *The migration surface: every site that must end up carrying a caller.* | 351 | 4 | **355** |
| **Basis B — committed-rule emission** (`ignores:` APPLIED — what `ast-grep scan -r <rule> .` actually prints) | **337** | 4 | **341** |

The A→B gap is exactly 14 hunks in the five ignored paths:
`internal/grpc/location_follow.go` 2, `cap_property.go` 2, `cap_world_query.go` 2,
`hostfunc/world_write.go` 4, `property/entity_mutator.go` 4.

> **351 is the migration surface; 337 is the emission.** A reader who conflates
> them will write an acceptance gate that is unsatisfiable by 14 hunks.

All 4 `package world` sites are in `internal/world/grpc_server.go`
(`:44, :62, :80, :101`). No file appears in both variants' outputs.

### Declarations

| Rule | Hunks |
|---|---|
| `world-caller-decl.yml` | **132** (18 files) |
| `world-caller-decl-internal.yml` | **30** (`internal/world/service.go` 21 + `internal/world/mutator.go` 9) |
| total | **162** |

No `ignores:` gap of the arg-2 kind, and no file appears in both outputs.

### Other

| Probe | Count |
|---|---|
| grouped declarations | 21 |
| `probe-subject-param.yml` pre-flip | 185 across 24 files |
| `probe-subject-param.yml` post-flip | 8 |
