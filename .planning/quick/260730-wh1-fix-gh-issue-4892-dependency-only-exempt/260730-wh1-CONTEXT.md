# Quick Task 260730-wh1: fix GH issue 4892 dependency-only exempt path list - Context

> ### 📌 Pre-fix snapshot — records the decisions as LOCKED before execution
>
> Captured before planning. Its verified-path table and matcher-limitation examples use
> `compose*.yaml` and the pre-review 10-entry set. Two refinements were agreed later, after
> code review surfaced contradictions: the compose files are listed individually (the glob
> also matched the gate-defining `compose.e2e{,.cover}.yaml`), and the lockfile subset is
> enumerated so "lockfile" is decidable under `scripts/`. `**/pyproject.toml` and
> `go.tool*.{mod,sum}` were also added.
>
> The three locked decisions themselves still hold — only their implementation surface
> widened. `Taskfile.yaml` `vars.DEPENDENCY_ONLY_PATHS` is authoritative for the path set.


**Gathered:** 2026-07-30
**Status:** Ready for planning

<domain>
## Task Boundary

Fix GitHub issue #4892 — the `Dependency-only` exempt path list introduced by PR #4889
(squash `9095b79a6`) names a file that does not exist and omits most of what Renovate
actually touches, making the accompanying claim "Renovate PRs are exempt by definition"
false for the majority of them.

In scope: correcting the dependency-only exemption definition in `CONTRIBUTING.md` and
`.github/PULL_REQUEST_TEMPLATE.md`, and establishing an authoritative machine-readable
source for it in `Taskfile.yaml`.

Out of scope: implementing the enforcement workflow (#4890), and building a
`lint:dependency-paths-sync`-style gate (see Claude's Discretion).

</domain>

<decisions>
## Implementation Decisions

### Shape of the exempt definition — LOCKED

Replace the literal five-path enumeration with **manifest/lockfile-shape globs**, and add
an authoritative `DEPENDENCY_ONLY_PATHS` variable to `Taskfile.yaml`, mirroring the
existing `DOCS_ONLY_PATHS` precedent (`Taskfile.yaml:30-40`).

`CONTRIBUTING.md` and `.github/PULL_REQUEST_TEMPLATE.md` restate the list in prose and
point at `Taskfile.yaml` as the authoritative version — exactly the pattern
`CONTRIBUTING.md:151-154` already uses for the documentation-only set.

Accepted consequence: this PR touches `Taskfile.yaml`, which #4889 deliberately made
**non-exempt**. Therefore this PR is *not* exempt and requires #4892 to carry the
`confirmed-bug` gate label before the PR is opened.

### `scripts/**` collision — LOCKED

A **lockfile** under `scripts/` is dependency-only exempt; any **non-lockfile** change
under `scripts/` remains gated. Rationale (from the issue): a lockfile is not a gate
definition. This keeps routine Renovate lock-maintenance PRs (e.g. merged #4847, which
touched `scripts/uv.lock` and `.claude/skills/holomush-dev/scripts/uv.lock`) out of the
chore-issue path, while `scripts/**` logic that defines `task pr-prep` stays gated.

The `Taskfile.yaml` / `scripts/**` non-exempt sentence MUST be amended to state this
carve-out explicitly, or it contradicts the new dependency set.

### Codegen-carrying dependency PRs — LOCKED

Delete "Renovate PRs are exempt by definition" — it is false as written — and replace it
with a path-derived rule plus an explicit statement that a dependency PR which **also**
carries regenerated code (`pkg/proto/**/*.pb.go`, `web/**/*_pb.ts`) is **not** exempt.

Grounding: `.github/renovate.json` defines custom managers for `buf.gen.yaml`,
`buf.gen.internal.yaml`, and `web/buf.gen.yaml`. Both `buf codegen` package rules set
`automerge: false` precisely because a pin bump does *not* regenerate the committed
stubs — a human runs `task proto` / `task web:generate` on the bump PR and commits the
generated output. Those PRs therefore carry real source diffs.

### Claude's Discretion

- Whether to add a `lint:dependency-paths-sync` gate. Leaning **no**: the existing
  `lint:docs-paths-sync` exists because `DOCS_ONLY_PATHS` is mirrored into two
  machine-consumed workflow files (`ci.yaml`, `ci-docs-skip.yaml`); it does *not* check
  `CONTRIBUTING.md` prose. `DEPENDENCY_ONLY_PATHS` has no machine consumer until #4890
  lands, so a sync gate would guard nothing today. Fold it into #4890.
- Exact ordering and prose wording within each edited section.

</decisions>

<specifics>
## Specific Ideas

### Ground truth — paths that actually exist (verified via `git ls-files`)

| Real path | Covered by |
| --- | --- |
| `go.mod`, `go.sum` | `**/go.mod`, `**/go.sum` |
| `gorules/go.mod`, `gorules/go.sum` (second Go module) | same |
| `web/package.json`, `site/package.json` | `**/package.json` |
| `web/pnpm-lock.yaml` | `**/pnpm-lock.yaml` |
| `web/pnpm-workspace.yaml` | `**/pnpm-workspace.yaml` |
| `site/bun.lock` | `**/bun.lock` |
| `scripts/uv.lock`, `.claude/skills/holomush-dev/scripts/uv.lock` | `**/uv.lock` |
| `compose.yaml`, `compose.prod.yaml`, `compose.cluster.yaml`, `compose.e2e.yaml`, `compose.e2e.cover.yaml` | `compose*.yaml` |
| `Dockerfile`, `docker/postgres-backup/Dockerfile` | `Dockerfile`, `**/Dockerfile` |

`web/bun.lock` — named in the current policy — **does not exist**.

### Renovate managers actually enabled (`.github/renovate.json`)

`gomod`, `npm`, `github-actions`, `dockerfile`, `docker-compose`, plus three custom regex
managers for buf codegen pins. `github-actions` output lands in `.github/workflows/**`,
already exempt via repo-config-only.

### Open Renovate PRs re-verified 2026-07-30

| PR | Files | Under current policy | Under new globs |
| --- | --- | --- | --- |
| #4851 | `site/bun.lock`, `site/package.json` | exempt only incidentally via `site/**` docs | exempt |
| #4848 | `web/package.json`, `web/pnpm-lock.yaml` | **NOT exempt** | exempt |
| #4550 | `compose.prod.yaml`, `go.mod`, `go.sum`, `site/bun.lock`, `site/package.json` | **NOT exempt** | exempt |
| #4847 (merged) | `scripts/uv.lock`, `.claude/skills/holomush-dev/scripts/uv.lock`, `site/bun.lock`, `web/pnpm-lock.yaml` | **NOT exempt** | exempt |

### CRITICAL — the existing glob compiler cannot compile these shapes

`scripts/docs-paths-regex.sh` supports exactly three forms:

1. the hardcoded special case `**/*.md` → `.*\.md`
2. `foo/**` → `foo/.*`
3. literal paths (dots escaped)

Any other `**` position is a **hard error** (`unsupported '**' position`, exit 1). So
`**/go.mod`, `**/package.json`, `**/uv.lock`, `**/Dockerfile` would all abort it.

Worse, `compose*.yaml` contains no `**`, so it falls through to the *literal* branch and
compiles to `compose*\.yaml` — in ERE that is `compos` + `e*` + `\.yaml`, which matches
`compose.yaml` but **not** `compose.prod.yaml`. A silently wrong matcher, not an error.

That script hardcodes `.vars.DOCS_ONLY_PATHS`, so nothing breaks today. But #4890 will
reach for it. The docs MUST carry an explicit note that `DEPENDENCY_ONLY_PATHS` needs its
own matcher — otherwise #4890 inherits a silently-wrong regex. This is the repo's
recurring "a check that cannot fail / silently wrong matcher" defect class.

</specifics>

<canonical_refs>
## Canonical References

- GitHub issue #4892 (this bug), #4890 (enforcement workflow that will consume the list)
- `CONTRIBUTING.md` — "Exempt by file path" section (lines ~148-167) and the chore-intake
  "Already exempt?" paragraph (lines ~109-114)
- `.github/PULL_REQUEST_TEMPLATE.md` lines ~46-60
- `.github/PULL_REQUEST_TEMPLATE/chore.md` lines ~24-27 (references the list, does not
  enumerate it — verify no edit needed)
- `Taskfile.yaml:30-40` — `DOCS_ONLY_PATHS`, the precedent being mirrored
- `scripts/docs-paths-regex.sh` — the glob compiler with the shape limitation above
- `.github/renovate.json` — enabled managers and the two `automerge: false` buf codegen rules

</canonical_refs>
