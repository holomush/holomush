// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type evalTestServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	gotAction, gotResource string
	gotToken               string
	allow                  bool
}

func (s *evalTestServer) Evaluate(ctx context.Context, req *pluginv1.PluginHostServiceEvaluateRequest) (*pluginv1.PluginHostServiceEvaluateResponse, error) {
	s.gotAction = req.GetAction()
	s.gotResource = req.GetResource()
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if tokens := md.Get(emitTokenHeader); len(tokens) > 0 {
			s.gotToken = tokens[0]
		}
	}
	return &pluginv1.PluginHostServiceEvaluateResponse{Allowed: s.allow, Reason: "ok", MatchedPolicy: "p1"}, nil
}

func TestHostEvaluateForwardsAndReturnsDecision(t *testing.T) {
	srv := &evalTestServer{allow: true}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &hostEvaluateClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	dec, err := client.Evaluate(context.Background(), "extend_publish_attempts", "scene:01SCENE")
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.Equal(t, "ok", dec.Reason)
	assert.Equal(t, "p1", dec.MatchedPolicy)
	assert.Equal(t, "extend_publish_attempts", srv.gotAction)
	assert.Equal(t, "scene:01SCENE", srv.gotResource)
}

func TestHostEvaluateNilClientFailsClosed(t *testing.T) {
	client := &hostEvaluateClient{}
	dec, err := client.Evaluate(context.Background(), "read", "scene:01SCENE")
	require.Error(t, err)
	assert.False(t, dec.Allowed)
}

// TestHostEvaluateFerriesDispatchToken asserts the client copies the host-issued
// per-dispatch token from the incoming command context to the outgoing Evaluate
// RPC, so the host can recover the subject (mirrors EmitEvent). Without this the
// host rejects with EMIT_TOKEN_MISSING.
func TestHostEvaluateFerriesDispatchToken(t *testing.T) {
	srv := &evalTestServer{allow: true}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &hostEvaluateClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	// Simulate the host-dispatched command context carrying the token.
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(emitTokenHeader, "dispatch-token-abc"),
	)

	dec, err := client.Evaluate(ctx, "pause", "scene:01SCENE")
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
	assert.Equal(t, "dispatch-token-abc", srv.gotToken,
		"Evaluate MUST ferry the dispatch token from the incoming command context to the host")
}
