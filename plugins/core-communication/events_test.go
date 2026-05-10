// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corecomm_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	corecomm "github.com/holomush/holomush/plugins/core-communication"
)

func TestEventTypesAreQualifiedWithPluginName(t *testing.T) {
	for _, et := range []corecomm.EventType{
		corecomm.EventTypeEmit,
		corecomm.EventTypeOOC,
		corecomm.EventTypePage,
		corecomm.EventTypePemit,
		corecomm.EventTypePose,
		corecomm.EventTypeSay,
		corecomm.EventTypeWhisper,
		corecomm.EventTypeWhisperNotice,
	} {
		assert.True(
			t,
			strings.HasPrefix(string(et), "core-communication:"),
			"event type %q must be qualified with plugin prefix", et,
		)
	}
}

func TestEventTypeAttributionIsExact(t *testing.T) {
	assert.Equal(t, corecomm.EventType("core-communication:say"), corecomm.EventTypeSay)
	assert.Equal(t, corecomm.EventType("core-communication:whisper"), corecomm.EventTypeWhisper)
}
