# Quick Task 260731-ea8: implement the issue-first gate enforcement workflow (#4890) - Context

**Gathered:** 2026-07-31
**Status:** Ready for planning

<domain>
## Task Boundary

Implement the CI workflow that enforces the issue-first gate documented in
`CONTRIBUTING.md` and the four typed PR templates (shipped by PR #4889,
squash `9095b79a6`). The gate is documented policy today and **nothing
automates it** — the `gate-violation` label exists but no workflow applies it.

Resolves GH #4890.

</domain>

<decisions>
## Implementation Decisions

### Enforcement severity — LOCKED

**Label + failing check run. Do NOT close PRs.**

On violation the workflow applies `gate-violation`, posts an explanatory
comment, and emits a **failing check run** on the PR head SHA. It never
closes a PR.

Rationale: the nine "auto-closed" phrasings in the templates were corrected
precisely because they asserted an enforcement that could not fire. A closing
workflow is the aggressive end of the range and a bug in the exemption matcher
turns into maintainer toil reopening legitimate PRs. A failing check run
carries the same blocking force (once required) with a reversible failure mode.

Consequence for the templates: the "auto-closed" wording MUST NOT be restored.
The truthful restoration is "flagged and blocked from merge", not "closed".

**Amended 2026-07-31 after research.** This section originally asserted the
closure phrasings "were corrected" during PR #4889. That is wrong. Ten
assertions describing closure survive on `main`, verified by
`rg -n -i 'closed without review|gets closed' …` (exit 0):

| File | Lines |
|---|---|
| `.github/PULL_REQUEST_TEMPLATE.md` | 36 |
| `.github/PULL_REQUEST_TEMPLATE/feature.md` | 20, 22 |
| `.github/PULL_REQUEST_TEMPLATE/enhancement.md` | 20, 22 |
| `.github/PULL_REQUEST_TEMPLATE/chore.md` | 20 |
| `.github/PULL_REQUEST_TEMPLATE/fix.md` | 20 |
| `CONTRIBUTING.md` | 15, 64, 84 |

What PR #4889 actually corrected was the claim that closure is **automatic**;
the surviving sentences are passive ("is closed without review") and describe
**maintainer practice**, which is true today and stays true after this change.

So: the research finding that these become "actively false" once the workflow
lands is **overstated and the planner MUST NOT act on it as stated**. A
maintainer retains the ability to close a non-compliant PR; the workflow
simply does not do it for them. These sentences are not falsified.

They are, however, now **incomplete** — they describe a human consequence
while saying nothing about the automated one, and a contributor reading them
will not learn that a check will go red. In scope for this task: amend each of
the ten to state the automated consequence (flagged `gate-violation` + a
failing required check) alongside the human one. Out of scope: deleting or
inverting them.

This also discharges the issue's "Done when" box that reads *"the nine
'auto-closed' phrasings can be restored to the templates truthfully, or the
decision to keep warn-only wording is recorded"* — the decision is recorded
here, and it is the second branch.

### Draft PRs — LOCKED

**Skip drafts. Evaluate on `ready_for_review`.**

Trigger list includes `ready_for_review`; the job short-circuits to a passing
check while `draft == true`. The "open a draft to get CI running before the
issue exists" workflow stays viable.

The router's original claim that draft PRs are "closed automatically" is
superseded and MUST NOT be restored.

### Required status check — LOCKED (with a design constraint attached)

**Yes — the check becomes a required context on protect-main ruleset
`11923801`, and whatever stand-in it needs ships in the same change.**

Hard constraint that follows, and the planner MUST honour it:

> **Exemption means the check reports GREEN. It does not mean the job is
> skipped and it does not mean the workflow is path-filtered.**

This is the whole defence against the #4878 / PR #4879 failure mode, where
adding `Vuln` to the ruleset without a matching `ci-docs-skip.yaml` stand-in
silently BLOCKED every docs-only PR with nothing failing. That bug exists
because `ci.yaml` carries `paths-ignore`. The enforcement workflow MUST NOT
carry `paths-ignore` — it runs on every PR and always emits its context, so
an exempt PR gets a green check from the same job rather than from a
same-named stand-in job.

Both open questions are now RESOLVED by research — the planner treats these
as settled, not as choices:

**1. No `ci-docs-skip.yaml` stand-in. Adding one would be harmful.**
`ci-docs-skip.yaml:6-18` states verbatim that it exists *because* `ci.yaml`
is path-filtered. The enforcement workflow has no path filter, so that
precondition is absent. A duplicate-named job would produce two check runs
under one required context. Record this as the answer to the issue's "if it
becomes a required check, a stand-in exists" box — the box is discharged by
"not applicable, and here is why", not by adding a job.

**2. The ruleset context MUST be added AFTER this PR merges — not in it.**
Ruleset write IS available to this session (`gh auth status` → `repo` scope,
repo `admin: true`), so the earlier "operator-only" note does not block us.
But adding the context inside this PR reproduces #4878 exactly, for a reason
that is structural rather than incidental:

> GitHub takes the workflow file for a `pull_request_target` run from the
> repository's **default branch**. A workflow introduced by a PR therefore
> **cannot run on its own PR** — it does not exist on `main` yet.

So the new context would be required and permanently `Pending` on this very
PR. Mandatory ordering, and the planner MUST encode it as ordered steps, not
as one step:

1. Merge the workflow to `main` with the context NOT yet required.
2. Observe it emit a real conclusion on the next unrelated PR.
3. Only then add the context to ruleset `11923801`.

Step 3 ships as a verified, copy-pasteable command in the PR body and as a
follow-up issue. Until step 3 lands, the "required check" half of this task
is reported as **PENDING — POST-MERGE**, never as done. Do not tick that
box on the strength of the workflow existing.

### Exemption path sets — LOCKED

**Taskfile vars are the single source of truth. Generalize the matcher.**

- Reuse `vars.DOCS_ONLY_PATHS` (Taskfile.yaml:30) as-is.
- Reuse `vars.DEPENDENCY_ONLY_PATHS` (Taskfile.yaml:98) as-is — its own
  comment names #4890 as its first consumer.
- Add a new `vars.REPO_CONFIG_ONLY_PATHS` for the repo-configuration set
  (`.github/**`).
- Hardcode the negative carve-out: `.github/CODEOWNERS` and a root-level
  `CODEOWNERS` are **never** exempt, even though `.github/**` matches the
  former. Changing review ownership is a governance decision and a
  self-exempting path to it is the same shape of hole as the
  `pr-template-exempt` bypass that was closed.

**The matcher must be written, not borrowed.** `scripts/docs-paths-regex.sh`
compiles only three glob shapes (hardcoded `**/*.md`, `foo/**`, and literal
paths). Any other `**` position hard-errors `unsupported '**' position`.
Worse, a `foo*.bar` shape contains no `**`, falls through to the literal
branch, and emits the ERE `foo*\.bar` — which in ERE reads `fo` + `o*` +
`\.bar`, matching `foo.bar` but not `foo.baz.bar`. `go.tool*.mod` in
`DEPENDENCY_ONLY_PATHS` has exactly that shape. This is a silently wrong
matcher, not an error, and it looks correct because the common case passes.
Do not point it at `DEPENDENCY_ONLY_PATHS` unqualified.

**Amended 2026-07-31 after research — write NO glob compiler at all.**

The locked decision said "generalize the matcher". Research found a strictly
better means that preserves the intent (Taskfile vars stay the single source
of truth) while deleting the compiler from the design: **use git's own glob
engine via `:(glob)` pathspec.**

```
git diff --name-only "$BASE...$HEAD" -- ':(exclude,glob)<pattern>' …
```

If that command returns no filenames, every changed file matched some exempt
pattern. No translation layer, no ERE, nothing to silently miscompile.
Verified against git 2.55.0 on all fifteen `DEPENDENCY_ONLY_PATHS` shapes,
including the `go.tool*.mod` shape that defeats `docs-paths-regex.sh` and the
root-level `**/go.mod` case that `Taskfile.yaml:95-97` requires to match.

Rejected alternatives, with the reason each was rejected:
- `gobwas/glob` — already a direct dependency (`go.mod:13`), but it does
  **not** match `**/go.mod` against a root-level `go.mod`. It would silently
  under-match root `go.mod` / `go.sum` / `package.json` / `pyproject.toml`,
  violating the guarantee `Taskfile.yaml:95-97` explicitly asks #4890 to
  preserve. Wrong in exactly the silent direction this task exists to stop.
- `doublestar` — correct, but only an indirect golangci-lint dependency;
  promoting it to a direct dep is unjustified when git already does the job.

**Carve-out caveat the planner MUST honour — this one is silent if missed.**
Git applies all `:(exclude)` pathspecs *last* and ANDs them together. The
CODEOWNERS carve-out therefore **cannot** be expressed inline in the same
invocation as the exempt patterns. It MUST be a separate positive query:

```
git diff --name-only "$BASE...$HEAD" -- ':(glob).github/CODEOWNERS' 'CODEOWNERS'
```

Non-empty ⇒ NOT exempt, regardless of what the exclude query returned.

### Not exempt — LOCKED (do not restore from an older diff)

- `Taskfile.yaml` and `scripts/**` are **NOT** exempt. They were exempt in
  earlier revisions of the #4889 branch and were deliberately removed: they
  define `task pr-prep` and the checks CI runs, so exempting them lets the
  gate's own definition change without a gated issue.
  - One carve-out inside that: a **listed lockfile** under `scripts/` is
    exempt because it matches a `DEPENDENCY_ONLY_PATHS` shape. The carve-out
    is bounded by that list, not by the word "lockfile" — `scripts/poetry.lock`
    is NOT exempt.
- `<!-- pr-template-exempt: … -->` waives **nothing**. It is informational
  only: it explains a typed-template mismatch and the issue-first gate still
  applies. The #4890 issue body still describes it as an exemption; that part
  of the body is superseded by the maintainer's follow-up comments.

### Resolved by research — treat as settled

**Issue-link extraction: use GraphQL `closingIssuesReferences`. Do NOT regex
the PR body.** Verified against PR #4889 → issue #4888. It returns the link,
the issue state, its repository, and its labels in one query, and it sidesteps
the "don't match `Closes #N` inside a fenced code block or an HTML comment"
problem *by construction* rather than by a cleverer regex — the typed
templates are full of HTML comments, so a regex here is a latent bug.

Hard filter that MUST be applied: `repository.nameWithOwner == "holomush/holomush"`.
Without it, a cross-repo link to some foreign repo's approved-looking issue
satisfies the gate. This is a real bypass, not a hypothetical.

**Check-run emission: use the job's own conclusion. Do NOT call
`gh api /repos/{owner}/{repo}/check-runs`.** Two verified facts make this the
simpler and more correct route:
- The ruleset's required context is the job's `name:` — byte-matching the six
  display names already in `ci.yaml`. So naming the job correctly *is* the
  integration.
- A `pull_request_target` job's check run attaches to the **PR head SHA**,
  despite `GITHUB_SHA` pointing at the base. Measured live rather than assumed.

So the job fails ⇒ the context goes red on the PR. Nothing to publish by hand.

### Claude's Discretion

- Trigger event-type list, given drafts are skipped. `edited` is likely
  required so that adding or removing a `Closes #N` link re-evaluates the gate
  — confirm rather than assume.
- Security posture statement for `pull_request_target`: it runs the base-ref
  workflow with a write token and MUST NOT check out or execute PR head code.
  The design has no reason to check out head at all.
- PR-type classification mechanism (typed-template h2 markers vs labels).
- Comment wording and idempotency strategy (update-in-place vs re-post).
- Whether the label is removed again when a violating PR is fixed.
- Test strategy for the matcher.

</decisions>

<specifics>
## Specific Ideas

Current exemption contract (from the maintainer's second follow-up comment on
#4890 — this table supersedes both the issue body and the first comment):

| Category | Paths | Waives |
|---|---|---|
| Documentation-only | `site/**`, `docs/**`, `**/*.md`, `.planning/**`, `.claude/{agents,commands,rules,agent-memory}/**`, `LICENSE`, `LICENSE_HEADER` | template + gate |
| Dependency-only | `vars.DEPENDENCY_ONLY_PATHS` (15 globs) | template + gate |
| Repo configuration-only | `.github/**` — **except** `CODEOWNERS` | template + gate |
| `Taskfile.yaml`, `scripts/**` | — | **nothing** — needs a chore issue |
| `<!-- pr-template-exempt: … -->` | any | **nothing** — informational only |

Gate labels that exist on the repo today (verified via `gh label list`):
`approved-feature`, `approved-enhancement`, `approved-chore`, `confirmed-bug`,
`gate-violation`, `needs-triage`, `needs-review`, `type: chore`.

Typed-template h2 markers for classification: `## Feature summary`,
`## What this enhancement improves`, `## What was broken`,
`## What this chore changes`.

</specifics>

<canonical_refs>
## Canonical References

- GH #4890 — the issue, plus two maintainer follow-up comments that supersede
  parts of its body.
- `Taskfile.yaml:25-115` — `DOCS_ONLY_PATHS` and `DEPENDENCY_ONLY_PATHS` with
  the load-bearing rationale comments, including the explicit warning to #4890
  about `scripts/docs-paths-regex.sh`.
- `CONTRIBUTING.md` — "### Exempt by file path" prose mirror.
- `.github/PULL_REQUEST_TEMPLATE.md` — "## Exemptions" prose mirror.
- `.github/workflows/ci.yaml`, `.github/workflows/ci-docs-skip.yaml` — the
  paths-ignore / stand-in pair whose failure mode this design must avoid.
- Protect-main ruleset `11923801` — eight required contexts as of 2026-07-28.
- GH #4878 / PR #4879 — the silent-block incident this design defends against.

</canonical_refs>
