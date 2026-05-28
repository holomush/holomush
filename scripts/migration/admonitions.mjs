// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// scripts/migration/admonitions.mjs — convert MkDocs admonition syntax to
// Starlight aside (:::) directives in site/src/content/docs/**/*.{md,mdx}.
//
// MkDocs input:
//   !!! note "Title"
//       body line 1
//       body line 2
//
// Starlight output:
//   :::note[Title]
//   body line 1
//   body line 2
//   :::
//
// Type mapping (per plan Task 5):
//   note | tip | info    → note
//   warning | caution    → caution
//   danger | error       → danger
//
// Both !!! and ??? markers are handled (Starlight has no collapsible aside).
// Idempotent: files already using ::: form are skipped if no !!! remain.
// Run with: bun scripts/migration/admonitions.mjs

import { readFileSync, writeFileSync } from 'node:fs';
import { globSync } from 'node:fs';
import * as path from 'node:path';

const REPO_ROOT = path.resolve(import.meta.dirname, '..', '..');
const DOCS_ROOT = path.join(REPO_ROOT, 'site', 'src', 'content', 'docs');

/** Map MkDocs admonition type to Starlight aside type. */
function mapType(mkdocsType) {
  const t = mkdocsType.toLowerCase();
  if (t === 'warning' || t === 'caution') return 'caution';
  if (t === 'danger' || t === 'error') return 'danger';
  // note | tip | info → note
  return 'note';
}

/**
 * Convert all MkDocs admonitions in a string to Starlight asides.
 * Returns the converted string (unchanged if no admonitions found).
 */
function convertAdmonitions(content) {
  // Regex: match a !!! or ??? line followed by any run of 4-space-indented lines.
  // The marker line: optional leading spaces (none expected at top level),
  // then !!! or ???, then type word, optional quoted title, rest of line.
  // Body: zero or more lines that are either blank or start with 4 spaces.
  const ADMONITION_RE = /^([ \t]*)(!{3}|\?{3})[ \t]+(\w+)(?:[ \t]+"([^"]*)")?[ \t]*\n((?:(?:    [^\n]*)?\n)*)/gm;

  return content.replace(ADMONITION_RE, (match, indent, marker, typeName, title, body) => {
    const starlightType = mapType(typeName);

    // Build opener: :::type[Title] or :::type (no title)
    const opener = title ? `:::${starlightType}[${title}]` : `:::${starlightType}`;

    // Dedent body by 4 spaces; preserve blank lines within.
    // body may end with a trailing newline which is fine — we trim trailing blank lines.
    const bodyLines = body.split('\n');
    // Remove final empty element from trailing newline split
    if (bodyLines.length > 0 && bodyLines[bodyLines.length - 1] === '') {
      bodyLines.pop();
    }
    const dedentedLines = bodyLines.map(line => {
      if (line === '') return '';
      if (line.startsWith('    ')) return line.slice(4);
      // Shouldn't happen given the regex, but pass through as-is
      return line;
    });

    // Trim trailing blank lines from body
    while (dedentedLines.length > 0 && dedentedLines[dedentedLines.length - 1] === '') {
      dedentedLines.pop();
    }

    const result = [opener, ...dedentedLines, ':::'].join('\n') + '\n';
    return indent + result;
  });
}

const files = globSync('**/*.{md,mdx}', { cwd: DOCS_ROOT }).sort();
let totalConverted = 0;
let totalFiles = 0;

for (const rel of files) {
  const filePath = path.join(DOCS_ROOT, rel);
  const original = readFileSync(filePath, 'utf8');

  // Idempotency check: skip if no MkDocs admonitions present
  if (!/^[ \t]*(!{3}|\?{3})[ \t]+\w/m.test(original)) {
    continue;
  }

  const converted = convertAdmonitions(original);
  if (converted === original) {
    continue;
  }

  // Count conversions
  const remaining = (converted.match(/^[ \t]*(!{3}|\?{3})[ \t]+\w/gm) || []).length;
  const original_count = (original.match(/^[ \t]*(!{3}|\?{3})[ \t]+\w/gm) || []).length;
  const count = original_count - remaining;

  writeFileSync(filePath, converted, 'utf8');
  console.log(`  converted ${count} admonition(s): ${rel}`);
  totalConverted += count;
  totalFiles++;
}

console.log(`\nDone: ${totalConverted} admonition(s) converted across ${totalFiles} file(s).`);
