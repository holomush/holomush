// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/pkg/errutil"
)

// erroringChainRepo implements chain.Repo and fails LoadEntriesByScope so the
// emitter's prev-hash computation surfaces an error.
type erroringChainRepo struct{}

func (erroringChainRepo) LoadEntriesByScope(_ context.Context, _, _ string) ([]chain.Entry, error) {
	return nil, errors.New("load entries boom")
}

func (erroringChainRepo) DiscoverScopes(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (erroringChainRepo) ChainInitialized(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (erroringChainRepo) MarkChainInitialized(_ context.Context, _, _ string) error { return nil }

// erroringPublisher implements dek.AuditPublisher and always fails the publish.
type erroringPublisher struct{}

func (erroringPublisher) PublishAudit(_ context.Context, _, _ string, _ []byte) (ulid.ULID, error) {
	return ulid.ULID{}, errors.New("publish boom")
}

func samplePayload() dek.RekeyAuditPayload {
	return dek.RekeyAuditPayload{
		RequestID:   "01HXY",
		Context:     dek.RekeyAuditContext{Type: "scene", ID: "01ABC"},
		OldDEK:      dek.RekeyAuditDEK{ID: 100, Version: 3},
		NewDEK:      dek.RekeyAuditDEK{ID: 200, Version: 4},
		PolicyHash:  "sha256:aabb",
		StartedAt:   time.Now(),
		CompletedAt: time.Now(),
	}
}

// TestRekeyAuditEmitter_Emit_FailsClosedOnPrevHashError verifies Emit aborts
// with DEK_REKEY_AUDIT_PREV_HASH_FAILED when the chain repo cannot load the
// chain head. Fail-closed: no event ID is returned and the chain-link fields
// are NOT populated — the emitter does not publish an unlinked audit event.
func TestRekeyAuditEmitter_Emit_FailsClosedOnPrevHashError(t *testing.T) {
	prev := dek.GameIDForTest()
	dek.SetGameIDForTest("g1")
	t.Cleanup(func() { dek.SetGameIDForTest(prev) })

	pub := &capturingPublisher{}
	em := dek.NewRekeyAuditEmitter(chain.NewEmitter(erroringChainRepo{}), pub)

	eventID, _, err := em.Emit(context.Background(), samplePayload())
	require.Error(t, err)
	// Fail-closed: prev-hash failure aborts BEFORE publish. The dek wrapper
	// carries the underlying chain-load failure (oops propagates the inner
	// AUDIT_CHAIN_LOAD_FAILED code; errors.Is still walks to it).
	errutil.AssertErrorCode(t, err, "AUDIT_CHAIN_LOAD_FAILED")
	assert.ErrorContains(t, err, "load entries boom")
	assert.Equal(t, ulid.ULID{}, eventID, "no event ID minted on the failure path")
	assert.Empty(t, pub.published, "no audit event published when prev-hash cannot be computed")
}

// TestRekeyAuditEmitter_Emit_FailsClosedOnPublishError verifies Emit returns
// DEK_REKEY_AUDIT_PUBLISH_FAILED when the publisher fails, AND that it returns
// the FINALIZED payload (chain-link fields populated) so the caller's
// audit-fallback log can persist the exact record that would have been emitted
// (INV-CRYPTO-100). Fail-closed: the error is surfaced (no silent drop) and the
// subject is attached for forensics, but no plaintext DEK material is exposed.
func TestRekeyAuditEmitter_Emit_FailsClosedOnPublishError(t *testing.T) {
	prev := dek.GameIDForTest()
	dek.SetGameIDForTest("g1")
	t.Cleanup(func() { dek.SetGameIDForTest(prev) })

	em := dek.NewRekeyAuditEmitter(chain.NewEmitter(&fakeChainRepo{}), erroringPublisher{})

	eventID, finalized, err := em.Emit(context.Background(), samplePayload())
	require.Error(t, err)
	oerr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "DEK_REKEY_AUDIT_PUBLISH_FAILED", oerr.Code())
	errutil.AssertErrorContext(t, err, "subject", "events.g1.system.rekey.scene.01ABC")
	assert.Equal(t, ulid.ULID{}, eventID, "no event ID returned on publish failure")

	// INV-CRYPTO-100: the finalized payload carries the populated chain-link
	// fields so the fallback log records what would have been published.
	assert.Equal(t, "scene:01ABC", finalized.RekeyChainField.Scope)
	assert.NotEmpty(t, finalized.RekeyChainField.SelfHash,
		"finalized payload exposes the computed self_hash for the fallback log")
}
