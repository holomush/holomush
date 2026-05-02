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

	"github.com/holomush/holomush/internal/eventbus"
)

// SessionBridgeEmitter wraps an Emitter to satisfy eventbus.SessionAuditEmitter.
type SessionBridgeEmitter struct {
	e *Emitter
}

// NewSessionBridgeEmitter returns a SessionBridgeEmitter wrapping e.
func NewSessionBridgeEmitter(e *Emitter) *SessionBridgeEmitter {
	return &SessionBridgeEmitter{e: e}
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
