// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package policy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
)

// TestCryptoPolicySubsystemFailsStartOnPublishError — INV-D17.
// Publisher returns an error; Start must short-circuit with
// CRYPTO_POLICY_EMIT_FAILED wrapping the inner error.
func TestCryptoPolicySubsystemFailsStartOnPublishError(t *testing.T) {
	pub := &fakePublisher{err: errors.New("simulated publish failure")}
	s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
		EmitDeps: policy.EmitDeps{
			GameID:          "subsysfail",
			ServerStartULID: ulid.Make().String(),
			ServerIdentity:  "holomush@test",
			Pool:            testPool,
			Publisher:       pub,
			Clock:           fixedClock{t: time.Unix(1700000000, 0).UTC()},
			Config:          policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}},
		},
		PolicyNames: []string{"dual_control_required"},
	})
	err := s.Start(context.Background())
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	// oops.Code() returns the deepest code in the chain. The outer wrap is
	// CRYPTO_POLICY_EMIT_FAILED; the inner cause is POLICY_EMIT_PUBLISH_FAILED.
	// Both confirm fail-closed per INV-D17 — assert the deepest (publish failure).
	assert.Equal(t, "POLICY_EMIT_PUBLISH_FAILED", o.Code())
}

// TestCryptoPolicySubsystemStartEmitsAllPolicyNames emits one event per
// configured policy_name. v1 uses "dual_control_required" as the single name.
func TestCryptoPolicySubsystemStartEmitsAllPolicyNames(t *testing.T) {
	pub := &fakePublisher{}
	gameID := "subsysok"
	subject := "events." + gameID + ".system.crypto_policy.dual_control_required"
	cleanupSubject(t, subject)

	s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
		EmitDeps: policy.EmitDeps{
			GameID:          gameID,
			ServerStartULID: ulid.Make().String(),
			ServerIdentity:  "holomush@test",
			Pool:            testPool,
			Publisher:       pub,
			Clock:           fixedClock{t: time.Unix(1700000000, 0).UTC()},
			Config:          policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}},
		},
		PolicyNames: []string{"dual_control_required"},
	})
	require.NoError(t, s.Start(context.Background()))
	require.Len(t, pub.Events(), 1, "should emit exactly one event for the single policy_name")
}
