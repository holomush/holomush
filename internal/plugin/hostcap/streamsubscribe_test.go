// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/session"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// recordingStreamReg records the registry calls a served stream.subscription RPC
// makes, so a test can assert a denied request never mutates the registry.
type recordingStreamReg struct {
	addCalled    bool
	removeCalled bool
	gotStream    string
	gotMode      session.ReplayMode
}

func (r *recordingStreamReg) AddStream(_ context.Context, _, stream string) error {
	r.addCalled = true
	r.gotStream = stream
	r.gotMode = session.ReplayModeFromCursor
	return nil
}

func (r *recordingStreamReg) AddStreamWithMode(_ context.Context, _, stream string, mode session.ReplayMode) error {
	r.addCalled = true
	r.gotStream = stream
	r.gotMode = mode
	return nil
}

func (r *recordingStreamReg) RemoveStream(_ context.Context, _, stream string) error {
	r.removeCalled = true
	r.gotStream = stream
	return nil
}

// subscribeHostCaps is a focused HostCapabilities stub for streamSubscriptionServer
// tests: configurable ABAC engine, game ID, stream registry, and owned emit
// domains. Everything else inherits stubHostCaps' nil accessors.
type subscribeHostCaps struct {
	stubHostCaps
	engine types.AccessPolicyEngine
	gameID string
	reg    plugins.StreamRegistry
	owned  []string
}

func (c subscribeHostCaps) AccessEngine() types.AccessPolicyEngine { return c.engine }
func (c subscribeHostCaps) GameID() string                         { return c.gameID }
func (c subscribeHostCaps) StreamRegistry() plugins.StreamRegistry { return c.reg }
func (c subscribeHostCaps) OwnedEmitDomains(string) []string       { return c.owned }

func newSubscribeServer(engine types.AccessPolicyEngine, reg plugins.StreamRegistry) hostv1.StreamSubscriptionServiceServer {
	return hostcap.NewStreamSubscriptionServer(hostcap.NewBase(subscribeHostCaps{
		engine: engine, gameID: "main", reg: reg, owned: []string{"channel"},
	}, "core-channels"))
}

// TestAddSessionStreamPermitsOwnRelativeStream proves an own-domain relative ref
// is authorized (with the broad write permit) and forwarded to the registry with
// the LIVE_ONLY mode mapped from the proto enum (R2-A + HIGH-2).
func TestAddSessionStreamPermitsOwnRelativeStream(t *testing.T) {
	reg := &recordingStreamReg{}
	srv := newSubscribeServer(policytest.AllowAllEngine(), reg)

	_, err := srv.AddSessionStream(context.Background(), &hostv1.AddSessionStreamRequest{
		SessionId:  "sess-1",
		Stream:     "channel.01CHAN0000000000000000000",
		ReplayMode: hostv1.StreamReplayMode_STREAM_REPLAY_MODE_LIVE_ONLY,
	})
	require.NoError(t, err)
	assert.True(t, reg.addCalled)
	assert.Equal(t, "channel.01CHAN0000000000000000000", reg.gotStream,
		"the relative ref is forwarded as-is (the ctrl path qualifies it)")
	assert.Equal(t, session.ReplayModeLiveOnly, reg.gotMode)
}

// TestAddSessionStreamRejectsInHandlerEvenWithBroadWritePermit proves the R2-B /
// R3-A in-handler fence denies forbidden/foreign/wildcard/pre-qualified inputs
// BEFORE the registry is touched, EVEN WITH the broad seed:plugin-stream-subscribe
// write permit active (AllowAllEngine). The in-handler fence, not the read-only
// forbids, is the load-bearing control.
func TestAddSessionStreamRejectsInHandlerEvenWithBroadWritePermit(t *testing.T) {
	tests := []struct {
		name, stream string
		wantCode     codes.Code
	}{
		{"system namespace", "system.rekey.ct.cid", codes.PermissionDenied},
		{"audit namespace", "audit.log.x", codes.PermissionDenied},
		{"crypto namespace", "crypto.policy.x", codes.PermissionDenied},
		{"foreign domain", "scene.01SCENE00000000000000000000", codes.PermissionDenied},
		{"wildcard", "channel.>", codes.InvalidArgument},
		{"pre-qualified subject", "events.main.channel.c1", codes.InvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := &recordingStreamReg{}
			srv := newSubscribeServer(policytest.AllowAllEngine(), reg) // engine would permit
			_, err := srv.AddSessionStream(context.Background(), &hostv1.AddSessionStreamRequest{
				SessionId: "sess-1", Stream: tc.stream,
			})
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, status.Code(err))
			assert.False(t, reg.addCalled, "registry must not be mutated on a denied subscribe")
		})
	}
}

// TestAddSessionStreamDeniedByPolicyDoesNotMutate proves a policy denial (engine
// deny on the qualified stream) fails closed before the registry.
func TestAddSessionStreamDeniedByPolicyDoesNotMutate(t *testing.T) {
	reg := &recordingStreamReg{}
	srv := newSubscribeServer(policytest.DenyAllEngine(), reg)
	_, err := srv.AddSessionStream(context.Background(), &hostv1.AddSessionStreamRequest{
		SessionId: "sess-1", Stream: "channel.c1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, reg.addCalled)
}

// TestRemoveSessionStreamRunsGuardThenForwards proves RemoveSessionStream applies
// the same guard and forwards the relative ref on success.
func TestRemoveSessionStreamRunsGuardThenForwards(t *testing.T) {
	reg := &recordingStreamReg{}
	srv := newSubscribeServer(policytest.AllowAllEngine(), reg)

	_, err := srv.RemoveSessionStream(context.Background(), &hostv1.RemoveSessionStreamRequest{
		SessionId: "sess-1", Stream: "channel.c1",
	})
	require.NoError(t, err)
	assert.True(t, reg.removeCalled)
	assert.Equal(t, "channel.c1", reg.gotStream)

	// A forbidden ref is rejected before the registry.
	reg2 := &recordingStreamReg{}
	srv2 := newSubscribeServer(policytest.AllowAllEngine(), reg2)
	_, err = srv2.RemoveSessionStream(context.Background(), &hostv1.RemoveSessionStreamRequest{
		SessionId: "sess-1", Stream: "system.rekey.ct.cid",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, reg2.removeCalled)
}
