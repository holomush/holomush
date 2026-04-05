// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"

	hashiplug "github.com/hashicorp/go-plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
)

// ServiceProvider is implemented by binary plugins that provide gRPC services
// and/or need initialization with service configuration.
type ServiceProvider interface {
	// RegisterServices registers the plugin's gRPC services on the go-plugin
	// transport. The registrar is the same grpc.Server that carries
	// PluginService, so additional services are multiplexed on the same
	// connection.
	RegisterServices(registrar grpc.ServiceRegistrar)

	// Init is called by the host after connection, providing the DB
	// connection string, required service addresses, etc.
	Init(ctx context.Context, config *pluginv1.ServiceConfig) error
}

// ServeWithServices starts the plugin server with service injection support.
// It is the service-aware counterpart of Serve. Plugins that provide gRPC
// services or need initialization should use this instead of Serve.
//
// The provider's RegisterServices is called during gRPC server setup, and its
// Init method is called when the host sends the Init RPC.
func ServeWithServices(config *ServeConfig, provider ServiceProvider) {
	if config == nil {
		panic("plugin: config cannot be nil")
	}
	if config.Handler == nil {
		panic("plugin: config.Handler cannot be nil")
	}
	if provider == nil {
		panic("plugin: provider cannot be nil")
	}
	hashiplug.Serve(&hashiplug.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]hashiplug.Plugin{
			"plugin": &grpcServicePlugin{
				handler:  config.Handler,
				provider: provider,
			},
		},
		GRPCServer: hashiplug.DefaultGRPCServer,
	})
}

// grpcServicePlugin extends grpcPlugin with service provider support.
type grpcServicePlugin struct {
	hashiplug.NetRPCUnsupportedPlugin
	handler  Handler
	provider ServiceProvider
}

// GRPCServer registers both PluginServiceServer and the provider's services.
func (p *grpcServicePlugin) GRPCServer(_ *hashiplug.GRPCBroker, s *grpc.Server) error {
	if p.handler == nil {
		return errors.New("plugin: handler is nil")
	}

	adapter := &pluginServerAdapter{
		handler:         p.handler,
		serviceProvider: p.provider,
	}
	if ch, ok := p.handler.(CommandHandler); ok {
		adapter.cmdHandler = ch
	}

	pluginv1.RegisterPluginServiceServer(s, adapter)

	// Let the provider register its own gRPC services on the same server.
	if p.provider != nil {
		p.provider.RegisterServices(s)
	}

	return nil
}

// GRPCClient is not implemented on the plugin side.
func (p *grpcServicePlugin) GRPCClient(_ context.Context, _ *hashiplug.GRPCBroker, _ *grpc.ClientConn) (interface{}, error) {
	return nil, errors.New("plugin: GRPCClient not implemented on plugin side")
}
