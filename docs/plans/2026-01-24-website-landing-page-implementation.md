# Website Landing Page Implementation Plan

**Date:** 2026-01-24
**Design:** [2026-01-24-website-landing-page-design.md](2026-01-24-website-landing-page-design.md)
**Status:** Ready for implementation

## Prerequisites

- Design document reviewed and approved
- `task docs:serve` working in current state

## Implementation Tasks

### Task 1: Update Site Configuration

**File:** `site/zensical.toml`

**Changes:**

1. Change `site_url` from `https://docs.holomush.dev` to `https://holomush.dev`
2. Change `site_name` from `HoloMUSH Documentation` to `HoloMUSH`

**Verification:** File parses correctly (no syntax errors)

---

### Task 2: Rewrite Landing Page

**File:** `site/docs/index.md`

**Changes:**

1. Replace entire content with new landing page structure:
   - Hero section with tagline and CTA
   - Feature cards grid (4 features)
   - Project status badge
   - Audience paths (Developers/Operators/Contributors)
   - Community section (GitHub only)

2. Remove broken links:
   - `developers/architecture.md` → remove
   - `operators/quickstart.md` → remove
   - `developers/plugins/tutorial.md` → remove
   - `contributors/roadmap.md` → remove
   - `changelog.md` → remove

3. Update plugin references:
   - "WASM plugins" → "Lua & Go plugins"

**Verification:** `task docs:build` succeeds

---

### Task 3: Update WASM References in Coding Standards

**File:** `site/docs/contributors/coding-standards.md`

**Changes:**

1. Find all references to WASM/WebAssembly
2. Update to reflect Lua & Go plugin system
3. Remove any WASM-specific coding standards that no longer apply

**Verification:** Content accurately reflects current architecture

---

### Task 4: Verify Build and Links

**Commands:**

```bash
task docs:build
task docs:serve
```

**Verification checklist:**

- [ ] Build completes without errors
- [ ] Landing page renders with hero section
- [ ] Feature cards display correctly
- [ ] All navigation links work
- [ ] No broken internal links
- [ ] Developers/Operators/Contributors sections accessible

---

## Task Dependencies

```text
Task 1 (config) ─┐
                 ├──► Task 4 (verify)
Task 2 (index)  ─┤
                 │
Task 3 (coding) ─┘
```

Tasks 1, 2, 3 are independent and MAY be done in parallel.
Task 4 MUST be done after all others complete.

## Rollback

If issues arise, revert changes with:

```bash
git checkout -- site/
```

## Definition of Done

- [ ] All 4 tasks completed
- [ ] `task docs:build` passes
- [ ] `task docs:serve` shows correct landing page
- [ ] No WASM references remain in site/docs
- [ ] No broken links in navigation
