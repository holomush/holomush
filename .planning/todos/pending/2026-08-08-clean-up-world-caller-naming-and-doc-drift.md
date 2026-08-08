---
created: 2026-08-08T23:44:51.692Z
title: Clean up world.Caller naming and doc drift
area: general
severity: minor
files:
  - internal/world/service.go:265
  - internal/world/outbox_actor_test.go:75-93
  - internal/world/caller_test.go:111-118
  - internal/plugin/objects_integration_test.go:53,77
  - scripts/README.md:74
  - scripts/codemod/README.md:92
  - scripts/codemod/world-caller-arg2.yml:65-71
  - test/meta/world_caller_census_test.go:44-47
---

## Problem

Non-behavioral findings from the Phase 02.1 code review (2026-08-08). Nothing
here changes runtime behavior; all gates were green with these present. Grouped
so they can be swept in one pass rather than dribbling into unrelated diffs.

### Naming

**WR-03** — `internal/world/service.go:265` and 22 siblings: the parameter is
still NAMED `subjectID` while now TYPED `Caller`, which produces the jarring
`subjectID.subject` at 13 sites. The name says "a string id"; the type says "an
opaque caller". Renaming is a mechanical sweep but touches 23 signatures, so it
was deliberately not folded into a phase that had already verified green.

### Tests that assert less than they appear to

**WR-04** — `internal/world/outbox_actor_test.go:75-93` extracts
`tt.caller.subject` and passes a plain string into `buildIntent`. That tests
`buildIntent` in isolation but bypasses the command-layer link the migration
actually changed, so it would not catch a regression in how a command derives
the Actor from its `Caller`.

**IN-05** — `internal/world/caller_test.go:116-118`: `stubLocationReader.Get`'s
nil branch is unreachable.

**IN-04** — `internal/plugin/objects_integration_test.go:53,77`:
`recordingCharacterMutator.lastSubjectID` is written but never read.

### Comments and docs that are now false

**WR-05** — `internal/world/caller_test.go:111-113`: the import-cycle rationale
justifying ~35 lines of hand-rolled doubles is FALSE for a `package world_test`
file, and is contradicted by `internal/world/export_test.go:12-21` added in the
same phase. (The import cycle IS real — it is why the file is `package
world_test` at all — but it does not justify the hand-rolled doubles, which is
what the comment claims.)

**IN-03** — `test/meta/world_caller_census_test.go:44-47`: the census header
self-contradicts on `checkAccess`.

**IN-01** — `scripts/README.md:74` and `scripts/codemod/README.md:92` disagree
on the constraint count.

### A latent codemod trap

**IN-06** — `scripts/codemod/world-caller-arg2.yml:65-71` carries permanent
`ignores:` lists. Those paths will be SILENTLY SKIPPED on any Phase 02.2 re-run
of the rules. The entries were correct for 02.1's flip; they are not
self-expiring, and nothing warns a future runner that coverage is narrower than
it looks.

### Informational — no action

From phase verification: the census has one theoretical bypass via struct
embedding (a command promoted onto `Service` through an embedded type). Not
applicable today — `type Service struct` embeds nothing, all 10 fields are
named. Recorded so a future reviewer does not re-derive it as novel. Note the
sibling ANONYMOUS-RECEIVER hole found in the same review WAS fixed in 02.1 by
`TestWorldServiceMethodsAllDeclareNamedReceivers`.

## Solution

Sweep in one pass, ideally alongside Phase 02.2 since it touches the same files:

- Rename the 23 `subjectID` parameters to `caller` (mechanical; `task fmt` after,
  since several sit in aligned blocks).
- Rework `outbox_actor_test.go` to drive a real command rather than `buildIntent`
  directly, so it pins the link the migration changed.
- Delete the unreachable nil branch and the write-only `lastSubjectID` field.
- Correct the three false/contradictory comments; reconcile the two README counts.
- Give `world-caller-arg2.yml`'s `ignores:` entries an expiry note explaining
  what they were for and that a 02.2 re-run must re-audit them.

## Provenance

`.planning/phases/02.1-world-caller-model/02.1-REVIEW.md` — status
`issues_found`, 0 Critical / 7 Warning / 6 Info. WR-01 and WR-02 from that review
were fixed in-phase; this todo carries the remainder.
