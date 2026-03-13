<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 13. Subject Prefix Normalization

> [Back to Decision Index](../README.md)

**Question:** The static system uses `char:` as the subject prefix for
characters, but resources already use `character:` (e.g., `character:01ABC`).
Should the ABAC system normalize?

**Decision:** Normalize to `character:` everywhere. ~~The adapter MUST accept
both `char:` and `character:` during migration, normalizing to `character:`
internally.~~ The `access` package SHOULD define prefix constants
(`SubjectCharacter`, `SubjectPlugin`, etc.) to prevent typos.

**Rationale:** Asymmetric prefixes (`char:` for subjects, `character:` for
resources) create confusion in policies and audit logs. Normalizing to
`character:` aligns subjects with resources and with Cedar conventions where
the principal type name matches the DSL type name.

_Note: [Decision #36](../phase-7.6/036-direct-replacement-no-adapter.md) removed the adapter. All call sites switch directly to
`character:` — the engine **MUST** reject the `char:` prefix with a clear
error rather than normalizing it. The prefix constants (`SubjectCharacter`,
`SubjectPlugin`, etc.) remain the recommended approach._
