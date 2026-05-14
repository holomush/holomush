---
name: clean-break-enumerate-callers
description: "Clean-break" proto reshapes citing the "no prod-shape discipline for undeployed codebases" rule MUST still enumerate every caller of dropped fields
metadata:
  type: feedback
---

# Clean-break proto reshape must enumerate every caller of dropped fields

`[feedback_no_prod_shape_for_undeployed]` exempts a spec from adding `reserved` markers / compat shims / deprecated field IDs. It does NOT exempt the spec from naming every caller of every dropped field.

**Why:** plan-writers can't infer migration scope from "clean break" alone. A dropped `map<string,string> headers` field with three callers becomes a "fix one field" task that silently breaks two tests.

**How to apply:** for every field added OR removed in a proto reshape, run `rg "Get<FieldName>" --type go` AND `rg "\.<FieldName>\s*[=:]"` across the repo. List every callsite in the spec's §migration. Test files count.

Seen 2026-05-13 in event-payload-crypto-phase7-plugin-sdk-design v2: §4.2 swapped `AuditEventRequest.event + headers` → `AuditEventRequest.row AuditRow`. `headers` map dropped silently. Three callers consumed it: `plugins/core-scenes/audit.go:160-237`, `test/integration/eventbus_e2e/plugin_audit_isolation_test.go:175-220`, `internal/eventbus/audit/plugin_consumer_unit_test.go:82`. Spec named one (audit.go).

Also: the new `AuditRow` shape carried `codec`, `dek_ref`, `dek_version` but NOT `schema_ver` even though `scene_log.schema_ver` is a NOT NULL column. The spec didn't say where `schema_ver` data comes from post-reshape.

Related: [[proto-path-verification]]
