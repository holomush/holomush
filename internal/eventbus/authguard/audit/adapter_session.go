// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package audit — bridge adapter for eventbus.SessionAuditEmitter.
//
// The Emitter uses audit.PluginDecryptRecord to avoid an import cycle
// (eventbus/authguard/audit imports eventbus). This bridge lets Emitter
// satisfy eventbus.SessionAuditEmitter by converting the local record type
// to the eventbus-local type.
package audit

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
)

// SessionBridgeEmitter wraps an Emitter to satisfy eventbus.SessionAuditEmitter.
type SessionBridgeEmitter struct {
	e *Emitter
}

// NewSessionBridgeEmitter returns a SessionBridgeEmitter wrapping e.
// Returns an error if e is nil: a nil Emitter would panic on the first emit call,
// and fail-closed rejection at construction time is safer than a runtime panic.
func NewSessionBridgeEmitter(e *Emitter) (*SessionBridgeEmitter, error) {
	if e == nil {
		return nil, oops.Code("AUDIT_SESSION_BRIDGE_NIL_EMITTER").
			Errorf("nil Emitter passed to NewSessionBridgeEmitter")
	}
	return &SessionBridgeEmitter{e: e}, nil
}

// EmitPluginDecrypt converts an eventbus.PluginDecryptRecord to
// audit.PluginDecryptRecord and delegates to the wrapped Emitter.
// Satisfies eventbus.SessionAuditEmitter.
func (b *SessionBridgeEmitter) EmitPluginDecrypt(ctx context.Context, rec eventbus.PluginDecryptRecord) error {
	inner := PluginDecryptRecord{
		PluginName:       rec.PluginName,
		PluginInstanceID: rec.PluginInstanceID,
		EventID:          rec.EventID,
		EventSubject:     rec.EventSubject,
		EventType:        rec.EventType,
		DEKRef:           rec.DEKRef,
		DEKVersion:       rec.DEKVersion,
		GrantID:          rec.GrantID,
	}
	return b.e.EmitPluginDecrypt(ctx, inner)
}
