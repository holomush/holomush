<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 82. Core-First Provider Registration Order

> [Back to Decision Index](../README.md)

**Question:** Should the engine enforce registration ordering between core
and plugin attribute providers to prevent plugin providers from starving
core providers under fair-share timeout scheduling?

**Context:** ADR [#59](059-fair-share-provider-timeout-scheduling.md) introduces
fair-share timeout scheduling where the global provider timeout budget is split
across all registered providers. If plugin providers register before core
providers, they may consume disproportionate timeout budget during resolution,
degrading core provider performance.

**Options Considered:**

1. **No ordering constraint** --- Rely on fair-share scheduling alone. Risk:
   plugin providers registering first get evaluated first and may exhaust
   shared resources (connections, CPU) before core providers execute.
2. **Core-first registration order** --- Engine enforces that all core providers
   register before plugin providers. Guarantees core providers are evaluated
   first in sequential resolution (ADR [#42](042-sequential-provider-resolution.md)).
3. **Priority-based scheduling** --- Add explicit priority levels to providers.
   More flexible but adds complexity to the provider interface.

**Decision:** Core-first registration order (option 2).

**Rationale:**

- Sequential resolution (ADR #42) already evaluates providers in registration
  order, so registration order directly determines execution priority
- Core providers supply fundamental attributes (subject, resource, environment)
  that plugin providers may depend on
- Enforcing at registration time is simpler than runtime priority scheduling
- Plugin providers are always additive --- they extend, not replace, core
  attributes

**Implementation:** The provider registry MUST reject plugin provider
registration attempts if any core provider has not yet registered. Registration
order: SubjectProvider, ResourceProvider, EnvironmentProvider, then plugin
providers.

**Review Finding:** H3 (PR #69 review)
**Bead:** holomush-5k1.370
