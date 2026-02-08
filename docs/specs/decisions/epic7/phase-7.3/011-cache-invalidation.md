<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 11. Cache Invalidation

> [Back to Decision Index](../README.md)

**Sean's input:** Use PostgreSQL LISTEN/NOTIFY from the start, not polling.

**Decision:** The Go policy store sends `pg_notify('policy_changed', policyID)`
in the same transaction as any CRUD operation. The engine subscribes and reloads
its in-memory cache on notification. No database triggers — the notification
call is explicit Go code.
