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
