// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard_test

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/authguard"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubDEKManager implements dek.Manager for unit tests.
// Only Participants is exercised; all other methods are no-ops.
type stubDEKManager struct{ parts []dek.Participant }

func (s *stubDEKManager) GetOrCreate(_ context.Context, _ dek.ContextID, _ []dek.Participant) (codec.Key, error) {
	return codec.Key{}, nil
}

func (s *stubDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return codec.Key{}, nil
}

func (s *stubDEKManager) Participants(_ context.Context, _ codec.KeyID, _ uint32) ([]dek.Participant, error) {
	return s.parts, nil
}

func (s *stubDEKManager) Add(_ context.Context, _ dek.ContextID, _ dek.Participant) error {
	return nil
}

func (s *stubDEKManager) EnsureParticipant(_ context.Context, _ dek.ContextID, _ dek.Participant) error {
	return nil
}

func (s *stubDEKManager) Rotate(_ context.Context, _ dek.ContextID, _ []dek.Participant, _ string) error {
	return nil
}

func (s *stubDEKManager) Rekey(_ context.Context, _ dek.ContextID, _ string, _ dek.OperatorFactors) error {
	return nil
}

func (s *stubDEKManager) ActiveDEKRow(_ context.Context, _ dek.ContextID) (dek.ActiveDEKRecord, error) {
	return dek.ActiveDEKRecord{}, nil // stub: unused in adapter tests
}

func (s *stubDEKManager) MintNewDEKForRekey(_ context.Context, _ int64) (int64, error) {
	return 0, nil // stub: unused in adapter tests
}

func (s *stubDEKManager) DestroyDEK(_ context.Context, _ int64) error {
	return nil // stub: unused in adapter tests
}

func (s *stubDEKManager) EvictCachedDEK(_ context.Context, _ int64) error {
	return nil // stub: unused in adapter tests
}

func TestDEKParticipantLookupAdapterDelegatesToManager(t *testing.T) {
	parts := []dek.Participant{{PlayerID: "01ABC", BindingID: "01DEF"}}
	mgr := &stubDEKManager{parts: parts}
	lookup := authguard.NewDEKParticipantLookup(mgr)

	got, err := lookup.Participants(context.Background(), codec.KeyID(1), 1)
	require.NoError(t, err)
	assert.Equal(t, parts, got)
}

// errorDEKManager wraps stubDEKManager but returns an error from Participants.
type errorDEKManager struct{ stubDEKManager }

func (e *errorDEKManager) Participants(_ context.Context, _ codec.KeyID, _ uint32) ([]dek.Participant, error) {
	return nil, oops.Errorf("simulated dek manager failure")
}

func TestDEKParticipantLookupAdapterPropagatesParticipantsError(t *testing.T) {
	mgr := &errorDEKManager{stubDEKManager: stubDEKManager{}}
	lookup := authguard.NewDEKParticipantLookup(mgr)

	_, err := lookup.Participants(context.Background(), codec.KeyID(1), 1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUTHGUARD_DEK_PARTICIPANTS_FAILED")
}
