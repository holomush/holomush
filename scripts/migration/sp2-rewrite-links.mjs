/*
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 HoloMUSH Contributors
 */

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
