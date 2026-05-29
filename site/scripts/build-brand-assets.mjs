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
