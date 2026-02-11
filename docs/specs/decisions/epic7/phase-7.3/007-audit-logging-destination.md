<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 7. Audit Logging Destination

> [Back to Decision Index](../README.md)

**Question:** Where should access decisions be logged?

**Options considered:**

| Option | Description                    | Pros                                                                | Cons                                                        |
| ------ | ------------------------------ | ------------------------------------------------------------------- | ----------------------------------------------------------- |
| A      | Event store                    | Queryable, replayable, consistent with architecture                 | Enormous volume, mixes auth noise into game streams         |
| B      | Separate PostgreSQL table      | Clean separation, independent retention policy, easy to query/purge | Additional table to manage                                  |
| C      | Structured logging only (slog) | Cheapest, no DB overhead                                            | Harder to query historically, depends on log infrastructure |

**Decision:** **Option B — Separate audit table.**

**Rationale:** Keeps the game event store clean while giving admins a queryable
audit trail. Configurable verbosity: log all denials by default, allow-decisions
only when audit mode is enabled. Denials are always interesting; allows are only
interesting when troubleshooting.
