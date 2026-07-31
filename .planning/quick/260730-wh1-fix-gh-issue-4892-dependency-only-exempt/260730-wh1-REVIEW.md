---
phase: 260730-wh1-fix-gh-issue-4892-dependency-only-exempt
reviewed: 2026-07-31T00:00:00Z
depth: quick
files_reviewed: 5
files_reviewed_list:
  - Taskfile.yaml
  - CONTRIBUTING.md
  - .github/PULL_REQUEST_TEMPLATE.md
  - .github/PULL_REQUEST_TEMPLATE/chore.md
  - .github/ISSUE_TEMPLATE/chore.yml
findings:
  critical: 0
  high: 2
  medium: 3
  low: 3
  total: 8
status: issues_found
---

# Code Review — #4892 dependency-only exemption path list

> ### 📌 Pre-fix snapshot — these findings CAUSED the final policy
>
> This review examined commits `2255f14f7` and `b8fdbc486`, which carried the 10-entry set.
> Its statements that there are "ten patterns" and that `**/pyproject.toml` is missing were
> **true of the tree it reviewed** and are what prompted commit `9cb8d8f53`. Both are fixed
> in the shipped policy.
>
> The same applies to the options weighed under H-1: option (a) — exempting all
> dependency-shaped files under `scripts/` — was **considered and rejected**. The shipped
> policy takes the lockfile-only route and enumerates that subset in `Taskfile.yaml`, so a
> `scripts/` manifest stays gated.
>
> Deliberately **not** rewritten: erasing a finding because it was acted on would remove the
> evidence that review caught two contradictions before merge.


**Reviewed:** 2026-07-31
**Depth:** quick (policy/config change)
**Branch:** `fix-4892-dep-exempt-paths` @ `b8fdbc486`
**Status:** issues_found — 0 CRITICAL, 2 HIGH, 3 MEDIUM, 3 LOW

## Summary

The mechanical work is clean: both prose mirrors list exactly the ten globs in
`vars.DEPENDENCY_ONLY_PATHS`, in the same order, with no typos; the YAML block scalar
survives; the `#exempt-by-file-path` anchor and all three inbound links resolve; and every
one of the four factual claims called out for verification checks out empirically (see
[Verified claims](#verified-claims)).

The defects are in the *policy shape*, not the transcription. Two are exactly the defect
class this change exists to eliminate — a diff that two rules classify with opposite
verdicts, and an exempt glob that swallows the very gate the same paragraph says must stay
gated.

---

## HIGH

### H-1: `scripts/**` lockfile carve-out contradicts the authoritative var

**Files:**
`Taskfile.yaml:39-42` (authority claim), `Taskfile.yaml:66-76` (the ten globs),
`CONTRIBUTING.md:176-183`, `.github/PULL_REQUEST_TEMPLATE.md:66-70`,
`.github/PULL_REQUEST_TEMPLATE/chore.md:26-27`, `.github/ISSUE_TEMPLATE/chore.yml:26-28`,
`CONTRIBUTING.md:114-115`

**Issue:** `Taskfile.yaml:41-42` declares the var authoritative — *"when they disagree with
this var, THIS var wins."* The var contains no manifest-vs-lockfile distinction and no
negation/precedence mechanism. All five prose sites then assert a distinction the var cannot
express:

> Any non-lockfile change under `scripts/` stays gated. — `CONTRIBUTING.md:183`

Walk the shapes under `scripts/` that are **not** lockfiles but **are** in the var:

| Diff | Var verdict (authoritative) | Prose verdict | Agree? |
|---|---|---|---|
| `scripts/uv.lock` | exempt (`**/uv.lock`) | exempt (lockfile carve-out) | ✅ |
| `scripts/pr-prep-lock.sh` | not exempt (no shape) | gated | ✅ |
| `scripts/package.json` | **exempt** (`**/package.json`) | **gated** (not a lockfile) | ❌ |
| `scripts/Dockerfile` | **exempt** (`**/Dockerfile`) | **gated** | ❌ |
| `scripts/go.mod` | **exempt** (`**/go.mod`) | **gated** | ❌ |
| `scripts/pnpm-workspace.yaml` | **exempt** | **gated** | ❌ |
| `scripts/go.sum` | exempt | **undecidable** — is `go.sum` "a lockfile"? | ⚠️ |

Five of the ten shapes produce opposite verdicts under `scripts/`, and `go.sum` is not
classifiable at all because "lockfile" is never defined. No such file exists under
`scripts/` today (only `pyproject.toml` + `uv.lock`), so this is latent — but the rule is
declared to be *by shape*, and the authority sentence guarantees the var wins, which means
the mitigation recorded as `T-4892-03` in the plan is unenforceable by construction: #4890
consuming `DEPENDENCY_ONLY_PATHS` will exempt `scripts/package.json`.

The same contradiction sits in `CONTRIBUTING.md:114-115` and in the two templates.

**Fix (pick one, apply to all five sites):**

*(a) Express the carve-out positively so it derives from the var instead of contradicting it:*

```markdown
`Taskfile.yaml` and `scripts/**` are deliberately **not** exempt, with one carve-out. They
define `task pr-prep` and the checks CI runs, so changing them changes the gate itself. The
carve-out: a file under `scripts/` that matches `DEPENDENCY_ONLY_PATHS` (for example
`scripts/uv.lock`) **is** exempt under the dependency-only rule — a dependency manifest or
lockfile is not a gate definition. Any other change under `scripts/` stays gated.
```

*(b) Or keep "lockfile-only" and make the var carry it*, by splitting the var into
`DEPENDENCY_LOCKFILE_PATHS` (`**/go.sum`, `**/pnpm-lock.yaml`, `**/bun.lock`, `**/uv.lock`)
and `DEPENDENCY_MANIFEST_PATHS` (the rest), and stating that only the lockfile set survives
the `scripts/**` gate. This also resolves the `go.sum` ambiguity.

---

### H-2: `compose*.yaml` exempts the E2E gate's own execution environment

**Files:** `Taskfile.yaml:74` (`compose*.yaml`), `Taskfile.yaml:75-76` (`Dockerfile`,
`**/Dockerfile`), `CONTRIBUTING.md:176-179`, `.github/PULL_REQUEST_TEMPLATE.md:66-67`

**Issue:** The paragraph immediately below the glob list states the governing principle:

> `Taskfile.yaml` and `scripts/**` are deliberately **not** exempt … they define `task
> pr-prep` and the checks CI runs, so changing them changes the gate itself.
> — `CONTRIBUTING.md:176-178`

`compose*.yaml` matches five tracked files, two of which *are* the gate:

```
compose.yaml  compose.cluster.yaml  compose.prod.yaml
compose.e2e.yaml  compose.e2e.cover.yaml
```

`Taskfile.yaml:258-306` runs the required `E2E Test` CI check as
`docker compose -f compose.yaml -f compose.e2e.yaml … run --rm playwright npx playwright
test`, and the coverage lane layers `compose.e2e.cover.yaml`. Under the new rule a PR that
edits `compose.e2e.yaml` alone — dropping a service, changing the playwright entrypoint,
shortening `stop_grace_period` (which `Taskfile.yaml:352` has a guard message about) —
bypasses the issue-first gate and the typed PR template entirely. That is precisely what
the `Taskfile.yaml` exclusion exists to prevent; the two rules contradict on principle.

Same class, separate blast radius:

- `Dockerfile` / `**/Dockerfile` — a Dockerfile is a *build definition*, not a dependency
  manifest. `RUN`, `COPY`, `ENTRYPOINT`, and `USER` edits now ship gate-free.
- `compose.prod.yaml` — production topology, ports, volumes, env.
- `**/pnpm-workspace.yaml` — carries `onlyBuiltDependencies`, `patchedDependencies`,
  `overrides`, and workspace membership, not just a package list.

Accepted threat `T-4892-01` (plan line 438) covers only `package.json` `scripts.postinstall`.
It does **not** cover Dockerfile `RUN`, compose service definitions, or pnpm-workspace build
allowlists, so none of this is inside the recorded acceptance.

**Fix:** Restrict the glob set to what Renovate actually rewrites in those files, or exclude
the gate-defining ones explicitly. Minimum viable edit — drop `compose.e2e*.yaml` from the
exempt surface and say why:

```yaml
  DEPENDENCY_ONLY_PATHS: |
    **/go.mod
    …
    compose.yaml
    compose.cluster.yaml
    compose.prod.yaml
    Dockerfile
    **/Dockerfile
```

…plus a prose sentence: *"`compose.e2e.yaml` and `compose.e2e.cover.yaml` are **not**
exempt — they define the required `E2E Test` check, so changing them changes the gate."*
If the broad `compose*.yaml` / `Dockerfile` surface is a deliberate accepted risk, widen
`T-4892-01` to name build-and-deploy definitions and say so in the doc; today it is neither
narrowed nor accepted.

---

## MEDIUM

### M-1: `**/uv.lock` is almost never reachable — `pyproject.toml` is missing

**File:** `Taskfile.yaml:73`, `CONTRIBUTING.md:158-160`

**Issue:** `**/uv.lock` was added to cover the Python surface, but the manifest that Renovate
edits in the *same commit* is absent. Two tracked pairs exist:

```
scripts/pyproject.toml                              scripts/uv.lock
.claude/skills/holomush-dev/scripts/pyproject.toml  .claude/skills/…/scripts/uv.lock
```

`scripts/pyproject.toml:10-14` declares `pytest>=8.0` and `ruff>=0.9` under
`[dependency-groups]`. `.github/renovate.json` sets no `enabledManagers`, so Renovate's
`pep621` manager is on by default; it bumps `pyproject.toml` and regenerates `uv.lock`
together. Because `pyproject.toml` matches no shape, every such PR is non-exempt — the
`uv.lock` entry buys nothing for the Renovate case that motivated the issue.

This is also internally inconsistent: `**/package.json` (npm manifest) and `**/go.mod` (Go
manifest) are both present; only the Python manifest is omitted.

**Fix:** Add `**/pyproject.toml` to `DEPENDENCY_ONLY_PATHS` and to both prose mirrors —
eleven globs, not ten. (Note it inherits the H-2 concern: `pyproject.toml` also carries
`[tool.ruff]` lint config, `scripts/pyproject.toml:19-25`. If that is unacceptable, then
`**/uv.lock` should be dropped instead, and the doc should say Python bumps are gated.)

### M-2: The stated reason buf-codegen PRs are non-exempt is wrong

**Files:** `CONTRIBUTING.md:166-171`, `.github/PULL_REQUEST_TEMPLATE.md:60-65`

**Issue:** The prose says such a PR is non-exempt because it *"also carries regenerated code
(`pkg/proto/**/*.pb.go`, `web/**/*_pb.ts`)"*. That is not the operative disqualifier. The
pins those Renovate rules bump live in `buf.gen.yaml`, `buf.gen.internal.yaml`, and
`web/buf.gen.yaml` (`.github/renovate.json` `customManagers[].managerFilePatterns`). None of
those three files matches any shape in any of the three exempt sets, so the PR is non-exempt
on the pin file alone — before a single stub is regenerated.

The practical harm: a contributor who bumps `web/buf.gen.yaml` and *forgets* to run
`task web:generate` reads the stated test ("does it carry regenerated code? no") and
concludes the PR is exempt. The correct answer is that it never was.

**Fix:** State the path fact, then the regeneration duty as a consequence:

```markdown
In particular, a buf codegen pin bump is **not** exempt: the pins live in `buf.gen.yaml`,
`buf.gen.internal.yaml`, and `web/buf.gen.yaml`, none of which is a dependency shape. The
`automerge: false` on those rules in `.github/renovate.json` exists so a human runs
`task proto` / `task web:generate` and commits the regenerated
`pkg/proto/**/*.pb.go` / `web/**/*_pb.ts` stubs — real source diffs, likewise not exempt.
```

### M-3: New prose load-bears on `renovate.json`, which is itself gate-exempt

**Files:** `CONTRIBUTING.md:168-171`, `.github/PULL_REQUEST_TEMPLATE.md:62-65`

**Issue:** The new text makes `automerge: false` in `.github/renovate.json` a load-bearing
safety property of the exemption policy ("*precisely so a human runs `task proto`*"). But
`.github/renovate.json` sits inside the repo-config-only exempt set (`.github/**`,
`CONTRIBUTING.md:172-175`), and unlike `CODEOWNERS` it has no carve-out. Flipping those two
rules to `automerge: true` is therefore itself a gate-free PR that silently removes the
control the exemption text relies on.

**Fix:** Extend the existing `CODEOWNERS` carve-out sentence in the repo-config bullet:

```markdown
- **Repo configuration-only** — … Two carve-outs inside that tree: a `CODEOWNERS` file is
  **not** exempt (review ownership is a governance decision), and neither is an
  `automerge` change in `.github/renovate.json`, because the dependency-only exemption
  above relies on those rules staying `automerge: false`.
```

---

## LOW

### L-1: `compose*.yaml` is root-anchored, breaking the "by shape" framing

**File:** `Taskfile.yaml:74`

`compose*.yaml` is the only entry without a `**/` form, and `*` does not cross `/` in git
`:(glob)` or doublestar — so `test/compose.foo.yaml` would not match. Meanwhile `Dockerfile`
gets deliberate belt-and-braces (`Dockerfile` + `**/Dockerfile`, `Taskfile.yaml:62-66`).
Renovate's `docker-compose` manager matches nested compose files, so a future nested one is
managed but not exempt. All five compose files are at root today, so this is latent.

**Fix:** If H-2 is resolved by enumerating compose files, this disappears. Otherwise add
`**/compose*.yaml` alongside, matching the Dockerfile pattern, and note the asymmetry in the
comment block.

### L-2: `go.tool.mod` / `go.tool-lint.mod` and their `.sum` files match no shape

**File:** `Taskfile.yaml:67-68`

Four tracked dependency manifests are uncovered: `go.tool.mod`, `go.tool.sum`,
`go.tool-lint.mod`, `go.tool-lint.sum` (`Taskfile.yaml:23-24` runs the toolchain from them).
`**/go.mod` does not match `go.tool.mod`. Gating them is defensible — they pin the linters
that *are* the gate, exactly the `Taskfile.yaml` rationale — but the doc's "by *shape* rather
than by a fixed enumeration" framing invites a reader to assume they are covered.

**Fix:** One sentence in the `Taskfile.yaml` comment block: *"`go.tool.mod` /
`go.tool-lint.mod` are deliberately NOT covered by `**/go.mod` — they pin the lint and test
toolchain, i.e. the gate itself."* Renovate does not manage them anyway
(`.github/renovate.json` narrows `gomod` to `/(^|/)go\.mod$/`).

### L-3: "confined to" phrasing makes a cross-set diff non-exempt

**Files:** `CONTRIBUTING.md:152-175`, `.github/PULL_REQUEST_TEMPLATE.md:45-72`

Each of the three bullets reads *"the diff is confined to …"*. A diff spanning two exempt
sets — say `docs/adr/foo.md` + `go.mod`, or `.github/workflows/ci.yaml` + `Dockerfile` — is
confined to neither set individually, so by the literal text it is **not** exempt even
though every file in it is exempt-listed. The closing sentence
(`CONTRIBUTING.md:185-186`, *"If your diff touches anything outside those paths"*) implies
the opposite — union semantics. Two readings, opposite verdicts. This predates the change
but the change adds a third set, making cross-set diffs likelier, and #4890 will need the
answer.

**Fix:** Replace the closing sentence with an explicit union statement:

```markdown
These three sets are not mutually exclusive, and a diff may span them: a PR is exempt when
**every** changed path falls in at least one exempt set. If any path falls outside all
three, the PR is not exempt — you still need a linked, approved issue.
```

---

## Verified claims

Every item flagged for verification checks out. Recording so a re-reviewer does not redo it.

| Claim | Site | Verdict |
|---|---|---|
| `scripts/docs-paths-regex.sh` hard-errors `unsupported '**' position` on leading `**/`, **eight of ten** entries | `Taskfile.yaml:49-52` | **TRUE.** `scripts/docs-paths-regex.sh:33-58` case order: multi-`**` → `**/*.md` → `*'/**'` → `*'**'*` (error) → literal. `**/go.mod` ends in `.mod`, not `/**`, so it hits the error branch. Exactly 8 of the 10 entries contain `**`; none is `**/*.md`. |
| `compose*.yaml` compiles to `compose*\.yaml`, matching `compose.yaml` but not `compose.prod.yaml` | `Taskfile.yaml:53-58` | **TRUE.** No `**`, so it hits the literal branch (`docs-paths-regex.sh:52-55`), dots escaped only. ERE `compose*` = `compos` + `e*`; output is anchored `^(…)$`. |
| `.github/renovate.json` buf codegen rules set `automerge: false` | `CONTRIBUTING.md:169`, `PULL_REQUEST_TEMPLATE.md:64` | **TRUE.** Both the `"buf codegen"` and `"buf codegen go"` `packageRules` carry `"automerge": false`, and their `description` fields state the "a human runs `task proto`" rationale verbatim. |
| git `:(glob)` treats `**/Dockerfile` as matching a root-level `Dockerfile` | `Taskfile.yaml:60-66` | **TRUE.** `git ls-files -- ':(glob)**/Dockerfile'` → `Dockerfile` + `docker/postgres-backup/Dockerfile`. Go doublestar agrees (`**` matches zero segments). The belt-and-braces bare entry is harmless. |
| Prose mirrors match the authoritative var exactly | all three | **TRUE.** Both `CONTRIBUTING.md:158-161` and `PULL_REQUEST_TEMPLATE.md:56-59` list the same ten globs in the same order as `Taskfile.yaml:66-76`. No extras, none missing, no glob-string typos. |
| `web/bun.lock` removal is correct | diff | **TRUE.** No `web/bun.lock` is tracked; `web/` uses `pnpm-lock.yaml` + `pnpm-workspace.yaml`. `site/bun.lock` is the only bun lockfile and is covered by `**/bun.lock`. |
| YAML still parses; block scalar intact | `.github/ISSUE_TEMPLATE/chore.yml` | **TRUE.** `yq -e '.' … ` exit 0; the `body[].attributes.value` markdown scalar renders with the new sentence and the trailing link intact. |
| `#exempt-by-file-path` anchor + three inbound links | — | **TRUE.** Heading `### Exempt by file path` at `CONTRIBUTING.md:148`. Inbound: `CONTRIBUTING.md:111`, `.github/PULL_REQUEST_TEMPLATE/chore.md:28`, `.github/ISSUE_TEMPLATE/chore.yml:29`. All resolve. No broken emphasis; longest added line 97 chars. |
| `site/package.json` + `site/bun.lock` overlap (docs-only via `site/**` **and** dependency-only) | — | **Benign.** Both rules return *exempt*; no verdict conflict, and `site/**` already exempted `site/package.json` before this change, so no widening. |

Scenarios from the review brief that resolve cleanly: `scripts/pr-prep-lock.sh` (gated,
consistent) · `go.mod`+`go.sum`+`pkg/proto/foo.pb.go` (not exempt, stated explicitly) ·
`.github/CODEOWNERS` (not exempt, explicit carve-out) · `Taskfile.yaml`+`go.mod` (not
exempt — `Taskfile.yaml` is in no set) · `scripts/uv.lock` (exempt, both rules agree).

---

_Reviewed: 2026-07-31_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: quick_
