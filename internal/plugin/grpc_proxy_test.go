// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGRPCServiceProxy(t *testing.T) {
	t.Run("creates proxy with registry", func(t *testing.T) {
		reg := NewServiceRegistry()
		proxy := NewGRPCServiceProxy(reg)
		assert.NotNil(t, proxy)
	})

	t.Run("returns a grpc.ServerOption from Handler()", func(t *testing.T) {
		reg := NewServiceRegistry()
		proxy := NewGRPCServiceProxy(reg)
		opt := proxy.Handler()
		assert.NotNil(t, opt)
	})
}

func TestExtractServiceName(t *testing.T) {
	t.Run("extracts service from standard gRPC method", func(t *testing.T) {
		assert.Equal(t, "holomush.scene.v1.SceneService", extractServiceName("/holomush.scene.v1.SceneService/CreateScene"))
	})

	t.Run("returns empty for malformed method without leading slash", func(t *testing.T) {
		assert.Equal(t, "", extractServiceName("holomush.scene.v1.SceneService/CreateScene"))
	})

	t.Run("returns empty for method without service separator", func(t *testing.T) {
		assert.Equal(t, "", extractServiceName("/nomethod"))
	})

	t.Run("handles simple service name", func(t *testing.T) {
		assert.Equal(t, "MyService", extractServiceName("/MyService/MyMethod"))
	})
}

func TestRawMessage(t *testing.T) {
	t.Run("round-trips data through marshal/unmarshal", func(t *testing.T) {
		msg := &rawMessage{}
		require.NoError(t, msg.Unmarshal([]byte("hello")))
		data, err := msg.Marshal()
		require.NoError(t, err)
		assert.Equal(t, []byte("hello"), data)
	})

	t.Run("reset clears data", func(t *testing.T) {
		msg := &rawMessage{data: []byte("hello")}
		msg.Reset()
		assert.Nil(t, msg.data)
	})
}
