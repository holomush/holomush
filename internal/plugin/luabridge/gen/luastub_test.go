// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectStubMessagesIncludesRequestAndResponse(t *testing.T) {
	services, err := collectServices()
	require.NoError(t, err)

	msgs, err := collectStubMessages(services)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, m := range msgs {
		names[m.ClassName] = true
	}
	// EmitService.EmitEvent has request EmitEventRequest + response EmitEventResponse.
	assert.True(t, names["holomush.msg.EmitEventRequest"], "request message class present")
	assert.True(t, names["holomush.msg.EmitEventResponse"], "response message class present")
}

func TestRenderLuaStubProducesMetaAndNamespaces(t *testing.T) {
	services, err := collectServices()
	require.NoError(t, err)
	msgs, err := collectStubMessages(services)
	require.NoError(t, err)

	out, err := renderLuaStub(services, msgs, ambientDecls)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(out, "---@meta"), "stub MUST begin with ---@meta")
	assert.Contains(t, out, "---@class holomush.host.emit")
	assert.Contains(t, out, "function emit.EmitEvent(req)")
	assert.Contains(t, out, "---@class holomush.msg.EmitEventRequest")
	assert.Contains(t, out, "---@class holomush.config")
	assert.Contains(t, out, "function holo.fmt.bold(text)")

	// Non-identifier capability tokens (world.query, command-registry, ...) are
	// registered by the runtime via L.SetGlobal under their literal string key, so
	// the stub MUST emit them in _G index form to be valid Lua.
	assert.Contains(t, out, `_G["world.query"] = {}`)
	assert.Contains(t, out, `_G["world.query"].QueryObject = function(req) end`)
	assert.Contains(t, out, `_G["command-registry"] = {}`)

	// The invalid bare forms (parse errors / wrong surface) MUST be gone.
	assert.NotContains(t, out, "\nworld.query = {}")
	assert.NotContains(t, out, "command-registry = {}")
}
