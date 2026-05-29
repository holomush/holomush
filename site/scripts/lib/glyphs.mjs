// site/scripts/lib/glyphs.mjs
// opentype.js v2.x ships a CommonJS module with a default export only — there
// is NO named `parse` export under Node ESM (`import { parse }` resolves to
// undefined and throws at call). Import the default and call parse()
// (loadSync is deprecated).
import opentype from 'opentype.js';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const FONT = resolve(here, '../../src/assets/brand/fonts/JetBrainsMono-Medium.ttf');
const _buf = readFileSync(FONT);
const font = opentype.parse(_buf.buffer.slice(_buf.byteOffset, _buf.byteOffset + _buf.byteLength));

// Return { d, width } for `text` rendered with baseline at (x,y), em size `size`.
export function glyphPath(text, x, y, size) {
  const path = font.getPath(text, x, y, size);
  const adv = font.getAdvanceWidth(text, size);
  return { d: path.toPathData(2), width: adv };
}

export const EM = font.unitsPerEm;
