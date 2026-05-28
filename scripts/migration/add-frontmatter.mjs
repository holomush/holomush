// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// scripts/migration/add-frontmatter.mjs — for each .md/.mdx under the docs
// collection: if no YAML frontmatter, take the first `# H1` as title, remove
// that H1 line, and prepend `---\ntitle: <h1>\n---`. Idempotent: skip files
// that already start with `---`.
import { readFileSync, writeFileSync } from 'node:fs';
import { globSync } from 'node:fs';

const files = globSync('site/src/content/docs/**/*.{md,mdx}');
let updated = 0;
let skipped = 0;

for (const f of files) {
  let s = readFileSync(f, 'utf8');
  if (s.startsWith('---')) {
    skipped++;
    continue;
  }
  const m = s.match(/^#\s+(.+)$/m);
  const title = (m ? m[1] : f.split('/').pop().replace(/\.mdx?$/, '')).replace(/"/g, '\\"');
  if (m) s = s.replace(m[0] + '\n', '');
  writeFileSync(f, `---\ntitle: "${title}"\n---\n\n${s.replace(/^\n+/, '')}`);
  updated++;
}

console.log(`Done: ${updated} files updated, ${skipped} already had frontmatter.`);
