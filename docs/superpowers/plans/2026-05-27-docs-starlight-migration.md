<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Docs Site Migration to Astro Starlight Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the HoloMUSH docs site from zensical (Python/uv) to Astro Starlight (Node/bun) as a strict lift-and-shift — identical content, structure, and navigation — and emit `llms.txt`.

**Architecture:** Astro + `@astrojs/starlight` at `site/`, content as an Astro collection at `site/src/content/docs/`, built with bun, deployed to the same Cloudflare Pages project. The explicit sidebar reproduces today's `zensical.toml` nav 1:1 (no IA change). All three doc generators (`docs:proto`, `docs:gen-events`, `generate:ebnf`) are re-pointed at the new tree. Mermaid renders client-side via a Starlight plugin to avoid the build-time conflict with Starlight's syntax highlighter.

**Tech Stack:** Astro 5, `@astrojs/starlight`, bun (pnpm/npm fallback), `starlight-llms-txt`, `@pasqal-io/starlight-client-mermaid`, Pagefind (built-in), Cloudflare Pages, `linkinator` (link check).

**Spec:** `docs/superpowers/specs/2026-05-27-docs-starlight-migration-design.md`
**ADRs:** `holomush-145ko` (adopt Astro Starlight), `holomush-qf2oo` (bun package manager), `holomush-xneg2` (client-side mermaid)

---

## File Structure

Files created / modified, grouped by responsibility:

| Path | Responsibility | Action |
| --- | --- | --- |
| `site/package.json` | Node project manifest (bun) — deps, scripts | Create |
| `site/astro.config.mjs` | Astro+Starlight config: title, logo, social, palette, sidebar, plugins | Create |
| `site/src/content.config.ts` | Astro content-collection definition (Starlight `docsLoader`/`docsSchema`) | Create |
| `site/src/content/docs/**` | Migrated markdown content (moved from `site/docs/**`) | Move + transform |
| `site/src/assets/**` | Logo, favicon, guide images | Move |
| `site/public/reference/policy-dsl.ebnf`, `…-railroad.html` | Static (non-markdown) generated artifacts | New generator destination |
| `site/src/styles/custom.css` | Brand palette (slate/deep-orange/amber) as Starlight CSS vars | Create |
| `site/.gitignore` | Ignore `node_modules`, `dist`, `.astro` | Create |
| `Taskfile.yaml` (`docs:*`, `docs:proto`, `docs:gen-events`, `generate:ebnf`) | Re-point build/setup/serve + generators | Modify |
| `scripts/gen-event-docs.sh` | Event-doc generator output path + frontmatter | Modify |
| `.github/workflows/site.yml` | bun setup + `astro build` + deploy `site/dist` | Modify |
| `scripts/tests/license-eye.bats`, `generate-ebnf-check.bats`, `pr-prep-docs-detection.bats`, `docs-paths-regex.bats` | Re-path stale `site/docs/` references | Modify |
| `site/zensical.toml`, `site/.python-version`, `site/pyproject.toml`/`uv.lock` | zensical/uv surface | Delete |
| `scripts/check-docs-parity.sh` + `scripts/tests/docs-parity.bats` | Parity manifest + meta-test (INV-1/INV-5) | Create |

> **Lifecycle reminder:** This plan runs in the `docs-starlight` jj workspace. Per the VCS preamble, commit with `jj commit -m "..."`/`jj describe`; never `jj new` before committing in-flight work. Run `task fmt:markdown` before each commit touching docs.
>
> **Codemod discipline:** The transform tasks (frontmatter, admonitions, links) are one-shot codemods over ~65 files. Each writes a small script under `scripts/migration/` (deleted in Task 19), runs it, and the verification is `astro build` + targeted `rg` checks. Do NOT hand-edit 65 files.

---

## Phase 1: Scaffold

### Task 1: Scaffold Astro + Starlight at `site/` (alongside zensical)

Bring up a buildable Starlight project in a temporary subdir, then move its scaffolding into `site/` next to the existing zensical files (which stay until Task 19). This keeps each commit buildable.

**Files:**

- Create: `site/package.json`, `site/astro.config.mjs`, `site/src/content.config.ts`, `site/.gitignore`, `site/tsconfig.json`
- Create: `site/src/content/docs/index.md` (temporary placeholder; replaced in Task 3)

- [ ] **Step 1: Scaffold into a scratch dir**

Run (from repo root):

```bash
cd /tmp && rm -rf hm-starlight && bun create astro@latest hm-starlight -- --template starlight --no-install --no-git --yes
```

Expected: `/tmp/hm-starlight/` with `astro.config.mjs`, `package.json`, `src/content.config.ts`, `src/content/docs/index.mdx`.

- [ ] **Step 2: Copy scaffold files into `site/`**

Copy `package.json`, `astro.config.mjs`, `src/content.config.ts`, `tsconfig.json`, and a `.gitignore` into `site/`. Add SPDX headers where the repo requires them (`*.mjs`, `*.ts` — see `task license:check`). Do not copy the scratch `src/content/docs/` yet (Task 3 moves the real content).

- [ ] **Step 3: Pin bun + install**

Run:

```bash
cd site && bun install
```

Expected: `site/bun.lock` created, `site/node_modules/` populated. Commit `bun.lock`.

- [ ] **Step 4: Add a placeholder home page and build**

Create `site/src/content/docs/index.md`:

```markdown
---
title: HoloMUSH
description: Modern MUSH platform with Lua & Go plugins
---

Placeholder — replaced during content migration.
```

- [ ] **Step 5: Verify the scaffold builds**

Run:

```bash
cd site && bunx astro build
```

Expected: build succeeds; `site/dist/index.html` exists.

- [ ] **Step 6: Commit**

`jj commit -m "build(site): scaffold Astro Starlight alongside zensical (holomush-cwnu0)"`

### Task 2: Base Starlight config (branding parity)

**Files:**

- Modify: `site/astro.config.mjs`
- Create: `site/src/styles/custom.css`
- Move: `site/docs/assets/logo.png`, `favicon.png` → `site/src/assets/` (copy now; the old paths are removed in Task 19)

- [ ] **Step 1: Write `astro.config.mjs` base config**

Reproduce the zensical metadata (`site/zensical.toml`: `site_name`, `site_description`, repo link, slate/deep-orange/amber palette):

```javascript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://holomush.dev',
  integrations: [
    starlight({
      title: 'HoloMUSH',
      description: 'Modern MUSH platform with Lua & Go plugins',
      logo: { src: './src/assets/logo.png', alt: 'HoloMUSH' },
      favicon: '/favicon.png',
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/holomush/holomush' }],
      customCss: ['./src/styles/custom.css'],
      // sidebar added in Task 12; plugins (mermaid, llms-txt) added in Tasks 8 & 13
    }),
  ],
});
```

- [ ] **Step 2: Brand palette in `custom.css`**

Map the zensical deep-orange/amber accents onto Starlight's CSS custom properties (`--sl-color-accent*`). Use the deep-orange hue family for `--sl-color-accent` and amber for hover. (Reference: Starlight "CSS & styling" — accent color tokens.)

- [ ] **Step 3: Build and eyeball**

Run: `cd site && bunx astro build` → PASS. Run `bunx astro dev` and confirm logo, title, and accent color render. (Manual visual check.)

- [ ] **Step 4: Commit**

`jj commit -m "build(site): base Starlight branding config (holomush-cwnu0)"`

---

## Phase 2: Content migration

### Task 3: Move content tree into the collection

**Files:**

- Move: `site/docs/**/*.md` → `site/src/content/docs/**/*.md`
- Move: `site/docs/guide/images/**`, `site/docs/assets/**` → `site/src/assets/**`
- Delete: temporary `site/src/content/docs/index.md` from Task 1 (real `index.md` arrives in the move)

- [ ] **Step 1: Move markdown**

```bash
mkdir -p site/src/content/docs
git -C . mv site/docs/* site/src/content/docs/ 2>/dev/null || (cp -R site/docs/. site/src/content/docs/ && echo "copied")
```

Then relocate assets to `site/src/assets/` and fix in-page image paths in a later codemod (Task 7 handles links; images are covered there too).

- [ ] **Step 2: Build (expect frontmatter errors)**

Run: `cd site && bunx astro build`
Expected: FAIL — pages without a `title` frontmatter error. This is expected and fixed in Task 4.

- [ ] **Step 3: Commit the move (build-red is acceptable mid-phase)**

`jj commit -m "refactor(site): move docs into Astro content collection (holomush-cwnu0)"`

### Task 4: Frontmatter codemod (title from H1)

**Files:**

- Create: `scripts/migration/add-frontmatter.mjs`
- Modify: all `site/src/content/docs/**/*.md`

- [ ] **Step 1: Write the codemod**

```javascript
// scripts/migration/add-frontmatter.mjs — for each .md/.mdx under the docs
// collection: if no YAML frontmatter, take the first `# H1` as title, remove
// that H1 line, and prepend `---\ntitle: <h1>\n---`. Idempotent: skip files
// that already start with `---`.
import { readFileSync, writeFileSync } from 'node:fs';
import { globSync } from 'node:fs';
const files = globSync('site/src/content/docs/**/*.{md,mdx}');
for (const f of files) {
  let s = readFileSync(f, 'utf8');
  if (s.startsWith('---')) continue;
  const m = s.match(/^#\s+(.+)$/m);
  const title = (m ? m[1] : f.split('/').pop().replace(/\.mdx?$/, '')).replace(/"/g, '\\"');
  if (m) s = s.replace(m[0] + '\n', '');
  writeFileSync(f, `---\ntitle: "${title}"\n---\n\n${s.replace(/^\n+/, '')}`);
}
```

- [ ] **Step 2: Run it**

Run: `bun scripts/migration/add-frontmatter.mjs`

> Run via **bun** (not `node` on a pre-22 runtime) — the codemod uses `fs.globSync`, which exists only in Node 22+ / Bun 1.x. Same applies to the Task 5/7 codemods.

- [ ] **Step 3: Build**

Run: `cd site && bunx astro build`
Expected: PASS (or remaining failures are admonition/tab/link issues handled in Tasks 5-7, not frontmatter). Confirm `rg -L '^---' site/src/content/docs -g '*.md' -g '*.mdx'` returns nothing missing frontmatter (every file starts with `---`).

- [ ] **Step 4: Commit**

`jj commit -m "refactor(site): add Starlight title frontmatter to all pages (holomush-cwnu0)"`

### Task 5: Admonitions → Starlight asides

**Files:**

- Create: `scripts/migration/admonitions.mjs`
- Modify: the 5 files containing `!!!`/`???` (`operating/authentication.md`, `operating/configuration.md`, `index.md`, `guide/building.md`, `extending/plugin-guide.md` — verify with `rg -l '^\s*(!!!|\?\?\?)' site/src/content/docs`)

- [ ] **Step 1: Write the codemod**

Map MkDocs admonition syntax to Starlight asides. MkDocs:

```text
!!! note "Title"
    body line
```

→ Starlight:

```text
:::note[Title]
body line
:::
```

Handle types `note|tip|info→note`, `warning|caution→caution`, `danger|error→danger`. Dedent the 4-space admonition body. Write `scripts/migration/admonitions.mjs` to transform all matches.

- [ ] **Step 2: Run + build**

Run: `bun scripts/migration/admonitions.mjs && cd site && bunx astro build`
Expected: PASS for the admonition files. Verify `rg -c '^\s*!!!' site/src/content/docs` returns 0.

- [ ] **Step 3: Commit**

`jj commit -m "refactor(site): convert MkDocs admonitions to Starlight asides (holomush-cwnu0)"`

### Task 6: Content tabs → MDX (`plugin-guide`)

**Files:**

- Modify → rename: `site/src/content/docs/extending/plugin-guide.md` → `plugin-guide.mdx`

- [ ] **Step 1: Convert the 10 `=== "Tab"` blocks**

Rename the file to `.mdx`, add the import, and convert each pymdownx tab group:

```mdx
import { Tabs, TabItem } from '@astrojs/starlight/components';

<Tabs>
  <TabItem label="Lua">…</TabItem>
  <TabItem label="Go">…</TabItem>
</Tabs>
```

- [ ] **Step 2: Build**

Run: `cd site && bunx astro build`
Expected: PASS. Verify no other file still uses `=== "`: `rg -l '^\s*=== "' site/src/content/docs` → empty.

- [ ] **Step 3: Commit**

`jj commit -m "refactor(site): convert plugin-guide content tabs to MDX Tabs (holomush-cwnu0)"`

### Task 7: Internal link + image path codemod

**Files:**

- Create: `scripts/migration/links.mjs`
- Modify: the ~45 files containing `](…​.md)` links and any moved-image references

- [ ] **Step 1: Write the codemod**

Rewrite `](path/to/page.md)` and `](page.md#anchor)` to Starlight slug links: strip `.md`/`.mdx`, make root-relative under the docs base (e.g. `](../operating/configuration.md)` → `](/operating/configuration/)`). Resolve relative paths against the file's own location. Rewrite moved image refs (`assets/…`, `images/…`) to the new `~/assets/` or `/` static path. Leave external `http(s)://` links untouched.

- [ ] **Step 2: Run + build**

Run: `bun scripts/migration/links.mjs && cd site && bunx astro build`
Expected: PASS. Spot-check: `rg -c '\]\([^)]*\.md[)#]' site/src/content/docs` → 0 internal `.md` links remain.

- [ ] **Step 3: Commit**

`jj commit -m "refactor(site): rewrite internal links to Starlight slugs (holomush-cwnu0)"`

### Task 8: Mermaid (client-side Starlight plugin)

**Files:**

- Modify: `site/astro.config.mjs`, `site/package.json`

- [ ] **Step 1: Install the plugin**

Run: `cd site && bun add @pasqal-io/starlight-client-mermaid`

> **Why this one:** build-time renderers (`astro-mermaid`, `rehype-mermaid`) conflict with Starlight's `astro-expressive-code` syntax highlighter (breaks all code-block highlighting) and `rehype-mermaid` needs Playwright in the build container. The client-side Starlight plugin avoids both (grounded: spec § References; `holomush-cwnu0` mermaid grounding note).

- [ ] **Step 2: Register it in the Starlight `plugins` array**

```javascript
import starlightClientMermaid from '@pasqal-io/starlight-client-mermaid';
// inside starlight({ ... }):
plugins: [starlightClientMermaid()],
```

- [ ] **Step 3: Build + verify code highlighting intact**

Run: `cd site && bunx astro build` → PASS. In `bunx astro dev`, open one of the 7 mermaid pages (e.g. `/operating/crypto-runbook/`) and confirm the diagram renders AND a nearby fenced code block still has syntax highlighting (the expressive-code regression check).

- [ ] **Step 4: Commit**

`jj commit -m "build(site): client-side mermaid via Starlight plugin (holomush-cwnu0)"`

---

## Phase 3: Generators

### Task 9: Re-point `docs:proto` into the collection

**Files:**

- Modify: `Taskfile.yaml` `docs:proto` (currently emits `site/docs/reference/grpc-api.md` at ~L1110-1127)

- [ ] **Step 1: Change output path + add frontmatter**

Point `--doc_opt`/output to `site/src/content/docs/reference/grpc-api.md`. Add a post-generation step (mirroring the existing `perl` link-fixup) that prepends Starlight frontmatter:

```bash
printf -- '---\ntitle: "gRPC API Reference"\n---\n\n%s' "$(cat site/src/content/docs/reference/grpc-api.md)" > site/src/content/docs/reference/grpc-api.md.tmp && mv site/src/content/docs/reference/grpc-api.md.tmp site/src/content/docs/reference/grpc-api.md
```

Update the task's `generates:` to the new path. Keep the proto input list unchanged (coverage is SP4).

- [ ] **Step 2: Regenerate + build + reproducibility**

Run: `task docs:proto && cd site && bunx astro build` → PASS. Run `task docs:proto` again, then `jj diff site/src/content/docs/reference/grpc-api.md` → empty (INV-4).

- [ ] **Step 3: Commit**

`jj commit -m "build(docs): emit gRPC reference into Starlight collection (holomush-cwnu0)"`

### Task 10: Re-point `docs:gen-events`

**Files:**

- Modify: `Taskfile.yaml` `docs:gen-events` (L1086-1095) `generates:` paths
- Modify: `scripts/gen-event-docs.sh` (output dir + per-file `title` frontmatter)

- [ ] **Step 1: Update the script output path**

In `scripts/gen-event-docs.sh`, change the output base from `site/docs/reference/events` to `site/src/content/docs/reference/events` and ensure each emitted `.md` (including `events.md`) begins with `---\ntitle: "<name>"\n---`.

- [ ] **Step 2: Update the Taskfile `generates:` list**

Point `docs:gen-events` `generates:` at `site/src/content/docs/reference/events/*.md` and `…/events.md`.

- [ ] **Step 3: Regenerate + build + reproducibility**

Run: `task docs:gen-events && cd site && bunx astro build` → PASS. Re-run and `jj diff` the events dir → empty (INV-4). Confirm `task docs:build` (which deps `docs:gen-events`) succeeds end-to-end.

- [ ] **Step 4: Commit**

`jj commit -m "build(docs): emit event reference into Starlight collection (holomush-cwnu0)"`

### Task 11: Re-point `generate:ebnf` to static assets

**Files:**

- Modify: `Taskfile.yaml` `generate:ebnf` + `generate:ebnf:check` (L~370-394)

- [ ] **Step 1: Change EBNF/HTML output to `site/public/reference/`**

The `.ebnf` and `.html` are not content pages — emit them to `site/public/reference/policy-dsl.ebnf` and `…-railroad.html` (served at `/reference/policy-dsl.ebnf`). Update `EBNF=`/`RAIL=` vars, the `sources:`/`generates:` lists, and the `generate:ebnf:check` paths.

- [ ] **Step 2: Fix the link from `access-control.md`**

The page that links the DSL artifacts (`reference/access-control.md`) must point at `/reference/policy-dsl.ebnf` and `/reference/policy-dsl-railroad.html` (root static paths). Update via Task 7's link codemod re-run or a targeted edit.

- [ ] **Step 3: Regenerate + build + check**

Run: `task generate:ebnf && task generate:ebnf:check && cd site && bunx astro build` → PASS. Confirm `site/dist/reference/policy-dsl.ebnf` exists in the build output.

- [ ] **Step 4: Commit**

`jj commit -m "build(docs): emit policy-DSL EBNF/railroad as static assets (holomush-cwnu0)"`

---

## Phase 4: Navigation + llms.txt

### Task 12: Explicit sidebar 1:1 from `zensical.toml`

**Files:**

- Modify: `site/astro.config.mjs` (add `sidebar`)

- [ ] **Step 1: Translate the nav**

Reproduce `site/zensical.toml`'s five sections and exact page order as a Starlight `sidebar` array. Each zensical entry `"operating/deployment.md"` becomes `{ slug: 'operating/deployment' }`. **Do not** use `autogenerate` (that surfaces orphans + superseded — deferred to SP2). Keep the same orphan-hidden set.

```javascript
sidebar: [
  { label: 'Guide', items: [
    { slug: 'guide' }, { slug: 'guide/the-world' }, { slug: 'guide/connecting' },
    { slug: 'guide/commands' }, { slug: 'guide/building' },
  ] },
  { label: 'Operating', items: [
    { slug: 'operating' }, { slug: 'operating/deployment' }, { slug: 'operating/installation' },
    { slug: 'operating/configuration' }, { slug: 'operating/database' },
    { slug: 'operating/authentication' }, { slug: 'operating/telnet-security' },
    { slug: 'operating/ca-rotation' }, { slug: 'operating/crypto-setup' },
    { slug: 'operating/operations' }, { slug: 'operating/sentry' },
    { slug: 'operating/verifying-releases' },
  ] },
  { label: 'Extending', items: [
    { slug: 'extending' }, { slug: 'extending/getting-started' },
    { slug: 'extending/plugin-guide' }, { slug: 'extending/plugin-config' },
    { slug: 'extending/access-control' }, { slug: 'extending/abac-attribute-resolver' },
    { slug: 'extending/event-sensitivity' }, { slug: 'extending/plugin-crypto-readback' },
    { slug: 'extending/api-guide' }, { slug: 'extending/events' },
  ] },
  { label: 'Contributing', items: [
    { slug: 'contributing' }, { slug: 'contributing/architecture' },
    { slug: 'contributing/coding-standards' }, { slug: 'contributing/authentication' },
    { slug: 'contributing/database-migrations' }, { slug: 'contributing/event-store' },
    { slug: 'contributing/event-delivery' }, { slug: 'contributing/event-emit-pipeline' },
    { slug: 'contributing/hostfunc-context-audit' }, { slug: 'contributing/lifecycle-and-health' },
    { slug: 'contributing/pr-guide' }, { slug: 'contributing/sessions' },
  ] },
  { label: 'Reference', items: [
    { slug: 'reference' }, { slug: 'reference/access-control' },
    { slug: 'reference/grpc-api' }, { slug: 'reference/events' },
  ] },
],
```

> This list mirrors `site/zensical.toml` (the five `[[project.nav]]` sections) exactly as of this workspace's `main`: **Guide 5, Operating 12, Extending 10, Contributing 12, Reference 4** entries. It includes the still-linked `contributing/event-delivery` (retirement is SP2). `extending/plugin-guide` keeps the `plugin-guide` slug even though the file becomes `.mdx` (slugs are extension-independent).
>
> **MANDATORY before committing:** re-read `site/zensical.toml` *in your working copy* (not a cached value) and diff the section entry-counts against the numbers above. The nav grows as features land — this exact list was already once stale by 4 pages. If the counts differ, reconcile before proceeding, or Task 18's parity check (INV-1) fails.

- [ ] **Step 2: Build + parity eyeball**

Run: `cd site && bunx astro build` → PASS. In `bunx astro dev`, confirm the sidebar shows the same five sections in the same order as the live zensical site, and that `index.md` (home) plus the orphans remain absent.

- [ ] **Step 3: Commit**

`jj commit -m "build(site): explicit sidebar reproducing zensical nav 1:1 (holomush-cwnu0)"`

### Task 13: `llms.txt` generation

**Files:**

- Modify: `site/astro.config.mjs`, `site/package.json`

- [ ] **Step 1: Install + register**

Run: `cd site && bun add starlight-llms-txt`. Add to the Starlight `plugins` array:

```javascript
import starlightLlmsTxt from 'starlight-llms-txt';
// inside plugins: [ … , starlightLlmsTxt({ projectName: 'HoloMUSH' }) ]
```

- [ ] **Step 2: Build + verify outputs**

Run: `cd site && bunx astro build`
Expected: `site/dist/llms.txt`, `site/dist/llms-full.txt`, `site/dist/llms-small.txt` all exist and are non-empty (INV-6).

```bash
for f in llms.txt llms-full.txt llms-small.txt; do test -s "site/dist/$f" && echo "OK $f" || echo "MISSING $f"; done
```

- [ ] **Step 3: Commit**

`jj commit -m "build(site): generate llms.txt via starlight-llms-txt (holomush-cwnu0)"`

---

## Phase 5: CI / tooling / lint

### Task 14: Re-point Taskfile `docs:*`

**Files:**

- Modify: `Taskfile.yaml` `docs:setup` (L1080), `docs:build` (L1097), `docs:serve` (L1104)

- [ ] **Step 1: Rewrite the three tasks**

```yaml
docs:setup:
  desc: Set up documentation dev environment
  dir: site
  cmds:
    - bun install   # fallback order bun → pnpm → npm per ADR holomush-qf2oo

docs:build:
  desc: Build documentation site
  deps: ['docs:gen-events']
  dir: site
  cmds:
    - bunx astro build

docs:serve:
  desc: Start documentation dev server
  dir: site
  cmds:
    - bunx astro dev
```

- [ ] **Step 2: Verify**

Run: `task docs:setup && task docs:build`
Expected: PASS; `site/dist/` populated (build deps `docs:gen-events`, which now writes into the collection).

- [ ] **Step 3: Commit**

`jj commit -m "build(docs): point Taskfile docs tasks at bun/astro (holomush-cwnu0)"`

### Task 15: CI deploy workflow

**Files:**

- Modify: `.github/workflows/site.yml`

- [ ] **Step 1: Replace uv/zensical with bun/astro**

Swap the `Install uv` + `Build site` steps for bun setup + astro build, and deploy `site/dist`:

```yaml
      - name: Install bun
        uses: oven-sh/setup-bun@<pinned-sha> # vN — verify latest on marketplace
        with:
          bun-version: latest
      - name: Build site
        working-directory: site
        run: bun install --frozen-lockfile && bunx astro build
      - name: Deploy to Cloudflare Pages
        if: github.ref == 'refs/heads/main'
        uses: cloudflare/wrangler-action@ebbaa1584979971c8614a24965b4405ff95890e0 # v4
        with:
          apiToken: ${{ secrets.CLOUDFLARE_API_TOKEN }}
          accountId: ${{ secrets.CLOUDFLARE_ACCOUNT_ID }}
          command: pages deploy site/dist --project-name=holomush-site
```

> Pin `oven-sh/setup-bun` to a release SHA verified on the GitHub marketplace at implementation time (per the project's action-version rule). The workflow-file edit triggers the security hook — apply the verbatim retry, don't rewrite to appease it.

- [ ] **Step 2: Verify locally what CI will run**

Run: `cd site && bun install --frozen-lockfile && bunx astro build` → PASS.

- [ ] **Step 3: Commit**

`jj commit -m "ci(site): build with bun/astro, deploy site/dist (holomush-cwnu0)"`

### Task 16: bats path fixes + docs-skip verification

**Files:**

- Modify: `scripts/tests/license-eye.bats:35`, `scripts/tests/generate-ebnf-check.bats:19`, `scripts/tests/pr-prep-docs-detection.bats`, `scripts/tests/docs-paths-regex.bats`

- [ ] **Step 1: Re-path `license-eye.bats`**

Change `site/docs/guide site/docs/operating site/docs/reference` → `site/src/content/docs/guide site/src/content/docs/operating site/src/content/docs/reference`. (Without this the `rg` runs on nonexistent paths, exits 1, and the test passes spuriously — masking the check.)

- [ ] **Step 2: Re-path `generate-ebnf-check.bats:19`**

Update `EBNF=site/docs/reference/policy-dsl.ebnf` to the new static path `site/public/reference/policy-dsl.ebnf` (matching Task 11).

- [ ] **Step 3: Refresh docs-detection example paths**

In `pr-prep-docs-detection.bats` and `docs-paths-regex.bats`, change `site/docs/index.md` examples to `site/src/content/docs/index.md`. (`DOCS_ONLY_PATHS` is already `site/**` so detection still works; this only keeps the fixtures honest. Confirm with `task docs-paths-sync`.)

- [ ] **Step 4: Run the bats suites**

Run: `bats scripts/tests/license-eye.bats scripts/tests/generate-ebnf-check.bats scripts/tests/pr-prep-docs-detection.bats scripts/tests/docs-paths-regex.bats`
Expected: all PASS.

- [ ] **Step 5: Commit**

`jj commit -m "test: re-path docs bats fixtures to content collection (holomush-cwnu0)"`

### Task 17: rumdl flavor + link-check + mdx check

**Files:**

- Modify: `site/.rumdl.toml` (L8 `flavor`, L17 `MD046`)
- Modify: `Taskfile.yaml` (add a `docs:linkcheck` task) and `.github/workflows/site.yml` (run it)

- [ ] **Step 1: Resolve `flavor = "mkdocs"`**

Starlight is not MkDocs. Change `flavor` to the rumdl default (or `commonmark`) and remove the `MD046` "for tabs" carve-out (tabs are now MDX). Run `task fmt:markdown` and fix any newly-surfaced violations in the migrated content.

- [ ] **Step 2: Add a link checker (INV-2)**

Add `docs:linkcheck` that builds then runs `linkinator` against `site/dist` (internal links only):

```yaml
docs:linkcheck:
  dir: site
  deps: ['docs:build']
  cmds:
    - bunx linkinator dist --recurse --silent
```

Add a CI step running `task docs:linkcheck`. (`astro check` is NOT a link checker — it only type-checks; add it separately for the `.mdx` file if desired.)

- [ ] **Step 3: Verify**

Run: `task docs:linkcheck`
Expected: PASS, zero broken internal links (INV-2).

- [ ] **Step 4: Commit**

`jj commit -m "ci(docs): rumdl flavor fix + linkinator link check (holomush-cwnu0)"`

---

## Phase 6: Parity, decommission, invariants

### Task 18: Parity manifest + meta-test (INV-1, INV-5)

**Files:**

- Create: `scripts/tests/fixtures/zensical-nav.txt`, `scripts/check-docs-parity.sh`, `scripts/tests/docs-parity.bats`

- [ ] **Step 1: Capture the zensical nav as a fixture (before Task 19 deletes `zensical.toml`)**

Extract every `"<path>.md"` entry from the five `[[project.nav]]` sections into a slug-per-line fixture:

```bash
mkdir -p scripts/tests/fixtures
rg -o '"([a-z0-9/-]+)\.md"' -r '$1' site/zensical.toml > scripts/tests/fixtures/zensical-nav.txt
wc -l scripts/tests/fixtures/zensical-nav.txt   # sanity: 43 entries (5+12+10+12+4)
```

This must run while `site/zensical.toml` still exists (this task precedes Task 19). Commit the fixture so the parity check survives decommission.

- [ ] **Step 2: Write the parity check**

`check-docs-parity.sh` reads `scripts/tests/fixtures/zensical-nav.txt` and asserts each slug has a corresponding built page under `site/dist/<slug>/index.html` (treating `<dir>/index` → `<dir>/index.html`). Also assert migrated page count == source markdown count under `site/src/content/docs` (INV-5).

- [ ] **Step 3: Write the meta-test**

`docs-parity.bats`: assert the fixture's entry count equals 43 (so the fixture can't silently shrink), then run `check-docs-parity.sh` and assert success.

- [ ] **Step 4: Run**

Run: `cd site && bunx astro build && bats scripts/tests/docs-parity.bats`
Expected: PASS.

- [ ] **Step 5: Commit**

`jj commit -m "test(docs): nav parity manifest + meta-test (holomush-cwnu0)"`

### Task 19: Decommission zensical (INV-7)

**Files:**

- Delete: `site/zensical.toml`, `site/docs/` (old tree), `site/pyproject.toml`, `site/uv.lock`, `site/.python-version` (whichever exist), `scripts/migration/`
- Modify: `.github/workflows/ci-docs-skip.yaml` if it names zensical-specific paths

- [ ] **Step 1: Remove the zensical/uv surface + migration scripts**

```bash
git rm -r site/zensical.toml site/docs 2>/dev/null; rm -rf scripts/migration
# remove site/pyproject.toml site/uv.lock site/.python-version if present
```

- [ ] **Step 2: Add the INV-7 guard**

Add a CI/`pr-prep` `rg` guard asserting no `zensical` or `uv run` remains in `Taskfile.yaml`, `.github/workflows/`, or `site/` config:

```bash
! rg -n 'zensical|uv run' Taskfile.yaml .github/workflows site --glob '!site/dist/**' --glob '!**/node_modules/**'
```

- [ ] **Step 3: Full build from clean**

Run: `rm -rf site/dist && task docs:build && task docs:linkcheck && bats scripts/tests/docs-parity.bats`
Expected: all PASS with the old tree gone.

- [ ] **Step 4: Commit**

`jj commit -m "build(site): decommission zensical/uv docs surface (holomush-cwnu0)"`

### Task 20: Final invariant sweep

**Files:** none (verification only)

- [ ] **Step 1: Run the full gate**

```bash
task docs:build              # INV-3 build green (deps gen-events)
task docs:proto && jj diff site/src/content/docs/reference/grpc-api.md   # INV-4 reproducible (empty)
task docs:linkcheck          # INV-2 no broken links
bats scripts/tests/docs-parity.bats        # INV-1 + INV-5
for f in llms.txt llms-full.txt llms-small.txt; do test -s site/dist/$f; done   # INV-6
rg -n 'zensical|uv run' Taskfile.yaml .github/workflows site --glob '!site/dist/**' --glob '!**/node_modules/**'   # INV-7 (expect 0)
```

Expected: every check passes / returns empty.

- [ ] **Step 2: Run `task pr-prep`**

Run: `task pr-prep` (docs-affecting; confirm it runs the right lane and is green).

- [ ] **Step 3: Commit (if pr-prep produced regenerated artifacts)**

`jj commit -m "build(site): finalize Starlight migration; all invariants green (holomush-cwnu0)"`

---

## Out of scope (follow-on sub-projects)

- **SP2** — Diátaxis re-bucketing, flip sidebar to `autogenerate`, orphan triage + superseded retirement (`event-delivery.md`, `legacy-id-cutover.md`).
- **SP0** — proto per-field doc comments + buf `COMMENTS` ratchet.
- **SP4** — complete gRPC service coverage (migrate `docs:proto` to buf; all 12 services with field descriptions).
<!-- adr-capture: sha256=642c0cc5552d09ea; session=2f5ef07e; ts=2026-05-27T20:08:58Z; adrs=holomush-xneg2 -->
