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

// --- GitHub assets --------------------------------------------------
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
