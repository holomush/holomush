// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// AuditRowOf returns the plugin-source-of-truth *pluginauditpb.AuditRow
// stamped on Event by audit.PluginHistoryRouter when converting a plugin's
// QueryHistoryResponse into an Event for the read path.
//
// Returns nil when the Event was not sourced from a plugin (e.g. a
// host-owned subject served from the JetStream hot tier or PostgreSQL
// cold tier).
//
// The Phase 7 read-side fence (history.PluginDowngradeFence) calls this
// to recover the plugin-supplied codec, dek_ref, and dek_version so it
// can apply the INV-P7-7 (manifest-set heuristic) and INV-P7-15 (DEK
// existence) checks against the plugin's original wire shape rather
// than the host-side projection.
//
// Lives in its own file so types.go stays focused on the canonical
// Event shape; this accessor is the only API surface for the unexported
// auditRow field.
func AuditRowOf(ev Event) *pluginauditpb.AuditRow {
	return ev.auditRow
}

// StampAuditRow is the package-export entry point used by
// audit.PluginHistoryRouter to stamp the plugin-source-of-truth row
// onto an Event after converting a QueryHistoryResponse. This is the
// ONLY production caller; the function is exported solely because the
// audit package lives in a sibling directory and cannot reach the
// unexported auditRow field directly.
//
// Plugin-runtime symmetry: Lua and binary plugins that surface rows
// via the same router both flow through here, so the field is stamped
// uniformly regardless of plugin runtime.
func StampAuditRow(ev *Event, row *pluginauditpb.AuditRow) {
	ev.auditRow = row
}
