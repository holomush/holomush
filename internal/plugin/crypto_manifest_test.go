// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestCryptoSectionParsesEmitsBlock(t *testing.T) {
	src := `
emits:
  - event_type: whisper
    sensitivity: always
    description: "Direct character-to-character private message."
  - event_type: pose
    sensitivity: may
  - event_type: presence
    sensitivity: never
`
	var got plugins.CryptoSection
	require.NoError(t, yaml.Unmarshal([]byte(src), &got))

	require.Len(t, got.Emits, 3)
	assert.Equal(t, "whisper", got.Emits[0].EventType)
	assert.Equal(t, plugins.SensitivityAlways, got.Emits[0].Sensitivity)
	assert.Equal(t, "Direct character-to-character private message.", got.Emits[0].Description)
	assert.Equal(t, plugins.SensitivityMay, got.Emits[1].Sensitivity)
	assert.Equal(t, plugins.SensitivityNever, got.Emits[2].Sensitivity)
}

func TestCryptoSectionParsesConsumesBlock(t *testing.T) {
	src := `
consumes:
  - subjects:
      - "events.*.character.*.whisper"
    requests_decryption:
      - "core-communication:whisper"
`
	var got plugins.CryptoSection
	require.NoError(t, yaml.Unmarshal([]byte(src), &got))

	require.Len(t, got.Consumes, 1)
	assert.Equal(t, []string{"events.*.character.*.whisper"}, got.Consumes[0].Subjects)
	assert.Equal(t, []string{"core-communication:whisper"}, got.Consumes[0].RequestsDecryption)
}

func TestLookupEmitSensitivityReturnsDeclaredValueForListedEventType(t *testing.T) {
	m := &plugins.Manifest{
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "scene.whisper", Sensitivity: plugins.SensitivityAlways},
				{EventType: "scene.pose", Sensitivity: plugins.SensitivityNever},
			},
		},
	}
	got := plugins.LookupEmitSensitivity(m, "scene.whisper")
	assert.Equal(t, plugins.SensitivityAlways, got)
}

func TestLookupEmitSensitivityDefaultsToNeverForUnlistedEventType(t *testing.T) {
	m := &plugins.Manifest{
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "scene.whisper", Sensitivity: plugins.SensitivityAlways},
			},
		},
	}
	got := plugins.LookupEmitSensitivity(m, "scene.pose")
	assert.Equal(t, plugins.SensitivityNever, got)
}

func TestLookupEmitSensitivityHandlesNilManifest(t *testing.T) {
	got := plugins.LookupEmitSensitivity(nil, "anything")
	assert.Equal(t, plugins.SensitivityNever, got)
}

func TestLookupEmitSensitivityHandlesEmptyEmits(t *testing.T) {
	m := &plugins.Manifest{Crypto: &plugins.CryptoSection{}}
	got := plugins.LookupEmitSensitivity(m, "anything")
	assert.Equal(t, plugins.SensitivityNever, got)
}

func TestLookupEmitSensitivityHandlesNilCryptoBlock(t *testing.T) {
	m := &plugins.Manifest{Crypto: nil} // crypto: block omitted from YAML
	got := plugins.LookupEmitSensitivity(m, "anything")
	assert.Equal(t, plugins.SensitivityNever, got)
}
