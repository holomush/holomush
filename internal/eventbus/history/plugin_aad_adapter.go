// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// AuditRowToEvent converts a plugin-returned *pluginauditpb.AuditRow
// into the *eventbusv1.Event shape consumed by aad.Build for AAD
// reconstruction (INV-P7-16, master INV-25).
//
// Behavior contract (spec §5.4 adapter table):
//
//   - Nil input → nil output.
//   - Per-field copy from AuditRow to *eventbusv1.Event for the AAD
//     canonical inputs: Id, Subject, Type, Timestamp, Actor.Kind,
//     Actor.Id — all verbatim via the proto getters so a nil Actor or
//     nil Timestamp passes through as nil (aad.Build tolerates both).
//   - NOT copied: codec, dek_ref, dek_version (passed as scalar args
//     to aad.Build); payload (the AEAD input, not AAD); schema_ver and
//     rendering (not in the AAD canonical input set per master §4.2,
//     verified at internal/eventbus/crypto/aad/aad.go:62-117).
//
// A regression in this function would manifest as EVERY sensitive
// plugin-stored event failing AEAD tag-check on decrypt, because the
// reconstructed AAD would no longer be byte-equal to the encrypt-side
// AAD computed at publish time. The integration test
// TestRoundTripPreservesAADWithSubMicrosecondNanos (INV-STORE-5, formerly
// INV-P7-16 per ADR holomush-f5h0) is the load-bearing guard.
func AuditRowToEvent(row *pluginauditpb.AuditRow) *eventbusv1.Event {
	if row == nil {
		return nil
	}
	return &eventbusv1.Event{
		Id:        row.GetId(),
		Subject:   row.GetSubject(),
		Type:      row.GetType(),
		Timestamp: row.GetTimestamp(),
		Actor:     row.GetActor(),
	}
}
