// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type evalTestServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	gotAction, gotResource string
	allow                  bool
}

func (s *evalTestServer) Evaluate(_ context.Context, req *pluginv1.PluginHostServiceEvaluateRequest) (*pluginv1.PluginHostServiceEvaluateResponse, error) {
	s.gotAction = req.GetAction()
	s.gotResource = req.GetResource()
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
