// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	accesstypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

type fakeParticipants struct{ list []dek.Participant }

func (f *fakeParticipants) Participants(_ context.Context, _ codec.KeyID, _ uint32) ([]dek.Participant, error) {
	return f.list, nil
}

type fakeManifest struct {
	allowed  map[string]map[string]bool
	readback bool
}

func (f *fakeManifest) PluginRequestsDecryption(plugin, eventType string) bool {
	if perPlugin := f.allowed[plugin]; perPlugin != nil {
		return perPlugin[eventType]
	}
	return false
}

func (f *fakeManifest) PluginCanReadBack(_, _ string) bool { return f.readback }

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

// Branch 4 — operator: INV-CRYPTO-24.
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

	tests := []struct {
		name         string
		p            authguard.ParticipantLookup
		m            authguard.ManifestLookup
		a            authguard.ABACEngine
		b            authguard.BackpressureChecker
		wantDepField string
	}{
		{"nil ParticipantLookup", nil, m, a, b, "ParticipantLookup"},
		{"nil ManifestLookup", p, nil, a, b, "ManifestLookup"},
		{"nil ABACEngine", p, m, nil, b, "ABACEngine"},
		{"nil BackpressureChecker", p, m, a, nil, "BackpressureChecker"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authguard.New(tc.p, tc.m, tc.a, tc.b)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "AUTHGUARD_DEPENDENCY_NIL")
			errutil.AssertErrorContext(t, err, "dependency", tc.wantDepField)
		})
	}
}

// errorParticipants returns an error from Participants.
type errorParticipants struct{}

func (e *errorParticipants) Participants(_ context.Context, _ codec.KeyID, _ uint32) ([]dek.Participant, error) {
	return nil, errors.New("fake: lookup failed")
}

// errorABACEngine returns an error from Evaluate.
type errorABACEngine struct{}

func (e *errorABACEngine) Evaluate(_ context.Context, _ accesstypes.AccessRequest) (accesstypes.Decision, error) {
	return accesstypes.Decision{}, errors.New("fake: abac eval failed")
}

// TestGuardCheckCharacterParticipantsLookupErrorPropagates verifies that when
// ParticipantLookup.Participants returns an error on the character branch,
// Guard.Check propagates it with AUTHGUARD_PARTICIPANTS_FAILED.
func TestGuardCheckCharacterParticipantsLookupErrorPropagates(t *testing.T) {
	p := &errorParticipants{}
	m := &fakeManifest{}
	a := policytest.AllowAllEngine()
	b := &fakeBackpressure{}
	g, err := authguard.New(p, m, a, b)
	require.NoError(t, err)

	id, err := authguard.NewCharacterIdentity("01ABC", "01XYZ", "01DEF")
	require.NoError(t, err)
	_, checkErr := g.Check(t.Context(), authguard.CheckRequest{Identity: id, KeyID: codec.KeyID(1), KeyVersion: 1})
	require.Error(t, checkErr)
	errutil.AssertErrorCode(t, checkErr, "AUTHGUARD_PARTICIPANTS_FAILED")
}

// TestGuardCheckPlayerParticipantsLookupErrorPropagates verifies that when
// ParticipantLookup.Participants returns an error on the player branch,
// Guard.Check propagates it with AUTHGUARD_PARTICIPANTS_FAILED.
func TestGuardCheckPlayerParticipantsLookupErrorPropagates(t *testing.T) {
	p := &errorParticipants{}
	m := &fakeManifest{}
	a := policytest.AllowAllEngine()
	b := &fakeBackpressure{}
	g, err := authguard.New(p, m, a, b)
	require.NoError(t, err)

	id, err := authguard.NewPlayerIdentity("01ABC")
	require.NoError(t, err)
	_, checkErr := g.Check(t.Context(), authguard.CheckRequest{Identity: id, KeyID: codec.KeyID(1), KeyVersion: 1})
	require.Error(t, checkErr)
	errutil.AssertErrorCode(t, checkErr, "AUTHGUARD_PARTICIPANTS_FAILED")
}

// TestGuardCheckPlayerABACEvalErrorPropagates verifies that when
// ABACEngine.Evaluate returns an error on the player branch after the
// participant-match succeeds, Guard.Check propagates it with
// AUTHGUARD_ABAC_EVAL_FAILED.
func TestGuardCheckPlayerABACEvalErrorPropagates(t *testing.T) {
	parts := []dek.Participant{{PlayerID: "01ABC", CharacterID: "01XYZ", BindingID: "01DEF"}}
	p := &fakeParticipants{list: parts}
	m := &fakeManifest{}
	a := &errorABACEngine{}
	b := &fakeBackpressure{}
	g, err := authguard.New(p, m, a, b)
	require.NoError(t, err)

	id, err := authguard.NewPlayerIdentity("01ABC")
	require.NoError(t, err)
	_, checkErr := g.Check(t.Context(), authguard.CheckRequest{Identity: id, KeyID: codec.KeyID(1), KeyVersion: 1})
	require.Error(t, checkErr)
	errutil.AssertErrorCode(t, checkErr, "AUTHGUARD_ABAC_EVAL_FAILED")
}

// TestGuardCheckPluginABACEvalErrorPropagates verifies that when
// ABACEngine.Evaluate returns an error on the plugin branch after the
// manifest check succeeds, Guard.Check propagates it with
// AUTHGUARD_ABAC_EVAL_FAILED.
func TestGuardCheckPluginABACEvalErrorPropagates(t *testing.T) {
	p := &fakeParticipants{}
	m := &fakeManifest{allowed: map[string]map[string]bool{"mod-filter": {"core-comm:whisper": true}}}
	a := &errorABACEngine{}
	b := &fakeBackpressure{}
	g, err := authguard.New(p, m, a, b)
	require.NoError(t, err)

	id, err := authguard.NewPluginIdentity("mod-filter", "01INST")
	require.NoError(t, err)
	_, checkErr := g.Check(t.Context(), authguard.CheckRequest{
		Identity: id, KeyID: codec.KeyID(1), KeyVersion: 1, EventType: "core-comm:whisper",
	})
	require.Error(t, checkErr)
	errutil.AssertErrorCode(t, checkErr, "AUTHGUARD_ABAC_EVAL_FAILED")
}

// TestCheckPluginReadbackPermitsWithManifestFlag verifies the read-back path
// permits on the manifest readback flag alone (gate g2). A DenyAllEngine ABAC
// proves the read-back grant carries no ABAC dependency (spec §7.5); OwnerMap
// (g1) is enforced upstream at the primitive entry.
func TestCheckPluginReadbackPermitsWithManifestFlag(t *testing.T) {
	t.Parallel()
	g, err := authguard.New(
		&fakeParticipants{},
		&fakeManifest{readback: true},
		policytest.DenyAllEngine(),
		&fakeBackpressure{},
	)
	require.NoError(t, err)

	id, err := authguard.NewPluginIdentity("core-scenes", "01INST")
	require.NoError(t, err)
	dec, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity:  id,
		EventType: "scene_pose",
		ReadBack:  true,
		KeyID:     codec.KeyID(7), KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.True(t, dec.Permit, "permit on manifest flag alone — no ABAC dependency")
	assert.Equal(t, authguard.PermitPluginReadbackGrant, dec.Code)
}

// TestCheckPluginReadbackDeniedWithoutManifestFlag verifies the read-back path
// denies when the manifest does not declare crypto.emits[].readback.
func TestCheckPluginReadbackDeniedWithoutManifestFlag(t *testing.T) {
	t.Parallel()
	g, err := authguard.New(
		&fakeParticipants{},
		&fakeManifest{readback: false},
		policytest.DenyAllEngine(),
		&fakeBackpressure{},
	)
	require.NoError(t, err)

	id, err := authguard.NewPluginIdentity("core-scenes", "01INST")
	require.NoError(t, err)
	dec, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity:  id,
		EventType: "scene_pose",
		ReadBack:  true,
		KeyID:     codec.KeyID(7), KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.False(t, dec.Permit)
	assert.Equal(t, authguard.DenyReadbackManifestMissing, dec.Code)
}

// TestCheckPluginReadbackFalseUsesLiveDeliveryGate verifies ReadBack=false
// routes to the existing live-delivery checkPlugin path, which denies when the
// manifest does not declare requests_decryption — regardless of the readback
// flag being set.
func TestCheckPluginReadbackFalseUsesLiveDeliveryGate(t *testing.T) {
	t.Parallel()
	g, err := authguard.New(
		&fakeParticipants{},
		&fakeManifest{readback: true}, // allowed nil → PluginRequestsDecryption false
		policytest.AllowAllEngine(),
		&fakeBackpressure{},
	)
	require.NoError(t, err)

	id, err := authguard.NewPluginIdentity("core-scenes", "01INST")
	require.NoError(t, err)
	dec, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity:  id,
		EventType: "scene_pose",
		ReadBack:  false,
		KeyID:     codec.KeyID(7), KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.False(t, dec.Permit, "live-delivery gate denies without requests_decryption")
	assert.Equal(t, authguard.DenyManifestDeclarationMissing, dec.Code)
}

// TestCheckPluginReadbackBackpressureDeniesEarly verifies the read-back path
// short-circuits on audit backpressure BEFORE the manifest check (gate g2). A
// fakeManifest with readback:true proves the deny comes from backpressure, not
// a missing manifest declaration — mirroring the live-delivery precedent in
// TestGuardBranchPluginBackpressureDeniesEarly.
func TestCheckPluginReadbackBackpressureDeniesEarly(t *testing.T) {
	t.Parallel()
	g, err := authguard.New(
		&fakeParticipants{},
		&fakeManifest{readback: true},
		policytest.AllowAllEngine(),
		&fakeBackpressure{throttle: true},
	)
	require.NoError(t, err)

	id, err := authguard.NewPluginIdentity("core-scenes", "01INST")
	require.NoError(t, err)
	dec, err := g.Check(t.Context(), authguard.CheckRequest{
		Identity:  id,
		EventType: "scene_pose",
		ReadBack:  true,
		KeyID:     codec.KeyID(7), KeyVersion: 1,
	})
	require.NoError(t, err)
	assert.False(t, dec.Permit)
	assert.Equal(t, authguard.DenyAuditBackpressure, dec.Code)
}
