<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 56. Audit Off Mode Includes System Bypasses

> [Back to Decision Index](../README.md)

**Review finding:** The spec required system_bypass logging in ALL audit modes
including `off`, but `off` mode was defined as "Nothing logged" in the
configuration table. These statements contradicted each other.

**Decision:** Update the `off` mode description from "Nothing" to "System
bypasses only." This preserves the security intent of always logging when the
`system` subject bypasses policy evaluation, while making the table description
accurate.

**Rationale:** System bypass events are security-significant — they indicate
operations running outside normal policy evaluation. Even in development
environments where `off` mode is used for performance, operators should be
aware of bypass activity. The volume is negligible (system operations are
rare), so there is no performance impact.

**Updated by:** [Decision #86](../general/086-audit-off-mode-logs-denials.md) — off mode now also logs denials

**Cross-reference:** Main spec, Audit Log Configuration section; bead
`holomush-75um`.
