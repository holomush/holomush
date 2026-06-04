// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"github.com/samber/oops"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// BootstrapVerbRegistry returns a VerbRegistry seeded with host-owned event
// types. This is the single public path for obtaining a seeded registry in
// production. Use NewVerbRegistry() for tests that need an empty registry.
//
// hostVersion is the build-time version of the holomush core binary
// (e.g., "0.4.2-rc1" or "dev"). The bootstrapper records each builtin
// registration with source "builtin" and version "host-" + hostVersion
// so plugin-version drift is visible in events_audit replays.
func BootstrapVerbRegistry(hostVersion string) (*VerbRegistry, error) {
	if hostVersion == "" {
		return nil, oops.Code("INVALID_REGISTRATION").Errorf("hostVersion must not be empty")
	}
	r := NewVerbRegistry()
	if err := registerBuiltinTypes(r, hostVersion); err != nil {
		return nil, err
	}
	return r, nil
}

// registerBuiltinTypes (unexported) registers host-owned event types in the
// given registry. Called only by BootstrapVerbRegistry. Plugin-owned types
// (say/pose/whisper from core-communication, object_* from core-objects)
// are registered by the plugin loader from each plugin's manifest verbs:
// block — see internal/plugin/manager.go — keeping plugin-owned types out
// of internal/core/ per the plugin-boundary discipline.
func registerBuiltinTypes(r *VerbRegistry, hostVersion string) error {
	sourceVersion := "host-" + hostVersion
	builtins := []VerbRegistration{
		// Movement
		{Type: "arrive", Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},
		{Type: "leave", Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},
		{
			Type: "move", Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin",
			MetadataKeys: []MetadataKey{
				{Key: "from_id", ValueType: "string"},
				{Key: "to_id", ValueType: "string"},
				{Key: "exit_name", ValueType: "string"},
			},
		},

		// State
		{
			Type: "location_state", Category: "state", Format: "snapshot", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE, Source: "builtin",
			MetadataKeys: []MetadataKey{
				{Key: "location", ValueType: "object"},
				{Key: "exits", ValueType: "array"},
				{Key: "present", ValueType: "array"},
			},
		},
		{
			Type: "exit_update", Category: "state", Format: "delta", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE, Source: "builtin",
			MetadataKeys: []MetadataKey{{Key: "exits", ValueType: "array"}},
		},

		// Command
		{Type: "command_response", Category: "command", Format: "narrative", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "command_error", Category: "command", Format: "error", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},

		// System
		{Type: "system", Category: "system", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},

		// Session lifecycle — intercepted by the gRPC Subscribe handler to send
		// STREAM_CLOSED to the owner character. The event is also forwarded on
		// non-self subscriptions, hence BOTH so all surfaces receive it.
		// Registered so RenderingPublisher does not block with EMIT_UNKNOWN_VERB.
		{Type: "session_ended", Category: "system", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},

		// Crypto audit (host-emit, persistence-only). DisplayTarget=AUDIT_ONLY
		// so the gRPC Subscribe handler drops these before send; the audit
		// projection persists them like any other event. Restores INV-D14
		// (audit emission persists) and INV-D17 (chain genesis emit persists)
		// for the sub-epic D host-emit subsystems (cryptoPolicySub + totpAuditSvc).
		{Type: "crypto.totp_locked", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
		{Type: "crypto.totp_cleared", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
		{Type: "crypto.totp_recovery_code_consumed", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
		{Type: "crypto.policy_set", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
		// Rekey audit (host-emit, persistence-only). Emitted by Phase 7 of the
		// Rekey orchestrator via rekeyAuditPublisherAdapter → RenderingPublisher.
		// AUDIT_ONLY: gRPC Subscribe handler drops it before delivery; the audit
		// projection persists it to events_audit. Registered here so
		// RenderingPublisher does not reject it with EMIT_UNKNOWN_VERB (INV-E14).
		{Type: "crypto.system.rekey", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
		// Operator-read audit events (host-emit, persistence-only). Emitted by
		// F's AdminReadStream handler at stream-start and stream-end respectively.
		// AUDIT_ONLY: gRPC Subscribe drops before delivery; audit projection
		// persists to events_audit. Registered here so RenderingPublisher does
		// not reject with EMIT_UNKNOWN_VERB (INV-CRYPTO-63).
		{Type: "crypto.system.operator_read", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
		{Type: "crypto.system.operator_read_completed", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
		// Phase 7 PluginDowngradeFence violation audit (host-emit, persistence-only).
		// Emitted by cmd/holomush/phase7_fence_wiring.go::violationEmitter on every
		// INV-CRYPTO-42 row refusal, via RenderingPublisher. AUDIT_ONLY so the gRPC
		// Subscribe handler drops the event before delivery; audit projection
		// persists it to events_audit on subject events.<game>.system.plugin_integrity_violation
		// (events.> prefix per INV-E26 — only events.> reaches the EVENTS stream filter).
		// Registered here so RenderingPublisher does not reject with
		// EMIT_UNKNOWN_VERB — without this entry the documented operator-facing
		// integrity-violation signal silently fails on every refusal.
		{Type: "system:plugin_integrity_violation", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
	}
	for _, b := range builtins {
		if err := r.RegisterWithSource(b, sourceVersion); err != nil {
			return err
		}
	}
	return nil
}
