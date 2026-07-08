---
name: review-abac
description: Adversarially review ABAC-domain code changes against the access-control spec before /gsd-code-review runs
---

@agent-abac-reviewer Review the ABAC-domain code changes described below
against the access-control architecture in `internal/access/`. This gate runs
ALONGSIDE `/gsd-code-review` for any change touching the access control surface
(`internal/access/`, policy DSL, attribute providers, authorization decision
points, seed policies). For purely ABAC-scoped changes, run it BEFORE
`/gsd-code-review` since it carries the dispositive verdict.

**Target:** $ARGUMENTS

**If no target was given:** review the full branch diff against the merge
base. Use `git diff origin/main...HEAD` to get the diff. Review the diff AND
the full files it touches.

**If a target was given:** treat it as either a path (review that file's
changes vs merge base), a commit revset (review that revset's diff), or a
PR number (fetch with `gh pr diff <n>` and review).

**Before writing findings:** read `docs/specs/2026-02-05-full-abac-design.md`
(the ABAC design spec) and any seed-policy changes against
`internal/access/policy/seed.go` and `seed_test.go`. You MUST understand the
default-deny posture and the policy DSL before judging compliance.

Apply the adversarial stance and review checklist from your agent prompt.
Produce findings grounded in `path:line` for code claims, with a binary
verdict.

**Ordering:** this gate runs alongside `/gsd-code-review` (or before, when a
change is purely ABAC-scoped). After abac-reviewer returns its verdict, then
run `/gsd-code-review` for the generic adversarial pass.
