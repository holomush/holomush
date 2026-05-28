<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Software Brand Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the dated glossy-orb `holomush.dev` brand with the "holographic terminal" system — a cyan app-tile mark (`h` + cutout) and a `>holomush_` monospace command-line wordmark with an amber underscore accent — across favicon, header logo, OG card, GitHub assets, the docs-site palette, and project guidance.

**Architecture:** A single re-runnable Node/bun generator (`site/scripts/build-brand-assets.mjs`) is the source of truth for all raster/vector assets. It uses `opentype.js` to outline JetBrains Mono glyphs to SVG paths (font-independent output, INV-3), composes the master SVGs (tile + light/dark lockups), and uses `sharp` (already a site dep) to rasterize PNG favicons and composite the OG card over a one-time fal.ai-generated backdrop. Site wiring is `astro.config.mjs` (logo/favicon/head) + `custom.css` (cyan-only Starlight accent tokens). Brand rules are codified in `.claude/rules/branding.md` + `site/CLAUDE.md`.

**Tech Stack:** Astro + Starlight, `opentype.js`, `sharp`, JetBrains Mono (OFL), fal.ai (Nano Banana Pro for OG backdrop), CSS custom properties.

**Spec:** `docs/superpowers/specs/2026-05-28-holomush-software-brand-refresh-design.md` (8 RFC2119 invariants — INV-1 amber-cursor-only, INV-3 outline-to-paths, INV-5 light/dark, INV-6 game-world-out-of-scope, INV-7 tokens-in-custom.css, INV-8 guidance).

**Palette tokens:** cyan-bright `#3dd6f7` · tile gradient `#34d6f6 → #1565c0` · cyan-deep `#1565c0` · amber `#ffb300` (cursor only) · ink `#0b0c0e`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `site/scripts/build-brand-assets.mjs` | **Create.** Generator: outlines glyphs, writes master SVGs, rasterizes PNGs, composites OG card. Source of truth. |
| `site/scripts/lib/glyphs.mjs` | **Create.** Loads JetBrains Mono TTF, returns SVG path `d` for a string at a baseline/size via opentype.js. |
| `site/src/assets/brand/fonts/JetBrainsMono-Medium.ttf` | **Create.** Committed OFL font for outlining (build-time only). |
| `site/src/assets/brand/og-backdrop.png` | **Create.** One-time fal.ai holographic backdrop (1200×630), committed source. |
| `site/src/assets/logo-dark.svg` | **Create.** Header lockup, dark mode (cyan-bright wordmark). Generated. |
| `site/src/assets/logo-light.svg` | **Create.** Header lockup, light mode (cyan-deep wordmark). Generated. |
| `site/public/favicon.svg` | **Create.** Primary favicon (tile, outlined `h` + amber underscore). Generated. |
| `site/public/favicon-16.png` / `-32.png` / `-48.png` | **Create.** PNG fallbacks. Generated. |
| `site/public/apple-touch-icon.png` | **Create.** 180×180 on opaque ink. Generated. |
| `site/public/og-card.png` | **Create.** 1200×630 social card. Generated. |
| `site/public/favicon.png` | **Replace** (regenerated 32px) or **delete** (superseded by `.svg`). |
| `site/src/assets/logo.png`, `site/src/assets/favicon.png` | **Delete.** Superseded. |
| `site/astro.config.mjs:14-15` | **Modify.** `logo: {light,dark}`, `favicon: '/favicon.svg'`, `head: [...]`. |
| `site/src/styles/custom.css:4-16` | **Modify.** Cyan-only Starlight accent tokens (light+dark) + `--brand-*` tokens. |
| `site/package.json` | **Modify.** Add `opentype.js` devDep + `brand:build` script. |
| `.claude/rules/branding.md` | **Create.** Auto-loading brand rules (paths-scoped). |
| `site/CLAUDE.md` | **Modify.** Add `## Branding` section. |
| `CLAUDE.md` (root) | **Modify.** Pointer to branding guidance. |
| `assets/brand/og-avatar.png`, README banner | **Create.** GitHub org avatar + README banner; embed banner in `README.md`. |

---

## Phase 1: Toolchain

### Task 1: Brand toolchain deps + font

**Files:**

- Modify: `site/package.json`
- Create: `site/src/assets/brand/fonts/JetBrainsMono-Medium.ttf`
- Create: `site/scripts/` (directory)

- [ ] **Step 1: Obtain the font**

Download JetBrains Mono from the official source (`https://github.com/JetBrains/JetBrainsMono/releases` — verify the latest release tag first; the family is OFL-licensed and redistributable). Extract `fonts/ttf/JetBrainsMono-Medium.ttf` into `site/src/assets/brand/fonts/`.

Run: `ls -la site/src/assets/brand/fonts/JetBrainsMono-Medium.ttf`
Expected: file present, ~200KB.

- [ ] **Step 2: Add the outlining dependency**

In `site/`, run: `bun add -d opentype.js`
Expected: `opentype.js` appears under `devDependencies` in `site/package.json`. (`sharp` is already a dependency — do not re-add.)

- [ ] **Step 3: Add the build script entry**

In `site/package.json` `scripts`, add:

```json
"brand:build": "node scripts/build-brand-assets.mjs"
```

- [ ] **Step 4: Verify opentype loads the font**

Run: `cd site && node -e "Promise.all([import('opentype.js'),import('node:fs')]).then(([o,fs])=>{const b=fs.readFileSync('src/assets/brand/fonts/JetBrainsMono-Medium.ttf');const f=o.parse(b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength));console.log('units/em',f.unitsPerEm,'h-path-ok',!!f.getPath('h',0,0,100).toPathData())})"`
Expected: prints `units/em 1000 h-path-ok true` (unitsPerEm may differ; the boolean MUST be `true`).

- [ ] **Step 5: Commit**

Commit (this is a jj-colocated repo — `jj describe`/`jj commit`, see the `jj:jujutsu` skill): `feat(brand): add JetBrains Mono + opentype.js for asset outlining (holomush-7daup)`

---

### Task 2: Glyph-outlining helper

**Files:**

- Create: `site/scripts/lib/glyphs.mjs`
- Test: inline node assertion (Step 2)

- [ ] **Step 1: Write the helper**

```javascript
// site/scripts/lib/glyphs.mjs
// opentype.js ESM exposes NAMED exports only (no default export). Use the
// namespace/named form under Node ESM, and parse() (loadSync is deprecated).
import { parse } from 'opentype.js';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const FONT = resolve(here, '../../src/assets/brand/fonts/JetBrainsMono-Medium.ttf');
const _buf = readFileSync(FONT);
const font = parse(_buf.buffer.slice(_buf.byteOffset, _buf.byteOffset + _buf.byteLength));

// Return { d, width } for `text` rendered with baseline at (x,y), em size `size`.
export function glyphPath(text, x, y, size) {
  const path = font.getPath(text, x, y, size);
  const adv = font.getAdvanceWidth(text, size);
  return { d: path.toPathData(2), width: adv };
}

export const EM = font.unitsPerEm;
```

- [ ] **Step 2: Verify it emits a path and advance width**

Run: `cd site && node -e "import('./scripts/lib/glyphs.mjs').then(m=>{const g=m.glyphPath('h',0,160,160);console.log('hasD', g.d.startsWith('M'), 'w>0', g.width>0)})"`
Expected: `hasD true w>0 true`

- [ ] **Step 3: Commit**

`feat(brand): glyph-to-path helper via opentype (holomush-7daup)`

---

## Phase 2: Master vector assets

### Task 3: Tile mark → `favicon.svg`

The tile: 256×256 rounded square, cyan gradient, lowercase `h` knocked out as a transparent cutout (via mask), solid amber underscore. The `h` is an outlined path (INV-3), not `<text>`.

**Files:**

- Modify: `site/scripts/build-brand-assets.mjs` (create in this task)
- Create (generated): `site/public/favicon.svg`

- [ ] **Step 1: Write the generator's tile section**

```javascript
// site/scripts/build-brand-assets.mjs
// All imports MUST stay at the top of the file (ESM requirement). `sharp` is
// imported here for use in later tasks (raster/composite); unused until Task 5.
import { writeFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import sharp from 'sharp';
import { glyphPath } from './lib/glyphs.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const PUBLIC = resolve(here, '../public');
const ASSETS = resolve(here, '../src/assets');
mkdirSync(PUBLIC, { recursive: true });

const CYAN1 = '#34d6f6', CYAN2 = '#1565c0', AMBER = '#ffb300';

// h outlined: baseline y=188, x=60, size 160 (see spec reference geometry)
const h = glyphPath('h', 60, 188, 160);

const tileSvg = `<svg viewBox="0 0 256 256" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="HoloMUSH">
<defs>
<linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${CYAN1}"/><stop offset="1" stop-color="${CYAN2}"/></linearGradient>
<mask id="cut"><rect width="256" height="256" rx="58" fill="#fff"/><path d="${h.d}" fill="#000"/></mask>
</defs>
<rect width="256" height="256" rx="58" fill="url(#g)" mask="url(#cut)"/>
<rect x="158" y="170" width="46" height="16" rx="3" fill="${AMBER}"/>
</svg>
`;
writeFileSync(`${PUBLIC}/favicon.svg`, tileSvg);
console.log('wrote favicon.svg');
```

- [ ] **Step 2: Run the generator**

Run: `cd site && node scripts/build-brand-assets.mjs`
Expected: prints `wrote favicon.svg`; `site/public/favicon.svg` exists.

- [ ] **Step 3: Verify it is valid SVG and renders at favicon sizes**

Run: `cd site && for s in 16 32 48; do rsvg-convert -w $s -h $s public/favicon.svg -o /tmp/fav-$s.png && magick identify /tmp/fav-$s.png | head -1; done`
Expected: three lines, each reporting a `PNG <s>x<s>` image with no parse error. Open `/tmp/fav-16.png` and confirm the `h` is readable and the amber underscore is visible (INV-2: underscore may be faint at 16px — acceptable as long as the `h` reads).

- [ ] **Step 4: Commit**

`feat(brand): generate tile favicon.svg with outlined h + amber cursor (holomush-7daup)`

---

### Task 4: Header lockups (light + dark) → `logo-light.svg` / `logo-dark.svg`

The horizontal lockup: tile (left) + `>holomush_` wordmark (right). Wordmark = dimmed prompt `>`, `holomush` in cyan (bright on dark, deep on light), trailing amber underscore. All glyphs outlined.

**Files:**

- Modify: `site/scripts/build-brand-assets.mjs`
- Create (generated): `site/src/assets/logo-dark.svg`, `site/src/assets/logo-light.svg`

- [ ] **Step 1: Add the lockup builder to the generator**

Append to `build-brand-assets.mjs` (before any final log):

```javascript
// --- Header lockups -------------------------------------------------
// Reusable inner tile (no aria; embedded). 96px tile on a 520x128 canvas.
function tileGroup(idSuffix) {
  // The tile is the full 256-unit artwork, scaled to 96px and offset into the
  // 128px-tall lockup canvas. The h path uses the same full-tile coords as the
  // standalone favicon (60,188,160).
  return `<g transform="translate(16,16) scale(0.375)">
<defs><linearGradient id="g${idSuffix}" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${CYAN1}"/><stop offset="1" stop-color="${CYAN2}"/></linearGradient>
<mask id="cut${idSuffix}"><rect width="256" height="256" rx="58" fill="#fff"/><path d="${glyphPath('h',60,188,160).d}" fill="#000"/></mask></defs>
<rect width="256" height="256" rx="58" fill="url(#g${idSuffix})" mask="url(#cut${idSuffix})"/>
<rect x="158" y="170" width="46" height="16" rx="3" fill="${AMBER}"/></g>`;
}

function lockup({ id, wordColor, promptOpacity }) {
  const baseY = 84, size = 54, wordX = 120;
  const prompt = glyphPath('>', wordX, baseY, size);
  const word = glyphPath('holomush', wordX + prompt.width, baseY, size);
  const cursorX = wordX + prompt.width + word.width + 8;
  return `<svg viewBox="0 0 ${Math.ceil(cursorX + 44)} 128" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="HoloMUSH">
${tileGroup(id)}
<path d="${prompt.d}" fill="${wordColor}" opacity="${promptOpacity}"/>
<path d="${word.d}" fill="${wordColor}"/>
<rect x="${cursorX}" y="${baseY - 8}" width="30" height="8" rx="2" fill="${AMBER}"/>
</svg>
`;
}

writeFileSync(`${ASSETS}/logo-dark.svg`, lockup({ id: 'd', wordColor: '#3dd6f7', promptOpacity: '0.5' }));
writeFileSync(`${ASSETS}/logo-light.svg`, lockup({ id: 'l', wordColor: '#1565c0', promptOpacity: '0.55' }));
console.log('wrote logo-dark.svg, logo-light.svg');
```

- [ ] **Step 2: Run and verify both lockups render**

Run: `cd site && node scripts/build-brand-assets.mjs && rsvg-convert src/assets/logo-dark.svg -o /tmp/logo-dark.png && rsvg-convert src/assets/logo-light.svg -o /tmp/logo-light.png && magick identify /tmp/logo-dark.png /tmp/logo-light.png`
Expected: two PNGs reported, no parse error. Open both: tile + `>holomush_` reads cleanly; underscore is amber; dark variant uses bright cyan, light variant uses deep cyan. If glyph spacing or vertical centering looks off, adjust `baseY`/`wordX`/`scale` constants and re-run (this is the one expected tuning loop — verify by eye against the brainstorm `board-final` reference).

- [ ] **Step 3: Commit**

`feat(brand): generate light/dark header lockups with outlined wordmark (holomush-7daup)`

---

## Phase 3: Rasterized assets

### Task 5: PNG favicons + apple-touch icon

**Files:**

- Modify: `site/scripts/build-brand-assets.mjs`
- Create (generated): `site/public/favicon-16.png`, `-32.png`, `-48.png`, `site/public/favicon.png`, `site/public/apple-touch-icon.png`

- [ ] **Step 1: Add sharp rasterization**

Append to `build-brand-assets.mjs` (the `sharp` import is already at the top of the file from Task 3 — do **not** add another import here):

```javascript
const faviconSvgBuf = Buffer.from(tileSvg);
for (const s of [16, 32, 48]) {
  await sharp(faviconSvgBuf, { density: 384 }).resize(s, s).png().toFile(`${PUBLIC}/favicon-${s}.png`);
}
// Legacy /favicon.png path (Safari/older) = 32px tile
await sharp(faviconSvgBuf, { density: 384 }).resize(32, 32).png().toFile(`${PUBLIC}/favicon.png`);
// apple-touch 180 on opaque ink (no transparency for iOS)
await sharp(faviconSvgBuf, { density: 720 })
  .resize(180, 180)
  .flatten({ background: '#0b0c0e' })
  .png().toFile(`${PUBLIC}/apple-touch-icon.png`);
console.log('wrote PNG favicons + apple-touch-icon');
```

(Note: the file's top-level code must run in an async context — wrap the script body in `async function main(){…}; await main();` or rely on top-level `await`, which Node ESM supports. Use top-level `await`.)

- [ ] **Step 2: Run and verify dimensions**

Run: `cd site && node scripts/build-brand-assets.mjs && for f in favicon-16 favicon-32 favicon-48 favicon apple-touch-icon; do magick identify public/$f.png | head -1; done`
Expected: `favicon-16` → 16×16, `-32`/`favicon` → 32×32, `-48` → 48×48, `apple-touch-icon` → 180×180, the apple-touch one fully opaque.

- [ ] **Step 3: Commit**

`feat(brand): rasterize PNG favicons + apple-touch icon via sharp (holomush-7daup)`

---

### Task 6: OG / social card (fal.ai backdrop + composite)

**Files:**

- Create: `site/src/assets/brand/og-backdrop.png` (one-time, generated by the implementing agent via fal.ai)
- Modify: `site/scripts/build-brand-assets.mjs`
- Create (generated): `site/public/og-card.png`

- [ ] **Step 1: Generate the backdrop (one-time, agent action)**

Using the fal.ai MCP `run_model` with `fal-ai/nano-banana-pro` (~$0.15), generate a 1200×630 atmospheric backdrop. Prompt: *"A subtle dark holographic backdrop, near-black ink ground (#0b0c0e) with faint cyan scanlines and a soft cyan glow gradient in the lower right, generous empty space on the left for a logo, no text, cinematic, 16:9 wide."* Download the result to `site/src/assets/brand/og-backdrop.png` and confirm it is 1200×630 (resize with sharp if the model returns another ratio). Commit it as a source asset.

Run: `magick identify site/src/assets/brand/og-backdrop.png | head -1`
Expected: `PNG 1200x630`.

- [ ] **Step 2: Composite the lockup over the backdrop**

Append to `build-brand-assets.mjs`:

```javascript
// OG card: dark lockup composited over the fal.ai backdrop, left-aligned
const ogLockupPng = await sharp(Buffer.from(lockup({ id: 'og', wordColor: '#3dd6f7', promptOpacity: '0.5' })), { density: 300 })
  .resize({ width: 760 }).png().toBuffer();
await sharp(`${ASSETS}/brand/og-backdrop.png`)
  .resize(1200, 630)
  .composite([{ input: ogLockupPng, left: 90, top: 250 }])
  .png().toFile(`${PUBLIC}/og-card.png`);
console.log('wrote og-card.png');
```

- [ ] **Step 3: Run and verify**

Run: `cd site && node scripts/build-brand-assets.mjs && magick identify public/og-card.png | head -1`
Expected: `PNG 1200x630`. Open it: lockup is legible, left-aligned, not clipped; amber cursor visible against cyan.

- [ ] **Step 4: Commit**

`feat(brand): OG social card via fal.ai backdrop + sharp composite (holomush-7daup)`

---

## Phase 4: Site integration

### Task 7: Recolor palette tokens (`custom.css`)

INV-1: Starlight accent slots are **cyan only**; amber appears nowhere in UI tokens (it lives only inside the logo SVGs). INV-7: all brand colors are `--brand-*` custom properties.

**Files:**

- Modify: `site/src/styles/custom.css`

- [ ] **Step 1: Replace the token block**

Replace `site/src/styles/custom.css` lines 4-16 (the comment + both `:root` blocks) with:

```css
/* Brand tokens — holographic terminal (INV-1, INV-7). Amber is logo-only. */
:root {
  --brand-cyan-bright: #3dd6f7;
  --brand-cyan-deep: #1565c0;
  --brand-amber: #ffb300; /* logo cursor only — never a UI accent (INV-1) */
  --brand-ink: #0b0c0e;

  /* Starlight accent = cyan only (dark mode default) */
  --sl-color-accent-low: #07303f;
  --sl-color-accent: #3dd6f7;
  --sl-color-accent-high: #b3ecfb;
}

:root[data-theme='light'] {
  --sl-color-accent-low: #cdeefb;
  --sl-color-accent: #1565c0;
  --sl-color-accent-high: #0a3a66;
}
```

- [ ] **Step 2: Verify no amber in Starlight tokens**

Run: `cd site && rg -n 'sl-color-accent' src/styles/custom.css && rg -nc 'ffb300' src/styles/custom.css`
Expected: the three accent vars resolve to cyan hexes only; `ffb300` appears exactly once (the `--brand-amber` definition with its INV-1 comment), never inside an `--sl-color-accent*` value.

- [ ] **Step 3: Commit**

`feat(brand): recolor docs accent to cyan, brand tokens (holomush-7daup)`

---

### Task 8: Wire `astro.config.mjs`

**Files:**

- Modify: `site/astro.config.mjs:14-15`

- [ ] **Step 1: Replace the logo + favicon lines and add head tags**

Replace `logo: { src: './src/assets/logo.png', alt: 'HoloMUSH' },` and `favicon: '/favicon.png',` with:

```javascript
      logo: {
        light: './src/assets/logo-light.svg',
        dark: './src/assets/logo-dark.svg',
        alt: 'HoloMUSH',
        replacesTitle: true,
      },
      favicon: '/favicon.svg',
      head: [
        { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '32x32', href: '/favicon-32.png' } },
        { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '16x16', href: '/favicon-16.png' } },
        { tag: 'link', attrs: { rel: 'apple-touch-icon', sizes: '180x180', href: '/apple-touch-icon.png' } },
        { tag: 'meta', attrs: { property: 'og:image', content: 'https://holomush.dev/og-card.png' } },
        { tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
        { tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
        { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: 'https://holomush.dev/og-card.png' } },
      ],
```

(`replacesTitle: true` because the lockup contains the wordmark — avoids a duplicate "HoloMUSH" text beside the logo.)

- [ ] **Step 2: Delete superseded source assets**

Run: `cd site && rm src/assets/logo.png src/assets/favicon.png`
Expected: both removed. (`public/favicon.png` is kept — regenerated in Task 5 as the legacy fallback.)

- [ ] **Step 3: Verify config parses**

Run: `cd site && node --check astro.config.mjs && rg -n "logo:|favicon:|og:image" astro.config.mjs`
Expected: no syntax error; logo uses `light`/`dark`, favicon is `/favicon.svg`, og:image present.

- [ ] **Step 4: Commit**

`feat(brand): wire new logo/favicon/og tags into Starlight config (holomush-7daup)`

---

### Task 9: Build verification

**Files:** none (verification only)

- [ ] **Step 1: Full asset regen + site build**

Run: `cd site && node scripts/build-brand-assets.mjs && task docs:build`
Expected: generator prints all "wrote …" lines; `task docs:build` exits 0 with the new assets bundled. (`task docs:build` wraps `bunx astro build` per `site/CLAUDE.md`.)

- [ ] **Step 2: Visual smoke test (light + dark)**

Run: `cd site && task docs:serve` and open the local URL. Confirm: header shows the cyan lockup; toggling light/dark swaps `logo-light`/`logo-dark`; favicon tile shows in the browser tab; links/active-nav render cyan (no orange, no amber).

- [ ] **Step 3: Commit (if any tuning landed)**

`chore(brand): regenerate assets, verify docs build (holomush-7daup)`

---

## Phase 5: Project guidance (INV-8)

### Task 10: `.claude/rules/branding.md`

**Files:**

- Create: `.claude/rules/branding.md`

- [ ] **Step 1: Write the rule (auto-loads on brand files)**

```markdown
---
paths:
  - "site/src/assets/**"
  - "site/src/styles/custom.css"
  - "site/public/favicon*"
  - "site/public/og-card.png"
  - "site/public/apple-touch-icon.png"
  - "site/astro.config.mjs"
  - "site/scripts/build-brand-assets.mjs"
  - "README.md"
---

# HoloMUSH Software Branding

The `holomush.dev` software brand is the **holographic terminal**: a cyan
app-tile mark and a `>holomush_` monospace command-line wordmark. Full design:
`docs/superpowers/specs/2026-05-28-holomush-software-brand-refresh-design.md`.

## Palette tokens (defined in `site/src/styles/custom.css`)

| Token | Hex | Role |
| --- | --- | --- |
| `--brand-cyan-bright` | `#3dd6f7` | wordmark + dark-mode links/accent |
| tile gradient | `#34d6f6 → #1565c0` | tile fill |
| `--brand-cyan-deep` | `#1565c0` | light-mode wordmark/links |
| `--brand-amber` | `#ffb300` | **cursor only** |
| `--brand-ink` | `#0b0c0e` | dark ground |

## Rules

| Requirement | Rule |
| --- | --- |
| **MUST NOT** use amber as a UI accent | `#ffb300` is the **cursor only** (tile underscore, wordmark cursor). Links, buttons, nav, fills are cyan. Putting amber in `--sl-color-accent*` is a bug (INV-1). |
| **MUST** keep brand colors in tokens | All brand hex live in `site/src/styles/custom.css` `--brand-*`. No hardcoded brand hex in components (INV-7). |
| **MUST** outline glyphs in logo SVGs | Logo/favicon SVGs carry the `h`/wordmark as vector `<path>`, not `<text>` — no runtime font dependency (INV-3). Regenerate via `site/scripts/build-brand-assets.mjs`. |
| **MUST** ship light + dark lockups | `logo-light.svg` / `logo-dark.svg` (INV-5). The tile is palette-stable. |
| **MUST NOT** touch game-world art | This brand is the **software/platform** only — never the game world / default setting (INV-6). |
| **MUST** regenerate, not hand-edit | Assets are generated by `build-brand-assets.mjs`. Edit the script + rerun `bun run brand:build`; don't hand-edit generated SVG/PNG. |
```

- [ ] **Step 2: Verify frontmatter parses (YAML)**

Run: `rg -n '^paths:' .claude/rules/branding.md && head -20 .claude/rules/branding.md`
Expected: `paths:` frontmatter present with the brand globs.

- [ ] **Step 3: Commit**

`docs(brand): add .claude/rules/branding.md guidance (holomush-7daup)`

---

### Task 11: `site/CLAUDE.md` Branding section + root pointer

**Files:**

- Modify: `site/CLAUDE.md`
- Modify: `CLAUDE.md` (root)

- [ ] **Step 1: Add `## Branding` to `site/CLAUDE.md`**

Insert after the `## Voice and Tone` section:

```markdown
## Branding

The site brand is the **holographic terminal**: a cyan app-tile mark (`h` +
cutout) and a `>holomush_` monospace wordmark with an **amber underscore
cursor** as the only warm accent. Assets are generated by
`scripts/build-brand-assets.mjs` (run `bun run brand:build`); never hand-edit
the generated SVG/PNG.

- **Amber (`#ffb300`) is the cursor only** — links, nav, and buttons are cyan.
- Brand colors live as `--brand-*` tokens in `src/styles/custom.css`.
- Logo ships light + dark variants (`src/assets/logo-{light,dark}.svg`).
- Full rules: `.claude/rules/branding.md`. Design:
  `docs/superpowers/specs/2026-05-28-holomush-software-brand-refresh-design.md`.
```

- [ ] **Step 2: Add a pointer to root `CLAUDE.md`**

In the root `CLAUDE.md` "Documentation Structure" section, add a row/line after the site-docs entry:

```markdown
**Branding:** The software brand (logo, favicon, palette) is defined in
`.claude/rules/branding.md` and `site/CLAUDE.md` — cyan tile + `>holomush_`
wordmark, amber cursor accent only.
```

- [ ] **Step 3: Verify markdown lint passes**

Run: `cd site && task lint:docs-symmetry 2>/dev/null; cd .. && bunx rumdl check --config site/.rumdl.toml site/CLAUDE.md .claude/rules/branding.md 2>/dev/null || echo "(run task lint to confirm in CI-pinned rumdl)"`
Expected: no markdown violations in the new/edited files (verify under `task lint` before PR; rumdl version skew is a known gotcha).

- [ ] **Step 4: Commit**

`docs(brand): branding section in site/CLAUDE.md + root pointer (holomush-7daup)`

---

## Phase 6: GitHub assets

### Task 12: Org avatar + README banner

**Files:**

- Modify: `site/scripts/build-brand-assets.mjs`
- Create (generated): `assets/brand/og-avatar.png` (460×460), `assets/brand/readme-banner.png`
- Modify: `README.md`

- [ ] **Step 1: Add avatar + banner outputs to the generator**

Append to `build-brand-assets.mjs`:

```javascript
// reuse mkdirSync already imported at the top of the file (Task 3)
const GH = resolve(here, '../../assets/brand');
mkdirSync(GH, { recursive: true });
// Org avatar: 460x460 tile on opaque ink
await sharp(Buffer.from(tileSvg), { density: 920 })
  .resize(460, 460).flatten({ background: '#0b0c0e' })
  .png().toFile(`${GH}/og-avatar.png`);
// README banner: 1280x320, dark lockup centered on ink
const bannerLockup = await sharp(Buffer.from(lockup({ id: 'bn', wordColor: '#3dd6f7', promptOpacity: '0.5' })), { density: 300 })
  .resize({ width: 720 }).png().toBuffer();
await sharp({ create: { width: 1280, height: 320, channels: 4, background: '#0b0c0e' } })
  .composite([{ input: bannerLockup, gravity: 'centre' }])
  .png().toFile(`${GH}/readme-banner.png`);
console.log('wrote og-avatar.png, readme-banner.png');
```

- [ ] **Step 2: Run and verify dimensions**

Run: `cd site && node scripts/build-brand-assets.mjs && magick identify ../assets/brand/og-avatar.png ../assets/brand/readme-banner.png | cat`
Expected: `og-avatar` 460×460 opaque, `readme-banner` 1280×320.

- [ ] **Step 3: Embed the banner in README**

`README.md` opens with an SPDX comment block (lines 1-4) then `# HoloMUSH` (line 6). Insert the banner **between the SPDX comment and the `# HoloMUSH` heading** — i.e. replace the blank line 5 region so the result reads:

```markdown
<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<p align="center"><img src="assets/brand/readme-banner.png" alt="HoloMUSH" width="640"></p>

# HoloMUSH
```

Do not place the image above the SPDX header. There is no existing banner to replace.

- [ ] **Step 4: Verify README renders the image path**

Run: `rg -n 'assets/brand/readme-banner.png' README.md && ls assets/brand/readme-banner.png`
Expected: reference present and file exists.

- [ ] **Step 5: Commit**

`feat(brand): org avatar + README banner (holomush-7daup)`

(The org avatar is uploaded to GitHub org settings manually — outside the repo build.)

---

## Acceptance (maps to spec)

- INV-1: `rg ffb300 site/src/styles/custom.css` shows amber only in `--brand-amber`, never in `--sl-color-accent*` (Task 7).
- INV-2: favicon `h` readable at 16px (Task 3 Step 3).
- INV-3: logo/favicon SVGs contain `<path>`, no `<text>` (Tasks 3-4) — `rg '<text' site/public/favicon.svg site/src/assets/logo-*.svg` returns nothing.
- INV-4: wordmark renders `>holomush_` with dim prompt + amber underscore (Task 4).
- INV-5: `logo-light.svg` + `logo-dark.svg` both ship, wired via `logo:{light,dark}` (Tasks 4, 8).
- INV-6: no game-world assets touched anywhere in this plan.
- INV-7: all brand hex are `--brand-*` tokens in `custom.css` (Task 7).
- INV-8: `.claude/rules/branding.md` + `site/CLAUDE.md` Branding + root pointer (Tasks 10-11).
- `task docs:build` green (Task 9); full asset set generated (favicon set, header light/dark, OG card, avatar, banner).

## Out of Scope

Game-world / default-setting branding (INV-6); `web/` PWA theming; body web-font loading (wordmark is outlined).
<!-- adr-capture: sha256=4139d0149b92f47e; session=cli; ts=2026-05-28T14:02:07Z; adrs= -->
