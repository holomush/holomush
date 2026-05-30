<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Owner-Partitioned Plugin Settings with Opaque Host Passthrough

**Status:** Accepted
**Decision:** holomush-74ib4
**Design bead:** holomush-iokti
**Date:** 2026-05-30

## Context

Plugins need to persist per-player and per-character preferences (the driving
case: content-warning block lists for the scene board, `holomush-iokti`). The
host already owns the `players.preferences` JSONB column through a typed
`auth.PlayerPreferences` struct that is marshaled **whole** on every write
(`internal/auth/postgres/player_repo.go:33,169`).

Three forces collide:

1. A generic flat-map writer over `players.preferences` would race the typed
   whole-struct marshal — the typed write silently drops any key the generic
   writer added (two writers, one column, data loss).
2. Typed host fields (the existing `auth.PlayerPreferences.Scenes.FocusReplayTail`
   is one) walk plugin-domain vocabulary into `internal/auth` and force an
   `internal/` edit for every new plugin preference — violating the plugin
   boundary (`holomush-z1e7`: plugins must not require `internal/` changes).
3. Per-plugin preference tables re-solve generic storage once per plugin,
   defeating the goal of solving preferences once for channels, forums, and
   every future plugin.

## Decision

Plugin preferences are stored via an **opaque passthrough partition** on the
host's typed preferences struct:

```go
type PlayerPreferences struct {
    MaxCharacters int
    Scenes        ScenePlayerPreferences
    Plugins       map[string]json.RawMessage `json:"plugins,omitempty"` // opaque
}
```

The settings substrate exposes owner-narrowed handles. `Settings` (the read
interface) is unchanged; stores return `Scoped`, which narrows to a partition:

```go
type Scoped interface {
    Settings                    // bare reads → host partition
    Owner(name string) Writable // narrow to a plugin's partition
    Host() Writable             // explicit host partition
}
```

`.Owner("core-scenes")` re-points the underlying `jsonMapSettings.data` at
`plugins["core-scenes"]`; writes commit via the **single** existing
`auth.PlayerRepository.Update` path (read-modify-write), so the typed marshal now
round-trips plugin keys instead of dropping them. The host never interprets the
contents of `Plugins`.

Namespace validation is **host-partition-only**: the view returned by
`.Owner(name)` carries `validateNamespace = false`, so the `RegisteredNamespaces`
allowlist (`internal/settings/namespaces.go`) applies only to the host partition.
Inside a plugin partition the plugin owns its keyspace, unchecked — adding a key
like `content.cw_block` requires **zero `internal/` edits**.

## Rationale

- Eliminates the two-writer clobber: the typed repo stays the sole marshal path
  for the column; `json:"plugins,omitempty"` + read-modify-write preserves other
  owners' keys.
- Keeps the host domain-ignorant (INV-10): no plugin vocabulary enters
  `internal/`; `content` is never added to `RegisteredNamespaces`.
- Reusable by every future plugin with zero per-key substrate cost (INV-12). The
  one-time `Plugins` field + owner-narrowing machinery is substrate, paid once.
- `ValidateNamespace` enforces the *host's* namespace contract, which is
  meaningless inside a partition the host does not interpret; scoping it by a
  factory-level flag is structural, not a per-call conditional.
- Distinct from `holomush-7pdhf` (opaque plugin manifest config): that covers
  init-time config delivery from the manifest; this covers runtime read/write
  preferences persisted per principal.

## Alternatives Considered

- **Typed host field** (`auth.PlayerPreferences.Content.CWBlock`). Rejected:
  embeds plugin vocabulary in `internal/auth` and requires an `internal/` edit
  per new plugin preference — the same leak as the existing `Scenes.FocusReplayTail`
  field, debt not to extend.
- **Generic flat-map over `players.preferences` without owner partitioning.**
  Rejected: collides with the typed whole-struct marshal — two writers, silent
  key loss.
- **Plugin-local table per plugin.** Rejected: re-solders generic preference
  storage per plugin; defeats "solve preferences once."
- **Unconditional `ValidateNamespace` on all partitions.** Rejected: would force
  registering every plugin key in `internal/settings/namespaces.go`, an
  `internal/` edit per plugin key.

## Consequences

- **Positive:** any plugin adds settings with zero `internal/` edits after the
  substrate ships; no silent data loss on the preferences column; the substrate
  is reusable across channels, forums, and future plugins.
- **Negative:** `auth.PlayerPreferences` gains a `map[string]json.RawMessage`
  field opaque to static analysis; plugin key typos fail soft (not-found) rather
  than loud.
- **Neutral:** existing typed fields are unaffected; game-scope partitioning uses
  a host-controlled `plugin/<name>/<key>` prefix (flat `holomush_system_info`
  k/v has no sub-object).

## References

- Spec: `docs/superpowers/specs/2026-05-29-scenes-phase-8-board-content-warnings-design.md` §1.1, §3.1 (A2–A4), INV-10, INV-12
- Related: `holomush-z1e7` (strict plugin boundary), `holomush-7pdhf` (opaque plugin manifest config — distinct), `holomush-uvbyt` (structural isolation of this substrate)
- Design bead: `holomush-iokti`
