// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

type settingsTestServer struct {
	pluginv1.UnimplementedPluginHostServiceServer
	gotScope       pluginv1.SettingScope
	gotPrincipalID string
	gotKey         string
	gotToken       string
	gotStringList  []string
	respFound      bool
	respStringList []string
}

func (s *settingsTestServer) recordToken(ctx context.Context) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if tokens := md.Get(emitTokenHeader); len(tokens) > 0 {
			s.gotToken = tokens[0]
		}
	}
}

func (s *settingsTestServer) GetSetting(ctx context.Context, req *pluginv1.PluginHostServiceGetSettingRequest) (*pluginv1.PluginHostServiceGetSettingResponse, error) {
	s.gotScope = req.GetScope()
	s.gotPrincipalID = req.GetPrincipalId()
	s.gotKey = req.GetKey()
	s.recordToken(ctx)
	return &pluginv1.PluginHostServiceGetSettingResponse{
		Found:      s.respFound,
		StringList: s.respStringList,
	}, nil
}

func (s *settingsTestServer) SetSetting(ctx context.Context, req *pluginv1.PluginHostServiceSetSettingRequest) (*pluginv1.PluginHostServiceSetSettingResponse, error) {
	s.gotScope = req.GetScope()
	s.gotPrincipalID = req.GetPrincipalId()
	s.gotKey = req.GetKey()
	s.gotStringList = req.GetStringList()
	s.recordToken(ctx)
	return &pluginv1.PluginHostServiceSetSettingResponse{}, nil
}

func TestPluginHostSettingsClientGetSettingForwardsRequestAndMapsResponse(t *testing.T) {
	srv := &settingsTestServer{respFound: true, respStringList: []string{"warn-a", "warn-b"}}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	values, found, err := client.GetSetting(context.Background(), SettingScopeGame, "01PRINCIPAL", "content_warnings")

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []string{"warn-a", "warn-b"}, values)
	assert.Equal(t, pluginv1.SettingScope_SETTING_SCOPE_GAME, srv.gotScope)
	assert.Equal(t, "01PRINCIPAL", srv.gotPrincipalID)
	assert.Equal(t, "content_warnings", srv.gotKey)
}

func TestPluginHostSettingsClientGetSettingReportsNotFoundWithEmptyValues(t *testing.T) {
	srv := &settingsTestServer{respFound: false}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	values, found, err := client.GetSetting(context.Background(), SettingScopeCharacter, "01CHAR", "content_warnings")

	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, values)
}

func TestPluginHostSettingsClientSetSettingForwardsRequestFields(t *testing.T) {
	srv := &settingsTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	err := client.SetSetting(context.Background(), SettingScopePlayer, "01PLAYER", "content_warnings", []string{"violence", "gore"})

	require.NoError(t, err)
	assert.Equal(t, pluginv1.SettingScope_SETTING_SCOPE_PLAYER, srv.gotScope)
	assert.Equal(t, "01PLAYER", srv.gotPrincipalID)
	assert.Equal(t, "content_warnings", srv.gotKey)
	assert.Equal(t, []string{"violence", "gore"}, srv.gotStringList)
}

func TestPluginHostSettingsClientGetSettingFailsClosedWhenClientIsNil(t *testing.T) {
	client := &pluginHostSettingsClient{}

	values, found, err := client.GetSetting(context.Background(), SettingScopeGame, "01PRINCIPAL", "content_warnings")

	require.Error(t, err)
	assert.False(t, found)
	assert.Nil(t, values)
}

func TestPluginHostSettingsClientSetSettingFailsClosedWhenClientIsNil(t *testing.T) {
	client := &pluginHostSettingsClient{}

	err := client.SetSetting(context.Background(), SettingScopeGame, "01PRINCIPAL", "content_warnings", []string{"x"})

	require.Error(t, err)
}

func TestPluginHostSettingsClientGetSettingFerriesDispatchTokenFromIncomingContext(t *testing.T) {
	srv := &settingsTestServer{respFound: true}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(emitTokenHeader, "dispatch-token-get"),
	)

	_, _, err := client.GetSetting(ctx, SettingScopeGame, "01PRINCIPAL", "content_warnings")

	require.NoError(t, err)
	assert.Equal(t, "dispatch-token-get", srv.gotToken,
		"GetSetting MUST ferry the dispatch token from the incoming command context to the host")
}

func TestPluginHostSettingsClientSetSettingFerriesDispatchTokenFromIncomingContext(t *testing.T) {
	srv := &settingsTestServer{}
	conn := startPluginHostServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: pluginv1.NewPluginHostServiceClient(conn)}

	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(emitTokenHeader, "dispatch-token-set"),
	)

	err := client.SetSetting(ctx, SettingScopeGame, "01PRINCIPAL", "content_warnings", []string{"x"})

	require.NoError(t, err)
	assert.Equal(t, "dispatch-token-set", srv.gotToken,
		"SetSetting MUST ferry the dispatch token from the incoming command context to the host")
}

func TestToProtoSettingScope(t *testing.T) {
	tests := []struct {
		name string
		in   SettingScope
		want pluginv1.SettingScope
	}{
		{"maps game scope", SettingScopeGame, pluginv1.SettingScope_SETTING_SCOPE_GAME},
		{"maps player scope", SettingScopePlayer, pluginv1.SettingScope_SETTING_SCOPE_PLAYER},
		{"maps character scope", SettingScopeCharacter, pluginv1.SettingScope_SETTING_SCOPE_CHARACTER},
		{"maps unspecified scope to unspecified", SettingScopeUnspecified, pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED},
		{"maps unknown scope to unspecified", SettingScope(99), pluginv1.SettingScope_SETTING_SCOPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toProtoSettingScope(tt.in))
		})
	}
}
