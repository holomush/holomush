// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// streamSubProvider implements StreamSubscriptionAware.
type streamSubProvider struct {
	ServiceProvider
	got StreamSubscription
}

func (p *streamSubProvider) SetStreamSubscription(s StreamSubscription) { p.got = s }

// Verifies: INV-PLUGIN-54
func TestStreamSubscriptionAwareFailsClosedWhenUndeclared(t *testing.T) {
	err := validateDeclaredCapabilities(&streamSubProvider{}, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CAPABILITY_NOT_DECLARED")
	assert.Contains(t, err.Error(), "stream.subscription")
}

// Verifies: INV-PLUGIN-54
func TestStreamSubscriptionAwarePassesWhenDeclared(t *testing.T) {
	err := validateDeclaredCapabilities(&streamSubProvider{}, []string{"stream.subscription"})
	require.NoError(t, err)
}

// fakeStreamSubClient records the requests it receives.
type fakeStreamSubClient struct {
	addReq *hostv1.AddSessionStreamRequest
	rmReq  *hostv1.RemoveSessionStreamRequest
}

func (f *fakeStreamSubClient) AddSessionStream(_ context.Context, in *hostv1.AddSessionStreamRequest, _ ...grpc.CallOption) (*hostv1.AddSessionStreamResponse, error) {
	f.addReq = in
	return &hostv1.AddSessionStreamResponse{}, nil
}

func (f *fakeStreamSubClient) RemoveSessionStream(_ context.Context, in *hostv1.RemoveSessionStreamRequest, _ ...grpc.CallOption) (*hostv1.RemoveSessionStreamResponse, error) {
	f.rmReq = in
	return &hostv1.RemoveSessionStreamResponse{}, nil
}

func TestStreamSubscriptionClientAddStreamMapsLiveOnly(t *testing.T) {
	fake := &fakeStreamSubClient{}
	c := &streamSubscriptionClient{client: fake}

	err := c.AddStream(context.Background(), "sess-1", "channel.c1", ReplayModeLiveOnly)
	require.NoError(t, err)
	require.NotNil(t, fake.addReq)
	assert.Equal(t, "sess-1", fake.addReq.GetSessionId())
	assert.Equal(t, "channel.c1", fake.addReq.GetStream())
	assert.Equal(t, hostv1.StreamReplayMode_STREAM_REPLAY_MODE_LIVE_ONLY, fake.addReq.GetReplayMode())
}

func TestStreamSubscriptionClientAddStreamMapsFromCursor(t *testing.T) {
	fake := &fakeStreamSubClient{}
	c := &streamSubscriptionClient{client: fake}

	err := c.AddStream(context.Background(), "sess-1", "channel.c1", ReplayModeFromCursor)
	require.NoError(t, err)
	assert.Equal(t, hostv1.StreamReplayMode_STREAM_REPLAY_MODE_FROM_CURSOR, fake.addReq.GetReplayMode())
}

func TestStreamSubscriptionClientRemoveStreamForwards(t *testing.T) {
	fake := &fakeStreamSubClient{}
	c := &streamSubscriptionClient{client: fake}

	err := c.RemoveStream(context.Background(), "sess-1", "channel.c1")
	require.NoError(t, err)
	require.NotNil(t, fake.rmReq)
	assert.Equal(t, "sess-1", fake.rmReq.GetSessionId())
	assert.Equal(t, "channel.c1", fake.rmReq.GetStream())
}

func TestStreamSubscriptionClientNilFailsClosed(t *testing.T) {
	c := &streamSubscriptionClient{}
	require.Error(t, c.AddStream(context.Background(), "s", "channel.c1", ReplayModeLiveOnly))
	require.Error(t, c.RemoveStream(context.Background(), "s", "channel.c1"))
}
