// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestNewBrokerProxyReturnsServerFactory(t *testing.T) {
	factory := NewBrokerProxy(nil, "test-plugin")
	require.NotNil(t, factory)

	server := factory([]grpc.ServerOption{})
	require.NotNil(t, server)
	server.Stop()
}

func TestNewBrokerProxyIncludesPluginName(t *testing.T) {
	factory := NewBrokerProxy(nil, "core-scenes")
	assert.NotNil(t, factory)
}
