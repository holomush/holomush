<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 87. Remove seed:property-system-forbid Policy

> [Back to Decision Index](../README.md)

**Question:** How should system properties be protected from non-admin access?

**Context:** The original seed policy set included a `seed:property-system-forbid`
forbid-effect policy that denied all characters access to system properties. Under
deny-overrides conflict resolution (ADR 004), a forbid policy cannot be overridden
by any permit policy. This effectively locked admins out of system properties,
since even the blanket `seed:admin-full-access` permit could not override the
forbid.

**Decision:** Remove the `seed:property-system-forbid` policy entirely. Rely on
default-deny: since no permit policy grants non-admin access to system properties,
they remain inaccessible to non-admins. Admins get access via the blanket
`seed:admin-full-access` permit, which is no longer blocked by a competing forbid.

**Rationale:** Default-deny achieves the same protection for non-admin characters
without blocking admin access. Under deny-overrides, forbid policies are absolute
barriers that no permit can overcome. The original forbid policy was
well-intentioned but created an unresolvable conflict: it denied the very admins
who need system property access for maintenance and debugging. Removing the forbid
and relying on the absence of a permit for non-admins is the correct pattern.

**Cross-reference:** SPEC-I6; ADR 004 (conflict resolution / deny-overrides);
seed policy `seed:admin-full-access`.
