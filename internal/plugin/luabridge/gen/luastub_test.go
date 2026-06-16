// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
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
