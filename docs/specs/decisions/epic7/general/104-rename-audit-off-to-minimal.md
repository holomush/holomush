<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 104. Rename Audit Mode `off` to `minimal`

> [Back to Decision Index](../README.md)

**Review finding (PR #69, Important #9 + Suggestion #3):** The audit mode named
`off` still logs system bypasses and explicit denials, making the name
misleading. Users expect `off` to mean "no logging at all."

**Decision:** Rename the `off` audit mode to `minimal`.

**Previous behavior (unchanged):**

| Mode           | Logs                                  |
| -------------- | ------------------------------------- |
| `minimal`      | System bypasses + denials             |
| `denials_only` | System bypasses + deny + default_deny |
| `all`          | All decisions incl. allow             |

The functional distinction between `minimal` and `denials_only` is that
`minimal` omits `default_deny` (system-failure denial) entries. Both modes
always log system bypass events (per Decision #56).

**Rationale:**

1. **Name clarity:** `off` implies zero logging, but the mode always logs system
   bypasses (Decision #56) and explicit denials (Decision #86). `minimal`
   accurately describes the behavior: the minimum viable audit trail.

2. **Operational safety:** Operators who set `off` expecting silence may miss
   that bypasses and denials are still logged. `minimal` sets correct
   expectations.

3. **No behavioral change:** Only the Go const name (`AuditOff` → `AuditMinimal`)
   and config string (`"off"` → `"minimal"`) change. Logging behavior is
   identical.

**Impact:**

- `05-storage-audit.md`: Update Go const and mode table
- All spec/plan references to `off` mode updated
- Decision #56 ("Audit Off Mode Includes System Bypasses"): title references
  `off` but rationale still applies — add cross-reference to this decision
- Decision #86 ("Audit Off Mode Logs Denials"): same treatment

**Related:** Decision #38 (Audit Log Configuration Modes), Decision #56
(Audit Off Mode Includes System Bypasses), Decision #86 (Audit Off Mode Logs
Denials)
