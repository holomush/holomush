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
// Composed glyph-by-glyph: opentype.js v2.x crashes shaping a multi-glyph string
// against JetBrains Mono ("substitutionType 62 ... not yet supported" — an
// unsupported GSUB ccmp lookup format). Per-glyph rendering bypasses the string
// shaper, and a monospace wordmark wants no kerning or code ligatures anyway.
export function glyphPath(text, x, y, size) {
  let cursor = x;
  let d = '';
  for (const ch of text) {
    d += font.getPath(ch, cursor, y, size).toPathData(2);
    cursor += font.getAdvanceWidth(ch, size);
  }
  return { d, width: cursor - x };
}

export const EM = font.unitsPerEm;
