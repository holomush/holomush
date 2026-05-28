// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// scripts/migration/links.mjs — rewrite internal links and image paths in
// site/src/content/docs/**/*.{md,mdx} for Starlight:
//
//   1. Image refs  ![alt](images/X.jpg)  →  relative path to site/src/assets/X
//   2. In-collection .md links           →  Starlight root-relative slugs (/slug/#anchor)
//   3. Cross-repo .md links              →  GitHub blob URLs
//   4. External http(s):// links         →  unchanged
//
// Idempotent: already-rewritten slugs (]/...) and https:// targets are skipped.
// Run with: bun scripts/migration/links.mjs

import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { globSync } from 'node:fs';
import * as path from 'node:path';

const REPO_ROOT = path.resolve(import.meta.dirname, '..', '..');
const DOCS_ROOT = path.join(REPO_ROOT, 'site', 'src', 'content', 'docs');
const ASSETS_DIR = path.join(REPO_ROOT, 'site', 'src', 'assets');
const GITHUB_BLOB_BASE = 'https://github.com/holomush/holomush/blob/main';

// Image basename extensions we look for in assets
const IMAGE_EXTS = new Set(['.jpg', '.jpeg', '.png', '.gif', '.svg', '.webp', '.avif']);

/**
 * Given an image target (e.g. "images/nexus.jpg" or "assets/nexus.jpg"),
 * return the basename to look up in site/src/assets/.
 */
function imageBasename(target) {
  return path.basename(target);
}

/**
 * Compute the relative path from a file's directory to the assets dir,
 * appended with the image filename.
 */
function rewriteImageTarget(fileDir, imgBasename) {
  const rel = path.relative(fileDir, path.join(ASSETS_DIR, imgBasename));
  // Ensure forward slashes (relevant on Windows, harmless on Unix)
  return rel.split(path.sep).join('/');
}

/**
 * Known repo-root top-level directories (used as anchors when fixing up
 * spurious prefixes in cross-repo paths).
 */
const REPO_ROOT_DIRS = ['docs', 'internal', 'pkg', 'cmd', 'api', 'plugins', 'scripts', 'web', 'test'];

/**
 * Given a resolved absolute path (which may or may not exist on disk),
 * compute the path relative to REPO_ROOT.  If the naive result doesn't
 * exist (e.g. it has a spurious `site/src/docs/` or `site/docs/` prefix
 * from stale relative-path counts in the original markdown), walk the
 * known repo-root dir anchors and return the first one whose path exists.
 */
function repoRelative(absPath) {
  const naive = path.relative(REPO_ROOT, absPath).split(path.sep).join('/');

  // Fast path: naive result is correct.
  if (existsSync(path.join(REPO_ROOT, naive))) return naive;

  // Slow path: strip bogus prefix introduced by mis-calibrated ../.. counts.
  // Find the last occurrence of a known repo-root top-level dir in the path.
  // We scan all occurrences and take the one that yields an existing file.
  const candidates = [];
  for (const dir of REPO_ROOT_DIRS) {
    // Look for /dir/ or the path starting with dir/
    const marker = '/' + dir + '/';
    let idx = naive.lastIndexOf(marker);
    while (idx !== -1) {
      candidates.push(naive.slice(idx + 1)); // strip leading /
      idx = naive.lastIndexOf(marker, idx - 1);
    }
    if (naive.startsWith(dir + '/')) {
      candidates.push(naive);
    }
  }

  for (const candidate of candidates) {
    if (existsSync(path.join(REPO_ROOT, candidate))) return candidate;
  }

  // Return naive even if broken — the caller will produce the wrong URL, which
  // is no worse than what we started with and will surface in verification.
  return naive;
}

/**
 * Build a Starlight root-relative slug from an absolute path that is
 * inside DOCS_ROOT.
 * e.g. /…/docs/operating/configuration.md → /operating/configuration/
 *      /…/docs/operating/index.md         → /operating/
 */
function buildSlug(absPath, anchor) {
  let rel = path.relative(DOCS_ROOT, absPath).split(path.sep).join('/');
  // Strip .md / .mdx extension
  rel = rel.replace(/\.(mdx?)$/, '');
  // Strip trailing /index → represents the section index
  rel = rel.replace(/\/index$/, '');
  // Remove leading index at root level
  if (rel === 'index') rel = '';
  const slug = '/' + rel + '/';
  return anchor ? slug + '#' + anchor : slug;
}

/**
 * Process a single file, returning [newContent, changeCount].
 */
function processFile(filePath) {
  const content = readFileSync(filePath, 'utf8');
  const fileDir = path.dirname(filePath);
  let result = content;
  let changes = 0;

  // ─── 0. Repair broken cross-repo GitHub blob URLs ───────────────────────
  // Already-written GitHub URLs whose repo-relative path doesn't exist get
  // fixed here.  Two failure modes:
  //   (a) spurious site/src/docs/ or site/docs/ prefix in the path
  //   (b) missing '/' separator between blob/main and the path
  //       (e.g. blob/maindocs/ instead of blob/main/docs/)
  //
  // The regex matches the repo prefix and then captures everything after it
  // up to a boundary character.  Using a broader prefix pattern allows us to
  // detect both correct (blob/main/) and broken (blob/maindocs/) variants.
  const GH_REPO_PREFIX = 'https://github.com/holomush/holomush/blob/main';
  result = result.replace(
    /https:\/\/github\.com\/holomush\/holomush\/blob\/main([^)#"\s]*)/g,
    (match, afterMain) => {
      // afterMain starts with '/' for correct URLs, or with a letter for broken ones.
      // Normalise: strip leading '/' if present to get the bare repo-relative path.
      const repoRel = afterMain.startsWith('/') ? afterMain.slice(1) : afterMain;
      // Separate anchor
      const [relPath, ...anchorParts] = repoRel.split('#');
      const anchor = anchorParts.length ? '#' + anchorParts.join('#') : '';
      if (existsSync(path.join(REPO_ROOT, relPath))) {
        // Path exists — rebuild with canonical slash to fix missing-slash variants.
        const canonical = `${GH_REPO_PREFIX}/${relPath}${anchor}`;
        if (canonical === match) return match; // already correct
        changes++;
        return canonical;
      }
      // Path doesn't exist — try to fix bogus prefix.
      const fixed = repoRelative(path.join(REPO_ROOT, relPath));
      if (fixed === relPath) return match; // can't improve — leave as-is
      changes++;
      return `${GH_REPO_PREFIX}/${fixed}${anchor}`;
    }
  );

  // ─── 1. Image rewrites ───────────────────────────────────────────────────
  // Match: ![alt](target) where target is NOT http(s):// and NOT already /…
  // Target is a path like "images/foo.jpg" or "assets/foo.jpg"
  result = result.replace(
    /!\[([^\]]*)\]\(([^)]+)\)/g,
    (match, alt, target) => {
      // Skip already-absolute URLs
      if (target.startsWith('http://') || target.startsWith('https://')) return match;
      // Skip already root-relative or absolute paths
      if (target.startsWith('/')) return match;

      const ext = path.extname(target).toLowerCase();
      if (!IMAGE_EXTS.has(ext)) return match;

      // Check if the basename exists in site/src/assets/
      const basename = imageBasename(target);
      const assetPath = path.join(ASSETS_DIR, basename);
      // Always rewrite if it looks like an image path under images/ or assets/
      const targetDir = path.dirname(target).split('/')[0];
      if (targetDir === 'images' || targetDir === 'assets' || targetDir === '.') {
        const newTarget = rewriteImageTarget(fileDir, basename);
        if (newTarget !== target) {
          changes++;
          return `![${alt}](${newTarget})`;
        }
      }
      return match;
    }
  );

  // ─── 2 & 3. Link rewrites ────────────────────────────────────────────────
  // Match markdown links: [text](target) and [text](target#anchor)
  // Also handle attribute syntax: [text](target.md){ .foo } — keep the attributes
  //
  // Regex captures: $1=text, $2=target (without anchor), $3=anchor (optional, without #)
  // After the closing ) there may be { ... } attributes — those are outside the match.
  result = result.replace(
    /\[([^\]]*)\]\(([^)#\s]+?)(#[^)]*?)?\)/g,
    (match, text, target, anchorWithHash) => {
      // Skip external URLs
      if (target.startsWith('http://') || target.startsWith('https://')) return match;
      // Skip already root-relative slugs (already rewritten)
      if (target.startsWith('/')) return match;
      // Skip anchor-only links
      if (!target) return match;
      // Only process .md and .mdx links
      if (!target.match(/\.(mdx?)$/)) return match;

      const anchor = anchorWithHash ? anchorWithHash.slice(1) : null; // strip leading #

      // Resolve target relative to the current file's directory
      const resolved = path.resolve(fileDir, target);

      // Is the resolved path under DOCS_ROOT?
      const rel = path.relative(DOCS_ROOT, resolved);
      const isInCollection = !rel.startsWith('..') && !path.isAbsolute(rel);

      changes++;
      if (isInCollection) {
        // ── In-collection: rewrite to Starlight slug ──────────────────────
        const slug = buildSlug(resolved, anchor);
        return `[${text}](${slug})`;
      } else {
        // ── Cross-repo: rewrite to GitHub blob URL ─────────────────────────
        const repoRel = repoRelative(resolved);
        const ghUrl = `${GITHUB_BLOB_BASE}/${repoRel}${anchor ? '#' + anchor : ''}`;
        return `[${text}](${ghUrl})`;
      }
    }
  );

  return [result, changes];
}

// ─── Main ──────────────────────────────────────────────────────────────────

const files = globSync('site/src/content/docs/**/*.{md,mdx}', { cwd: REPO_ROOT });
let totalFiles = 0;
let totalChanges = 0;

for (const relFile of files) {
  const filePath = path.join(REPO_ROOT, relFile);
  const [newContent, changes] = processFile(filePath);
  if (changes > 0) {
    writeFileSync(filePath, newContent, 'utf8');
    totalFiles++;
    totalChanges += changes;
    console.log(`  ${relFile} (${changes} rewrites)`);
  }
}

console.log(`\nDone: ${totalChanges} rewrites across ${totalFiles} files.`);
