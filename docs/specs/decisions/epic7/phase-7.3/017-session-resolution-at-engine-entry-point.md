<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 17. Session Resolution at Engine Entry Point

> [Back to Decision Index](../README.md)

**Review finding:** The spec stated sessions are "resolved to their associated
character" but didn't specify where this happens — in the engine, in a provider,
or at the adapter layer. This ambiguity affects the entire provider architecture.

**Decision:** Session resolution happens at the engine entry point, BEFORE
attribute resolution. The engine rewrites `session:web-123` to
`character:01ABC` by querying the session store, then proceeds as if the caller
passed the character subject directly.

**Rationale:** Policies are always evaluated as `principal is character`, never
`principal is session`. Resolving at the entry point keeps the provider layer
clean — `CharacterProvider` only handles characters, not sessions. The
`Session Resolver` in the architecture diagram exists solely for this lookup,
not as an attribute contributor.
