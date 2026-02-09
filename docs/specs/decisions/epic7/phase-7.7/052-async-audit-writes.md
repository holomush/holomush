<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 52. Async Audit Writes

> [Back to Decision Index](../README.md)

**Review finding:** The spec didn't specify whether audit log writes are
synchronous or asynchronous, which has major performance implications.

**Decision:** Audit log inserts use async writes via a buffered channel.
`Evaluate()` enqueues the audit entry to a channel, and a background goroutine
batch-writes to PostgreSQL. When the channel is full, increment counter metric
`abac_audit_failures_total{reason="channel_full"}` and drop the entry. Audit
logging is best-effort and MUST NOT block authorization decisions.

**Rationale:** Synchronous audit writes in the authorization hot path would add
latency to every access check. Async writes decouple authorization performance
from audit I/O. The best-effort model accepts that some audit entries may be
lost under extreme load, which is preferable to blocking authorization.

**Cross-reference:** Main spec, Audit Log section.
