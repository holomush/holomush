// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeHistoryClient is a stub PluginAuditServiceClient used only by the
// router tests. Only QueryHistory is exercised; the other methods return
// a canned "not implemented" error so accidental calls fail loudly.
type fakeHistoryClient struct {
	pluginv1.PluginAuditServiceClient
	stream    pluginv1.PluginAuditService_QueryHistoryClient
	returnErr error
	lastReq   *pluginv1.QueryHistoryRequest
}

func (f *fakeHistoryClient) QueryHistory(_ context.Context, req *pluginv1.QueryHistoryRequest, _ ...grpc.CallOption) (pluginv1.PluginAuditService_QueryHistoryClient, error) {
	f.lastReq = req
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return f.stream, nil
}

// fakeStream implements PluginAuditService_QueryHistoryClient using a
// preloaded slice of responses. Exhausts with io.EOF.
type fakeStream struct {
	grpc.ClientStream
	resps []*pluginv1.QueryHistoryResponse
	idx   int
}

func (s *fakeStream) Recv() (*pluginv1.QueryHistoryResponse, error) {
	if s.idx >= len(s.resps) {
		return nil, io.EOF
	}
	r := s.resps[s.idx]
	s.idx++
	return r, nil
}

func (s *fakeStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (s *fakeStream) Trailer() metadata.MD         { return metadata.MD{} }
func (s *fakeStream) CloseSend() error             { return nil }
func (s *fakeStream) Context() context.Context     { return context.Background() }
func (s *fakeStream) SendMsg(_ any) error          { return nil }
func (s *fakeStream) RecvMsg(_ any) error          { return nil }

// stubProvider resolves one plugin name to a canned client. Returns
// (nil, false) for any other name so the router surfaces the missing-
// client error path deterministically.
type stubProvider struct {
	name   string
	client pluginv1.PluginAuditServiceClient
}

func (s stubProvider) PluginAuditClient(name string) (pluginv1.PluginAuditServiceClient, bool) {
	if name == s.name && s.client != nil {
		return s.client, true
	}
	return nil, false
}

func TestPluginHistoryRouterForwardsQueryAndAdaptsStream(t *testing.T) {
	t.Parallel()

	id := core.NewULID()
	idBytes := id.Bytes()

	fs := &fakeStream{
		resps: []*pluginv1.QueryHistoryResponse{
			{Event: &eventbusv1.Event{
				Id:        idBytes[:],
				Subject:   "events.main.scene.01ABC",
				Type:      "scene.lifecycle.created",
				Timestamp: timestamppb.New(time.Unix(1, 0)),
				Payload:   []byte(`{"k":"v"}`),
			}},
		},
	}
	fc := &fakeHistoryClient{stream: fs}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core-scenes", client: fc})

	stream, err := router.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:   "events.main.scene.01ABC",
		PageSize:  50,
		Direction: eventbus.DirectionForward,
	})
	require.NoError(t, err)
	require.NotNil(t, stream)
	require.NotNil(t, fc.lastReq)
	assert.Equal(t, "events.main.scene.01ABC", fc.lastReq.GetSubject())
	assert.Equal(t, int32(1), fc.lastReq.GetDirection())

	ev, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, id, ev.ID)
	assert.Equal(t, eventbus.Subject("events.main.scene.01ABC"), ev.Subject)
	assert.Equal(t, eventbus.Type("scene.lifecycle.created"), ev.Type)

	_, err = stream.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)
	assert.NoError(t, stream.Close())
}

func TestPluginHistoryRouterReturnsErrorWhenClientMissing(t *testing.T) {
	t.Parallel()
	router := audit.NewPluginHistoryRouter(stubProvider{name: "other"})
	_, err := router.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:  "events.main.scene.01ABC",
		PageSize: 50,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_HISTORY_CLIENT_MISSING")
}

// fakeStreamErr returns a non-EOF error on every Recv call — used to cover
// the AUDIT_PLUGIN_HISTORY_RECV_FAILED branch.
type fakeStreamErr struct {
	fakeStream
	err error
}

func (s *fakeStreamErr) Recv() (*pluginv1.QueryHistoryResponse, error) {
	return nil, s.err
}

func TestPluginHistoryRouterPropagatesRPCCreateError(t *testing.T) {
	t.Parallel()
	fc := &fakeHistoryClient{returnErr: errors.New("rpc unavailable")}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core", client: fc})
	_, err := router.QueryHistory(context.Background(), "core", eventbus.HistoryQuery{
		Subject: "events.main.plugin.x", PageSize: 10,
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_HISTORY_RPC_FAILED")
}

func TestPluginHistoryRouterWrapsRecvError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("transport closed")
	fs := &fakeStreamErr{err: sentinel}
	fc := &fakeHistoryClient{stream: fs}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core", client: fc})
	stream, err := router.QueryHistory(context.Background(), "core", eventbus.HistoryQuery{
		Subject: "events.main.plugin.x", PageSize: 10,
	})
	require.NoError(t, err)
	_, err = stream.Next(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_HISTORY_RECV_FAILED")
}

func TestPluginHistoryRouterRejectsEmptyEvent(t *testing.T) {
	t.Parallel()
	fs := &fakeStream{resps: []*pluginv1.QueryHistoryResponse{
		{Event: nil},
	}}
	fc := &fakeHistoryClient{stream: fs}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core", client: fc})
	stream, err := router.QueryHistory(context.Background(), "core", eventbus.HistoryQuery{
		Subject: "events.main.plugin.x", PageSize: 10,
	})
	require.NoError(t, err)
	_, err = stream.Next(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_HISTORY_EMPTY_EVENT")
}

func TestPluginHistoryRouterAcceptsRawULIDBytes(t *testing.T) {
	t.Parallel()
	id := core.NewULID()
	raw := id.Bytes()
	fs := &fakeStream{resps: []*pluginv1.QueryHistoryResponse{
		{Event: &eventbusv1.Event{
			Id:        raw[:],
			Subject:   "events.main.plugin.x",
			Type:      "t",
			Timestamp: timestamppb.New(time.Unix(1, 0)),
			Actor: &eventbusv1.Actor{
				Kind: eventbusv1.ActorKind_ACTOR_KIND_PLAYER,
				Id:   raw[:],
			},
		}},
	}}
	fc := &fakeHistoryClient{stream: fs}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core", client: fc})
	stream, err := router.QueryHistory(context.Background(), "core", eventbus.HistoryQuery{
		Subject: "events.main.plugin.x", PageSize: 10,
	})
	require.NoError(t, err)
	ev, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, id, ev.ID)
	assert.Equal(t, eventbus.ActorKindPlayer, ev.Actor.Kind)
}

func TestPluginHistoryRouterNextRespectsContextCancel(t *testing.T) {
	t.Parallel()
	fs := &fakeStream{resps: []*pluginv1.QueryHistoryResponse{}}
	fc := &fakeHistoryClient{stream: fs}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core", client: fc})
	stream, err := router.QueryHistory(context.Background(), "core", eventbus.HistoryQuery{
		Subject: "events.main.plugin.x", PageSize: 10,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = stream.Next(ctx)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_HISTORY_CTX")
}

func TestPluginHistoryStreamCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	fs := &fakeStream{}
	fc := &fakeHistoryClient{stream: fs}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core", client: fc})
	stream, err := router.QueryHistory(context.Background(), "core", eventbus.HistoryQuery{
		Subject: "events.main.plugin.x", PageSize: 10,
	})
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	// A Next after Close returns EOF per contract.
	_, err = stream.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)
}

func TestPluginHistoryRouterForwardsCursors(t *testing.T) {
	t.Parallel()

	fc := &fakeHistoryClient{stream: &fakeStream{}}
	router := audit.NewPluginHistoryRouter(stubProvider{name: "core-scenes", client: fc})

	after := core.NewULID()
	before := core.NewULID()
	notBefore := time.Unix(100, 0)
	notAfter := time.Unix(200, 0)

	_, err := router.QueryHistory(context.Background(), "core-scenes", eventbus.HistoryQuery{
		Subject:   "events.main.scene.01ABC",
		AfterID:   after,
		BeforeID:  before,
		NotBefore: notBefore,
		NotAfter:  notAfter,
		Direction: eventbus.DirectionBackward,
		PageSize:  25,
	})
	require.NoError(t, err)
	require.NotNil(t, fc.lastReq)
	a := after.Bytes()
	b := before.Bytes()
	assert.Equal(t, a[:], fc.lastReq.GetAfter())
	assert.Equal(t, b[:], fc.lastReq.GetBefore())
	assert.Equal(t, int32(2), fc.lastReq.GetDirection())
	assert.Equal(t, int32(25), fc.lastReq.GetPageSize())
	assert.Equal(t, notBefore.Unix(), fc.lastReq.GetNotBefore().AsTime().Unix())
	assert.Equal(t, notAfter.Unix(), fc.lastReq.GetNotAfter().AsTime().Unix())
}
