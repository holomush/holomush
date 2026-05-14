---
name: existing-hook-claim-verification
description: Claims that an invariant relies on an "existing callback / reload hook / watcher" require a path:symbol citation
metadata:
  type: feedback
---

# "Existing callback hook" claims need a path:symbol citation

When a spec says "refreshed on manifest reload via existing manifest-registry hook" or "uses the existing watcher" — the hook MUST be named with `path:symbol`. Otherwise the invariant the hook supports is unprovable.

**Why:** generic callback infrastructure ("registrar / unregistrar / `OnChange`") is often misremembered as supporting reload when in fact it's load-only. The invariant under test becomes a ghost.

**How to apply:** spec section that cites the hook → grep `internal/<package>/manager*.go` or `*loader*.go` for `Reload`, `Refresh`, `Subscribe`, `OnChange`, `Watch`. If only initial-load callbacks exist (e.g., `WithAttributeProviderRegistrar`), require the spec to either (a) cite the symbol verbatim, (b) downscale the invariant to "atomic on initial load", or (c) spec the new callback shape.

Seen 2026-05-13 in event-payload-crypto-phase7-plugin-sdk-design v2: §4.4 said "no new manifest-watch infrastructure" required. Probe-searched `internal/plugin/`: found `RegisterPluginProviderFunc`, `WithAttributeProviderRegistrar`, `WithAttributeProviderUnregistrar`, `alias_seeder.SeedManifestAliases`, `Manager.unregisterPluginProviders`. None is a manifest-set-changed callback. INV-P7-8 ("atomic refresh on manifest reload") was unprovable as drafted.
