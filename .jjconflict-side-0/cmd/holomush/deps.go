// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	cryptotls "crypto/tls"
	"net"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/holomush/holomush/internal/control"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/observability"
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

	// EventStoreFactory creates an event store from a database URL.
	// Default: store.NewPostgresEventStore
	EventStoreFactory func(ctx context.Context, url string) (EventStore, error)

	// TLSCertEnsurer generates or loads TLS certificates.
	// Default: ensureTLSCerts
	TLSCertEnsurer func(certsDir, gameID string) (*cryptotls.Config, error)

	// DatabaseURLGetter returns the database URL.
	// Default: reads from DATABASE_URL environment variable
	DatabaseURLGetter func() string

	// MigratorFactory creates a database migrator.
	// Default: store.NewMigrator
	MigratorFactory func(databaseURL string) (AutoMigrator, error)

	// AutoMigrateGetter returns whether auto-migration is enabled.
	// Default: parseAutoMigrate (reads HOLOMUSH_DB_AUTO_MIGRATE env var)
	AutoMigrateGetter func() bool

	// PolicyBootstrapper seeds policies and creates audit log partitions.
	// Default: extracts pool from event store, runs policy.Bootstrap().
	// Fatal if nil and event store does not expose a connection pool (ADR #92).
	// Tests MUST set this to a no-op to avoid requiring a real database.
	PolicyBootstrapper func(ctx context.Context, skipSeedMigrations bool) error
}

// GatewayDeps contains injectable dependencies for the gateway command.
// All fields with nil values will use their default implementations.
type GatewayDeps struct {
	CommonDeps

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

// EventStore interface wraps the methods used by core from store.PostgresEventStore.
type EventStore interface {
	Close()
	InitGameID(ctx context.Context) (string, error)
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
	Close() error
}

// AutoMigrator is the minimal interface for startup auto-migration.
type AutoMigrator interface {
	Up() error
	Close() error
}
