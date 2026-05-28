<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Docs Diátaxis IA Implementation Plan (SP2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-organize the Starlight docs into an audience-first / Diátaxis-within folder structure, flip the sidebar to `autogenerate`, retire two superseded docs, and rewrite internal links — preserving branding byte-identical.

**Architecture:** A single checked-in **slug map** (`scripts/migration/sp2-slug-map.tsv`) drives everything: a move script relocates each doc into its audience/mode folder, a link codemod rewrites root-absolute links via the same map, and a parity check verifies coverage. **The sidebar is flipped to `autogenerate` first** — once the nav has no per-slug references, subsequent deletes/moves can't break the build (an explicit `{slug}` sidebar throws `AstroUserError` on a missing target). Content is **not** rewritten for mode-purity (follow-up beads). No redirects (SP1 stance).

**Tech Stack:** Astro Starlight, bun, `linkinator` (link check), bats (invariant tests), `jj` (VCS). Codemods are bun-run `.mjs` (Node 22+/Bun `fs.globSync`).

**Spec:** `docs/superpowers/specs/2026-05-28-docs-diataxis-ia-design.md`
**Design bead:** `holomush-44nxc` · **Program anchor:** `holomush-rkwyb`

---

## File Structure

| Path | Responsibility | Action |
| --- | --- | --- |
| `scripts/migration/sp2-slug-map.tsv` | Old→new slug map (52 moves) — DRY driver for moves, links, parity | Create |
| `scripts/migration/sp2-move.mjs` | Reads map; creates mode dirs + moves files | Create |
| `scripts/migration/sp2-rewrite-links.mjs` | Rewrites root-absolute internal links via the map | Create |
| `site/astro.config.mjs` | `sidebar` → `autogenerate` per section (branding fields untouched) | Modify |
| `site/src/content/docs/**` | Docs relocated into `<audience>/<mode>/` buckets | Move |
| `site/src/content/docs/contributing/event-delivery.md`, `operating/legacy-id-cutover.md` | Superseded | Delete |
| `site/tsconfig.json` | Add `~/*` → `src/*` path alias (preserve `include`/`exclude`) | Modify |
| `scripts/check-docs-ia.sh` + `scripts/tests/docs-ia.bats` | Parity (INV-1), one-bucket (INV-2), retired-gone (INV-3), branding (INV-5), nav≤7 (INV-6) | Create |
| `scripts/tests/fixtures/sp2-slug-map.tsv` | Slug-map retained as fixture after move (Task 10) | Move |

> **Lifecycle:** runs in the `sp2-diataxis` jj workspace (branched from `main@origin`). Commit per task with `jj commit`/`jj describe`; run `task fmt:markdown` before each docs commit. Codemods under `scripts/migration/` are deleted in Task 10 (the map is kept as a fixture). Run `.mjs` codemods with **bun**, not pre-22 `node` (`fs.globSync`).
>
> **Ordering rationale:** sidebar→autogenerate (Task 2) precedes retire (Task 3) and move (Task 4) precisely so the build never references a moved/deleted slug. `autogenerate:{directory}` re-derives the nav from whatever files exist, so it tolerates the retire and the re-bucketing transparently (orphans simply surface as soon as autogenerate is on — that is the goal).

---

## Phase 1: Foundation

### Task 1: Author the slug-map fixture

The single source of truth for the re-org. Format: `old-slug<TAB>new-slug`, one move per line; retires and unchanged pages are NOT listed (they don't move).

**Files:**

- Create: `scripts/migration/sp2-slug-map.tsv`

- [ ] **Step 1: Write the map** (verbatim — mirrors the spec's bucketing tables)

```tsv
guide/the-world	guide/explanation/the-world
guide/connecting	guide/how-to/connecting
guide/building	guide/how-to/building
guide/commands	guide/reference/commands
operating/installation	operating/how-to/deploy/installation
operating/deployment	operating/how-to/deploy/deployment
operating/verifying-releases	operating/how-to/deploy/verifying-releases
operating/database	operating/how-to/database
operating/ca-rotation	operating/how-to/ca-rotation
operating/crypto-setup	operating/how-to/crypto/crypto-setup
operating/crypto-runbook	operating/how-to/crypto/crypto-runbook
operating/crypto-monitoring	operating/how-to/crypto/crypto-monitoring
operating/telnet-security	operating/how-to/telnet-security
operating/sentry	operating/how-to/sentry
operating/plugin-reloads	operating/how-to/plugin-reloads
operating/sandbox-operations	operating/how-to/sandbox/sandbox-operations
operating/sandbox-restore	operating/how-to/sandbox/sandbox-restore
operating/operations	operating/how-to/operations
operating/configuration	operating/reference/configuration
operating/authentication	operating/explanation/authentication
operating/plugin-security	operating/explanation/plugin-security
extending/getting-started	extending/tutorials/getting-started
extending/lua-plugins	extending/tutorials/lua-plugins
extending/binary-plugins	extending/tutorials/binary-plugins
extending/plugin-guide	extending/tutorials/plugin-guide
extending/verb-registration	extending/how-to/verb-registration
extending/audit-events	extending/how-to/audit-events
extending/event-sensitivity	extending/how-to/event-sensitivity
extending/plugin-config	extending/how-to/plugin-config
extending/plugin-crypto-readback	extending/how-to/plugin-crypto-readback
extending/plugin-host-evaluate	extending/how-to/plugin-host-evaluate
extending/access-control	extending/how-to/access-control
extending/abac-attribute-resolver	extending/how-to/abac-attribute-resolver
extending/api-guide	extending/reference/api-guide
extending/substrate-contract	extending/reference/substrate-contract
extending/actor-kinds-claimable	extending/reference/actor-kinds-claimable
extending/events	extending/reference/events
extending/audit-chain	extending/explanation/audit-chain
contributing/database-migrations	contributing/how-to/database-migrations
contributing/pr-guide	contributing/how-to/pr-guide
contributing/pr-prep	contributing/how-to/pr-prep
contributing/quarantine	contributing/how-to/quarantine
contributing/integration-tests	contributing/how-to/integration-tests
contributing/sessions	contributing/how-to/sessions
contributing/coding-standards	contributing/reference/coding-standards
contributing/architecture	contributing/explanation/architecture
contributing/authentication	contributing/explanation/authentication
contributing/event-store	contributing/explanation/event-store
contributing/event-emit-pipeline	contributing/explanation/event-emit-pipeline
contributing/gateway-boundary	contributing/explanation/gateway-boundary
contributing/hostfunc-context-audit	contributing/explanation/hostfunc-context-audit
contributing/lifecycle-and-health	contributing/explanation/lifecycle-and-health
```

> `extending/events` (⚑) overlaps `reference/events` (generated). Before committing, open both: if `extending/events` duplicates the generated reference, drop that row from the map and instead **retire it + cross-link to `/reference/events/`** (add to Task 3's retire list). Decide per content.

- [ ] **Step 2: Validate the map**

```bash
cd /Volumes/Code/github.com/holomush/.worktrees/sp2-diataxis
awk -F'\t' 'NF!=2{print "BAD ROW: "$0; bad=1} END{if(bad)exit 1; print NR" rows"}' scripts/migration/sp2-slug-map.tsv
cut -f1 scripts/migration/sp2-slug-map.tsv | while read s; do test -f "site/src/content/docs/$s.md" -o -f "site/src/content/docs/$s.mdx" || echo "MISSING SOURCE: $s"; done
cut -f2 scripts/migration/sp2-slug-map.tsv | sort | uniq -d | sed 's/^/DUP TARGET: /'
```

Expected: `52 rows`, no `MISSING SOURCE`, no `DUP TARGET`.

- [ ] **Step 3: Commit**

`jj commit -m "docs(ia): add SP2 slug map fixture (holomush-44nxc)"`

### Task 2: Flip the sidebar to autogenerate (BEFORE any move/delete)

Doing this first removes every per-slug reference from the nav, so the build survives the retire (Task 3) and move (Task 4). `autogenerate` re-derives the nav from whatever files exist — orphans surface immediately (intended).

**Files:**

- Modify: `site/astro.config.mjs` (`sidebar` field only)

- [ ] **Step 1: Replace the explicit `sidebar` array**

Replace the entire `sidebar: [ … ]` (currently 43 explicit `{ slug }` entries) with autogenerate per audience section. Touch **nothing else** in the file (branding fields stay byte-identical — INV-5):

```javascript
      sidebar: [
        { label: 'Guide', autogenerate: { directory: 'guide' } },
        { label: 'Operating', autogenerate: { directory: 'operating' } },
        { label: 'Extending', autogenerate: { directory: 'extending' } },
        { label: 'Contributing', autogenerate: { directory: 'contributing' } },
        { label: 'Reference', autogenerate: { directory: 'reference' } },
      ],
```

- [ ] **Step 2: Build — now green and stays green**

```bash
cd site && bunx astro build
```

Expected: PASS. The sidebar now lists current files (including the ~21 orphans, flat for now) with no broken references.

- [ ] **Step 3: Commit**

`jj commit -m "docs(ia): autogenerate sidebar (pre-move, unbreakable nav) (holomush-44nxc)"`

---

## Phase 2: Retire superseded docs

### Task 3: Delete the two superseded docs + scrub inbound links

**Files:**

- Delete: `site/src/content/docs/contributing/event-delivery.md`, `site/src/content/docs/operating/legacy-id-cutover.md`

- [ ] **Step 1: Confirm they're still superseded**

```bash
rg -n "Superseded" site/src/content/docs/contributing/event-delivery.md
rg -q "legacy_id" internal/eventbus/no_legacy_id_grep_test.go && echo "legacy_id elimination still enforced"
```

Expected: superseded banner present; grep-test still enforces elimination.

- [ ] **Step 2: Find inbound links to either doc**

```bash
rg -n '\]\((/[^)]*event-delivery|/[^)]*legacy-id-cutover)' site/src/content/docs
```

- [ ] **Step 3: Remove/repoint those links**

For each hit: if incidental, delete the link (keep prose); if it points to replacement material, repoint (event-delivery → `/contributing/explanation/event-store/`; legacy-id-cutover → drop). No link may resolve to a retired slug.

- [ ] **Step 4: Delete the files**

```bash
rm -f site/src/content/docs/contributing/event-delivery.md site/src/content/docs/operating/legacy-id-cutover.md
```

- [ ] **Step 5: Build + verify zero inbound links (INV-3)**

```bash
cd site && bunx astro build && cd ..
rg -c '\](/[^)]*(event-delivery|legacy-id-cutover))' site/src/content/docs || echo "0 inbound links — good"
```

Expected: build PASS (autogenerate already dropped the deleted pages); no inbound-link matches.

- [ ] **Step 6: Commit**

`jj commit -m "docs(ia): retire superseded event-delivery + legacy-id-cutover (holomush-44nxc)"`

---

## Phase 3: Move docs into buckets

### Task 4: Move every doc per the slug map

**Files:**

- Create: `scripts/migration/sp2-move.mjs`
- Move: all 52 mapped docs under `site/src/content/docs/`

- [ ] **Step 1: Write the move script**

```javascript
// scripts/migration/sp2-move.mjs — read the slug map; for each old→new,
// create the new parent dir and move the file (preserving .md/.mdx ext).
import { readFileSync, existsSync, mkdirSync, renameSync } from 'node:fs';
import { dirname } from 'node:path';
const base = 'site/src/content/docs';
const rows = readFileSync('scripts/migration/sp2-slug-map.tsv', 'utf8')
  .trim().split('\n').map(l => l.split('\t'));
for (const [oldSlug, newSlug] of rows) {
  const ext = existsSync(`${base}/${oldSlug}.mdx`) ? '.mdx' : '.md';
  const src = `${base}/${oldSlug}${ext}`;
  const dst = `${base}/${newSlug}${ext}`;
  if (!existsSync(src)) { console.error('MISSING', src); process.exit(1); }
  mkdirSync(dirname(dst), { recursive: true });
  renameSync(src, dst);
}
console.log(`moved ${rows.length} files`);
```

- [ ] **Step 2: Run it**

```bash
bun scripts/migration/sp2-move.mjs
```

Expected: `moved 52 files`.

- [ ] **Step 3: Verify sources gone, targets present**

```bash
cut -f1 scripts/migration/sp2-slug-map.tsv | while read s; do test -e "site/src/content/docs/$s.md" -o -e "site/src/content/docs/$s.mdx" && echo "STILL THERE: $s"; done
cut -f2 scripts/migration/sp2-slug-map.tsv | while read s; do test -e "site/src/content/docs/$s.md" -o -e "site/src/content/docs/$s.mdx" || echo "MISSING TARGET: $s"; done
```

Expected: no output.

- [ ] **Step 4: Build (green; content links are stale until Task 5)**

```bash
cd site && bunx astro build
```

Expected: `astro build` SUCCEEDS — the autogenerated sidebar (Task 2) re-derives from the new tree, and stale **content** `](/old/)` links are hrefs that don't fail `astro build` (caught by `linkinator` in Task 8, fixed in Task 5).

- [ ] **Step 5: Commit**

`jj commit -m "docs(ia): move docs into audience/mode buckets (holomush-44nxc)"`

---

## Phase 4: Links + alias

### Task 5: Rewrite internal links via the slug map

**Files:**

- Create: `scripts/migration/sp2-rewrite-links.mjs`
- Modify: all docs containing links to moved slugs

- [ ] **Step 1: Write the link codemod**

```javascript
// scripts/migration/sp2-rewrite-links.mjs — rewrite root-absolute internal
// links ](/old/) → ](/new/) for every moved slug. The slug is terminated by
// `/?(#…)?)` so a longer slug (/operating/operations-foo) is NOT matched by a
// shorter one (/operating/operations). Operates on all .md/.mdx in the collection.
import { readFileSync, writeFileSync, globSync } from 'node:fs';
const map = new Map(readFileSync('scripts/migration/sp2-slug-map.tsv', 'utf8')
  .trim().split('\n').map(l => l.split('\t')));
const files = globSync('site/src/content/docs/**/*.{md,mdx}');
for (const f of files) {
  let s = readFileSync(f, 'utf8'); let changed = false;
  for (const [oldSlug, newSlug] of map) {
    const re = new RegExp(`\\]\\(/${oldSlug}/?(#[^)]*)?\\)`, 'g');
    s = s.replace(re, (_m, anchor = '') => { changed = true; return `](/${newSlug}/${anchor})`; });
  }
  if (changed) writeFileSync(f, s);
}
console.log('links rewritten');
```

- [ ] **Step 2: Run it + verify no stale links**

```bash
bun scripts/migration/sp2-rewrite-links.mjs
cut -f1 scripts/migration/sp2-slug-map.tsv | while read s; do rg -q "\]\(/$s/?[)#]" site/src/content/docs && echo "STALE LINK to $s"; done
```

Expected: `links rewritten`, no `STALE LINK`.

- [ ] **Step 3: Commit**

`jj commit -m "docs(ia): rewrite internal links to new slugs (holomush-44nxc)"`

### Task 6: Intra-group sidebar order + `~/*` tsconfig alias

**Files:**

- Modify: section/ordered pages' frontmatter; `site/tsconfig.json`

- [ ] **Step 1: Set intra-group order where alphabetical is wrong**

`autogenerate` orders alphabetically by default. For pages whose order matters (e.g. `getting-started` before `lua-plugins`; `installation` before `deployment`; section `index` first), add `sidebar:` frontmatter:

```yaml
---
title: "Getting Started with Plugins"
sidebar:
  order: 1
---
```

Apply `order: 0` to each section `index`; ordered sequences get ascending `order`; leave the rest alphabetical.

- [ ] **Step 2: Add the `~/*` alias (preserve existing `include`/`exclude`)**

The current `site/tsconfig.json` is `{ "extends": "astro/tsconfigs/strict", "include": [".astro/types.d.ts", "**/*"], "exclude": ["dist"] }`. Add `compilerOptions` **without dropping** the other keys:

```jsonc
{
  "extends": "astro/tsconfigs/strict",
  "compilerOptions": { "baseUrl": ".", "paths": { "~/*": ["src/*"] } },
  "include": [".astro/types.d.ts", "**/*"],
  "exclude": ["dist"]
}
```

- [ ] **Step 3: Build + eyeball nav**

```bash
cd site && bunx astro build
```

Then `bunx astro dev` — confirm all 5 sections render with mode subgroups, every previously-orphaned doc appears, ordering is right, and the accent color/logo are unchanged.

- [ ] **Step 4: Commit**

`jj commit -m "docs(ia): sidebar order frontmatter + ~/* tsconfig alias (holomush-44nxc)"`

---

## Phase 5: Verification + follow-ups

### Task 7: Invariant check scripts (INV-1/2/3/5/6)

**Files:**

- Create: `scripts/check-docs-ia.sh`, `scripts/tests/docs-ia.bats`

- [ ] **Step 1: Write `check-docs-ia.sh`** — it MUST assert:

- **INV-1 parity:** every content slug (all `.md`/`.mdx` under `site/src/content/docs` except root `index.mdx`) resolves to a built page `site/dist/<slug>/index.html` after `task docs:build`.
- **INV-2 one-bucket:** no `.md`/`.mdx` sits directly under an audience dir except `index.*` — every non-index doc is under `<audience>/<mode>/…` (`reference/` is exempt; it's flat by design).
- **INV-3 retired-gone:** `contributing/event-delivery.*` and `operating/legacy-id-cutover.*` are absent and no link resolves to their slugs (`rg` over the collection).
- **INV-5 branding:** compare the working copy against **`main@origin`** (the SP2 branch base) using the **jj-native** `jj diff --from main@origin -- <paths>` (the worktree has no `.git`, so bare `git diff` fails): assert `site/src/styles/custom.css`, `site/src/assets/logo.png`, `site/public/favicon.png` are **unchanged** (empty diff); `site/astro.config.mjs` differs **only** within the `sidebar` field; `site/tsconfig.json` differs **only** by the added `compilerOptions.paths` alias.
- **INV-6 nav≤7:** ≤7 top-level sidebar sections; flag any single mode folder with >7 direct children (operators' `how-to` is sub-grouped per the spec).

- [ ] **Step 2: Write `docs-ia.bats`** wrapping the script with `assert_success`, plus a meta-test asserting `scripts/tests/fixtures/sp2-slug-map.tsv` (relocated in Task 10) has 52 rows. (Until Task 10 relocates it, point the meta-test at `scripts/migration/sp2-slug-map.tsv`; update the path in Task 10 Step 1.)

- [ ] **Step 3: Run**

```bash
cd site && bunx astro build && cd .. && bats scripts/tests/docs-ia.bats
```

Expected: PASS.

- [ ] **Step 4: Commit**

`jj commit -m "test(ia): IA parity/bucket/retire/branding/nav invariants (holomush-44nxc)"`

### Task 8: Link-check + llms.txt (INV-4, INV-7)

**Files:** none (uses `task docs:linkcheck` from SP1; verification only)

- [ ] **Step 1: Build + link-check**

```bash
task docs:linkcheck
```

Expected: zero broken internal links (INV-4).

- [ ] **Step 2: Verify llms outputs regenerated (INV-7)**

```bash
for f in llms.txt llms-full.txt llms-small.txt; do test -s "site/dist/$f" && echo "OK $f" || echo "MISSING $f"; done
```

Expected: all OK.

### Task 9: File follow-up content-surgery beads

**Files:** none (bd only)

- [ ] **Step 1: For each ⚑ doc that mixes modes, file a follow-up**

For each of `operating/operations`, `operating/authentication`, `operating/plugin-security`, `extending/plugin-guide`, `extending/plugin-config`, `extending/access-control`, `extending/substrate-contract`, `extending/events`, `contributing/integration-tests`, `contributing/hostfunc-context-audit` — open it; if it mixes Diátaxis modes, file:

```bash
bd create --type=task --priority=3 -l theme:docs-platform \
  --title="Mode-purity rewrite: <new-slug>" \
  --description="<new-slug> mixes Diátaxis modes; split/rewrite to one pure mode. Follow-up from SP2 (holomush-44nxc). See docs/superpowers/specs/2026-05-28-docs-diataxis-ia-design.md."
```

If a doc is already single-mode, note it and skip. Record the filed IDs: `bd note holomush-44nxc "SP2 follow-ups: <ids>"`.

### Task 10: Final sweep + cleanup + pr-prep

**Files:**

- Move: `scripts/migration/sp2-slug-map.tsv` → `scripts/tests/fixtures/sp2-slug-map.tsv`
- Delete: `scripts/migration/` (one-shot codemods)

- [ ] **Step 1: Keep the map as a fixture; delete the codemods**

```bash
mkdir -p scripts/tests/fixtures
mv scripts/migration/sp2-slug-map.tsv scripts/tests/fixtures/sp2-slug-map.tsv
rm -rf scripts/migration
```

Update the `docs-ia.bats` 52-row meta-test path (Task 7 Step 2) to `scripts/tests/fixtures/sp2-slug-map.tsv`. `check-docs-ia.sh` derives the retired list inline (does not depend on the map at steady state).

- [ ] **Step 2: Full sweep**

```bash
cd site && rm -rf dist && cd .. && task docs:build && task docs:linkcheck && bats scripts/tests/docs-ia.bats
```

Expected: all green.

- [ ] **Step 3: `task fmt:markdown` then `task pr-prep`**

```bash
task fmt:markdown && task pr-prep
```

Expected: pass marker `✓ Fast PR checks passed`.

- [ ] **Step 4: Commit**

`jj commit -m "docs(ia): finalize Diátaxis re-org; retain slug-map fixture, drop codemods (holomush-44nxc)"`

---

## Out of scope (follow-on)

- Mode-purity content rewrites (Task 9 beads).
- Net-new tutorials (player getting-started, etc.).
- SP0 (proto comments), SP4 (gRPC coverage).
<!-- adr-capture: sha256=3a25324db37e82b1; session=2f5ef07e; ts=2026-05-28T13:14:26Z; adrs=holomush-md3k4,holomush-38kmt -->
