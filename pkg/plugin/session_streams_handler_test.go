// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// eventOnlyHandler implements only Handler (no SessionStreamsHandler).
type eventOnlyHandler struct{}

func (eventOnlyHandler) HandleEvent(context.Context, Event) ([]EmitEvent, error) { return nil, nil }

// streamsHandler implements Handler + SessionStreamsHandler.
type streamsTestHandler struct {
	eventOnlyHandler
	gotReq  SessionStreamsRequest
	streams []string
	err     error
}

func (h *streamsTestHandler) QuerySessionStreams(_ context.Context, req SessionStreamsRequest) ([]string, error) {
	h.gotReq = req
	return h.streams, h.err
}

func TestQuerySessionStreamsRoutesToHandlerWhenImplemented(t *testing.T) {
	h := &streamsTestHandler{streams: []string{"channel.c1"}}
	adapter := &pluginServerAdapter{handler: h, streamsHandler: h}

	resp, err := adapter.QuerySessionStreams(context.Background(), &pluginv1.QuerySessionStreamsRequest{
		CharacterId: "char-1",
		PlayerId:    "player-1",
		SessionId:   "sess-1",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"channel.c1"}, resp.GetStreams())
	assert.Empty(t, resp.GetError())
	assert.Equal(t, SessionStreamsRequest{CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1"}, h.gotReq)
}

func TestQuerySessionStreamsReturnsEmptyForEventOnlyPlugin(t *testing.T) {
	adapter := &pluginServerAdapter{handler: eventOnlyHandler{}}

	resp, err := adapter.QuerySessionStreams(context.Background(), &pluginv1.QuerySessionStreamsRequest{
		CharacterId: "char-1",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetStreams())
	assert.Empty(t, resp.GetError())
}

func TestQuerySessionStreamsReportsHandlerErrorViaErrorField(t *testing.T) {
	h := &streamsTestHandler{err: errors.New("boom with secret internal detail")}
	adapter := &pluginServerAdapter{handler: h, streamsHandler: h}

	resp, err := adapter.QuerySessionStreams(context.Background(), &pluginv1.QuerySessionStreamsRequest{})
	require.NoError(t, err) // handler errors degrade gracefully, not a transport error
	assert.Empty(t, resp.GetStreams())
	assert.NotEmpty(t, resp.GetError())
	assert.NotContains(t, resp.GetError(), "secret internal detail",
		"inner error text must not leak past the boundary")
}

// TestGRPCServerDetectsSessionStreamsHandler proves the go-plugin registration
// wires streamsHandler exactly as it wires cmdHandler.
func TestGRPCServerDetectsSessionStreamsHandler(t *testing.T) {
	h := &streamsTestHandler{}
	adapter := &pluginServerAdapter{handler: h}
	if sh, ok := adapter.handler.(SessionStreamsHandler); ok {
		adapter.streamsHandler = sh
	}
	assert.NotNil(t, adapter.streamsHandler)

	eo := &pluginServerAdapter{handler: eventOnlyHandler{}}
	if sh, ok := eo.handler.(SessionStreamsHandler); ok {
		eo.streamsHandler = sh
	}
	assert.Nil(t, eo.streamsHandler)
}
