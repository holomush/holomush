<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 66. Sync Audit Writes for System Bypass

> [Back to Decision Index](../README.md)

**Context:** The evaluation algorithm (spec lines 1696-1745) short-circuits at
step 1 for system bypass subjects (e.g., `"system"`), returning immediately
without policy evaluation. System bypass is used for privileged operations like
server startup, seed policy bootstrap, and administrative maintenance tasks.

**Problem:** Should system bypass audit entries follow the async write path
(high throughput, potential loss under channel-full conditions) or sync write
path (guaranteed persistence, ~1-2ms latency)?

**Decision:** System bypass audit entries MUST use the synchronous write path,
identical to the handling for denials (`deny` and `default_deny` effects).

**Rationale:**

1. **Audit integrity for privileged operations:** System bypass grants
   unrestricted access. Recording these operations is critical for security
   forensics and compliance. Missing audit entries for privileged actions could
   hide malicious behavior or misconfiguration.

2. **Negligible performance cost:** System bypass operations are rare — server
   startup, seed policy bootstrap, emergency admin maintenance. The ~1-2ms sync
   write latency is acceptable for these infrequent operations.

3. **Consistent security semantics:** Denials use sync writes because they're
   security-significant (evidence of blocked unauthorized access). System
   bypasses are equally security-significant (evidence of privileged access).
   Both should have the same guaranteed persistence semantics.

4. **Prevents audit gaps:** Async writes with a full channel would drop system
   bypass entries. Sync writes with WAL fallback ensure no gaps in the audit
   trail for privilege escalation.

**Implementation note:** System bypass audit entries follow the same dual-path
logic as denials:

- Primary: Synchronous write to PostgreSQL before returning from `Evaluate()`
- Fallback: If primary fails, append to WAL file (`$XDG_STATE_HOME/holomush/audit-wal.jsonl`)
- Catastrophic: If both fail, log to stderr and increment `abac_audit_failures_total{reason="wal_failed"}`

**Cross-references:**

- Task 17 (Phase 7.3): Engine implementation with system bypass handling
- Task 19 (Phase 7.3): Audit logger with sync/async write paths
- ADR 56: Audit off mode includes system bypasses
