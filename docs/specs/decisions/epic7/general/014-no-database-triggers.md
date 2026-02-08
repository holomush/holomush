<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 14. No Database Triggers

> [Back to Decision Index](../README.md)

**Sean's hard constraint:** No database triggers or stored procedures. All logic
must live in Go application code. PostgreSQL is storage only.

**Impact:** Visibility defaults, LISTEN/NOTIFY notifications, and version
history management are all handled in Go store implementations.
