<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 51. Session Integrity Error Classification

> [Back to Decision Index](../README.md)

**Review finding:** The spec treated session-character-integrity errors (where
a character is deleted while sessions referencing it still exist) as a normal
operational path.

**Decision:** Classify as a bug/exceptional condition, not a normal path. Log
at CRITICAL level. Operators SHOULD configure alerting. Transactional cleanup
requirement (delete sessions in same transaction as character deletion)
unchanged.

**Rationale:** If the transactional cleanup works correctly, this error should
never occur. Its presence indicates a defect in session invalidation logic or
data corruption — it deserves CRITICAL severity, not INFO.

**Cross-reference:** Main spec, Session Subject Resolution section.
