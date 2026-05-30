// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestHostCommandListerNilClientFailsClosed(t *testing.T) {
	c := &hostCommandClient{client: nil}
	_, err := c.ListCommands(context.Background(), "01HCHAR0000000000000000AAA")
	require.Error(t, err)
}

// stubCommandHostClient embeds the generated client interface (nil) and overrides
// only ListCommands, so unrelated methods are never called by these tests.
type stubCommandHostClient struct {
	pluginv1.PluginHostServiceClient
	resp *pluginv1.PluginHostServiceListCommandsResponse
	err  error
}

func (s *stubCommandHostClient) ListCommands(_ context.Context, _ *pluginv1.PluginHostServiceListCommandsRequest, _ ...grpc.CallOption) (*pluginv1.PluginHostServiceListCommandsResponse, error) {
	return s.resp, s.err
}

func TestHostCommandListerMapsResponse(t *testing.T) {
	stub := &stubCommandHostClient{resp: &pluginv1.PluginHostServiceListCommandsResponse{
		Commands: []*pluginv1.PluginHostServiceCommandInfo{
			{Name: "look", Help: "look around", Usage: "look", Source: "core"},
		},
		Incomplete: true,
	}}
	c := &hostCommandClient{client: stub}
	got, err := c.ListCommands(context.Background(), "01HCHAR0000000000000000AAA")
	require.NoError(t, err)
	require.Len(t, got.Commands, 1)
	assert.Equal(t, "look", got.Commands[0].Name)
	assert.Equal(t, "look around", got.Commands[0].Help)
	assert.Equal(t, "look", got.Commands[0].Usage)
	assert.Equal(t, "core", got.Commands[0].Source)
	assert.True(t, got.Incomplete)
}
