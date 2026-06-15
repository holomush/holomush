// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/samber/oops"
	"google.golang.org/grpc"
)

const brokerPrefix = "broker:"

// PluginHostServiceName is the reserved broker service name used by binary
// plugins to call back into the host for event emission and other host-owned
// operations.
const PluginHostServiceName = "holomush.plugin.v1.PluginHostService"

// ParseBrokerServices parses the required_services map from InitRequest
// into a map of service name to broker ID. Each value must have the format
// "broker:<uint32>".
func ParseBrokerServices(services map[string]string) (map[string]uint32, error) {
	result := make(map[string]uint32, len(services))
	for name, value := range services {
		if !strings.HasPrefix(value, brokerPrefix) {
			return nil, fmt.Errorf("service %q: expected %q prefix, got %q", name, brokerPrefix, value)
		}
		idStr := strings.TrimPrefix(value, brokerPrefix)
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("service %q: invalid broker ID %q: %w", name, idStr, err)
		}
		result[name] = uint32(id)
	}
	return result, nil
}

// BrokerServiceID resolves a single broker-backed service ID from the
// required_services init map.
func BrokerServiceID(services map[string]string, serviceName string) (uint32, error) {
	parsed, err := ParseBrokerServices(services)
	if err != nil {
		return 0, err
	}
	id, ok := parsed[serviceName]
	if !ok {
		return 0, fmt.Errorf("service %q not found in required services", serviceName)
	}
	return id, nil
}

// dialPluginHost dials the plugin host service via the given broker and
// returns a *grpc.ClientConn. Callers wrap the conn in service-specific
// clients (EventSink, FocusClient). This helper exists so a single
// plugin process holds one connection to the host for all host-facing
// SDK facades.
func dialPluginHost(broker brokerDialer, services map[string]string) (*grpc.ClientConn, error) {
	if broker == nil {
		return nil, oops.New("plugin host broker is not configured")
	}
	brokerID, err := BrokerServiceID(services, PluginHostServiceName)
	if err != nil {
		return nil, oops.With("service", PluginHostServiceName).Wrap(err)
	}
	conn, err := broker.DialWithOptions(
		brokerID,
		grpc.WithAuthority("holomush-plugin-host"),
		// Ferry the host-vouched dispatch envelope from each incoming delivery
		// onto plugin→host calls so scoped capabilities resolve their fence
		// host-side (plugin-runtime-symmetry with the Lua bufconn, INV-PLUGIN-51).
		grpc.WithChainUnaryInterceptor(dispatchFerryInterceptor()),
	)
	if err != nil {
		return nil, oops.With("service", PluginHostServiceName).Wrap(err)
	}
	return conn, nil
}
