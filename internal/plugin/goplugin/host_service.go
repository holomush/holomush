// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"google.golang.org/grpc"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// newPluginHostServiceServer builds the single broker *grpc.Server a binary
// plugin dials back into. Every host-brokered capability is served as a
// capability-scoped holomush.plugin.host.v1 service on this one server, reached
// through the single broker handshake (pkg/plugin.PluginHostServiceName names
// the broker channel, not a gRPC service). The former monolithic
// holomush.plugin.v1.PluginHostService is gone (holomush-eykuh.1, Task 12): its
// authoritative handler bodies now live on the per-capability servers, which
// after holomush-eykuh.2 are the runtime-neutral hostcap servers consumed by both
// the binary and Lua runtimes through the HostCapabilities port (INV-PLUGIN-49).
//
// *Host satisfies hostcap.HostCapabilities (the binary adapter), so the binary
// registration is hostcap.RegisterCapabilities with the BinaryDefaultSet. That
// set omits World/Property/Session — they have no binary consumer in this
// sub-spec — so they are deliberately NOT registered here.
func newPluginHostServiceServer(host *Host, pluginName string) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		hostcap.RegisterCapabilities(server, hostcap.NewBase(host, pluginName), hostcap.BinaryDefaultSet)
		return server
	}
}

// sdkActorKindToCore maps a plugin-SDK ActorKind to the core ActorKind. It is
// retained on the binary host alongside the broker wiring; unknown kinds fall
// back to ActorPlugin (the least-privileged plugin-owned actor).
func sdkActorKindToCore(kind pluginsdk.ActorKind) core.ActorKind {
	switch kind {
	case pluginsdk.ActorCharacter:
		return core.ActorCharacter
	case pluginsdk.ActorSystem:
		return core.ActorSystem
	default:
		return core.ActorPlugin
	}
}
