// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubEngine returns a fixed decision/error and records the request.
type stubEngine struct {
	decision types.Decision
	err      error
	gotReq   types.AccessRequest
	called   bool
}

func (s *stubEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	s.called = true
	s.gotReq = req
	return s.decision, s.err
}

func (s *stubEngine) CanPerformAction(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

// recordingAuditor captures audit events.
type recordingAuditor struct{ events []audit.Event }

func (r *recordingAuditor) Log(_ context.Context, e audit.Event) error {
	r.events = append(r.events, e)
	return nil
}

func TestEvaluate_AllowEmitsAuditAndReturnsDecision(t *testing.T) {
	eng := &stubEngine{decision: types.NewDecision(types.EffectAllow, "permitted by p", "p")}
	aud := &recordingAuditor{}

	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine:     eng,
		Auditor:    aud,
		PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC",
		Action:     "extend_publish_attempts",
		Resource:   "scene:01SCENE",
	})

	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.Equal(t, "p", dec.MatchedPolicy)
	assert.Equal(t, "character:01ABC", eng.gotReq.Subject)
	assert.Equal(t, "extend_publish_attempts", eng.gotReq.Action)
	assert.Equal(t, "scene:01SCENE", eng.gotReq.Resource)
	require.Len(t, aud.events, 1)
	assert.Equal(t, "character:01ABC", aud.events[0].Subject)
	assert.Equal(t, audit.SourcePlugin, aud.events[0].Source)
	assert.Equal(t, "core-scenes", aud.events[0].Component)
	assert.Equal(t, types.EffectAllow, aud.events[0].Effect)
}

func TestEvaluate_EntitlementRejectsForeignType(t *testing.T) {
	eng := &stubEngine{decision: types.NewDecision(types.EffectAllow, "", "p")}
	aud := &recordingAuditor{}

	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: aud, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "read", Resource: "server:global",
	})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVALUATE_UNENTITLED_TYPE")
	assert.False(t, dec.Allowed)
	assert.False(t, eng.called, "engine MUST NOT be consulted for an unentitled resource type")
}

func TestEvaluate_CommandCarveOutAllowed(t *testing.T) {
	eng := &stubEngine{decision: types.NewDecision(types.EffectAllow, "", "p")}
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: &recordingAuditor{}, PluginName: "lua-plug",
		OwnedTypes: map[string]bool{}, // Lua: empty
		Subject:    "character:01ABC", Action: "execute", Resource: "command:foo",
	})
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.True(t, eng.called)
}

func TestEvaluate_EmptyActionRejected(t *testing.T) {
	_, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: &stubEngine{}, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "", Resource: "scene:01SCENE",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVALUATE_EMPTY_ACTION")
}

func TestEvaluate_MalformedResourceRejected(t *testing.T) {
	for _, res := range []string{"noseparator", ":noid", "notype:", "", "a:b:c", "type:id:extra"} {
		_, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
			Engine: &stubEngine{}, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
			OwnedTypes: map[string]bool{"scene": true},
			Subject:    "character:01ABC", Action: "read", Resource: res,
		})
		require.Errorf(t, err, "resource %q must be rejected", res)
		errutil.AssertErrorCode(t, err, "EVALUATE_BAD_RESOURCE")
	}
}

func TestEvaluate_EmptySubjectFailsClosed(t *testing.T) {
	eng := &stubEngine{}
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "", Action: "read", Resource: "scene:01SCENE",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVALUATE_NO_SUBJECT")
	assert.False(t, dec.Allowed)
	assert.False(t, eng.called, "no authenticated subject MUST fail closed before the engine")
}

func TestEvaluate_EngineErrorFailsClosed(t *testing.T) {
	eng := &stubEngine{err: assertAnErr()}
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "read", Resource: "scene:01SCENE",
	})
	require.Error(t, err)
	assert.False(t, dec.Allowed, "engine error MUST NOT fail open")
}

func TestEvaluate_DefaultDenyOnUnmatchedPolicy(t *testing.T) {
	eng := &stubEngine{decision: types.NewDecision(types.EffectDefaultDeny, "no match", "")}
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: eng, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "read", Resource: "scene:01SCENE",
	})
	require.NoError(t, err)
	assert.False(t, dec.Allowed)
}

func TestEvaluate_NilEngineFailsClosed(t *testing.T) {
	dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
		Engine: nil, Auditor: &recordingAuditor{}, PluginName: "core-scenes",
		OwnedTypes: map[string]bool{"scene": true},
		Subject:    "character:01ABC", Action: "read", Resource: "scene:01SCENE",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVALUATE_NO_ENGINE")
	assert.False(t, dec.Allowed)
}

func TestActorSubject(t *testing.T) {
	tests := []struct {
		name  string
		actor core.Actor
		want  string
	}{
		{"character", core.Actor{Kind: core.ActorCharacter, ID: "01ABC"}, "character:01ABC"},
		{"plugin", core.Actor{Kind: core.ActorPlugin, ID: "core-scenes"}, "plugin:core-scenes"},
		{"system", core.Actor{Kind: core.ActorSystem, ID: "whatever"}, "system"},
		{"unknown", core.Actor{Kind: core.ActorKind(99), ID: "anything"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pluginauthz.ActorSubject(tt.actor))
		})
	}
}

func assertAnErr() error { return context.DeadlineExceeded }
