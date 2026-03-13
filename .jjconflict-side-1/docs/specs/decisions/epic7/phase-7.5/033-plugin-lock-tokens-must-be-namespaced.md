<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 33. Plugin Lock Tokens MUST Be Namespaced

> [Back to Decision Index](../README.md)

**Review finding:** Token conflict resolution was fatal at startup (server
refuses to start on collision), but plugins only SHOULD namespace their tokens.
Without enforcement, a plugin registering `score` would collide with any future
core token or another plugin's `score`, causing server startup failures that are
hard to diagnose.

**Decision:** Plugin lock tokens MUST use a dot-separated prefix that **exactly
matches** their plugin ID (e.g., plugin `reputation` registers
`reputation.score`, plugin `crafting` registers `crafting.type`). Abbreviations
are not allowed — the prefix before the first `.` MUST equal the plugin ID
string. The engine validates this at registration time — plugin tokens without
the correct namespace prefix are rejected.

**Rationale:** Fatal startup errors from token collisions should be preventable,
not just detectable. Requiring namespacing makes collisions structurally
impossible between plugins (each has a unique ID) while core tokens remain
un-namespaced.

**Clarification:** These are separate checks:

1. **Namespace enforcement:** Plugin tokens MUST be prefixed with the plugin's
   own ID. The engine rejects tokens that don't match the registering plugin's
   ID prefix. This prevents cross-plugin conflicts (plugin A cannot register
   `pluginB.score`).

2. **Duplicate plugin detection:** If two plugins with identical IDs are
   loaded, the second plugin's registration MUST fail with a clear error
   ("plugin ID already registered"). This check happens before token
   registration and prevents deployment errors.

These are separate checks: namespace enforcement prevents cross-plugin
conflicts; duplicate plugin ID detection prevents deployment errors.
