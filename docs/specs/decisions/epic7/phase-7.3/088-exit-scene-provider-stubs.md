<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 88. Add ExitProvider and SceneProvider Stubs

> [Back to Decision Index](../README.md)

**Question:** How should `exit:` and `scene:` resource types be handled during
ABAC migration?

**Context:** Two production call sites use `exit:` resources. Without any provider
registered for these resource types, policy evaluation would fail to resolve
attributes and seed policies could not match exit resources by type. The full
attribute schemas for exits and scenes are non-trivial and not needed for initial
ABAC migration.

**Decision:** Add minimal stub providers (ExitProvider, SceneProvider) to T16a
scope. Stubs return only `{type, id}` — enough for target matching
(`resource is exit`). Full attribute schemas are deferred to backlog tasks. Add
seed policies for exit resources using only target matching.

**Rationale:** Stubs provide minimum viability for the two existing `exit:` call
sites without requiring full provider implementations. Target matching on resource
type (`resource is exit`) is sufficient for the seed policies needed during
migration. Full attribute implementations (exit direction, lock state, scene
privacy, participant lists) are independent backlog items that do not block the
critical path.

**Consequences:**

- ExitProvider and SceneProvider stubs are added in T16a alongside other simple
  providers
- Seed policies for exits use only `resource is exit` target matching
- Full exit/scene attribute schemas become backlog tasks
- No impact on critical path timeline

**Cross-reference:** SPEC-I1; T16a (simple providers); ADR 085 (PropertyProvider
not on critical path).
