<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-jpn4w; do not edit manually; use `/adr update holomush-jpn4w` -->

# Package in-repo Claude Code tooling as a @skills-dir plugin

**Date:** 2026-06-30
**Status:** Accepted
**Decision:** holomush-jpn4w
**Deciders:** Sean Brandt

## Context

The repo accumulated eight loose `.claude/commands/*.md` files with no
namespacing, no bundled-script support, and no co-location of metadata. Claude
Code offers three plugin-discovery mechanisms: `@skills-dir` (zero-config,
project-scope, loaded in place from `.claude/skills/<name>/` when a
`.claude-plugin/plugin.json` is present), a local marketplace
(`extraKnownMarketplaces` + `enabledPlugins` in `.claude/settings.json`), and a
hypothetical `.claude/plugins/` auto-discovery. The choice determines the
invocation namespace, the trust model, and whether future in-repo tooling can
reference bundled scripts via `${CLAUDE_PLUGIN_ROOT}`.

## Decision

Package all in-repo Claude Code developer tooling as a `@skills-dir` plugin
named `holomush-dev` under `.claude/skills/holomush-dev/`, discovered
zero-config at session start from the repository root. Skills are invoked
`/holomush-dev:<name>`; bundled scripts are referenced via
`${CLAUDE_PLUGIN_ROOT}/scripts/`.

## Rationale

- The local-marketplace route has an active blocking upstream bug
  (anthropics/claude-code#23978): a directory-source relative path does not
  resolve, forcing a manual `/plugin marketplace add ./` per clone —
  unacceptable for a shared team tool.
- `.claude/plugins/` auto-discovery is unimplemented and upstream-rejected
  (anthropics/claude-code#58147), so it is not an option in Claude Code 2.1.x.
- `@skills-dir` is the only mechanism that is both zero-config and stable in the
  current release; `.claude/` is git-tracked, so cloning the repo is sufficient.
- The `${CLAUDE_PLUGIN_ROOT}`-relative reference model lets bundled scripts
  travel with the plugin in VCS without hardcoded paths.
- Namespacing under `holomush-dev:` makes tool origin discoverable and
  eliminates collision risk with installed marketplace plugins sharing short
  verbs like `review-code`.

## Alternatives Considered

- **`@skills-dir` plugin at `.claude/skills/holomush-dev/` (chosen):**
  zero-config, no per-clone step, VCS-carried, `${CLAUDE_PLUGIN_ROOT}` script
  co-location, namespaced. Cwd-sensitive (launch from repo root; sub-dir
  sessions need `/reload-plugins`).
- **Local marketplace (rejected):** cwd-independent, but heavier and blocked by
  bug #23978 requiring a manual per-clone install.
- **`.claude/plugins/` auto-discovery (rejected):** would be ideal, but does not
  exist (upstream #58147 open/rejected).
- **Keep flat `.claude/commands/*.md` (rejected):** no migration cost, but no
  namespace, no `${CLAUDE_PLUGIN_ROOT}`, scripts and skills cannot travel
  together, and verbs collide with installed marketplace plugins.

## Consequences

- Positive: VCS-tracked `.claude/` carries the complete plugin with no external
  install; skills are namespaced and collision-free; future in-repo tooling
  follows the same pattern and can bundle scripts/agents under one plugin root.
- Negative: the plugin loads only from the directory Claude Code is launched in;
  sub-directory sessions require `/reload-plugins`. Every jj workspace root must
  carry `.claude/` (it does, since `.claude/` is git-tracked), and the cwd
  caveat must be documented.
- Neutral: existing loose `.claude/skills/*` skills (no `.claude-plugin/`
  manifest) continue to coexist; installed marketplace plugins (dev-flow, etc.)
  are unaffected.
