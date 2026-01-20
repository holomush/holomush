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

// Compile-time check that GRPCPlugin implements goplugin.GRPCPlugin.
var _ goplugin.GRPCPlugin = (*GRPCPlugin)(nil)

// HandshakeConfig is imported from pluginsdk to ensure host and plugins
// use identical configuration. Do not define locally to prevent drift.
var HandshakeConfig = pluginsdk.HandshakeConfig

// PluginMap is the map of plugins we can dispense.
var PluginMap = map[string]goplugin.Plugin{
	"plugin": &GRPCPlugin{},
}

// GRPCPlugin implements go-plugin's Plugin interface for gRPC.
// This is the host-side implementation; plugins use pluginsdk.Serve() instead.
type GRPCPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
}

// GRPCServer is required by go-plugin's GRPCPlugin interface but is never
// called on the host side. Plugins use pluginsdk.Serve() which provides its
// own GRPCServer implementation.
func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, _ *grpc.Server) error {
	return errors.New("goplugin: GRPCServer not implemented on host side")
}

// GRPCClient returns a plugin client (called by host process).
func (p *GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return pluginv1.NewPluginClient(c), nil
}
