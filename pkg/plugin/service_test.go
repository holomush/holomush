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
	"google.golang.org/grpc"
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

type testFullAdapterHandler struct {
	adapterTestHandler
}

func (h *testFullAdapterHandler) HandleCommand(_ context.Context, req CommandRequest) (*CommandResponse, error) {
	return OK("handled: " + req.Command), nil
}
