<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 106. `minimal` Audit Mode Is Alias for `denials_only`

> [Back to Decision Index](../README.md)

**Review finding (PR #69, Critical #3):** After Decision #56 (system bypasses
in minimal) and Decision #86 (denials in minimal), both `minimal` and
`denials_only` log the same events: system bypasses + deny + default_deny. The
three-tier audit system is effectively two-tier.

**Decision:** Document `minimal` as an alias for `denials_only`, reserved for
future differentiation.

**Audit modes after this decision:**

| Mode           | Logs                                      | Notes                          |
| -------------- | ----------------------------------------- | ------------------------------ |
| `minimal`      | System bypasses + deny + default_deny     | Alias for `denials_only`       |
| `denials_only` | System bypasses + deny + default_deny     | Canonical name                 |
| `all`          | All decisions incl. allow                 |                                |

**Rationale:**

1. **No functional distinction remains.** Decision #56 added system bypass
   logging to `minimal`. Decision #86 added denial logging. These additions
   make `minimal` and `denials_only` log-identical.

2. **Alias preserves naming stability.** Collapsing to two modes would be a
   breaking spec change. Keeping `minimal` as a recognized alias avoids
   churn in specs, ADRs, and future implementation code.

3. **Future differentiation possible.** If a real distinction is needed later
   (e.g., `minimal` omits attribute snapshots), the name is already reserved.

**Impact:**

- `05-storage-audit.md`: Document alias relationship in mode table
- Decision #104: Remove incorrect claim that `minimal` omits `default_deny`
- Code: `AuditMinimal` const maps to same behavior as `AuditDenialsOnly`

**Related:** Decision #38 (Audit Log Configuration Modes), Decision #56
(System Bypasses in Minimal), Decision #86 (Minimal Logs Denials),
Decision #104 (Rename Off to Minimal)
