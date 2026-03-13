<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 75. Dual-Use Resource Prefixes (exit:, scene:, character: as Resource)

> [Back to Decision Index](../README.md)

**Question:** The Reserved Prefixes table and DSL prefix mapping omit three
resource types actively used in production code: `exit:` (4 call sites),
`scene:` (scene CRUD operations), and `character:` used as a resource (3 call
sites). Should these be added, and how should `character:` dual-use be handled?

**Options Considered:**

1. **Add all three as Resource Strings** — `character:` becomes dual-use
   (subject AND resource), engine distinguishes by `AccessRequest` field.
2. **Introduce separate prefixes** — e.g., `res-character:` to avoid ambiguity.
3. **Generic fallback** — Allow unknown prefixes with a default provider.

**Decision:** Option 1. Add `exit:`, `scene:`, and `character:` to the Resource
Strings section of the Reserved Prefixes table and DSL prefix mapping. The
`character:` prefix is dual-use: it appears in both Subject Strings and Resource
Strings. The engine distinguishes context by the `AccessRequest` field
(`Subject` vs `Resource`). Existing world providers already resolve these
resource types; no new `AttributeProvider` is needed.

**Rationale:** The engine's prefix parser operates on the string in a specific
`AccessRequest` field, so there is no ambiguity — `character:01ABC` in the
`Subject` field is a subject, in the `Resource` field it is a resource.
Introducing separate prefixes (option 2) would require migrating existing call
sites for no benefit. A generic fallback (option 3) weakens validation.

**Cross-reference:** Spec Reserved Prefixes table (lines 80-94), DSL prefix
mapping (lines 319-329), `internal/world/service.go` call sites.
Bead: `holomush-5k1.274`.
