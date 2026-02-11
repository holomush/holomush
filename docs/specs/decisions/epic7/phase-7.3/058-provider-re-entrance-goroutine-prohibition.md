<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 58. Provider Re-Entrance Goroutine Prohibition

> [Back to Decision Index](../README.md)

**Review finding:** The context-based re-entrance sentinel only prevents
synchronous re-entrance on the same goroutine. A provider spawning a new
goroutine that calls `Evaluate()` with `context.Background()` bypasses the
guard entirely, potentially causing deadlock or state corruption.

**Decision:** Add an explicit MUST NOT prohibition in the provider contract
stating that providers MUST NOT spawn goroutines that call back into
`Evaluate()`. Detection remains convention-based — no runtime goroutine ID
tracking. Integration tests SHOULD include a scenario verifying that
goroutine-based re-entry is handled (panic or error).

**Security requirement (S2 - holomush-5k1.340):** The re-entrance guard MUST
be request-scoped (context-based), not goroutine-scoped. Guard MUST detect
evaluations spawned from attribute provider goroutines. Tests MUST verify
re-entrance detection across goroutine boundaries.

**Rationale:** Runtime goroutine ID tracking (via `sync.Map` or similar) adds
complexity and performance overhead to every `Evaluate()` call. At the current
scale, convention enforcement through code review and clear contract
documentation is sufficient. The MUST NOT clause makes the prohibition explicit
rather than implicit, and the integration test catches violations in CI rather
than production.

**Cross-reference:** Main spec, Attribute Providers section; [Decision #31](031-provider-re-entrance-prohibition.md)
(Provider Re-Entrance Prohibition); bead `holomush-npmk`.
