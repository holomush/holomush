// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// mockServiceProxy implements plugins.ServiceProxy for testing PluginHostService.
type mockServiceProxy struct {
	plugins.ServiceProxy // embed to satisfy interface; only override what we test

	queryLocationResult     *plugins.LocationResult
	queryLocationErr        error
	queryCharacterResult    *plugins.CharacterResult
	queryCharacterErr       error
	queryLocCharsResult     []plugins.CharacterResult
	queryLocCharsErr        error
	emitEventErr            error
	kvGetValue              string
	kvGetFound              bool
	kvGetErr                error
	kvSetErr                error
	kvDeleteErr             error
	logCalled               bool
	logLevel                string
	logMessage              string
}

func (m *mockServiceProxy) QueryLocation(_ context.Context, _, _ string) (*plugins.LocationResult, error) {
	return m.queryLocationResult, m.queryLocationErr
}

func (m *mockServiceProxy) QueryCharacter(_ context.Context, _, _ string) (*plugins.CharacterResult, error) {
	return m.queryCharacterResult, m.queryCharacterErr
}

func (m *mockServiceProxy) QueryLocationCharacters(_ context.Context, _, _ string) ([]plugins.CharacterResult, error) {
	return m.queryLocCharsResult, m.queryLocCharsErr
}

func (m *mockServiceProxy) EmitEvent(_ context.Context, _, _ string, _ []byte) error {
	return m.emitEventErr
}

func (m *mockServiceProxy) Log(_ context.Context, level, message string) {
	m.logCalled = true
	m.logLevel = level
	m.logMessage = message
}

func (m *mockServiceProxy) KVGet(_ context.Context, _, _ string) (string, bool, error) {
	return m.kvGetValue, m.kvGetFound, m.kvGetErr
}

func (m *mockServiceProxy) KVSet(_ context.Context, _, _, _ string) error {
	return m.kvSetErr
}

func (m *mockServiceProxy) KVDelete(_ context.Context, _, _ string) error {
	return m.kvDeleteErr
}

func TestPluginHostService_QueryLocation(t *testing.T) {
	tests := []struct {
		name       string
		result     *plugins.LocationResult
		proxyErr   error
		wantErr    bool
		wantNilLoc bool
	}{
		{
			name: "success",
			result: &plugins.LocationResult{
				ID: "loc-1", Name: "Town Square", Description: "A bustling square", Type: "outdoor", OwnerID: "owner-1",
			},
		},
		{
			name:       "not found returns empty response",
			result:     nil,
			wantNilLoc: true,
		},
		{
			name:     "proxy error",
			proxyErr: errors.New("db down"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &mockServiceProxy{queryLocationResult: tt.result, queryLocationErr: tt.proxyErr}
			svc := NewPluginHostService(proxy, slog.Default())

			resp, err := svc.QueryLocation(context.Background(), &pluginv1.PluginHostServiceQueryLocationRequest{
				SubjectId:  "char-1",
				LocationId: "loc-1",
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNilLoc {
				assert.Nil(t, resp.GetLocation())
				return
			}
			loc := resp.GetLocation()
			assert.Equal(t, "loc-1", loc.GetId())
			assert.Equal(t, "Town Square", loc.GetName())
			assert.Equal(t, "A bustling square", loc.GetDescription())
			assert.Equal(t, "outdoor", loc.GetType())
			assert.Equal(t, "owner-1", loc.GetOwnerId())
		})
	}
}

func TestPluginHostService_QueryCharacter(t *testing.T) {
	tests := []struct {
		name     string
		result   *plugins.CharacterResult
		proxyErr error
		wantErr  bool
		wantNil  bool
	}{
		{
			name: "success",
			result: &plugins.CharacterResult{
				ID: "char-1", PlayerID: "player-1", Name: "Alice", Description: "A hero", LocationID: "loc-1",
			},
		},
		{
			name:    "not found",
			result:  nil,
			wantNil: true,
		},
		{
			name:     "proxy error",
			proxyErr: errors.New("db down"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &mockServiceProxy{queryCharacterResult: tt.result, queryCharacterErr: tt.proxyErr}
			svc := NewPluginHostService(proxy, slog.Default())

			resp, err := svc.QueryCharacter(context.Background(), &pluginv1.PluginHostServiceQueryCharacterRequest{
				SubjectId:   "char-1",
				CharacterId: "char-1",
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, resp.GetCharacter())
				return
			}
			ch := resp.GetCharacter()
			assert.Equal(t, "char-1", ch.GetId())
			assert.Equal(t, "Alice", ch.GetName())
			assert.Equal(t, "player-1", ch.GetPlayerId())
		})
	}
}

func TestPluginHostService_QueryLocationCharacters(t *testing.T) {
	tests := []struct {
		name     string
		results  []plugins.CharacterResult
		proxyErr error
		wantErr  bool
		wantLen  int
	}{
		{
			name: "two characters",
			results: []plugins.CharacterResult{
				{ID: "c1", Name: "Alice"},
				{ID: "c2", Name: "Bob"},
			},
			wantLen: 2,
		},
		{
			name:    "empty location",
			results: nil,
			wantLen: 0,
		},
		{
			name:     "proxy error",
			proxyErr: errors.New("db down"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &mockServiceProxy{queryLocCharsResult: tt.results, queryLocCharsErr: tt.proxyErr}
			svc := NewPluginHostService(proxy, slog.Default())

			resp, err := svc.QueryLocationCharacters(context.Background(), &pluginv1.PluginHostServiceQueryLocationCharactersRequest{
				SubjectId:  "char-1",
				LocationId: "loc-1",
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, resp.GetCharacters(), tt.wantLen)
		})
	}
}

func TestPluginHostService_EmitEvent(t *testing.T) {
	tests := []struct {
		name     string
		proxyErr error
		wantErr  bool
	}{
		{"success", nil, false},
		{"proxy error", errors.New("emit failed"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &mockServiceProxy{emitEventErr: tt.proxyErr}
			svc := NewPluginHostService(proxy, slog.Default())

			_, err := svc.EmitEvent(context.Background(), &pluginv1.PluginHostServiceEmitEventRequest{
				Stream:    "location:123",
				EventType: "say",
				Payload:   []byte(`{"text":"hello"}`),
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPluginHostService_Log(t *testing.T) {
	proxy := &mockServiceProxy{}
	svc := NewPluginHostService(proxy, slog.Default())

	resp, err := svc.Log(context.Background(), &pluginv1.PluginHostServiceLogRequest{
		Level:   "info",
		Message: "test log message",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, proxy.logCalled)
	assert.Equal(t, "info", proxy.logLevel)
	assert.Equal(t, "test log message", proxy.logMessage)
}

func TestPluginHostService_KVGet(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		found     bool
		proxyErr  error
		wantErr   bool
		wantFound bool
	}{
		{"found", "stored-value", true, nil, false, true},
		{"not found", "", false, nil, false, false},
		{"proxy error", "", false, errors.New("db down"), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &mockServiceProxy{kvGetValue: tt.value, kvGetFound: tt.found, kvGetErr: tt.proxyErr}
			svc := NewPluginHostService(proxy, slog.Default())

			resp, err := svc.KVGet(context.Background(), &pluginv1.PluginHostServiceKVGetRequest{
				PluginName: "test-plugin",
				Key:        "my-key",
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantFound, resp.GetFound())
			if tt.wantFound {
				assert.Equal(t, tt.value, resp.GetValue())
			}
		})
	}
}

func TestPluginHostService_KVSet(t *testing.T) {
	tests := []struct {
		name     string
		proxyErr error
		wantErr  bool
	}{
		{"success", nil, false},
		{"proxy error", errors.New("db down"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &mockServiceProxy{kvSetErr: tt.proxyErr}
			svc := NewPluginHostService(proxy, slog.Default())

			_, err := svc.KVSet(context.Background(), &pluginv1.PluginHostServiceKVSetRequest{
				PluginName: "test-plugin",
				Key:        "my-key",
				Value:      "my-value",
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPluginHostService_KVDelete(t *testing.T) {
	tests := []struct {
		name     string
		proxyErr error
		wantErr  bool
	}{
		{"success", nil, false},
		{"proxy error", errors.New("db down"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &mockServiceProxy{kvDeleteErr: tt.proxyErr}
			svc := NewPluginHostService(proxy, slog.Default())

			_, err := svc.KVDelete(context.Background(), &pluginv1.PluginHostServiceKVDeleteRequest{
				PluginName: "test-plugin",
				Key:        "my-key",
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNewPluginHostService_NilLogger(t *testing.T) {
	proxy := &mockServiceProxy{}
	svc := NewPluginHostService(proxy, nil)
	assert.NotNil(t, svc)
	assert.NotNil(t, svc.logger)
}
