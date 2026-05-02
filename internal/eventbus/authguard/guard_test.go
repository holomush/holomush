// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
)

type fakeParticipants struct{ list []dek.Participant }

func (f *fakeParticipants) Participants(_ context.Context, _ codec.KeyID, _ uint32) ([]dek.Participant, error) {
	return f.list, nil
}

type fakeManifest struct{ allowed map[string]map[string]bool }

func (f *fakeManifest) PluginRequestsDecryption(plugin, eventType string) bool {
	if perPlugin := f.allowed[plugin]; perPlugin != nil {
		return perPlugin[eventType]
	}
	return false
}

type fakeBackpressure struct{ throttle bool }

func (f *fakeBackpressure) ShouldThrottle(_ string) bool { return f.throttle }

// newGuardWithFakes builds a Guard with the test fixtures.
func newGuardWithFakes(t *testing.T, parts []dek.Participant, abacAllow bool) authguard.AuthGuard {
	t.Helper()
	p := &fakeParticipants{list: parts}
	m := &fakeManifest{allowed: map[string]map[string]bool{
		"mod-filter": {"core-comm:whisper": true},
	}}
	var abac authguard.ABACEngine
	if abacAllow {
		abac = policytest.AllowAllEngine()
	} else {
		abac = policytest.DenyAllEngine()
	}
	b := &fakeBackpressure{throttle: false}
	g, err := authguard.New(p, m, abac, b)
	require.NoError(t, err)
	return g
}

// Branch 1 — character is participant.
func TestGuardBranchCharacterParticipantPermits(t *testing.T) {
	parts := []dek.Participant{{PlayerID: "01ABC", CharacterID: "01XYZ", BindingID: "01DEF"}}
	g := newGuardWithFakes(t, parts, false)

	id, err := authguard.NewCharacterIdentity("01ABC", "01XYZ", "01DEF")
	require.NoError(t, err)
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
		EventType: "core-comm:whisper", EventID: idgen.New(),
	})
	require.NoError(t, err)
	assert.True(t, decision.Permit)
	assert.Equal(t, authguard.PermitParticipant, decision.Code)
}

func TestGuardBranchCharacterNonParticipantDenies(t *testing.T) {
	parts := []dek.Participant{{PlayerID: "01OTHER", CharacterID: "01OTHERCHAR", BindingID: "01OTHERBIND"}}
	g := newGuardWithFakes(t, parts, false)

	id, _ := authguard.NewCharacterIdentity("01ABC", "01XYZ", "01DEF")
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.False(t, decision.Permit)
	assert.Equal(t, authguard.DenyNotParticipant, decision.Code)
}

// Branch 2 — player history read.
func TestGuardBranchPlayerHistoryReadPermitsWhenABACAllows(t *testing.T) {
	parts := []dek.Participant{{PlayerID: "01ABC", CharacterID: "01PRIORCHAR", BindingID: "01PRIORBIND"}}
	g := newGuardWithFakes(t, parts, true)

	id, _ := authguard.NewPlayerIdentity("01ABC")
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.True(t, decision.Permit)
	assert.Equal(t, authguard.PermitPlayerHistory, decision.Code)
}

func TestGuardBranchPlayerNeverParticipatedDenies(t *testing.T) {
	parts := []dek.Participant{{PlayerID: "01OTHER", CharacterID: "01X", BindingID: "01Y"}}
	g := newGuardWithFakes(t, parts, true)

	id, _ := authguard.NewPlayerIdentity("01ABC")
	decision, err := g.Check(t.Context(), authguard.CheckRequest{Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1})
	require.NoError(t, err)
	assert.False(t, decision.Permit)
	assert.Equal(t, authguard.DenyPlayerNeverParticipated, decision.Code)
}

// Branch 3 — plugin: manifest+ABAC permits.
func TestGuardBranchPluginPermits(t *testing.T) {
	g := newGuardWithFakes(t, nil, true)
	id, _ := authguard.NewPluginIdentity("mod-filter", "01INST")
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
		EventType: "core-comm:whisper",
	})
	require.NoError(t, err)
	assert.True(t, decision.Permit)
	assert.Equal(t, authguard.PermitPluginGrant, decision.Code)
}

// Branch 3 — plugin: manifest missing.
func TestGuardBranchPluginManifestMissingDenies(t *testing.T) {
	g := newGuardWithFakes(t, nil, true)
	id, _ := authguard.NewPluginIdentity("mod-filter", "01INST")
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, EventType: "core-comm:undeclared",
	})
	require.NoError(t, err)
	assert.False(t, decision.Permit)
	assert.Equal(t, authguard.DenyManifestDeclarationMissing, decision.Code)
}

// Branch 2 — player participated but ABAC denies the read.
func TestGuardBranchPlayerParticipatedButABACDeniesReturnsDenyPlayerNoABACGrant(t *testing.T) {
	parts := []dek.Participant{{PlayerID: "01ABC", CharacterID: "01PRIORCHAR", BindingID: "01PRIORBIND"}}
	g := newGuardWithFakes(t, parts, false) // abacAllow=false → DenyAllEngine

	id, _ := authguard.NewPlayerIdentity("01ABC")
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.False(t, decision.Permit)
	assert.Equal(t, authguard.DenyPlayerNoABACGrant, decision.Code)
	assert.NotNil(t, decision.ABACDecision, "ABAC decision must be attached for trace")
}

// Branch 3 — plugin: manifest declared, backpressure ok, ABAC denies.
func TestGuardBranchPluginManifestPresentButABACDeniesReturnsDenyNoABACGrant(t *testing.T) {
	g := newGuardWithFakes(t, nil, false) // abacAllow=false → DenyAllEngine

	id, _ := authguard.NewPluginIdentity("mod-filter", "01INST")
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, KeyID: codec.KeyID(42), KeyVersion: 1,
		EventType: "core-comm:whisper",
	})
	require.NoError(t, err)
	assert.False(t, decision.Permit)
	assert.Equal(t, authguard.DenyNoABACGrant, decision.Code)
	assert.NotNil(t, decision.ABACDecision, "ABAC decision must be attached for trace")
}

// Branch 3 — backpressure pre-check.
func TestGuardBranchPluginBackpressureDeniesEarly(t *testing.T) {
	p := &fakeParticipants{}
	m := &fakeManifest{allowed: map[string]map[string]bool{"mod-filter": {"core-comm:whisper": true}}}
	a := policytest.AllowAllEngine()
	b := &fakeBackpressure{throttle: true}
	g, err := authguard.New(p, m, a, b)
	require.NoError(t, err)

	id, _ := authguard.NewPluginIdentity("mod-filter", "01INST")
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, EventType: "core-comm:whisper",
	})
	require.NoError(t, err)
	assert.False(t, decision.Permit)
	assert.Equal(t, authguard.DenyAuditBackpressure, decision.Code)
}

// Branch 4 — operator: INV-43.
func TestGuardBranchOperatorAlwaysDenies(t *testing.T) {
	g := newGuardWithFakes(t, nil, true)
	id := authguard.NewOperatorIdentity()
	decision, err := g.Check(t.Context(), authguard.CheckRequest{Identity: id})
	require.NoError(t, err)
	assert.False(t, decision.Permit)
	assert.Equal(t, authguard.DenyOperatorUseAdminRPC, decision.Code)
}

func TestGuardBranchUnknownKindDenies(t *testing.T) {
	g := newGuardWithFakes(t, nil, true)
	decision, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity: authguard.Identity{Kind: authguard.IdentityKindUnknown},
	})
	require.NoError(t, err)
	assert.False(t, decision.Permit)
	assert.Equal(t, authguard.DenyUnknownIdentityKind, decision.Code)
}

func TestGuardNewRejectsNilDependencies(t *testing.T) {
	p := &fakeParticipants{}
	m := &fakeManifest{}
	a := policytest.AllowAllEngine()
	b := &fakeBackpressure{}

	_, err := authguard.New(nil, m, a, b)
	require.Error(t, err)

	_, err = authguard.New(p, nil, a, b)
	require.Error(t, err)

	_, err = authguard.New(p, m, nil, b)
	require.Error(t, err)

	_, err = authguard.New(p, m, a, nil)
	require.Error(t, err)
}
