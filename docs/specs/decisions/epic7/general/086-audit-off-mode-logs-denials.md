<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 86. Audit Off Mode Logs Denials

> [Back to Decision Index](../README.md)

**Question:** Should audit mode `minimal` (originally called `off`) suppress denial logging?

**Context:** Security requirement S3 states that denials MUST be logged regardless
of audit mode. However, the original `off` mode semantics (after ADR 056) logged
only system bypasses, creating a contradiction: deny and default_deny outcomes
would be silently dropped in `off` mode, violating S3.

**Decision:** Audit mode `minimal` (renamed from `off` per Decision #104) MUST
still log denials (`deny` and `default_deny` outcomes) and system bypasses. Only
`allow` records are suppressed. This resolves the contradiction between S3 and
the original `off` mode description.

**Rationale:** Denial logging is a security requirement. The cost of logging
denials is minimal compared to the security risk of blind spots in access control
auditing. The name `minimal` means "minimal audit logging" (no allow records),
accurately describing the behavior. Denial volume is expected to be low in normal
operation, so this adds negligible overhead even in performance-sensitive
environments.

**Updated by:** [Decision #104](./104-rename-audit-off-to-minimal.md) — mode renamed from `off` to `minimal`

**Cross-reference:** SPEC-C3; security requirement S3; ADR 056 (system bypasses
in off mode); ADR 038 (audit log configuration modes).
