// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package source_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/history/source"
)

type stubDEKManager struct {
	key codec.Key
	err error
}

func (s *stubDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return s.key, s.err
}

// Other dek.Manager methods unused in resolver tests; embed and panic if called.
func (s *stubDEKManager) GetOrCreate(context.Context, dek.ContextID, []dek.Participant) (codec.Key, error) {
	panic("unused")
}

func (s *stubDEKManager) Participants(context.Context, codec.KeyID, uint32) ([]dek.Participant, error) {
	panic("unused")
}

func (s *stubDEKManager) Add(context.Context, dek.ContextID, dek.Participant) error { panic("unused") }

func (s *stubDEKManager) EnsureParticipant(context.Context, dek.ContextID, dek.Participant) error {
	panic("unused")
}

func (s *stubDEKManager) Rotate(context.Context, dek.ContextID, []dek.Participant, string) error {
	panic("unused")
}

func (s *stubDEKManager) Rekey(context.Context, dek.ContextID, string, dek.OperatorFactors) error {
	panic("unused")
}

func (s *stubDEKManager) ActiveDEKRow(_ context.Context, _ dek.ContextID) (dek.ActiveDEKRecord, error) {
	panic("unused")
}

func (s *stubDEKManager) MintNewDEKForRekey(context.Context, int64) (int64, error) {
	panic("unused")
}

func (s *stubDEKManager) DestroyDEK(context.Context, int64) error { panic("unused") }

func (s *stubDEKManager) EvictCachedDEK(context.Context, int64) error { panic("unused") }

func TestSimpleResolver_IdentityCodecBypassesResolve(t *testing.T) {
	env := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		Codec: codec.NameIdentity,
	})
	r := source.NewSimpleResolver(&stubDEKManager{})
	got, err := r.Resolve(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, source.TierHot, got.SourceTier)
}

func TestSimpleResolver_PropagatesResolveError(t *testing.T) {
	env := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		Codec:      codec.NameXChaCha20v1,
		KeyID:      42,
		KeyVersion: 3,
	})
	expectedErr := oops.Code("DEK_NOT_FOUND").Errorf("missing")
	r := source.NewSimpleResolver(&stubDEKManager{err: expectedErr})
	_, err := r.Resolve(context.Background(), env)
	require.Error(t, err)
	require.True(t, errors.Is(err, expectedErr) || err == expectedErr) //nolint:errorlint // plan-verbatim: oops errors may not wrap via errors.Is
}
