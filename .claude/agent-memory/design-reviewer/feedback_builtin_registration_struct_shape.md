---
name: feedback-builtin-registration-struct-shape
description: Spec snippets adding entries to `internal/core/builtins.go` often invent struct shapes that don't match `VerbRegistration`. Always cross-check field-by-field against the actual `internal/core/registry.go` type and an existing in-tree precedent (e.g., the rekey entry at `builtins.go:93`).
metadata:
  type: feedback
---

**Rule:** When reviewing a spec that adds an event-type registration to
`internal/core/builtins.go`, verify the proposed struct literal field-by-field
against the actual `VerbRegistration` type in `internal/core/registry.go:14-30`
AND against at least one existing in-tree registration of similar kind. Common
failure modes:

- `Category: AuditOnly` — `Category` is a plain string ("system", "movement"),
  NOT an enum. "AUDIT_ONLY" lives in the separate `DisplayTarget` field as
  `corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY`.
- `ProducedBy: "core"` — this field does not exist. The ownership field is
  `Source: "builtin"`.
- Missing `Format` / `DisplayTarget` — both are required by `registerNoLock`
  validation (`registry.go:54-89`); empty values produce `INVALID_REGISTRATION`
  at boot.

**Why:** Spec authors paraphrase the registration shape from memory. The
spec's promised invariant (e.g., "MUST be registered with `AuditOnly`
category") becomes ungrounded because the corresponding test surface
doesn't exist on the actual struct. Plan-writers either compile-fail
(literal copy) or silently rewrite the invariant.

**How to apply:** For any spec that names `internal/core/builtins.go`,
extract the relevant `VerbRegistration` literal block from the spec and
diff it field-by-field against an existing precedent (e.g., the rekey
entry at `internal/core/builtins.go:93`). Reject the spec if any
field is missing, misspelled, or assigned to a wrong type.

**Seen:** 2026-05-12 in event-payload-crypto-phase5-sub-epic-f r4:
spec §3.8 declared `{Category: AuditOnly, ProducedBy: "core"}` for two
new operator_read event types. Real shape per `builtins.go:93` is
`{Category: "system", Format: "audit", DisplayTarget:
corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"}`. The
spec's invariant INV-F13 ("registered with `AuditOnly` category") was
ungrounded because Category is a different field surface than
DisplayTarget.
