// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// recordingEngine captures the AccessRequest it is handed and returns a fixed
// decision, so a test can assert what resource AuthorizeStreamRead evaluates.
type recordingEngine struct {
	gotResource string
	allow       bool
}

func (e *recordingEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	e.gotResource = req.Resource
	if e.allow {
		return types.NewDecision(types.EffectAllow, "test", "test:permit"), nil
	}
	return types.NewDecision(types.EffectDefaultDeny, "test", ""), nil
}

func (e *recordingEngine) CanPerformAction(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

// Verifies: INV-PLUGIN-50
func TestAuthorizeStreamReadQualifiesRelativeStreamBeforeEvaluating(t *testing.T) {
	// The plugin sends a DOMAIN-RELATIVE stream reference; the gate MUST qualify it
	// (events.<gameID>.<rel>) before evaluating, so the system/audit/crypto forbids
	// (keyed on the qualified resource.stream.name) can match. This is the bug
	// holomush-xakba fixes: evaluating the un-qualified form let system reads slip
	// past the forbid.
	eng := &recordingEngine{allow: true}
	dec, err := pluginauthz.AuthorizeStreamRead(context.Background(), pluginauthz.StreamReadInput{
		Engine:     eng,
		PluginName: "p",
		Subject:    access.PluginSubject("p"),
		GameID:     "main",
		Stream:     "system.rekey.01CT000.01CID00", // domain-relative
	})
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.Equal(t, "stream:events.main.system.rekey.01CT000.01CID00", eng.gotResource,
		"the ABAC resource must be the QUALIFIED stream so forbids can match")
}

// Verifies: INV-PLUGIN-50
func TestAuthorizeStreamReadRejectsBeforeEngine(t *testing.T) {
	// Inputs that must fail closed BEFORE the engine is consulted (the engine's
	// gotResource must stay empty):
	//   - unqualifiable refs (no gameID) → STREAM_QUALIFY_FAILED;
	//   - wildcard subjects ('>' / '*') → a read-across-all-streams the
	//     concrete-name system/audit/crypto forbids cannot match (fw118.4);
	//   - pre-qualified ("events."-prefixed) refs → would skip host-gameID scoping
	//     and allow cross-game reads (fw118.4 residual).
	tests := []struct {
		name, gameID, stream string
	}{
		{"unqualifiable: no game id", "", "system.rekey.x"},
		{"wildcard: bare >", "main", ">"},
		{"wildcard: trailing .>", "main", "location.>"},
		{"wildcard: * token", "main", "scene.*.ic"},
		{"wildcard: pre-qualified .>", "main", "events.main.>"},
		{"wildcard: cross-game .>", "main", "events.other.>"},
		{"pre-qualified: own game concrete", "main", "events.main.location.01LOCAAAAAAAAAAAAAAAAAA"},
		{"pre-qualified: cross-game concrete", "main", "events.other.location.01LOCAAAAAAAAAAAAAAAAAA"},
		{"pre-qualified: system", "main", "events.main.system.rekey.01CT000.01CID00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng := &recordingEngine{allow: true}
			_, err := pluginauthz.AuthorizeStreamRead(context.Background(), pluginauthz.StreamReadInput{
				Engine:     eng,
				PluginName: "p",
				Subject:    access.PluginSubject("p"),
				GameID:     tc.gameID,
				Stream:     tc.stream,
			})
			require.Error(t, err, "%q must be rejected", tc.stream)
			assert.Empty(t, eng.gotResource, "must be rejected before the engine is consulted")
		})
	}
}

// Verifies: INV-PLUGIN-50
func TestAuthorizeStreamReadDeniesWhenPolicyDenies(t *testing.T) {
	eng := &recordingEngine{allow: false}
	dec, err := pluginauthz.AuthorizeStreamRead(context.Background(), pluginauthz.StreamReadInput{
		Engine:     eng,
		PluginName: "p",
		Subject:    access.PluginSubject("p"),
		GameID:     "main",
		Stream:     "system.rekey.x", // domain-relative
	})
	require.NoError(t, err)
	assert.False(t, dec.Allowed)
}
