// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package grpc provides gRPC client and server implementations for HoloMUSH.
package grpc

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/samber/oops"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
)

// Client wraps a gRPC connection to the Core service.
type Client struct {
	conn              *grpc.ClientConn
	client            corev1.CoreServiceClient
	contentClient     contentv1.ContentServiceClient
	sceneAccessClient sceneaccessv1.SceneAccessServiceClient
}

// ClientConfig holds configuration for the gRPC client.
type ClientConfig struct {
	// Address is the target gRPC server address (e.g., "localhost:9000")
	Address string

	// TLSConfig for mTLS authentication. If nil, insecure connection is used.
	TLSConfig *tls.Config

	// KeepaliveTime is how often to ping the server (default: 10s)
	KeepaliveTime time.Duration

	// KeepaliveTimeout is how long to wait for ping response (default: 5s)
	KeepaliveTimeout time.Duration
}

// NewClient creates a new gRPC client connected to the Core service.
// The context parameter is reserved for future use (e.g., connection timeouts).
func NewClient(_ context.Context, cfg ClientConfig) (*Client, error) {
	if cfg.Address == "" {
		return nil, oops.Code("INVALID_CONFIG").Errorf("address is required")
	}

	// Set defaults
	if cfg.KeepaliveTime == 0 {
		cfg.KeepaliveTime = 30 * time.Second
	}
	if cfg.KeepaliveTimeout == 0 {
		cfg.KeepaliveTimeout = 5 * time.Second
	}

	// Build dial options
	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                cfg.KeepaliveTime,
			Timeout:             cfg.KeepaliveTimeout,
			PermitWithoutStream: true,
		}),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}

	// Configure TLS
	if cfg.TLSConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(cfg.TLSConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials())) // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection
	}

	// Create client connection
	conn, err := grpc.NewClient(cfg.Address, opts...)
	if err != nil {
		return nil, oops.Code("CONNECTION_FAILED").With("address", cfg.Address).Wrap(err)
	}

	return &Client{
		conn:              conn,
		client:            corev1.NewCoreServiceClient(conn),
		contentClient:     contentv1.NewContentServiceClient(conn),
		sceneAccessClient: sceneaccessv1.NewSceneAccessServiceClient(conn),
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return oops.Code("CLOSE_FAILED").Wrap(err)
		}
	}
	return nil
}

// HandleCommand processes a game command.
func (c *Client) HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	resp, err := c.client.HandleCommand(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "HandleCommand").Wrap(err)
	}
	return resp, nil
}

// TranslateSubscribeErr converts a gRPC status error from the Subscribe RPC
// (either the synchronous dispatch error or a stream.Recv() error) into an
// oops-coded error the gateways can classify. The server collapses every
// session-ownership failure mode to SESSION_NOT_FOUND (enumeration-safe,
// I-SEC-1) and stamps it with a wire code via subscribeSessionNotFound, so it
// crosses the wire as codes.Unauthenticated (or codes.NotFound); either maps
// back to the SESSION_NOT_FOUND oops code so the gateway reconnect loops (web
// runSubscribeOnce / telnet resubscribe) can treat a reaped session as terminal
// rather than retrying for the full reconnect ceiling. Everything else stays
// RPC_FAILED (transient → reconnect).
//
// Exported so the telnet gateway can apply the SAME classification to the
// stream.Recv() error it observes directly: grpc-go's server-streaming dispatch
// returns (nonNilStream, nil) from Subscribe and defers the handler error to the
// first Recv, bypassing the Client.Subscribe wrapper below.
func TranslateSubscribeErr(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return oops.Code("RPC_FAILED").With("method", "Subscribe").Wrap(err)
	}
	switch st.Code() {
	case codes.NotFound, codes.Unauthenticated:
		return oops.Code("SESSION_NOT_FOUND").Wrap(err)
	default:
		return oops.Code("RPC_FAILED").With("method", "Subscribe").Wrap(err)
	}
}

// Subscribe opens a stream of events for the session.
func (c *Client) Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
	stream, err := c.client.Subscribe(ctx, req)
	if err != nil {
		return nil, TranslateSubscribeErr(err)
	}
	return stream, nil
}

// Disconnect ends a session.
func (c *Client) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	resp, err := c.client.Disconnect(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "Disconnect").Wrap(err)
	}
	return resp, nil
}

// GetCommandHistory retrieves command history for a session.
func (c *Client) GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	resp, err := c.client.GetCommandHistory(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "GetCommandHistory").Wrap(err)
	}
	return resp, nil
}

// AuthenticatePlayer validates credentials and returns a player token.
func (c *Client) AuthenticatePlayer(ctx context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
	resp, err := c.client.AuthenticatePlayer(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "AuthenticatePlayer").Wrap(err)
	}
	return resp, nil
}

// SelectCharacter selects a character and creates or reattaches a game session.
func (c *Client) SelectCharacter(ctx context.Context, req *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
	resp, err := c.client.SelectCharacter(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "SelectCharacter").Wrap(err)
	}
	return resp, nil
}

// CreatePlayer creates a new player account.
func (c *Client) CreatePlayer(ctx context.Context, req *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
	resp, err := c.client.CreatePlayer(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "CreatePlayer").Wrap(err)
	}
	return resp, nil
}

// CreateCharacter creates a new character for an authenticated player.
func (c *Client) CreateCharacter(ctx context.Context, req *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error) {
	resp, err := c.client.CreateCharacter(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "CreateCharacter").Wrap(err)
	}
	return resp, nil
}

// ListCharacters lists characters for an authenticated player.
func (c *Client) ListCharacters(ctx context.Context, req *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error) {
	resp, err := c.client.ListCharacters(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListCharacters").Wrap(err)
	}
	return resp, nil
}

// ListAllCharacters delegates to CoreService.ListAllCharacters (directory).
func (c *Client) ListAllCharacters(ctx context.Context, req *corev1.ListAllCharactersRequest) (*corev1.ListAllCharactersResponse, error) {
	resp, err := c.client.ListAllCharacters(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListAllCharacters").Wrap(err)
	}
	return resp, nil
}

// RequestPasswordReset requests a password reset.
func (c *Client) RequestPasswordReset(ctx context.Context, req *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	resp, err := c.client.RequestPasswordReset(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "RequestPasswordReset").Wrap(err)
	}
	return resp, nil
}

// ConfirmPasswordReset confirms a password reset with a token.
func (c *Client) ConfirmPasswordReset(ctx context.Context, req *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error) {
	resp, err := c.client.ConfirmPasswordReset(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ConfirmPasswordReset").Wrap(err)
	}
	return resp, nil
}

// Logout ends a web session.
func (c *Client) Logout(ctx context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	resp, err := c.client.Logout(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "Logout").Wrap(err)
	}
	return resp, nil
}

// translateCheckPlayerSessionErr re-injects an oops auth-failure code on
// codes.Unauthenticated so callers (e.g. the gateway's cookie-collision
// gate predicate) can distinguish "cookie invalid" from genuine
// transport/lookup failures. All other errors collapse to RPC_FAILED so
// transport problems are not mistaken for legitimate auth failures.
//
// The server collapses three distinct codes (PLAYER_SESSION_NOT_FOUND,
// PLAYER_SESSION_EXPIRED, SESSION_NOT_FOUND) into a single codes.Unauthenticated
// status. The original message body still carries the granular code text
// for log diagnostics.
func translateCheckPlayerSessionErr(err error) error {
	if statusErr, ok := status.FromError(err); ok && statusErr.Code() == codes.Unauthenticated {
		return oops.Code("PLAYER_SESSION_NOT_FOUND").
			With("method", "CheckPlayerSession").
			Errorf("%s", statusErr.Message())
	}
	return oops.Code("RPC_FAILED").With("method", "CheckPlayerSession").Wrap(err)
}

// CheckPlayerSession validates a player session token.
func (c *Client) CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error) {
	resp, err := c.client.CheckPlayerSession(ctx, req)
	if err != nil {
		return nil, translateCheckPlayerSessionErr(err)
	}
	return resp, nil
}

// CreateGuest creates an ephemeral guest player and character.
func (c *Client) CreateGuest(ctx context.Context, req *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error) {
	resp, err := c.client.CreateGuest(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "CreateGuest").Wrap(err)
	}
	return resp, nil
}

// QueryStreamHistory reads paginated event history from a stream.
func (c *Client) QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error) {
	resp, err := c.client.QueryStreamHistory(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "QueryStreamHistory").Wrap(err)
	}
	return resp, nil
}

// ListSessionStreams returns the set of streams the session is subscribed to.
func (c *Client) ListSessionStreams(ctx context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error) {
	resp, err := c.client.ListSessionStreams(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListSessionStreams").Wrap(err)
	}
	return resp, nil
}

// ListPlayerSessions returns the caller's active PlayerSessions.
func (c *Client) ListPlayerSessions(ctx context.Context, req *corev1.ListPlayerSessionsRequest) (*corev1.ListPlayerSessionsResponse, error) {
	resp, err := c.client.ListPlayerSessions(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListPlayerSessions").Wrap(err)
	}
	return resp, nil
}

// RevokePlayerSession revokes a specific PlayerSession owned by the caller.
func (c *Client) RevokePlayerSession(ctx context.Context, req *corev1.RevokePlayerSessionRequest) (*corev1.RevokePlayerSessionResponse, error) {
	resp, err := c.client.RevokePlayerSession(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "RevokePlayerSession").Wrap(err)
	}
	return resp, nil
}

// RevokeOtherPlayerSessions revokes all PlayerSessions for the caller except
// the current one.
func (c *Client) RevokeOtherPlayerSessions(ctx context.Context, req *corev1.RevokeOtherPlayerSessionsRequest) (*corev1.RevokeOtherPlayerSessionsResponse, error) {
	resp, err := c.client.RevokeOtherPlayerSessions(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "RevokeOtherPlayerSessions").Wrap(err)
	}
	return resp, nil
}

// ListFocusPresence returns the presence entries for the session's focus context.
func (c *Client) ListFocusPresence(ctx context.Context, req *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error) {
	resp, err := c.client.ListFocusPresence(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListFocusPresence").Wrap(err)
	}
	return resp, nil
}

// ListAvailableCommands returns the ABAC-filtered command set for the session's character.
func (c *Client) ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error) {
	resp, err := c.client.ListAvailableCommands(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListAvailableCommands").Wrap(err)
	}
	return resp, nil
}

// RefreshConnection bumps the liveness lease for a connection (I-LIVE-1).
func (c *Client) RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error) {
	resp, err := c.client.RefreshConnection(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "RefreshConnection").Wrap(err)
	}
	return resp, nil
}

// GetContent retrieves a single content item by key from the content service.
func (c *Client) GetContent(ctx context.Context, req *contentv1.GetContentRequest) (*contentv1.GetContentResponse, error) {
	resp, err := c.contentClient.GetContent(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "GetContent").Wrap(err)
	}
	return resp, nil
}

// ListContent returns content items matching a key prefix from the content service.
func (c *Client) ListContent(ctx context.Context, req *contentv1.ListContentRequest) (*contentv1.ListContentResponse, error) {
	resp, err := c.contentClient.ListContent(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListContent").Wrap(err)
	}
	return resp, nil
}

// CoreClient returns the underlying gRPC CoreClient interface for advanced usage.
func (c *Client) CoreClient() corev1.CoreServiceClient {
	return c.client
}

// ListScenesForViewer returns the public scene board filtered by the player's preferences.
func (c *Client) ListScenesForViewer(ctx context.Context, req *sceneaccessv1.ListScenesForViewerRequest) (*sceneaccessv1.ListScenesForViewerResponse, error) {
	resp, err := c.sceneAccessClient.ListScenesForViewer(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListScenesForViewer").Wrap(err)
	}
	return resp, nil
}

// GetSceneForViewer loads one scene's metadata for the verified player's owned character.
func (c *Client) GetSceneForViewer(ctx context.Context, req *sceneaccessv1.GetSceneForViewerRequest) (*sceneaccessv1.GetSceneForViewerResponse, error) {
	resp, err := c.sceneAccessClient.GetSceneForViewer(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "GetSceneForViewer").Wrap(err)
	}
	return resp, nil
}

// ListMyScenes returns every non-archived scene the verified player's owned character participates in.
func (c *Client) ListMyScenes(ctx context.Context, req *sceneaccessv1.ListMyScenesRequest) (*sceneaccessv1.ListMyScenesResponse, error) {
	resp, err := c.sceneAccessClient.ListMyScenes(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListMyScenes").Wrap(err)
	}
	return resp, nil
}

// WatchScene auto-joins the verified player's owned character into an open active scene as an observer.
func (c *Client) WatchScene(ctx context.Context, req *sceneaccessv1.WatchSceneRequest) (*sceneaccessv1.WatchSceneResponse, error) {
	resp, err := c.sceneAccessClient.WatchScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "WatchScene").Wrap(err)
	}
	return resp, nil
}

// CreateScene creates a new scene owned by the verified player's character, delegating
// to SceneAccessService which validates identity and forwards to the plugin SceneService.
func (c *Client) CreateScene(ctx context.Context, req *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error) {
	resp, err := c.sceneAccessClient.CreateScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "CreateScene").Wrap(err)
	}
	return resp, nil
}

// EndScene delegates to SceneAccessService.EndScene (identity-resolving facade).
func (c *Client) EndScene(ctx context.Context, req *sceneaccessv1.EndSceneRequest) (*sceneaccessv1.EndSceneResponse, error) {
	resp, err := c.sceneAccessClient.EndScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "EndScene").Wrap(err)
	}
	return resp, nil
}

// PauseScene delegates to SceneAccessService.PauseScene (identity-resolving facade).
func (c *Client) PauseScene(ctx context.Context, req *sceneaccessv1.PauseSceneRequest) (*sceneaccessv1.PauseSceneResponse, error) {
	resp, err := c.sceneAccessClient.PauseScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "PauseScene").Wrap(err)
	}
	return resp, nil
}

// ResumeScene delegates to SceneAccessService.ResumeScene (identity-resolving facade).
func (c *Client) ResumeScene(ctx context.Context, req *sceneaccessv1.ResumeSceneRequest) (*sceneaccessv1.ResumeSceneResponse, error) {
	resp, err := c.sceneAccessClient.ResumeScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ResumeScene").Wrap(err)
	}
	return resp, nil
}

// MuteScene delegates to SceneAccessService.MuteScene (identity-resolving facade).
func (c *Client) MuteScene(ctx context.Context, req *sceneaccessv1.MuteSceneRequest) (*sceneaccessv1.MuteSceneResponse, error) {
	resp, err := c.sceneAccessClient.MuteScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "MuteScene").Wrap(err)
	}
	return resp, nil
}

// SetSceneNotifyPref delegates to SceneAccessService.SetSceneNotifyPref (identity-resolving facade).
func (c *Client) SetSceneNotifyPref(ctx context.Context, req *sceneaccessv1.SetSceneNotifyPrefRequest) (*sceneaccessv1.SetSceneNotifyPrefResponse, error) {
	resp, err := c.sceneAccessClient.SetSceneNotifyPref(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "SetSceneNotifyPref").Wrap(err)
	}
	return resp, nil
}

// UpdateScene delegates to SceneAccessService.UpdateScene (identity-resolving facade).
func (c *Client) UpdateScene(ctx context.Context, req *sceneaccessv1.UpdateSceneRequest) (*sceneaccessv1.UpdateSceneResponse, error) {
	resp, err := c.sceneAccessClient.UpdateScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "UpdateScene").Wrap(err)
	}
	return resp, nil
}

// ExportScene renders the verified player's owned character's scene IC log to a downloadable document.
func (c *Client) ExportScene(ctx context.Context, req *sceneaccessv1.ExportSceneRequest) (*sceneaccessv1.ExportSceneResponse, error) {
	resp, err := c.sceneAccessClient.ExportScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ExportScene").Wrap(err)
	}
	return resp, nil
}

// SetSceneFocus sets the per-connection scene focus for the verified player's character.
func (c *Client) SetSceneFocus(ctx context.Context, req *sceneaccessv1.SetSceneFocusRequest) (*sceneaccessv1.SetSceneFocusResponse, error) {
	resp, err := c.sceneAccessClient.SetSceneFocus(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "SetSceneFocus").Wrap(err)
	}
	return resp, nil
}

// ListPublishedScenes pages through publicly visible PUBLISHED scene archives.
func (c *Client) ListPublishedScenes(ctx context.Context, req *sceneaccessv1.ListPublishedScenesRequest) (*sceneaccessv1.ListPublishedScenesResponse, error) {
	resp, err := c.sceneAccessClient.ListPublishedScenes(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "ListPublishedScenes").Wrap(err)
	}
	return resp, nil
}

// GetPublicSceneArchive reads a published scene archive without participant authentication.
func (c *Client) GetPublicSceneArchive(ctx context.Context, req *sceneaccessv1.GetPublicSceneArchiveRequest) (*sceneaccessv1.GetPublicSceneArchiveResponse, error) {
	resp, err := c.sceneAccessClient.GetPublicSceneArchive(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "GetPublicSceneArchive").Wrap(err)
	}
	return resp, nil
}

// DownloadPublicSceneArchive returns a PUBLISHED scene archive rendered in the requested format.
func (c *Client) DownloadPublicSceneArchive(ctx context.Context, req *sceneaccessv1.DownloadPublicSceneArchiveRequest) (*sceneaccessv1.DownloadPublicSceneArchiveResponse, error) {
	resp, err := c.sceneAccessClient.DownloadPublicSceneArchive(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "DownloadPublicSceneArchive").Wrap(err)
	}
	return resp, nil
}

// InviteToScene delegates to SceneAccessService.InviteToScene (identity-resolving facade).
func (c *Client) InviteToScene(ctx context.Context, req *sceneaccessv1.InviteToSceneRequest) (*sceneaccessv1.InviteToSceneResponse, error) {
	resp, err := c.sceneAccessClient.InviteToScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "InviteToScene").Wrap(err)
	}
	return resp, nil
}

// KickFromScene delegates to SceneAccessService.KickFromScene (identity-resolving facade).
func (c *Client) KickFromScene(ctx context.Context, req *sceneaccessv1.KickFromSceneRequest) (*sceneaccessv1.KickFromSceneResponse, error) {
	resp, err := c.sceneAccessClient.KickFromScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "KickFromScene").Wrap(err)
	}
	return resp, nil
}

// TransferOwnership delegates to SceneAccessService.TransferOwnership (identity-resolving facade).
func (c *Client) TransferOwnership(ctx context.Context, req *sceneaccessv1.TransferOwnershipRequest) (*sceneaccessv1.TransferOwnershipResponse, error) {
	resp, err := c.sceneAccessClient.TransferOwnership(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "TransferOwnership").Wrap(err)
	}
	return resp, nil
}

// LeaveScene delegates to SceneAccessService.LeaveScene (identity-resolving facade).
func (c *Client) LeaveScene(ctx context.Context, req *sceneaccessv1.LeaveSceneRequest) (*sceneaccessv1.LeaveSceneResponse, error) {
	resp, err := c.sceneAccessClient.LeaveScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "LeaveScene").Wrap(err)
	}
	return resp, nil
}

// StartScenePublish delegates to SceneAccessService.StartScenePublish.
func (c *Client) StartScenePublish(ctx context.Context, req *sceneaccessv1.StartScenePublishRequest) (*sceneaccessv1.StartScenePublishResponse, error) {
	resp, err := c.sceneAccessClient.StartScenePublish(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "StartScenePublish").Wrap(err)
	}
	return resp, nil
}

// CastPublishSceneVote delegates to SceneAccessService.CastPublishSceneVote.
func (c *Client) CastPublishSceneVote(ctx context.Context, req *sceneaccessv1.CastPublishSceneVoteRequest) (*sceneaccessv1.CastPublishSceneVoteResponse, error) {
	resp, err := c.sceneAccessClient.CastPublishSceneVote(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "CastPublishSceneVote").Wrap(err)
	}
	return resp, nil
}

// WithdrawScenePublish delegates to SceneAccessService.WithdrawScenePublish.
func (c *Client) WithdrawScenePublish(ctx context.Context, req *sceneaccessv1.WithdrawScenePublishRequest) (*sceneaccessv1.WithdrawScenePublishResponse, error) {
	resp, err := c.sceneAccessClient.WithdrawScenePublish(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "WithdrawScenePublish").Wrap(err)
	}
	return resp, nil
}

// GetPublishedScene delegates to SceneAccessService.GetPublishedScene.
func (c *Client) GetPublishedScene(ctx context.Context, req *sceneaccessv1.GetPublishedSceneRequest) (*sceneaccessv1.GetPublishedSceneResponse, error) {
	resp, err := c.sceneAccessClient.GetPublishedScene(ctx, req)
	if err != nil {
		return nil, oops.Code("RPC_FAILED").With("method", "GetPublishedScene").Wrap(err)
	}
	return resp, nil
}
