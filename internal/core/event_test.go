// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventvocab"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func TestActorKind_String(t *testing.T) {
	tests := []struct {
		name     string
		input    ActorKind
		expected string
	}{
		{"character", ActorCharacter, "character"},
		{"system", ActorSystem, "system"},
		{"plugin", ActorPlugin, "plugin"},
		{"unknown", ActorKind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.input.String())
		})
	}
}

func TestHostEventTypesMatchPluginSDKReExports(t *testing.T) {
	// The host's authoritative event-type strings live in internal/eventvocab.
	// pkg/plugin re-exports them as pluginsdk.HostEventType* so plugin
	// code (which cannot import internal/core or internal/eventvocab) has
	// typed references. Verify the two sides agree string-for-string.
	cases := []struct {
		name string
		host eventvocab.EventType
		sdk  pluginsdk.EventType
	}{
		{"host and sdk agree on system event type string", eventvocab.EventTypeSystem, pluginsdk.HostEventTypeSystem},
		{"host and sdk agree on session_ended event type string", eventvocab.EventTypeSessionEnded, pluginsdk.HostEventTypeSessionEnded},
		{"host and sdk agree on command_response event type string", eventvocab.EventTypeCommandResponse, pluginsdk.HostEventTypeCommandResponse},
		{"host and sdk agree on command_error event type string", eventvocab.EventTypeCommandError, pluginsdk.HostEventTypeCommandError},
		{"host and sdk agree on arrive event type string", eventvocab.EventTypeArrive, pluginsdk.HostEventTypeArrive},
		{"host and sdk agree on leave event type string", eventvocab.EventTypeLeave, pluginsdk.HostEventTypeLeave},
		{"host and sdk agree on move event type string", eventvocab.EventTypeMove, pluginsdk.HostEventTypeMove},
		{"host and sdk agree on location_state event type string", eventvocab.EventTypeLocationState, pluginsdk.HostEventTypeLocationState},
		{"host and sdk agree on exit_update event type string", eventvocab.EventTypeExitUpdate, pluginsdk.HostEventTypeExitUpdate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, string(c.host), string(c.sdk),
				"host event-type drift between internal/eventvocab and pkg/plugin")
		})
	}
}

func TestSystemActorULIDRendersAsCanonicalCrockford(t *testing.T) {
	assert.Equal(t, "00000000000000000000000001", SystemActorULID.String())
}

func TestWorldServiceActorULIDRendersAsCanonicalCrockford(t *testing.T) {
	assert.Equal(t, "00000000000000000000000002", WorldServiceActorULID.String())
}

func TestActorSystemIDIsSystemActorULIDString(t *testing.T) {
	assert.Equal(t, SystemActorULID.String(), ActorSystemID,
		"ActorSystemID MUST equal SystemActorULID.String() post-w9ml")
}

func TestIsSentinelULIDIdentifiesKnownSentinels(t *testing.T) {
	assert.True(t, IsSentinelULID(SystemActorULID))
	assert.True(t, IsSentinelULID(WorldServiceActorULID))
}

func TestIsSentinelULIDRejectsZeroULID(t *testing.T) {
	assert.False(t, IsSentinelULID(ulid.ULID{}),
		"all-zero ULID is reserved as 'no sentinel' (tag 0x00)")
}

func TestIsSentinelULIDRejectsEntropyULID(t *testing.T) {
	entropy := NewULID()
	assert.False(t, IsSentinelULID(entropy),
		"entropy ULIDs MUST NOT be classified as sentinels")
}

func TestSentinelTagsUnique(t *testing.T) {
	all := map[byte]string{}
	check := func(label string, id ulid.ULID) {
		require.True(t, IsSentinelULID(id), "%s must satisfy IsSentinelULID", label)
		tag := id[15]
		if existing, ok := all[tag]; ok {
			t.Fatalf("sentinel tag-byte collision: %s and %s both use 0x%02x", existing, label, tag)
		}
		all[tag] = label
	}
	check("SystemActorULID", SystemActorULID)
	check("WorldServiceActorULID", WorldServiceActorULID)
}
