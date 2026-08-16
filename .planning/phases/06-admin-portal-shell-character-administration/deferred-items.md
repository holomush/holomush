# Deferred items — phase 06

Discovered during execution, OUT OF SCOPE for the plan that found them, and
deliberately not repaired there. Each names the change that caused it.

## 1. `test/integration/charname` is RED at HEAD — a second casualty of migration 000057

**Found during:** plan 06-05, Task 3 (`task test:int` over the whole tree).

**Caused by:** plan 06-04, commit `5d596505b`, which added
`internal/store/migrations/000057_character_admin_search_indexes.sql`.

**Not caused by 06-05:** `git diff --name-only 61b4fb246~1 -- internal/store/migrations/ test/integration/charname/ internal/charname/`
returns EMPTY — this plan touches no migration and no charname file.

**Symptom:** 8 of 24 specs fail, all on the same fixture precondition:

```
000056 must be withheld from the staged corpus; staging recipe: copy the real
.sql files except 000056 into a temp dir and leave the global registry ENABLED
Expected <int64>: 57 not to be >= <int>: 56
```

**Root cause — the fixture contradicts itself.**
`test/integration/charname/name_uniqueness_test.go:95-131` stages a pre-index
schema by copying every migration EXCEPT `000056_`, and its own comment reads
*"Everything else, including every version above it that might exist later, is
copied verbatim."* Nine lines later it asserts, for every collected source,
`Expect(src.Version).NotTo(BeNumerically(">=", 56))`.

Those two statements cannot both hold. The comment permits versions above 56;
the assertion forbids them. The fixture was correct only while 56 was the
highest migration in the tree, and it broke the moment 000057 landed.

**The staged schema itself is still correct.** `provider.Up(ctx)` succeeds — the
chain applies cleanly with 000056 withheld — so the database really is in the
pre-unique-index state the specs need. Only the precondition loop is wrong.

**Suggested repair (NOT applied here):** narrow the assertion from a `>= 56`
range to the one version actually withheld — e.g. assert the collected source
set does not CONTAIN version 56 — matching the staging recipe the same message
describes. That preserves the property (000056 absent implies the pre-index
state) and stops the fixture breaking on every future migration.

**Why 06-05 did not repair it:** it is in neither this plan's `files_modified`
nor its scope boundary, and it is another plan's fixture. Repairing another
plan's assertion is exactly the kind of change that can mask what that
assertion was protecting. It is logged here so it survives to the phase gate.

**This is the SECOND breakage from the same migration.** The first was
`internal/store`'s hand-written migration census, which 06-04 fixed post-merge
in `b6de718d`. A migration's blast radius in this repo reaches hand-written
corpus assertions in at least two unrelated suites; a third should be assumed
until searched for.
