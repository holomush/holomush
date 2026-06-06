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

func TestLookupEmitSensitivity(t *testing.T) {
	declared := &plugins.Manifest{
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "scene.whisper", Sensitivity: plugins.SensitivityAlways},
				{EventType: "scene.pose", Sensitivity: plugins.SensitivityNever},
			},
		},
	}
	tests := []struct {
		name      string
		manifest  *plugins.Manifest
		eventType string
		want      plugins.Sensitivity
	}{
		{
			name:      "returns declared value for listed event type",
			manifest:  declared,
			eventType: "scene.whisper",
			want:      plugins.SensitivityAlways,
		},
		{
			name:      "defaults to never for unlisted event type",
			manifest:  declared,
			eventType: "scene.pose-mismatch",
			want:      plugins.SensitivityNever,
		},
		{
			name:      "handles nil manifest",
			manifest:  nil,
			eventType: "anything",
			want:      plugins.SensitivityNever,
		},
		{
			name:      "handles empty emits",
			manifest:  &plugins.Manifest{Crypto: &plugins.CryptoSection{}},
			eventType: "anything",
			want:      plugins.SensitivityNever,
		},
		{
			name:      "handles nil crypto block (crypto: omitted from YAML)",
			manifest:  &plugins.Manifest{Crypto: nil},
			eventType: "anything",
			want:      plugins.SensitivityNever,
		},
		// holomush-50zqs: a bare crypto.emits entry must match a plugin-
		// qualified WIRE type (core-communication:page) via composition with
		// the plugin's own name.
		{
			name: "matches bare entry against own-qualified wire type",
			manifest: &plugins.Manifest{
				Name: "core-communication",
				Crypto: &plugins.CryptoSection{
					Emits: []plugins.CryptoEmit{{EventType: "page", Sensitivity: plugins.SensitivityAlways}},
				},
			},
			eventType: "core-communication:page",
			want:      plugins.SensitivityAlways,
		},
		{
			name: "still matches a bare wire type (core-scenes convention)",
			manifest: &plugins.Manifest{
				Name: "core-scenes",
				Crypto: &plugins.CryptoSection{
					Emits: []plugins.CryptoEmit{{EventType: "scene_pose", Sensitivity: plugins.SensitivityAlways}},
				},
			},
			eventType: "scene_pose",
			want:      plugins.SensitivityAlways,
		},
		{
			name: "does not match a foreign plugin prefix",
			manifest: &plugins.Manifest{
				Name: "core-communication",
				Crypto: &plugins.CryptoSection{
					Emits: []plugins.CryptoEmit{{EventType: "page", Sensitivity: plugins.SensitivityAlways}},
				},
			},
			eventType: "other-plugin:page",
			want:      plugins.SensitivityNever,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plugins.LookupEmitSensitivity(tt.manifest, tt.eventType)
			assert.Equal(t, tt.want, got)
		})
	}
}
