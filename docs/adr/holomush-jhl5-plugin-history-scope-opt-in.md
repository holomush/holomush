<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Plugin Manifests Opt-In to history_scope (vs Spec's Exempt-List Framing)

**Date:** 2026-05-17
**Status:** Accepted
**Decision:** holomush-jhl5
**Deciders:** HoloMUSH Contributors

## Context

The history-scope privacy spec's I-PRIV-7 invariant requires plugin-owned subjects that adopt history-replay semantics divergent from the grid/scene model to declare them. The spec's framing implied an **exempt-list** strategy — plugins emitting under `location:*` or `scene:*:*` get the spec-defined behavior automatically; only "custom" plugin-owned namespaces need an explicit `history_scope:` declaration.

Plan-review surfaced a fundamental ambiguity: the spec's §3 grid/scene rows reference event-stream **prefixes** (`location:01ABC`, `scene:01XYZ:ic`), but plugin manifests use bare **namespace** identifiers (`location`, `scene`, `core-communication`, etc.). The mapping between stream prefix and manifest namespace is not 1:1. An exempt-list strategy invented at plan time (e.g., `nsLocation = "location"; nsScene = "scene"`) would either silently miss real grid-emitting plugins (e.g., `core-communication` emits to `location:*` streams but its manifest namespace identifier is `core-communication`) or accidentally exempt the wrong set.

Concrete cost: `core-scenes` and `core-communication` would either bypass validation incorrectly (silent inheritance — exactly what I-PRIV-7 forbids) or fail to load with a confusing error.

## Decision

**Every plugin manifest with a non-empty `emits:` field MUST declare `history_scope:` explicitly.** No exempt list. Validation runs inside `(*Manifest).Validate` after `validateEmits(m.Emits)` succeeds (`internal/plugin/manifest.go:441`). Valid values are a closed enum: `grid`, `scene`, `custom`.

```go
if m.HistoryScope != "" && !validHistoryScopes[m.HistoryScope] {
    return oops.With("plugin", m.Name).
        Errorf("history_scope %q is invalid; valid: grid, scene, custom", m.HistoryScope)
}
if len(m.Emits) > 0 && m.HistoryScope == "" {
    return oops.With("plugin", m.Name).
        With("emits", m.Emits).
        Errorf("plugin emits events but manifest does not declare history_scope (holomush-iwzt I-PRIV-7)")
}
```

Existing in-tree plugins MUST migrate as part of the I-PRIV-7 implementation:

- `plugins/core-communication/plugin.yaml` → `history_scope: grid`
- `plugins/core-scenes/plugin.yaml` → `history_scope: scene`
- Any future emitting plugin → declare or fail validation

## Rationale

**Closes the silent-inheritance hole I-PRIV-7 forbids.** With an exempt-list, a plugin author can ship a new plugin that emits under an unexpected namespace and silently inherit permissive history semantics. With opt-in, every plugin is forced to make the choice explicit.

**Eliminates the stream-prefix vs manifest-namespace mapping ambiguity.** The spec's §3 references stream subjects; the manifest references namespaces. Either we maintain a mapping table (fragile; missed entries leak), or we don't depend on it. Opt-in removes the dependency entirely.

**One-time migration cost is bounded.** Only two existing plugins (`core-communication`, `core-scenes`) need updates; both have unambiguous correct values. Future plugins pay zero migration cost — they declare on creation.

**Validation runs at manifest load, not at emit time.** Plugins that violate I-PRIV-7 fail at startup; no per-event hot-path cost.

## Alternatives Considered

**A. Exempt-list strategy (spec's original framing).** Rejected: the `location` / `scene` exempt strings are stream prefixes, not manifest namespaces. Would either silently miss real grid/scene plugins or accidentally exempt the wrong ones. No stable mapping exists between the two layers.

**B. Default `history_scope: custom` when omitted, with a warning slog.** Rejected: warnings get ignored. The whole point of I-PRIV-7's "silent inheritance forbidden" is that defaults must not let a plugin author skip the decision. Opt-in is enforcement; default-with-warning is a polite request.

**C. Per-stream-prefix matching at validate time (parse `emits:` strings as if they were stream prefixes).** Rejected: `emits:` is documented as bare namespaces; treating them as stream prefixes is a layering violation and breaks current plugins whose namespace names happen to be `core-communication` / `core-scenes` rather than `location` / `scene`.

**D (selected): opt-in declaration mandatory for all emitting plugins.**

## Consequences

- **MUST** add `history_scope: <value>` to every existing plugin manifest with a non-empty `emits:` field at the same time the I-PRIV-7 validation lands. Otherwise `task test` fails on manifest load for every existing plugin.
- **MUST** document the closed-enum values (`grid`, `scene`, `custom`) in plugin-author documentation as part of the I-PRIV-7 rollout.
- **MUST NOT** introduce a default value or warning-on-omission path that lets new plugins skip the decision.
- Forward-looking: every new plugin author must choose. The cost is one line in the manifest; the benefit is the silent-inheritance hole stays closed.

## References

- Spec: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` I-PRIV-7
- Plan: `docs/superpowers/plans/2026-05-17-history-scope-privacy.md` Task 21
- Bead: `holomush-iwzt`
- Related ADRs: `holomush-ghpx` (NATS source-of-truth — related plugin/host contract pattern)
- Plan-reviewer findings that drove the decision: round 2 NB1 (stream-prefix vs manifest-namespace), round 3 B-R3.4 (filename/loader correction with migration enumeration)
