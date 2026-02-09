<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 85. PropertyProvider Not on Critical Path

> [Back to Decision Index](../README.md)

**Question:** Should T16b (PropertyProvider) have a dependency edge to T17 (AccessPolicyEngine)?

**Context:**

The implementation plan uses a parallel chain design where PropertyProvider (T16b) is on an orange-highlighted "parallel PropertyProvider chain" (T4a → T16b), while the main critical path runs through T15 (core providers) and T16a (simple providers) before reaching T17 (AccessPolicyEngine).

The dependency graph shows:

- **Critical path to T17:** T15 (core providers) → T16a (simple providers) → T17 (engine)
- **Parallel path:** T4a (property model) → T16b (PropertyProvider)

There is no edge from T16b to T17.

**Decision:** T16b (PropertyProvider) is intentionally NOT a dependency of T17 (AccessPolicyEngine). This is correct and should remain as specified.

**Rationale:**

1. **Engine works without PropertyProvider:** The AccessPolicyEngine requires only the core attribute providers (T15) and simple providers (T16a) to function. It can evaluate policies using subject/resource/environment attributes without needing object properties.

2. **PropertyProvider is an enhancement:** T16b adds the ability to query object properties via the `property.*` namespace (e.g., `property.owner`, `property.flags.dark`). This is a valuable feature but not required for the engine to operate.

3. **Parallel development:** Keeping T16b off the critical path allows it to be developed in parallel with T17-T18 (engine implementation), reducing overall timeline.

4. **Correct dependency chain:** T16b depends on T4a (property model) which defines the property system. The engine (T17) depends on T16a (simple providers like `actor.roles`, `resource.type`) which are simpler and easier to implement first.

5. **Integration point is later:** PropertyProvider will be registered alongside other providers in the provider registry. The engine doesn't need to know about PropertyProvider during its implementation—only that it can query registered providers.

**Consequences:**

- PropertyProvider integration happens after T17 completes
- Initial engine testing uses core and simple providers only
- Full property-based policies become available once T16b integrates
- No impact on critical path timeline
