<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Plugin Manifest Typed Config Schema Is the Single Source of Truth for All Runtimes

**Date:** 2026-05-27
**Status:** Accepted
**Decision:** holomush-ikozq
**Deciders:** HoloMUSH Contributors

## Context

Given that plugin runtime config crosses the host↔plugin boundary as an opaque
map ([[holomush-7pdhf]]), each runtime still needs to turn those string values
into typed values. Binary plugins do this naturally via Go struct tags; Lua
plugins via accessor calls. Left to each runtime, that is **two independent
declarations** of a key's type and default — e.g. "`vote_window` is a duration
defaulting to `168h`" written once in a Go struct and again in Lua accessor
calls. Those declarations can drift, which is a latent violation of the
**plugin-runtime-symmetry** invariant (`.claude/rules/plugin-runtime-symmetry.md`),
a project MUST: binary and Lua plugins must be treated identically by the host.

## Decision

The plugin manifest `config:` block is a **typed schema** — each key declares
`{type, default, required, description}` — and is the **single source of truth**
driving **both** runtimes:

- Binary plugins decode via `pluginsdk.DecodeConfig[T]` (mapstructure).
- Lua plugins read via `holomush.config` typed accessors.
- The host derives type validation and the default/override merge from this one
  schema, so binary and Lua typing are **identical by construction**, not by
  discipline.

v1 supports **scalar types only** (`duration`/`int`/`bool`/`string`);
enum/nested/list types are deferred to a future extension.

## Alternatives Considered

- **Flat string map + read-site typing** (manifest carries only key→string;
  each runtime declares the type where it reads the value). Rejected: it
  re-introduces two independent, driftable typing declarations (the symmetry
  hazard); it has no auto-generated config documentation; and it cannot do
  load-time type validation, only runtime. It traded a little less manifest
  machinery for a standing discipline burden — and `koanf`/`mapstructure` were
  already in the module graph, so the schema path was affordable.

## Consequences

**Positive**

- Binary and Lua typing can never silently diverge for a given key — symmetry by
  construction.
- The config surface is self-documenting from the manifest (drives the
  `site/docs/extending/` config reference).
- Load-time fail-fast on a misauthored manifest (`PLUGIN_CONFIG_TYPE_INVALID`,
  `PLUGIN_CONFIG_MISSING_REQUIRED`) is possible only because the host knows each
  key's declared type from the schema.

**Negative**

- The manifest JSON schema must be regenerated (`task generate:schema` →
  `schemas/plugin.schema.json`) and committed whenever the `config:` shape
  changes — enforced by the schema-drift CI check.
- Scalar-only in v1; richer types require a follow-up.

**Neutral**

- The host's schema awareness is limited to generic types; it does not interpret
  key semantics (see [[holomush-7pdhf]]).
