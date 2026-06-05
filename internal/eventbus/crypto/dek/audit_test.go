// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// fakeChainRepo implements chain.Repo, returning empty entries (genesis).
type fakeChainRepo struct{}

func (f *fakeChainRepo) LoadEntriesByScope(_ context.Context, _, _ string) ([]chain.Entry, error) {
	return nil, nil
}

func (f *fakeChainRepo) DiscoverScopes(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (f *fakeChainRepo) ChainInitialized(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (f *fakeChainRepo) MarkChainInitialized(_ context.Context, _, _ string) error {
	return nil
}

// capturedPublish holds a single captured PublishAudit call.
type capturedPublish struct {
	Subject string
	Type    string
	Payload []byte
}

// capturingPublisher implements dek.AuditPublisher.
type capturingPublisher struct {
	published []capturedPublish
}

func (c *capturingPublisher) PublishAudit(_ context.Context, subject, evType string, payload []byte) (ulid.ULID, error) {
	c.published = append(c.published, capturedPublish{
		Subject: subject,
		Type:    evType,
		Payload: payload,
	})
	return ulid.Make(), nil
}

// TestRekeyAuditEmitter_Emit_PopulatesChainLinkage verifies the genesis case:
// - subject is "events.<game>.system.rekey.<ct>.<cid>"
// - type is "crypto.system.rekey"
// - decoded payload has populated rekey_chain block (scope, nil prev_hash, non-empty self_hash)
// Satisfies TDD acceptance criteria from bead holomush-jxo8.7.17.
func TestRekeyAuditEmitter_Emit_PopulatesChainLinkage(t *testing.T) {
	prevGameID := dek.GameIDForTest()
	dek.SetGameIDForTest("g1")
	t.Cleanup(func() { dek.SetGameIDForTest(prevGameID) })

	fakeRepo := &fakeChainRepo{}
	em := chain.NewEmitter(fakeRepo)
	publisher := &capturingPublisher{}

	auditEm := dek.NewRekeyAuditEmitter(em, publisher)
	payload := dek.RekeyAuditPayload{
		RequestID:   "01HXY...",
		Context:     dek.RekeyAuditContext{Type: "scene", ID: "01ABC"},
		OldDEK:      dek.RekeyAuditDEK{ID: 100, Version: 3},
		NewDEK:      dek.RekeyAuditDEK{ID: 200, Version: 4},
		PolicyHash:  "sha256:aabb",
		StartedAt:   time.Now(),
		CompletedAt: time.Now(),
	}
	eventID, finalized, err := auditEm.Emit(context.Background(), payload)
	require.NoError(t, err)
	require.NotEmpty(t, eventID)
	require.NotEmpty(t, finalized.RekeyChainField.SelfHash, "finalized payload exposes computed self_hash")

	require.Len(t, publisher.published, 1)
	pub := publisher.published[0]
	require.Equal(t, "events.g1.system.rekey.scene.01ABC", pub.Subject)
	require.Equal(t, "crypto.system.rekey", pub.Type)

	// The published payload MUST contain a populated rekey_chain block.
	var decoded dek.RekeyAuditPayload
	require.NoError(t, json.Unmarshal(pub.Payload, &decoded))
	require.Equal(t, "scene:01ABC", decoded.RekeyChainField.Scope)
	require.Nil(t, decoded.RekeyChainField.PrevHash, "first emit = genesis")
	require.NotEmpty(t, decoded.RekeyChainField.SelfHash)
}

// TestRekeyHandlerFor_ValidateRegistration ensures RekeyHandlerFor produces
// a Handler whose Chain passes ValidateRegistration (INV-CRYPTO-113/INV-CRYPTO-114/INV-CRYPTO-115).
func TestRekeyHandlerFor_ValidateRegistration(t *testing.T) {
	h := dek.RekeyHandlerFor("testgame")
	require.NoError(t, chain.ValidateRegistration(h.Chain))
}

// TestRekeyHandlerFor_SubjectFor verifies that SubjectFor produces the expected
// subject for a given scope.
func TestRekeyHandlerFor_SubjectFor(t *testing.T) {
	h := dek.RekeyHandlerFor("mygame")
	subject := h.SubjectFor("scene:01ABC")
	require.Equal(t, "events.mygame.system.rekey.scene.01ABC", subject)
}

// TestRekeyHandlerFor_ScopeFromSubject round-trips SubjectFor / ScopeFromSubject.
func TestRekeyHandlerFor_ScopeFromSubject(t *testing.T) {
	h := dek.RekeyHandlerFor("mygame")
	subject := h.SubjectFor("scene:01ABC")
	scope, err := h.ScopeFromSubject(subject)
	require.NoError(t, err)
	require.Equal(t, "scene:01ABC", scope)
}

// TestRekeyHandlerFor_ScopeFromPayload verifies INV-CRYPTO-114 independent extraction.
func TestRekeyHandlerFor_ScopeFromPayload(t *testing.T) {
	h := dek.RekeyHandlerFor("mygame")
	raw := []byte(`{"context":{"type":"scene","id":"01ABC"}}`)
	scope, err := h.ScopeFromPayload(raw)
	require.NoError(t, err)
	require.Equal(t, "scene:01ABC", scope)
}

// TestRekeyHandlerFor_Canonicalize verifies Canonicalize returns stable JCS bytes.
func TestRekeyHandlerFor_Canonicalize(t *testing.T) {
	h := dek.RekeyHandlerFor("mygame")
	a := []byte(`{"b":2,"a":1}`)
	b := []byte(`{"a":1,"b":2}`)
	ca, err := h.Canonicalize(a)
	require.NoError(t, err)
	cb, err := h.Canonicalize(b)
	require.NoError(t, err)
	require.Equal(t, ca, cb)
}
