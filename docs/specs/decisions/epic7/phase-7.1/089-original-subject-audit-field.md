<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 89. Add original_subject Field to Audit Log Schema

> [Back to Decision Index](../README.md)

**Question:** Should the audit log preserve the original session subject before
session resolution converts it to a character subject?

**Context:** The evaluation algorithm (Step 2, session resolution) converts
`session:*` subjects to `character:*` subjects before policy evaluation. The
current audit log schema records only the resolved `subject` field, losing the
original session identity. This makes it impossible to trace which session
initiated an action from audit logs alone.

**Decision:** Add an `original_subject TEXT` column to the `access_audit_log`
table. Populated only when session resolution occurs (i.e., when the original
subject differs from the resolved subject). NULL when no resolution happened.

**Rationale:** Session attribution is critical for security forensics. If an
account is compromised, operators need to identify which session was used, not
just which character performed the action. The field is nullable and only
populated during resolution, so storage impact is minimal for system-subject and
direct-character evaluations.

**Alternatives Considered:**

- **Log session ID in attributes JSONB:** Less discoverable, harder to index and
  query for forensic purposes.
- **Separate session audit table:** Over-engineered for a single field; would
  require joins for basic audit queries.

**Implications:**

- `access_audit_log` schema gains one nullable TEXT column
- Phase 7.1 Task 2 migration must include the column
- Audit writer in the evaluation engine must populate the field during Step 2
- Existing queries remain unaffected (new column is nullable)

**Cross-references:**

- [05-storage-audit.md](../../../abac/05-storage-audit.md) — audit log schema
- [04-resolution-evaluation.md](../../../abac/04-resolution-evaluation.md) — Step 2
  session resolution
- Phase 7.1 Task 2 — audit log migration
