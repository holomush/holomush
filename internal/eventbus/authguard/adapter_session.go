// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package authguard — bridge adapters for eventbus.SessionAuthGuard.
//
// eventbus.SessionAuthGuard uses eventbus-local types (SessionCheckRequest,
// SessionDecision, SessionIdentity) to break the import cycle:
//
//	eventbus → authguard → plugin → eventbus
//
// This file provides adapters so that Guard (and any other AuthGuard
// implementation) can be wired into the subscriber and history-reader
// options without the caller needing to write the conversion manually.
package authguard

import (
	"context"

	"github.com/holomush/holomush/internal/eventbus"
)

// SessionBridgeGuard wraps a Guard to satisfy eventbus.SessionAuthGuard by
// converting between the authguard-local and eventbus-local request/decision
// types.
type SessionBridgeGuard struct {
	g *Guard
}

// NewSessionBridgeGuard returns a SessionBridgeGuard wrapping g.
func NewSessionBridgeGuard(g *Guard) *SessionBridgeGuard {
	return &SessionBridgeGuard{g: g}
}

// Check converts an eventbus.SessionCheckRequest to authguard.CheckRequest,
// delegates to the wrapped Guard, and converts the result back to
// eventbus.SessionDecision. Satisfies eventbus.SessionAuthGuard.
func (b *SessionBridgeGuard) Check(ctx context.Context, req eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	inner := CheckRequest{
		Identity:   fromSessionIdentity(req.Identity),
		KeyID:      req.KeyID,
		KeyVersion: req.KeyVersion,
		EventType:  req.EventType,
		EventID:    req.EventID,
	}
	dec, err := b.g.Check(ctx, inner)
	if err != nil {
		return eventbus.SessionDecision{}, err
	}
	return eventbus.SessionDecision{
		Permit:  dec.Permit,
		GrantID: dec.GrantID,
	}, nil
}

// fromSessionIdentity converts an eventbus.SessionIdentity to an
// authguard.Identity.
func fromSessionIdentity(id eventbus.SessionIdentity) Identity {
	return Identity{
		Kind:        IdentityKind(id.Kind),
		PlayerID:    id.PlayerID,
		CharacterID: id.CharacterID,
		BindingID:   id.BindingID,
		PluginName:  id.PluginName,
		InstanceID:  id.InstanceID,
	}
}
