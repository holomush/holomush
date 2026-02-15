// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEntityRef(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantType    string
		wantID      string
		wantErr     bool
		wantErrCode string
	}{
		// Subject prefixes
		{
			name:     "character subject",
			input:    "character:01ABC",
			wantType: "character",
			wantID:   "01ABC",
		},
		{
			name:     "plugin subject",
			input:    "plugin:echo-bot",
			wantType: "plugin",
			wantID:   "echo-bot",
		},
		{
			name:     "system subject (no ID)",
			input:    "system",
			wantType: "system",
			wantID:   "",
		},
		{
			name:     "session subject",
			input:    "session:abc123",
			wantType: "session",
			wantID:   "abc123",
		},

		// Resource prefixes
		{
			name:     "location resource",
			input:    "location:01XYZ",
			wantType: "location",
			wantID:   "01XYZ",
		},
		{
			name:     "object resource",
			input:    "object:01DEF",
			wantType: "object",
			wantID:   "01DEF",
		},
		{
			name:     "command resource",
			input:    "command:dig",
			wantType: "command",
			wantID:   "dig",
		},
		{
			name:     "property resource",
			input:    "property:01GHI",
			wantType: "property",
			wantID:   "01GHI",
		},
		{
			name:     "stream resource with compound ID",
			input:    "stream:location:01XYZ",
			wantType: "stream",
			wantID:   "location:01XYZ",
		},
		{
			name:     "exit resource",
			input:    "exit:01JKL",
			wantType: "exit",
			wantID:   "01JKL",
		},
		{
			name:     "scene resource",
			input:    "scene:01MNO",
			wantType: "scene",
			wantID:   "01MNO",
		},

		// Error cases
		{
			name:        "empty string",
			input:       "",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
		{
			name:        "unknown prefix",
			input:       "bogus:01ABC",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
		{
			name:        "legacy char prefix",
			input:       "char:01ABC",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
		{
			name:        "empty ID after character prefix",
			input:       "character:",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
		{
			name:        "empty ID after location prefix",
			input:       "location:",
			wantErr:     true,
			wantErrCode: "INVALID_ENTITY_REF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typeName, id, err := access.ParseEntityRef(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.wantErrCode)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, typeName)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestSubjectPrefixConstants(t *testing.T) {
	assert.Equal(t, "character:", access.SubjectCharacter)
	assert.Equal(t, "plugin:", access.SubjectPlugin)
	assert.Equal(t, "system", access.SubjectSystem)
	assert.Equal(t, "session:", access.SubjectSession)
}

func TestResourcePrefixConstants(t *testing.T) {
	assert.Equal(t, "character:", access.ResourceCharacter)
	assert.Equal(t, "location:", access.ResourceLocation)
	assert.Equal(t, "object:", access.ResourceObject)
	assert.Equal(t, "command:", access.ResourceCommand)
	assert.Equal(t, "property:", access.ResourceProperty)
	assert.Equal(t, "stream:", access.ResourceStream)
	assert.Equal(t, "exit:", access.ResourceExit)
	assert.Equal(t, "scene:", access.ResourceScene)
}

func TestSessionErrorCodeConstants(t *testing.T) {
	assert.Equal(t, "infra:session-invalid", access.ErrCodeSessionInvalid)
	assert.Equal(t, "infra:session-store-error", access.ErrCodeSessionStoreError)
}

func TestCharacterSubject(t *testing.T) {
	tests := []struct {
		name     string
		charID   string
		expected string
	}{
		{
			name:     "ULID string",
			charID:   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			expected: "character:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		},
		{
			name:     "simple ID",
			charID:   "test-id",
			expected: "character:test-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := access.CharacterSubject(tt.charID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCharacterSubject_EmptyID_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.CharacterSubject: empty charID would bypass access control", func() {
		access.CharacterSubject("")
	})
}

func TestLocationResource(t *testing.T) {
	tests := []struct {
		name       string
		locationID string
		expected   string
	}{
		{
			name:       "ULID string",
			locationID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			expected:   "location:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		},
		{
			name:       "simple ID",
			locationID: "room-1",
			expected:   "location:room-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := access.LocationResource(tt.locationID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLocationResource_EmptyID_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.LocationResource: empty locationID would create invalid resource reference", func() {
		access.LocationResource("")
	})
}

func TestExitResource(t *testing.T) {
	tests := []struct {
		name     string
		exitID   string
		expected string
	}{
		{
			name:     "ULID string",
			exitID:   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			expected: "exit:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		},
		{
			name:     "simple ID",
			exitID:   "exit-north",
			expected: "exit:exit-north",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := access.ExitResource(tt.exitID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExitResource_EmptyID_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.ExitResource: empty exitID would create invalid resource reference", func() {
		access.ExitResource("")
	})
}

func TestObjectResource(t *testing.T) {
	tests := []struct {
		name     string
		objectID string
		expected string
	}{
		{
			name:     "ULID string",
			objectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			expected: "object:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		},
		{
			name:     "simple ID",
			objectID: "sword-1",
			expected: "object:sword-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := access.ObjectResource(tt.objectID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestObjectResource_EmptyID_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.ObjectResource: empty objectID would create invalid resource reference", func() {
		access.ObjectResource("")
	})
}

func TestSceneResource(t *testing.T) {
	tests := []struct {
		name     string
		sceneID  string
		expected string
	}{
		{
			name:     "ULID string",
			sceneID:  "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			expected: "scene:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		},
		{
			name:     "simple ID",
			sceneID:  "midnight-meeting",
			expected: "scene:midnight-meeting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := access.SceneResource(tt.sceneID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSceneResource_EmptyID_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.SceneResource: empty sceneID would create invalid resource reference", func() {
		access.SceneResource("")
	})
}

func TestCommandResource(t *testing.T) {
	tests := []struct {
		name        string
		commandName string
		expected    string
	}{
		{
			name:        "single word command",
			commandName: "dig",
			expected:    "command:dig",
		},
		{
			name:        "compound command name",
			commandName: "teleport-self",
			expected:    "command:teleport-self",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := access.CommandResource(tt.commandName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCommandResource_EmptyName_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.CommandResource: empty commandName would create invalid resource reference", func() {
		access.CommandResource("")
	})
}

func TestCharacterResource(t *testing.T) {
	tests := []struct {
		name     string
		charID   string
		expected string
	}{
		{
			name:     "ULID string",
			charID:   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			expected: "character:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		},
		{
			name:     "simple ID",
			charID:   "player-alice",
			expected: "character:player-alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := access.CharacterResource(tt.charID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCharacterResource_EmptyID_Panics(t *testing.T) {
	assert.PanicsWithValue(t, "access.CharacterResource: empty charID would create invalid resource reference", func() {
		access.CharacterResource("")
	})
}
