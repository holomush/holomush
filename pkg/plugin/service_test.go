// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

// --- compile-time interface checks ---

func TestServiceProvider_InterfaceSatisfied(_ *testing.T) {
	var _ ServiceProvider = (*testServiceProvider)(nil)
}

// --- ServeWithServices validation ---

func TestServeWithServices_NilConfigPanics(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic for nil config")
		assert.Equal(t, "plugin: config cannot be nil", r)
	}()
	ServeWithServices(nil, &testServiceProvider{})
}

func TestServeWithServices_NilHandlerPanics(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic for nil handler")
		assert.Equal(t, "plugin: config.Handler cannot be nil", r)
	}()
	ServeWithServices(&ServeConfig{Handler: nil}, &testServiceProvider{})
}

func TestServeWithServices_NilProviderPanics(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic for nil provider")
		assert.Equal(t, "plugin: provider cannot be nil", r)
	}()
	ServeWithServices(&ServeConfig{Handler: &adapterTestHandler{}}, nil)
}

// --- grpcServicePlugin ---

func TestGRPCServicePlugin_GRPCServer_RegistersBothServices(t *testing.T) {
	handler := &adapterTestHandler{}
	provider := &testServiceProvider{}
	p := &grpcServicePlugin{handler: handler, provider: provider}

	s := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- unit test server
	defer s.Stop()

	err := p.GRPCServer(nil, s)
	require.NoError(t, err)

	info := s.GetServiceInfo()
	assert.Contains(t, info, "holomush.plugin.v1.PluginService",
		"expected PluginService to be registered")
	assert.True(t, provider.registerCalled,
		"expected RegisterServices to be called on provider")
}

func TestGRPCServicePlugin_GRPCServer_NilHandlerReturnsError(t *testing.T) {
	p := &grpcServicePlugin{handler: nil, provider: &testServiceProvider{}}

	s := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- unit test server
	defer s.Stop()

	err := p.GRPCServer(nil, s)
	require.Error(t, err)
	assert.Equal(t, "plugin: handler is nil", err.Error())
}

func TestGRPCServicePlugin_GRPCServer_DetectsCommandHandler(t *testing.T) {
	handler := &testFullAdapterHandler{}
	provider := &testServiceProvider{}
	p := &grpcServicePlugin{handler: handler, provider: provider}

	s := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- unit test server
	defer s.Stop()

	err := p.GRPCServer(nil, s)
	require.NoError(t, err)

	info := s.GetServiceInfo()
	assert.Contains(t, info, "holomush.plugin.v1.PluginService",
		"expected PluginService to be registered when handler implements CommandHandler")
	assert.True(t, provider.registerCalled,
		"expected RegisterServices to be called on provider")
}

func TestGRPCServicePlugin_GRPCClient_ReturnsError(t *testing.T) {
	p := &grpcServicePlugin{}
	client, err := p.GRPCClient(context.Background(), nil, nil)
	require.Error(t, err)
	assert.Nil(t, client)
}

// --- Init RPC on pluginServerAdapter ---

func TestPluginServerAdapter_Init_WithoutProvider(t *testing.T) {
	adapter := &pluginServerAdapter{handler: &adapterTestHandler{}}

	resp, err := adapter.Init(context.Background(), &pluginv1.InitRequest{
		Config: &pluginv1.ServiceConfig{ConnectionString: "postgres://localhost/test"},
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.GetProvidedServices())
}

func TestPluginServerAdapter_Init_DelegatesToProvider(t *testing.T) {
	provider := &testServiceProvider{}
	adapter := &pluginServerAdapter{
		handler:         &adapterTestHandler{},
		serviceProvider: provider,
	}

	cfg := &pluginv1.ServiceConfig{
		ConnectionString: "postgres://localhost/db",
		RequiredServices: map[string]string{"scene": "localhost:9090"},
	}

	resp, err := adapter.Init(context.Background(), &pluginv1.InitRequest{Config: cfg})
	require.NoError(t, err)
	assert.NotNil(t, resp)

	require.NotNil(t, provider.initConfig, "expected provider.Init to be called")
	assert.Equal(t, "postgres://localhost/db", provider.initConfig.GetConnectionString())
	assert.Equal(t, "localhost:9090", provider.initConfig.GetRequiredServices()["scene"])
}

var errInitFailed = errors.New("init failed")

func TestPluginServerAdapter_Init_ProviderErrorPropagates(t *testing.T) {
	provider := &testServiceProvider{initErr: errInitFailed}
	adapter := &pluginServerAdapter{
		handler:         &adapterTestHandler{},
		serviceProvider: provider,
	}

	_, err := adapter.Init(context.Background(), &pluginv1.InitRequest{
		Config: &pluginv1.ServiceConfig{},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, errInitFailed)
}

func TestPluginServerAdapter_Init_NilConfig(t *testing.T) {
	provider := &testServiceProvider{}
	adapter := &pluginServerAdapter{
		handler:         &adapterTestHandler{},
		serviceProvider: provider,
	}

	resp, err := adapter.Init(context.Background(), &pluginv1.InitRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)

	// Provider.Init should still be called; ServiceConfig will be nil (proto default)
	assert.True(t, provider.initCalled)
}

func TestPluginServerAdapterInitInjectsBrokerBackedEventSinkIntoServiceProvider(t *testing.T) {
	hostService := &testPluginHostServiceServer{}
	hostConn := startPluginHostServiceTestServer(t, hostService)

	provider := &eventSinkInitProvider{}
	adapter := &pluginServerAdapter{
		handler:         &adapterTestHandler{},
		serviceProvider: provider,
		brokerDialer: &testBrokerDialer{
			conns: map[uint32]*grpc.ClientConn{7: hostConn},
		},
	}

	_, err := adapter.Init(context.Background(), &pluginv1.InitRequest{
		Config: &pluginv1.ServiceConfig{
			RequiredServices: map[string]string{
				PluginHostServiceName: "broker:7",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, provider.initCalled, "expected provider.Init to run")
	require.Len(t, hostService.requests, 1)
	assert.Equal(t, "scene:01SCENE", hostService.requests[0].GetStream())
	assert.Equal(t, string(EventTypeSystem), hostService.requests[0].GetEventType())
	assert.Equal(t, []byte(`{"kind":"init"}`), hostService.requests[0].GetPayload())
}

func TestPluginServerAdapterHandleCommandRestoresTrustedIncomingActor(t *testing.T) {
	handler := &actorAwareCommandHandler{}
	adapter := &pluginServerAdapter{
		handler:    handler,
		cmdHandler: handler,
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		actorKindHeader, "0",
		actorIDHeader, "char-alice",
	))
	_, err := adapter.HandleCommand(ctx, &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{Command: "scene"},
	})
	require.NoError(t, err)
	require.True(t, handler.sawActor)
	assert.Equal(t, ActorCharacter, handler.kind)
	assert.Equal(t, "char-alice", handler.id)
}

func TestEventSinkEmitForwardsTrustedActorMetadata(t *testing.T) {
	hostService := &testPluginHostServiceServer{}
	hostConn := startPluginHostServiceTestServer(t, hostService)

	sink, err := newEventSinkFromBroker(&testBrokerDialer{
		conns: map[uint32]*grpc.ClientConn{7: hostConn},
	}, map[string]string{
		PluginHostServiceName: "broker:7",
	})
	require.NoError(t, err)

	emitCtx := context.WithValue(context.Background(), actorMetadataContextKey{}, actorMetadata{
		kind: ActorCharacter,
		id:   "char-alice",
	})
	err = sink.Emit(emitCtx, EmitIntent{
		Subject: "scene:01SCENE",
		Type:    EventTypeSystem,
		Payload: `{"kind":"created"}`,
	})
	require.NoError(t, err)
	require.Len(t, hostService.requests, 1)
	require.True(t, hostService.sawActor)
	assert.Equal(t, ActorCharacter, hostService.actorKind)
	assert.Equal(t, "char-alice", hostService.actorID)
	assert.Equal(t, []byte(`{"kind":"created"}`), hostService.requests[0].GetPayload())
}

func TestEventSinkEmitReturnsErrorWhenClientIsMissing(t *testing.T) {
	sink := &pluginHostEventSink{}

	err := sink.Emit(context.Background(), EmitIntent{
		Subject: "scene:01SCENE",
		Type:    EventTypeSystem,
		Payload: `{"kind":"created"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestNewEventSinkFromBrokerReturnsErrorWhenBrokerIsMissing(t *testing.T) {
	sink, err := newEventSinkFromBroker(nil, map[string]string{
		PluginHostServiceName: "broker:7",
	})
	require.Error(t, err)
	assert.Nil(t, sink)
	assert.Contains(t, err.Error(), "broker is not configured")
}

func TestNewEventSinkFromBrokerReturnsErrorWhenPluginHostServiceIsMissing(t *testing.T) {
	sink, err := newEventSinkFromBroker(&testBrokerDialer{}, map[string]string{})
	require.Error(t, err)
	assert.Nil(t, sink)
	assert.Contains(t, err.Error(), PluginHostServiceName)
}

func TestNewEventSinkFromBrokerReturnsErrorWhenDialFails(t *testing.T) {
	sink, err := newEventSinkFromBroker(&testBrokerDialer{}, map[string]string{
		PluginHostServiceName: "broker:7",
	})
	require.Error(t, err)
	assert.Nil(t, sink)
	assert.Contains(t, err.Error(), "unknown broker id")
}

func TestPluginServerAdapterInitRoutesServiceOriginatedEmitThroughSharedEmitter(t *testing.T) {
	service := &emittingPluginHostServiceServer{
		pluginName: "core-scenes",
	}
	hostConn := startPluginHostServiceTestServer(t, service)

	provider := &eventSinkInitProvider{}
	adapter := &pluginServerAdapter{
		handler:         &adapterTestHandler{},
		serviceProvider: provider,
		brokerDialer: &testBrokerDialer{
			conns: map[uint32]*grpc.ClientConn{7: hostConn},
		},
	}

	ctx := context.WithValue(context.Background(), actorMetadataContextKey{}, actorMetadata{
		kind: ActorCharacter,
		id:   "char-alice",
	})
	_, err := adapter.Init(ctx, &pluginv1.InitRequest{
		Config: &pluginv1.ServiceConfig{
			RequiredServices: map[string]string{
				PluginHostServiceName: "broker:7",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, provider.initCalled, "expected provider.Init to run")

	require.Len(t, service.requests, 1)
	assert.Equal(t, "scene:01SCENE", service.requests[0].GetStream())
	assert.Equal(t, ActorCharacter, service.actorKind)
	assert.Equal(t, "char-alice", service.actorID)
	assert.Equal(t, []byte(`{"kind":"init"}`), service.requests[0].GetPayload())
}

// --- test doubles ---

type testServiceProvider struct {
	registerCalled bool
	initCalled     bool
	initConfig     *pluginv1.ServiceConfig
	initErr        error
}

func (p *testServiceProvider) RegisterServices(_ grpc.ServiceRegistrar) {
	p.registerCalled = true
}

func (p *testServiceProvider) Init(_ context.Context, config *pluginv1.ServiceConfig) error {
	p.initCalled = true
	p.initConfig = config
	if p.initErr != nil {
		return p.initErr
	}
	return nil
}

type eventSinkInitProvider struct {
	initCalled bool
	sink       EventSink
}

func (p *eventSinkInitProvider) RegisterServices(_ grpc.ServiceRegistrar) {}

func (p *eventSinkInitProvider) Init(ctx context.Context, _ *pluginv1.ServiceConfig) error {
	p.initCalled = true
	if p.sink == nil {
		return errors.New("event sink not injected")
	}
	return p.sink.Emit(ctx, EmitIntent{
		Subject: "scene:01SCENE",
		Type:    EventTypeSystem,
		Payload: `{"kind":"init"}`,
	})
}

func (p *eventSinkInitProvider) SetEventSink(sink EventSink) {
	p.sink = sink
}

type actorAwareCommandHandler struct {
	adapterTestHandler
	sawActor bool
	kind     ActorKind
	id       string
}

func (h *actorAwareCommandHandler) HandleCommand(ctx context.Context, _ CommandRequest) (*CommandResponse, error) {
	kind, id, ok := actorMetadataFromContext(ctx)
	h.sawActor = ok
	h.kind = kind
	h.id = id
	return OK("ok"), nil
}

type testBrokerDialer struct {
	conns map[uint32]*grpc.ClientConn
}

func (d *testBrokerDialer) DialWithOptions(id uint32, _ ...grpc.DialOption) (*grpc.ClientConn, error) {
	conn, ok := d.conns[id]
	if !ok {
		return nil, errors.New("unknown broker id")
	}
	return conn, nil
}

type testPluginHostServiceServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	mu        sync.Mutex
	requests  []*pluginv1.PluginHostServiceEmitEventRequest
	sawActor  bool
	actorKind ActorKind
	actorID   string
}

func (s *testPluginHostServiceServer) EmitEvent(ctx context.Context, req *pluginv1.PluginHostServiceEmitEventRequest) (*pluginv1.PluginHostServiceEmitEventResponse, error) {
	s.mu.Lock()
	s.requests = append(s.requests, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    req.GetStream(),
		EventType: req.GetEventType(),
		Payload:   append([]byte(nil), req.GetPayload()...),
	})
	s.actorKind, s.actorID, s.sawActor = ActorMetadataFromIncomingContext(ctx)
	s.mu.Unlock()
	return &pluginv1.PluginHostServiceEmitEventResponse{}, nil
}

type emittingPluginHostServiceServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	pluginName string
	mu         sync.Mutex
	requests   []*pluginv1.PluginHostServiceEmitEventRequest
	actorKind  ActorKind
	actorID    string
}

func (s *emittingPluginHostServiceServer) EmitEvent(ctx context.Context, req *pluginv1.PluginHostServiceEmitEventRequest) (*pluginv1.PluginHostServiceEmitEventResponse, error) {
	kind, id, ok := ActorMetadataFromIncomingContext(ctx)
	if !ok {
		kind = ActorPlugin
		id = s.pluginName
	}
	s.mu.Lock()
	s.actorKind = kind
	s.actorID = id
	s.requests = append(s.requests, &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    req.GetStream(),
		EventType: req.GetEventType(),
		Payload:   append([]byte(nil), req.GetPayload()...),
	})
	s.mu.Unlock()
	return &pluginv1.PluginHostServiceEmitEventResponse{}, nil
}

func startPluginHostServiceTestServer(t *testing.T, srv pluginv1.PluginHostServiceServer) *grpc.ClientConn {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- bufconn test server
	pluginv1.RegisterPluginHostServiceServer(server, srv)

	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient("passthrough:///plugin-host-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection -- bufconn test client
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

type testFullAdapterHandler struct {
	adapterTestHandler
}

func (h *testFullAdapterHandler) HandleCommand(_ context.Context, req CommandRequest) (*CommandResponse, error) {
	return OK("handled: " + req.Command), nil
}

// --- FocusClient injection ---

type focusClientInitProvider struct {
	initCalled  bool
	focusClient FocusClient
}

func (p *focusClientInitProvider) RegisterServices(_ grpc.ServiceRegistrar) {}

func (p *focusClientInitProvider) Init(ctx context.Context, _ *pluginv1.ServiceConfig) error {
	p.initCalled = true
	if p.focusClient == nil {
		return errors.New("focus client not injected")
	}
	// Exercise the injected client to prove it reaches the host.
	return p.focusClient.JoinFocus(ctx, "sess-1", FocusKey{Kind: FocusKindScene, TargetID: "scene-1"})
}

func (p *focusClientInitProvider) SetFocusClient(client FocusClient) {
	p.focusClient = client
}

func TestPluginServerAdapterInitInjectsFocusClientIntoServiceProvider(t *testing.T) {
	srv := &focusTestServer{}
	hostConn := startPluginHostServiceTestServer(t, srv)

	provider := &focusClientInitProvider{}
	adapter := &pluginServerAdapter{
		handler:         &adapterTestHandler{},
		serviceProvider: provider,
		brokerDialer: &testBrokerDialer{
			conns: map[uint32]*grpc.ClientConn{7: hostConn},
		},
	}

	_, err := adapter.Init(context.Background(), &pluginv1.InitRequest{
		Config: &pluginv1.ServiceConfig{
			RequiredServices: map[string]string{
				PluginHostServiceName: "broker:7",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, provider.initCalled, "expected provider.Init to run")
	require.NotNil(t, provider.focusClient, "expected FocusClient to be injected")
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.joinReqs, 1, "expected the injected client's JoinFocus call to reach the host")
	assert.Equal(t, "sess-1", srv.joinReqs[0].GetSessionId())
}

type dualAwareProvider struct {
	focusClient FocusClient
	sink        EventSink
}

func (p *dualAwareProvider) RegisterServices(_ grpc.ServiceRegistrar)                {}
func (p *dualAwareProvider) Init(_ context.Context, _ *pluginv1.ServiceConfig) error { return nil }
func (p *dualAwareProvider) SetEventSink(s EventSink)                                { p.sink = s }
func (p *dualAwareProvider) SetFocusClient(c FocusClient)                            { p.focusClient = c }

func TestPluginServerAdapterInitInjectsBothEventSinkAndFocusClient(t *testing.T) {
	hostConn := startPluginHostServiceTestServer(t, &focusTestServer{})

	provider := &dualAwareProvider{}
	adapter := &pluginServerAdapter{
		handler:         &adapterTestHandler{},
		serviceProvider: provider,
		brokerDialer: &testBrokerDialer{
			conns: map[uint32]*grpc.ClientConn{7: hostConn},
		},
	}

	_, err := adapter.Init(context.Background(), &pluginv1.InitRequest{
		Config: &pluginv1.ServiceConfig{
			RequiredServices: map[string]string{
				PluginHostServiceName: "broker:7",
			},
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, provider.sink, "expected EventSink injection")
	assert.NotNil(t, provider.focusClient, "expected FocusClient injection")
}
