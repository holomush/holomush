// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

type settingsTestServer struct {
	hostv1.UnimplementedSettingsServiceServer
	gotScope       hostv1.SettingScope
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

func (s *settingsTestServer) GetSetting(ctx context.Context, req *hostv1.GetSettingRequest) (*hostv1.GetSettingResponse, error) {
	s.gotScope = req.GetScope()
	s.gotPrincipalID = req.GetPrincipalId()
	s.gotKey = req.GetKey()
	s.recordToken(ctx)
	return &hostv1.GetSettingResponse{
		Found:      s.respFound,
		StringList: s.respStringList,
	}, nil
}

func (s *settingsTestServer) SetSetting(ctx context.Context, req *hostv1.SetSettingRequest) (*hostv1.SetSettingResponse, error) {
	s.gotScope = req.GetScope()
	s.gotPrincipalID = req.GetPrincipalId()
	s.gotKey = req.GetKey()
	s.gotStringList = req.GetStringList()
	s.recordToken(ctx)
	return &hostv1.SetSettingResponse{}, nil
}

// startSettingsServiceTestServer starts an in-process gRPC server that
// registers SettingsService from the given test double. The returned
// *grpc.ClientConn is ready to use and cleaned up via t.Cleanup.
func startSettingsServiceTestServer(t *testing.T, srv *settingsTestServer) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- bufconn test server
	hostv1.RegisterSettingsServiceServer(server, srv)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient(
		"passthrough:///settings-service-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection -- bufconn test client
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestPluginHostSettingsClientGetSettingForwardsRequestAndMapsResponse(t *testing.T) {
	srv := &settingsTestServer{respFound: true, respStringList: []string{"warn-a", "warn-b"}}
	conn := startSettingsServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: hostv1.NewSettingsServiceClient(conn)}

	values, found, err := client.GetSetting(context.Background(), SettingScopeGame, "01PRINCIPAL", "content_warnings")

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []string{"warn-a", "warn-b"}, values)
	assert.Equal(t, hostv1.SettingScope_SETTING_SCOPE_GAME, srv.gotScope)
	assert.Equal(t, "01PRINCIPAL", srv.gotPrincipalID)
	assert.Equal(t, "content_warnings", srv.gotKey)
}

func TestPluginHostSettingsClientGetSettingReportsNotFoundWithEmptyValues(t *testing.T) {
	srv := &settingsTestServer{respFound: false}
	conn := startSettingsServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: hostv1.NewSettingsServiceClient(conn)}

	values, found, err := client.GetSetting(context.Background(), SettingScopeCharacter, "01CHAR", "content_warnings")

	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, values)
}

func TestPluginHostSettingsClientSetSettingForwardsRequestFields(t *testing.T) {
	srv := &settingsTestServer{}
	conn := startSettingsServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: hostv1.NewSettingsServiceClient(conn)}

	err := client.SetSetting(context.Background(), SettingScopePlayer, "01PLAYER", "content_warnings", []string{"violence", "gore"})

	require.NoError(t, err)
	assert.Equal(t, hostv1.SettingScope_SETTING_SCOPE_PLAYER, srv.gotScope)
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
	conn := startSettingsServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: hostv1.NewSettingsServiceClient(conn)}

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
	conn := startSettingsServiceTestServer(t, srv)
	client := &pluginHostSettingsClient{client: hostv1.NewSettingsServiceClient(conn)}

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
		want hostv1.SettingScope
	}{
		{"maps game scope", SettingScopeGame, hostv1.SettingScope_SETTING_SCOPE_GAME},
		{"maps player scope", SettingScopePlayer, hostv1.SettingScope_SETTING_SCOPE_PLAYER},
		{"maps character scope", SettingScopeCharacter, hostv1.SettingScope_SETTING_SCOPE_CHARACTER},
		{"maps unspecified scope to unspecified", SettingScopeUnspecified, hostv1.SettingScope_SETTING_SCOPE_UNSPECIFIED},
		{"maps unknown scope to unspecified", SettingScope(99), hostv1.SettingScope_SETTING_SCOPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toProtoSettingScope(tt.in))
		})
	}
}
