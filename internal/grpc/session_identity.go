// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/authguard"
)

// buildCharacterIdentity returns the typed session identity for the character
// when crypto is active and a binding repo is wired; otherwise the zero
// (passthrough) identity. Single source of truth for the Subscribe and
// QueryStreamHistory identity gate (formerly duplicated at server.go:995 /
// query_stream_history.go:306).
//
// Binding lookup is only performed when a KEK is wired (cryptoActive).
// Without a KEK, skip this so characters without a binding row don't
// break Subscribe or QueryStreamHistory in KEK-less deployments.
//
// Errors are returned without a top-level oops code so call sites can wrap
// them with the appropriate surface-specific code (SUBSCRIBE_BINDING_LOOKUP_FAILED
// or HISTORY_BINDING_LOOKUP_FAILED) that remains observable via oops.AsOops.
func (s *CoreServer) buildCharacterIdentity(ctx context.Context, playerID, characterID string) (eventbus.SessionIdentity, error) {
	if s.bindings == nil || !s.cryptoActive {
		return eventbus.SessionIdentity{}, nil
	}
	bindingID, err := s.bindings.Current(ctx, characterID)
	if err != nil {
		return eventbus.SessionIdentity{}, oops.With("character_id", characterID).
			With("cause", "binding_lookup_failed").Wrap(err)
	}
	identity, err := authguard.NewCharacterIdentity(playerID, characterID, bindingID)
	if err != nil {
		return eventbus.SessionIdentity{}, oops.With("cause", "identity_invalid").Wrap(err)
	}
	return authguard.ToSessionIdentity(identity), nil
}
