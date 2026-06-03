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
// can apply the INV-CRYPTO-42 (manifest-set heuristic) and INV-CRYPTO-50 (DEK
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
//
// Unique-pointer contract: callers MUST pass a row pointer that is
// uniquely owned by ev. Sharing a single *pluginauditpb.AuditRow
// across multiple events causes Event.Refused to mutate all aliases
// (the method nils auditRow.Payload through the shared pointer); the
// production router satisfies this by allocating a fresh row per
// gRPC Recv.
func StampAuditRow(ev *Event, row *pluginauditpb.AuditRow) {
	ev.auditRow = row
}

// Refused returns a metadata-only value-copy of e with the supplied
// NoPlaintextReason stamped. Payload is cleared and — critically —
// the embedded auditRow's Payload is also nilled so a future reader
// of the row metadata via the proto field (e.g. an operator-read
// classifier extending its inspection to plugin rows) cannot recover
// the original cleartext. Diagnostic metadata (codec, dek_ref,
// dek_version, etc.) is preserved on the auditRow.
//
// Aliasing note: the auditRow pointer is copied verbatim into the
// returned value-copy, so nilling auditRow.Payload also strips the
// proto field on the original e.auditRow. This is intentional given
// the StampAuditRow unique-pointer contract — the row was freshly
// allocated for this Event by the router, so no other Event aliases
// it. Master spec INV-CRYPTO-15 (refused row payload empty).
//
// Slice-header semantics: nilling Payload drops the slice header but
// does not zero the underlying byte array. Concurrent readers that
// captured a slice copy BEFORE the refusal still see the bytes; this
// is acceptable because INV-CRYPTO-15 governs proto-field reachability, not
// GC-bounded in-memory residue.
//
// This method lives in package eventbus because auditRow is unexported;
// callers in sibling packages (e.g. history.PluginDowngradeFence) MUST
// use this single canonical "refuse" semantic rather than rolling their
// own — keeps the invariant local to the field's owning package.
func (e Event) Refused(reason NoPlaintextReason) Event {
	refused := e
	refused.MetadataOnly = true
	refused.NoPlaintextReason = reason
	refused.Payload = nil
	if refused.auditRow != nil {
		refused.auditRow.Payload = nil
	}
	return refused
}
