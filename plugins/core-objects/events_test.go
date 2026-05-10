// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package coreobj_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	coreobj "github.com/holomush/holomush/plugins/core-objects"
)

func TestEventTypesAreQualifiedWithPluginName(t *testing.T) {
	for _, et := range []coreobj.EventType{
		coreobj.EventTypeObjectCreate,
		coreobj.EventTypeObjectDestroy,
		coreobj.EventTypeObjectUse,
		coreobj.EventTypeObjectExamine,
		coreobj.EventTypeObjectGive,
	} {
		assert.True(
			t,
			strings.HasPrefix(string(et), "core-objects:"),
			"event type %q must be qualified with plugin prefix", et,
		)
	}
}

func TestEventTypeAttributionIsExact(t *testing.T) {
	assert.Equal(t, coreobj.EventType("core-objects:object_create"), coreobj.EventTypeObjectCreate)
	assert.Equal(t, coreobj.EventType("core-objects:object_use"), coreobj.EventTypeObjectUse)
}
