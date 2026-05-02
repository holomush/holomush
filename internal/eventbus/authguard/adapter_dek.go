// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

type dekParticipantAdapter struct{ mgr dek.Manager }

// NewDEKParticipantLookup wraps a dek.Manager as a ParticipantLookup.
func NewDEKParticipantLookup(mgr dek.Manager) ParticipantLookup {
	return &dekParticipantAdapter{mgr: mgr}
}

func (a *dekParticipantAdapter) Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]dek.Participant, error) {
	parts, err := a.mgr.Participants(ctx, keyID, version)
	if err != nil {
		return nil, oops.Code("AUTHGUARD_DEK_PARTICIPANTS_FAILED").Wrap(err)
	}
	return parts, nil
}
