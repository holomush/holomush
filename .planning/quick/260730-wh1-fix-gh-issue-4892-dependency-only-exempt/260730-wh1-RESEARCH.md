# Quick Task 260730-wh1: Research — verify + gaps

**Researched:** 2026-07-30
**Scope:** targeted verification of CONTEXT.md's grounding claims, not a broad survey.

## Q1 — Glob-compiler claim (`scripts/docs-paths-regex.sh`)

**Verdict: VERIFIED (both halves).**

`scripts/docs-paths-regex.sh:34-56` has **four** branches, case-matched in order. Three of
them *accept* a glob; the third rejects:

1. `'**/*.md'` → hardcoded `.*\.md` (`:39-41`)
2. `*'/**'` → `foo/**` → `foo/.*`, dots escaped (`:42-48`)
3. any other `**` occurrence → hard error `unsupported '**' position`, `exit 1` (`:49-52`)
4. anything else falls to the literal-path branch, only dots escaped (`:53-56`)

> **Correction (post-review, CodeRabbit thread 2).** An earlier draft of this section said
> "exactly three shapes" while listing four branches, and claimed that *none* of the
> dependency patterns could be compiled. Both were wrong. Branch 4 handles a true literal
> correctly, and the final 15-entry set contains four such literals. The corrected
> per-pattern classification is the table below.

So `**/go.mod`, `**/package.json`, `**/uv.lock`, `**/pnpm-lock.yaml`, `**/Dockerfile`,
`**/bun.lock` all hit branch 3 (leading `**/` that isn't the hardcoded `**/*.md` case) and
would hard-error the script — `exit 1` on the first such glob, aborting compilation entirely
for `DOCS_ONLY_PATHS` too if the two lists were ever compiled by the same helper.

`compose*.yaml` contains no `**` at all, so it falls through every case to branch 4 (literal,
dot-escaped only) and compiles to `compose*\.yaml`. Empirically confirmed via `rg` (ERE
engine, same alternation semantics the script targets):

```
$ printf 'compose.yaml\n'      | rg -e '^(compose*\.yaml)$'; echo EXIT:$?
compose.yaml
EXIT:0
$ printf 'compose.prod.yaml\n' | rg -e '^(compose*\.yaml)$'; echo EXIT:$?
EXIT:1
```

Confirmed: `compose*\.yaml` matches `compose.yaml` (the literal `e*` quantifier applied to
the preceding `e` allows zero-or-more `e`s, so `compos` + `e` + `.yaml`) but does **not**
match `compose.prod.yaml`. A silently-wrong matcher, not a hard error — exactly as CONTEXT.md
claimed.

### Per-pattern classification against the FINAL 15-entry set

Every entry classified as **supported**, **hard-failing**, or **silently miscompiled**.
`compose*.yaml` is absent from the shipped set — it was replaced by three literals during
the post-review revision, precisely because the glob also matched the E2E compose files.

| Pattern | Branch | Verdict |
| --- | --- | --- |
| `**/go.sum` | 3 | hard-fail `exit 1` |
| `**/pnpm-lock.yaml` | 3 | hard-fail |
| `**/bun.lock` | 3 | hard-fail |
| `**/uv.lock` | 3 | hard-fail |
| `**/go.mod` | 3 | hard-fail |
| `**/package.json` | 3 | hard-fail |
| `**/pyproject.toml` | 3 | hard-fail |
| `**/pnpm-workspace.yaml` | 3 | hard-fail |
| `**/Dockerfile` | 3 | hard-fail |
| `go.tool*.sum` | 4 | **silently miscompiled** |
| `go.tool*.mod` | 4 | **silently miscompiled** |
| `Dockerfile` | 4 | **supported** (exact literal, no metacharacters) |
| `compose.yaml` | 4 | **supported** |
| `compose.prod.yaml` | 4 | **supported** |
| `compose.cluster.yaml` | 4 | **supported** |

Totals: **9 hard-fail, 2 silent miscompile, 4 supported.**

The two silent cases have the same shape as the retired `compose*.yaml`. Verified:

```
go\.tool*\.mod  →  go.tool.mod MATCH  |  go.tool-lint.mod MISS
go\.tool*\.sum  →  go.tool.sum MATCH  |  go.tool-lint.sum MISS
```

`go\.too` + `l*` + `\.mod` consumes the single `l` and then requires `.mod` immediately, so
the `-lint` variants never match. Both files exist and are tracked.

**Implication for #4890:** the compiler cannot be pointed at `DEPENDENCY_ONLY_PATHS`
unqualified — it aborts on the first of nine leading-`**/` entries, and would silently
half-cover the two `go.tool*` entries even if the hard failures were removed. This is inert
for THIS task (no machine consumer exists yet — Q2), but #4890 must generalize the compiler
(a real `**/foo` prefix branch plus genuine glob→ERE translation, not a literal-escape
fallback) or write a separate matcher. The four supported literals are not a reason to reuse
it: partial support is what makes the failure silent.

## Q2 — Every consumer of `DOCS_ONLY_PATHS` / the dependency exemption list

**Verdict: VERIFIED, with one addition CONTEXT.md's canonical-refs list omitted.**

Full consumer set for `DOCS_ONLY_PATHS` (all confirmed machine consumers of the Taskfile var):

| Consumer | Evidence |
|---|---|
| `scripts/docs-paths-regex.sh:21` | reads `.vars.DOCS_ONLY_PATHS` via `yq -e` |
| `scripts/lint-docs-paths-sync.sh:28` | reads it, diffs against `ci.yaml` / `ci-docs-skip.yaml` |
| `Taskfile.yaml:714-717` (`lint:docs-paths-sync`) | wraps the sync script |
| `Taskfile.yaml:981,1071` (`pr-prep`/`pr-prep:full` inline `cmd:`) | calls `docs-paths-regex.sh` to build `DOCS_REGEX` for docs-only detection |
| `.github/workflows/ci.yaml` `paths-ignore:` | mirror (byte-identical, enforced) |
| `.github/workflows/ci-docs-skip.yaml` `paths:` + comment `:26-28` | mirror + doc comment |
| `.github/workflows/benchmark-check.yml:12` | comment only, references the concept (workflow currently `workflow_dispatch`-only, disabled) |
| `.github/workflows/scripts-tests.yaml:63` | comment noting `yq` is required for the two scripts above |
| `scripts/tests/docs-paths-regex.bats`, `scripts/tests/lint-docs-paths-sync.bats` | bats coverage for both scripts |
| `scripts/tests/Taskfile.test.yaml:47` | fixture Taskfile used by bats to test the sync/regex scripts in isolation (avoids recursion) |
| `site/src/content/docs/contributing/how-to/pr-prep.md:166,174,232,234` | contributor docs page — prose description of the sync rule |
| `docs/superpowers/specs/2026-05-14-pr-prep-docs-fast-lane-design.md` | original design spec (historical) |
| `docs/superpowers/specs/2026-05-27-docs-starlight-migration-design.md` | later migration spec touching the same globs |
| `.claude/rules/references/plan-review-learnings.md:145-147` | plan-review learnings entry citing this exact glob-compiler defect class |

Nothing else — no meta-test in `test/meta/` references `DOCS_ONLY_PATHS`, no ADR under
`docs/adr/` does either (`rg` for both across `docs/adr/` returned zero hits).

For the **dependency-only exemption list itself**, there is currently **no machine consumer
at all** — it exists only as prose in `CONTRIBUTING.md` and the PR templates (see Q3). No
workflow, script, or bats test reads or asserts on it today. This confirms CONTEXT.md's
"Claude's Discretion" reasoning that a `lint:dependency-paths-sync` gate would guard nothing
yet.

**Gap vs. CONTEXT.md's canonical-refs list:** the site docs page
`site/src/content/docs/contributing/how-to/pr-guide.md:83` also mentions "Dependency-only,
repo-config-only... PRs are exempt from the typed template" — it does **not** enumerate the
list (just links to `CONTRIBUTING.md`), so no edit is required there, but it should be
noted as a place that references the concept and would go stale in spirit (not literally)
if the exemption model changed shape. No action needed; flagging for completeness.

## Q3 — Every prose enumeration of the dependency-only exemption

**Verdict: VERIFIED — exactly two files literally enumerate the (wrong) list; two more reference the concept without enumerating (no edit needed).**

Literal enumeration with `web/bun.lock` (must change):

- `CONTRIBUTING.md:155-156` — "**Dependency-only** — the diff is confined to `go.mod`,
  `go.sum`, `web/package.json`, `web/bun.lock`, or `site/bun.lock`. Renovate PRs are exempt
  by definition."
- `.github/PULL_REQUEST_TEMPLATE.md:50-51` — identical sentence, same wording.

Reference-only, no enumeration, no `web/bun.lock`, **verified no edit needed**:

- `.github/PULL_REQUEST_TEMPLATE/chore.md:24-27` — "Dependency-only, repo-config-only
  (`.github/**`, but **not** `CODEOWNERS`), and documentation-only PRs are exempt... Full
  path lists: [CONTRIBUTING.md](...#exempt-by-file-path)."
- `.github/ISSUE_TEMPLATE/chore.yml:23-27` — same pattern, links to CONTRIBUTING.md.
- `site/src/content/docs/contributing/how-to/pr-guide.md:83` — same pattern, links to
  CONTRIBUTING.md (this one was not in CONTEXT.md's canonical-refs list; confirmed here).
- `.github/PULL_REQUEST_TEMPLATE/{fix,enhancement,feature}.md` — checked, zero mentions of
  "exempt" or "dependency-only" in any of the three.
- `CONTRIBUTING.md:109` ("Already exempt?" chore-intake paragraph) — references the concept
  by name, points at `#exempt-by-file-path`, does not enumerate; no `web/bun.lock` text
  there. No separate edit needed beyond what's already required at `:155-156`.

No other file in the repo contains the literal string `web/bun.lock` (confirmed via
whole-repo `rg`) — only the two prose sites above and the CONTEXT.md draft itself.

## Q4 — Lint gotchas for this diff

**Verdict: VERIFIED — concrete task list identified.**

Files this task touches: `CONTRIBUTING.md` (markdown), `.github/PULL_REQUEST_TEMPLATE.md`
(markdown), `Taskfile.yaml` (YAML).

| Concern | Task | Evidence |
|---|---|---|
| Markdown lint | `task lint:markdown` → `rumdl check --exclude 'site,.git,.serena,.claude,.planning' .` (`Taskfile.yaml:663`) | `.rumdl.toml` excludes `site`, not root `.github/**` or `CONTRIBUTING.md` — both ARE linted |
| Markdown format | `task fmt:markdown` → `rumdl fmt ...` (`Taskfile.yaml:906`); checked (no mutation) via `task fmt:check` → `rumdl fmt --check ...` (`Taskfile.yaml:923-924`) | same exclude list |
| MD041 (first-line-H1) | Enabled (not in `.rumdl.toml`'s `disable = [...]` list, `.rumdl.toml:5-14`) | Not a fresh risk here — both `CONTRIBUTING.md:1-5` and `.github/PULL_REQUEST_TEMPLATE.md:1-6` already open with an SPDX HTML-comment block then `# <H1>`, and this task only edits existing bullet prose inside them, not the file head |
| MD013 (line length) | **Disabled** (`.rumdl.toml:6`) | Free-form prose wrapping; no forced line-length reflow, but match the existing ~90-char soft-wrap style used at `CONTRIBUTING.md:155-156` for readability |
| YAML lint | `task lint:yaml` → `{{.GO_TOOL_LINT}} yamlfmt -lint .` (`Taskfile.yaml:666-668`) | Covers `Taskfile.yaml`; no exclude for it in `.yamlfmt` |
| YAML format | `task fmt:yaml` → `yamlfmt .` (`Taskfile.yaml:909-911`) | `retain_line_breaks: true`, `max_line_length: 120` (`.yamlfmt:5-11`) — a multi-line `DEPENDENCY_ONLY_PATHS: \|` block mirroring `DOCS_ONLY_PATHS`'s shape (`Taskfile.yaml:30-40`) will format cleanly under these settings since that's the existing precedent's exact shape |
| SPDX headers | `task license:check` / `license:add` (`Taskfile.yaml`) — n/a here, both target files already carry SPDX headers, no new files created |
| No dedicated Taskfile schema/YAML-language-server check | Confirmed none exists (see Q5) | — |

**No table-reflow risk:** neither `CONTRIBUTING.md:148-167` nor
`.github/PULL_REQUEST_TEMPLATE.md:44-60` contains a markdown table in the sections being
edited — both are bullet-list prose, so `dprint`/rumdl table-column-alignment concerns
(MD060, already disabled anyway per `.rumdl.toml:13`) don't apply.

**Exact task names to run before considering this complete:** `task lint:markdown`,
`task lint:yaml`, `task fmt:check` (covers both markdown-fmt-check and would surface any
YAML drift caught separately by `task fmt:yaml`), and full `task lint` / `task pr-prep`
per the repo's standard gate (this repo's CLAUDE.md landing-the-plane requirement).

**On the `task test` requirement** (CodeRabbit thread 3): CLAUDE.md requires `task test`
before claiming completion, and it is satisfied here rather than omitted — `task pr-prep`'s
fast lane runs schema/license/lint/fmt/**unit**/build/bats, so running `task pr-prep` runs
the unit suite. It is listed above as the umbrella gate rather than as a separate step. This
diff touches no Go code, so `task test` in isolation would add no signal beyond what
pr-prep already executed (observed `status=pass`, `exit=0`).

## Q5 — Strict/exhaustive `vars:` parsing risk

**Verdict: VERIFIED — no risk. No strict/exhaustive parser exists.**

- Neither `scripts/docs-paths-regex.sh` nor `scripts/lint-docs-paths-sync.sh` enumerates or
  asserts the full `vars:` key set — both use targeted `yq -e '.vars.<SPECIFIC_KEY>'`
  lookups (`docs-paths-regex.sh:21`, `lint-docs-paths-sync.sh:28`), which ignore all other
  keys entirely.
- No bats test asserts the full `vars:` block or key count — `rg 'vars\.' scripts/tests/*.bats`
  only matches two bats files' own synthetic *fixture* Taskfiles (`docs-paths-regex.bats`,
  `lint-docs-paths-sync.bats`), which construct isolated single-key fixture files, not the
  real repo `Taskfile.yaml`.
- No `# yaml-language-server: $schema=...` comment or JSON-schema file constrains
  `Taskfile.yaml`'s shape (confirmed via `rg` — zero hits).
- `lefthook.yaml`/pre-commit config does not exist in this repo (retired per
  `docs/superpowers/specs/2026-05-25-retire-lefthook-license-eye-design.md`) — no hook-level
  gate either.
- go-task itself does not validate `vars:` against a fixed schema; unknown/extra top-level
  var keys are simply available as template variables.

Adding `DEPENDENCY_ONLY_PATHS` as a new sibling key under `vars:` (mirroring
`DOCS_ONLY_PATHS`, `Taskfile.yaml:30-40`) is safe and will not break any existing parser,
lint, or test.

## Pitfalls for the implementer

1. **Don't reuse `scripts/docs-paths-regex.sh` for the new list as-is.** It hard-errors on
   any `**/foo` glob other than the hardcoded `**/*.md`, and silently miscompiles
   `compose*.yaml`. State this limitation explicitly in the docs/Taskfile comment (per the
   locked decision) rather than letting #4890 discover it the hard way.
2. **This PR is self-non-exempt.** Editing `Taskfile.yaml` triggers the repo's own
   "`Taskfile.yaml` and `scripts/**` are not exempt" rule (`CONTRIBUTING.md:151-152`) — the
   PR needs issue #4892 carrying `confirmed-bug` before opening, exactly as CONTEXT.md
   already notes. Don't let the fix's own diff violate the rule it's fixing.
3. **Two prose sites, not one, need the `web/bun.lock` sentence removed:**
   `CONTRIBUTING.md:155-156` AND `.github/PULL_REQUEST_TEMPLATE.md:50-51` — both carry
   *identical* wording; a fix applied to only one leaves the other stating the false claim.
4. **`chore.md` / `chore.yml` / `pr-guide.md` need NO enumeration edit** — they only link to
   `CONTRIBUTING.md#exempt-by-file-path` — but double-check after editing CONTRIBUTING.md
   that the `#exempt-by-file-path` anchor still resolves to the same heading (rumdl's
   relative-link check MD057 is disabled, so a renamed heading anchor would NOT be caught by
   lint — verify by eye).
5. **`scripts/**` carve-out sentence must be added, not just implied.** The existing
   "`Taskfile.yaml` and `scripts/**` are deliberately not exempt" sentence
   (`CONTRIBUTING.md:151-154`, `.github/PULL_REQUEST_TEMPLATE.md:56-57`) currently makes NO
   lockfile exception; leaving it as-is after adding `**/uv.lock` to the dependency-only glob
   set creates a direct contradiction (a `scripts/uv.lock`-only diff would match both "exempt"
   and "not exempt" prose) — both sentences need the explicit carve-out per the locked
   decision.
6. **Preserve `yamlfmt`'s literal-block-scalar formatting when adding the new var.** Copy
   `DOCS_ONLY_PATHS`'s exact `KEY: |` + indented-list shape (`Taskfile.yaml:30-40`) rather
   than inventing a different YAML shape (flow sequence, etc.) — `yamlfmt -lint` enforces a
   specific style and the block-scalar form is the only one proven to round-trip cleanly
   through the existing `yq -e '.vars.X'` extraction pattern used by the sibling scripts.
7. **Run `task lint:docs-paths-sync` after editing `Taskfile.yaml`** even though this task
   doesn't touch `DOCS_ONLY_PATHS` — any YAML-structural edit to the `vars:` block is worth
   a sanity check that `yamlfmt`/`yq` still parse the file correctly and the existing sync
   check still passes untouched.
