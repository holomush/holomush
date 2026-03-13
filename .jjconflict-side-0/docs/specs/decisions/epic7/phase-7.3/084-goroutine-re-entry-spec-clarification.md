<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 84. Goroutine Re-Entry Spec Clarification

> [Back to Decision Index](../README.md)

**Question:** The spec contains contradictory language about goroutine-based
re-entry detection. Line 448 says the sentinel "does NOT detect
cross-goroutine re-entrance" while line 454 says tests "MUST verify that
goroutine-based re-entry attempts are detected." Which is correct?

**Context:** The ABAC engine uses a context-value sentinel to detect
re-entrance (01-core-types.md, Enforcement section). This sentinel is stored
in the calling goroutine's context, so it can only detect synchronous
re-entrance on the same call stack. Cross-goroutine re-entrance (a provider
spawning a new goroutine that calls Evaluate()) bypasses the sentinel entirely.

The plan already correctly handles this by testing only same-goroutine
re-entry detection. The spec language needs to match.

**Options Considered:**

1. **Clarify line 454** --- Change "goroutine-based re-entry" to
   "same-goroutine re-entry" to match the sentinel's actual capability.
2. **Add cross-goroutine detection** --- Implement goroutine-global state
   for detection. Rejected: adds complexity, hurts concurrent performance.
3. **Remove the test requirement** --- Weaken the spec. Rejected: sentinel
   testing is still valuable for regression prevention.

**Decision:** Clarify line 454 (option 1).

**Rationale:**

- The sentinel detects same-goroutine (synchronous) re-entrance only
- Cross-goroutine re-entrance is prevented by the MUST NOT prohibition in
  the provider contract, enforced through convention and code review
- Tests should verify the sentinel works for its intended scope
  (same-goroutine), not for cross-goroutine scenarios it cannot detect
- The plan already implements this correctly

**Implementation:** Update 01-core-types.md line 454 to say "same-goroutine
re-entry attempts" and update the test spec in 08-testing-appendices.md to
match.

**Review Finding:** C2 (PR #69 review)
**Bead:** holomush-5k1.359
