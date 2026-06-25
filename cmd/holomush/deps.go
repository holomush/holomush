// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	cryptotls "crypto/tls"
	"net"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/holomush/holomush/internal/bootstrap"
	"github.com/holomush/holomush/internal/control"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/xdg"
	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
)

// CommonDeps contains injectable dependencies shared by multiple commands.
// All fields with nil values will use their default implementations.
type CommonDeps struct {
	// CertsDirGetter returns the certificates directory path.
	// Default: xdg.CertsDir
	CertsDirGetter func() (string, error)

	// ControlTLSLoader loads TLS config for the control gRPC server.
	// Default: control.LoadControlServerTLS
	ControlTLSLoader func(certsDir, component string) (*cryptotls.Config, error)

	// ControlServerFactory creates a control gRPC server.
	// Default: control.NewGRPCServer
	ControlServerFactory func(component string, shutdownFunc control.ShutdownFunc) (ControlServer, error)

	// ObservabilityServerFactory creates an observability server.
	// Default: observability.NewServer
	ObservabilityServerFactory func(addr string, readinessChecker observability.ReadinessChecker) ObservabilityServer
}

// CoreDeps contains injectable dependencies for the core command.
// All fields with nil values will use their default implementations.
type CoreDeps struct {
	CommonDeps

	// TLSCertEnsurer generates or loads TLS certificates.
	// Default: ensureTLSCerts
	TLSCertEnsurer func(certsDir, gameID string) (*cryptotls.Config, error)

	// DatabaseURLGetter returns the database URL.
	// Default: reads from DATABASE_URL environment variable
	DatabaseURLGetter func() string

	// MigratorFactory creates a database migrator.
	// Default: store.NewMigrator
	MigratorFactory func(databaseURL string) (bootstrap.AutoMigrator, error)

	// AutoMigrateGetter returns whether auto-migration is enabled.
	// Default: parseAutoMigrate (reads HOLOMUSH_DB_AUTO_MIGRATE env var)
	AutoMigrateGetter func() bool
}

// applyDefaults fills nil fields with their default implementations.
func (d *CoreDeps) applyDefaults() {
	if d.TLSCertEnsurer == nil {
		d.TLSCertEnsurer = ensureTLSCerts
	}
	if d.ControlTLSLoader == nil {
		d.ControlTLSLoader = control.LoadControlServerTLS
	}
	if d.ControlServerFactory == nil {
		d.ControlServerFactory = func(component string, shutdownFunc control.ShutdownFunc) (ControlServer, error) {
			return control.NewGRPCServer(component, shutdownFunc)
		}
	}
	if d.ObservabilityServerFactory == nil {
		d.ObservabilityServerFactory = func(addr string, readinessChecker observability.ReadinessChecker) ObservabilityServer {
			return observability.NewServer(addr, readinessChecker)
		}
	}
	if d.CertsDirGetter == nil {
		d.CertsDirGetter = xdg.CertsDir
	}
	if d.DatabaseURLGetter == nil {
		d.DatabaseURLGetter = func() string {
			return os.Getenv("DATABASE_URL")
		}
	}
	if d.MigratorFactory == nil {
		d.MigratorFactory = func(url string) (bootstrap.AutoMigrator, error) {
			return store.NewMigrator(url)
		}
	}
	if d.AutoMigrateGetter == nil {
		d.AutoMigrateGetter = parseAutoMigrate
	}
}

// GatewayDeps contains injectable dependencies for the gateway command.
// All fields with nil values will use their default implementations.
type GatewayDeps struct {
	CommonDeps

	// CertPollTimeout is the maximum time to wait for TLS certificates to
	// become available. The gateway polls for certs on startup, allowing it
	// to start before the core process has generated them.
	// Default: 30s (defaultCertPollTimeout)
	CertPollTimeout time.Duration

	// GameIDExtractor extracts game ID from CA certificate.
	// Default: control.ExtractGameIDFromCA
	GameIDExtractor func(certsDir string) (string, error)

	// ClientTLSLoader loads TLS config for the gRPC client.
	// Default: tls.LoadClientTLS
	ClientTLSLoader func(certsDir, clientName, gameID string) (*cryptotls.Config, error)

	// GRPCClientFactory creates a gRPC client to the core service.
	// Default: holoGRPC.NewClient
	GRPCClientFactory func(ctx context.Context, cfg holoGRPC.ClientConfig) (GRPCClient, error)

	// ListenerFactory creates a network listener.
	// Default: net.Listen
	ListenerFactory func(network, address string) (net.Listener, error)
}

// ControlServer interface wraps the methods used from control.GRPCServer.
type ControlServer interface {
	Start(addr string, tlsConfig *cryptotls.Config) (<-chan error, error)
	Stop(ctx context.Context) error
}

// ObservabilityServer interface wraps the methods used from observability.Server.
type ObservabilityServer interface {
	Start() (<-chan error, error)
	Stop(ctx context.Context) error
	Addr() string
	MustRegister(cs ...prometheus.Collector)
}

// GRPCClient interface wraps the methods used from holoGRPC.Client.
type GRPCClient interface {
	HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error)
	Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error)
	Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
	GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error)
	// Auth RPCs (two-phase login)
	AuthenticatePlayer(ctx context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error)
	SelectCharacter(ctx context.Context, req *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error)
	CreatePlayer(ctx context.Context, req *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error)
	CreateCharacter(ctx context.Context, req *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error)
	ListCharacters(ctx context.Context, req *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error)
	RequestPasswordReset(ctx context.Context, req *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error)
	ConfirmPasswordReset(ctx context.Context, req *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error)
	Logout(ctx context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error)
	CheckPlayerSession(ctx context.Context, req *corev1.CheckPlayerSessionRequest) (*corev1.CheckPlayerSessionResponse, error)
	CreateGuest(ctx context.Context, req *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error)
	QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error)
	ListSessionStreams(ctx context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error)
	// Session management RPCs
	ListPlayerSessions(ctx context.Context, req *corev1.ListPlayerSessionsRequest) (*corev1.ListPlayerSessionsResponse, error)
	RevokePlayerSession(ctx context.Context, req *corev1.RevokePlayerSessionRequest) (*corev1.RevokePlayerSessionResponse, error)
	RevokeOtherPlayerSessions(ctx context.Context, req *corev1.RevokeOtherPlayerSessionsRequest) (*corev1.RevokeOtherPlayerSessionsResponse, error)
	// Presence RPCs
	ListFocusPresence(ctx context.Context, req *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error)
	// Command introspection
	ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error)
	// Liveness RPCs
	RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error)
	// Content RPCs
	GetContent(ctx context.Context, req *contentv1.GetContentRequest) (*contentv1.GetContentResponse, error)
	ListContent(ctx context.Context, req *contentv1.ListContentRequest) (*contentv1.ListContentResponse, error)
	// Scene access RPCs (facade)
	ListScenesForViewer(ctx context.Context, req *sceneaccessv1.ListScenesForViewerRequest) (*sceneaccessv1.ListScenesForViewerResponse, error)
	GetSceneForViewer(ctx context.Context, req *sceneaccessv1.GetSceneForViewerRequest) (*sceneaccessv1.GetSceneForViewerResponse, error)
	ListMyScenes(ctx context.Context, req *sceneaccessv1.ListMyScenesRequest) (*sceneaccessv1.ListMyScenesResponse, error)
	WatchScene(ctx context.Context, req *sceneaccessv1.WatchSceneRequest) (*sceneaccessv1.WatchSceneResponse, error)
	CreateScene(ctx context.Context, req *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error)
	EndScene(ctx context.Context, req *sceneaccessv1.EndSceneRequest) (*sceneaccessv1.EndSceneResponse, error)
	PauseScene(ctx context.Context, req *sceneaccessv1.PauseSceneRequest) (*sceneaccessv1.PauseSceneResponse, error)
	ResumeScene(ctx context.Context, req *sceneaccessv1.ResumeSceneRequest) (*sceneaccessv1.ResumeSceneResponse, error)
	ExportScene(ctx context.Context, req *sceneaccessv1.ExportSceneRequest) (*sceneaccessv1.ExportSceneResponse, error)
	SetSceneFocus(ctx context.Context, req *sceneaccessv1.SetSceneFocusRequest) (*sceneaccessv1.SetSceneFocusResponse, error)
	ListPublishedScenes(ctx context.Context, req *sceneaccessv1.ListPublishedScenesRequest) (*sceneaccessv1.ListPublishedScenesResponse, error)
	GetPublicSceneArchive(ctx context.Context, req *sceneaccessv1.GetPublicSceneArchiveRequest) (*sceneaccessv1.GetPublicSceneArchiveResponse, error)
	DownloadPublicSceneArchive(ctx context.Context, req *sceneaccessv1.DownloadPublicSceneArchiveRequest) (*sceneaccessv1.DownloadPublicSceneArchiveResponse, error)
	Close() error
}
