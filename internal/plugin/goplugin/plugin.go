// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"errors"

	goplugin "github.com/hashicorp/go-plugin"
	pluginv1 "github.com/holomush/holomush/internal/proto/holomush/plugin/v1"
	"github.com/holomush/holomush/pkg/pluginsdk"
	"google.golang.org/grpc"
)

// HandshakeConfig is imported from pluginsdk to ensure host and plugins
// use identical configuration. Do not define locally to prevent drift.
var HandshakeConfig = pluginsdk.HandshakeConfig

// PluginMap is the map of plugins we can dispense.
var PluginMap = map[string]goplugin.Plugin{
	"plugin": &GRPCPlugin{},
}

// GRPCPlugin implements go-plugin's Plugin interface for gRPC.
// It enables the host to communicate with binary plugins via gRPC.
type GRPCPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	// Impl is used by the plugin-side (not used by host).
	Impl pluginv1.PluginServer
}

// GRPCServer registers the plugin server (called by plugin process).
func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	if p.Impl == nil {
		return errors.New("goplugin: plugin implementation is nil")
	}
	pluginv1.RegisterPluginServer(s, p.Impl)
	return nil
}

// GRPCClient returns a plugin client (called by host process).
func (p *GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return pluginv1.NewPluginClient(c), nil
}
