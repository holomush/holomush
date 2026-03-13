<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 50. Plugin Attribute Collision Behavior

> [Back to Decision Index](../README.md)

**Review finding:** The original spec had a fatal startup error when a plugin
registered an attribute colliding with a core attribute, which was
disproportionate — one bad plugin would brick the entire server.

**Decision:** Reject the plugin's provider registration and continue startup.
Server remains operational with other plugins. Log at ERROR level with plugin
ID and colliding attribute name. Plugin is disabled.

**Rationale:** A single misconfigured plugin should not prevent server startup.
Rejecting the individual plugin preserves availability while core attribute
guarantees remain intact.

**Cross-reference:** Main spec, Provider Registration Order section.
