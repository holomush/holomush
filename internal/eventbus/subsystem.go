// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	natsexporter "github.com/nats-io/prometheus-nats-exporter/exporter"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/telemetry"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/xdg"
)

// StreamName is the single JetStream stream that holds all events.
const StreamName = "EVENTS"

// SubjectFilter is the stream subject filter — every event lands here.
const SubjectFilter = "events.>"

// embeddedServerName is the NATS server name used for the embedded instance.
// It appears in server logs and monitoring output.
const embeddedServerName = "holomush-embedded"

// embeddedClientName identifies the host connection on the embedded server.
const embeddedClientName = "holomush-host"

// readyTimeout bounds how long Start waits for the embedded NATS server to
// become ready for in-process connections.
const readyTimeout = 10 * time.Second

// startExporterFn is the seam Start() uses to launch the NATS exporter.
// In production it wraps telemetry.StartNATSExporter. Tests can override
// it to exercise the exporter-start-failure rollback branch without
// needing a real exporter port conflict.
var startExporterFn = telemetry.StartNATSExporter

// Subsystem wraps an embedded NATS server and exposes a JetStream context.
//
// Phase A invariant: the subsystem runs in production but has no publishers,
// subscribers, or consumers attached. Phase B will attach them. Stream
// declaration is idempotent so repeated Start calls (or future cluster-mode
// bootstrap) do not conflict.
type Subsystem struct {
	cfg      Config
	server   *server.Server
	conn     *nats.Conn
	js       jetstream.JetStream
	storage  jetstream.StorageType
	exporter *natsexporter.NATSExporter
}

// NewSubsystem constructs the subsystem from a Config.
// FileStorage is the default; tests override via NewSubsystemWithStorage.
func NewSubsystem(cfg Config) *Subsystem {
	return NewSubsystemWithStorage(cfg, jetstream.FileStorage)
}

// NewSubsystemWithStorage allows tests to use MemoryStorage for speed.
func NewSubsystemWithStorage(cfg Config, storage jetstream.StorageType) *Subsystem {
	return &Subsystem{cfg: cfg.Defaults(), storage: storage}
}

// ID returns lifecycle.SubsystemEventBus.
func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemEventBus }

// DependsOn returns nil — the event bus has no subsystem dependencies.
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID { return nil }

// Start establishes the NATS transport for the configured mode, obtains a
// JetStream context, and declares the EVENTS stream (D-03 provision policy).
// In embedded mode it brings the in-process NATS server up; in external mode
// (CLUSTER-01) it dials the configured cluster with creds/TLS and fails closed
// at boot if unreachable (D-02). Any failure mid-way rolls back earlier state
// (shutdown server if any, close conn).
// codecov:ignore — rollback branches exercised by integration and E2E tests
func (s *Subsystem) Start(ctx context.Context) error {
	if s.conn != nil {
		return nil // already started (embedded or external)
	}

	conn, js, err := s.connect()
	if err != nil {
		return err
	}
	s.conn = conn
	s.js = js

	if err := s.EnsureStream(ctx); err != nil {
		s.rollbackStart(conn)
		return err
	}

	// Optional Prometheus exporter. Embedded-only (OQ-7): it scrapes the
	// embedded NATS server's HTTP monitor endpoint via server.MonitorAddr(),
	// which does not exist in external mode (s.server is nil — the external
	// cluster exposes its own /varz that the runbook points at). Guarded by
	// exporterEnabled so an external deployment with PrometheusExporter=true
	// never dereferences the nil server.
	if s.exporterEnabled() {
		monitorAddr := s.server.MonitorAddr()
		if monitorAddr == nil || monitorAddr.Port <= 0 {
			s.rollbackStart(conn)
			return oops.Code("EVENTBUS_EXPORTER_MONITOR_UNBOUND").
				Errorf("PrometheusExporter requires MonitorPort > 0 to bind the NATS HTTP monitor")
		}
		exp, expErr := startExporterFn(monitorAddr.Port, s.cfg.ExporterPort)
		if expErr != nil {
			s.rollbackStart(conn)
			return oops.Code("EVENTBUS_EXPORTER_START_FAILED").
				With("monitor_port", monitorAddr.Port).
				Wrap(expErr)
		}
		s.exporter = exp
	}
	return nil
}

// connect establishes the mode-dependent NATS transport and returns a
// connection plus JetStream context. The embedded case (default) brings up the
// in-process server as a side effect (s.server); the external case (CLUSTER-01)
// dials the configured cluster. Each case owns its own rollback on failure so a
// half-built transport never leaks. s.conn/s.js are set by Start, not here.
func (s *Subsystem) connect() (*nats.Conn, jetstream.JetStream, error) {
	if s.cfg.Mode == ModeExternal {
		return s.connectExternal()
	}
	return s.connectEmbedded()
}

// connectEmbedded brings up the in-process NATS server and opens an in-process
// connection. It sets and rolls back s.server internally; on success s.server
// is live and the returned conn/js are attached to it.
func (s *Subsystem) connectEmbedded() (*nats.Conn, jetstream.JetStream, error) {
	storeDir, err := s.resolveStoreDir()
	if err != nil {
		return nil, nil, err
	}

	opts := &server.Options{
		ServerName: embeddedServerName,
		JetStream:  true,
		StoreDir:   storeDir,
		DontListen: true,
		NoSigs:     true,
		LogtimeUTC: true,
		HTTPPort:   s.cfg.MonitorPort,
	}

	srv, err := server.NewServer(opts)
	if err != nil {
		return nil, nil, oops.Code("EVENTBUS_SERVER_NEW_FAILED").Wrap(err)
	}
	s.server = srv
	go s.server.Start()

	if !s.server.ReadyForConnections(readyTimeout) {
		s.server.Shutdown()
		// WaitForShutdown mirrors the other rollback branches so a retry
		// cannot race the previous server's filestore teardown.
		s.server.WaitForShutdown()
		s.server = nil
		return nil, nil, oops.Code("EVENTBUS_SERVER_NOT_READY").
			With("timeout", readyTimeout.String()).
			Errorf("embedded NATS server did not become ready")
	}

	conn, err := nats.Connect(
		"",
		nats.InProcessServer(s.server),
		nats.Name(embeddedClientName),
		// DrainTimeout bounds the drain phase in Stop. Pair with the
		// context-aware WaitForShutdown wait so Stop honors caller
		// deadlines even if NATS internal shutdown stalls.
		nats.DrainTimeout(readyTimeout),
	)
	if err != nil {
		s.server.Shutdown()
		s.server.WaitForShutdown()
		s.server = nil
		return nil, nil, oops.Code("EVENTBUS_CONNECT_FAILED").Wrap(err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		s.server.Shutdown()
		s.server.WaitForShutdown()
		s.server = nil
		return nil, nil, oops.Code("EVENTBUS_JETSTREAM_CTX_FAILED").Wrap(err)
	}
	return conn, js, nil
}

// connectExternal dials the external NATS cluster (CLUSTER-01) with creds/TLS
// and obtains a JetStream context. There is no embedded server in this mode:
// s.server stays nil and the exporter block is skipped (OQ-7). A dial failure
// is already coded EVENTBUS_EXTERNAL_CONNECT_FAILED by dialExternal (D-02).
func (s *Subsystem) connectExternal() (*nats.Conn, jetstream.JetStream, error) {
	conn, err := dialExternal(s.cfg)
	if err != nil {
		return nil, nil, err
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, nil, oops.Code("EVENTBUS_JETSTREAM_CTX_FAILED").Wrap(err)
	}
	return conn, js, nil
}

// exporterEnabled reports whether the embedded-only Prometheus exporter should
// start. External mode has no embedded server to scrape (OQ-7), so the exporter
// is embedded-only regardless of the PrometheusExporter flag — the external
// cluster exposes its own monitoring endpoint (runbook, D-16).
func (s *Subsystem) exporterEnabled() bool {
	return s.cfg.Mode == ModeEmbedded && s.cfg.PrometheusExporter
}

// rollbackStart unwinds a partially-started subsystem after a mid-Start
// failure: it closes the connection, nils the conn/js seams, and — in embedded
// mode — shuts the in-process server down so a retry cannot race the previous
// server's filestore teardown. In external mode s.server is nil, so only the
// connection is closed (the external cluster is not ours to shut down).
func (s *Subsystem) rollbackStart(conn *nats.Conn) {
	conn.Close()
	s.conn = nil
	s.js = nil
	if s.server != nil {
		s.server.Shutdown()
		s.server.WaitForShutdown()
		s.server = nil
	}
}

// EnsureStream creates or updates the EVENTS stream idempotently.
func (s *Subsystem) EnsureStream(ctx context.Context) error {
	if s.js == nil {
		return oops.Code("EVENTBUS_NOT_STARTED").Errorf("EnsureStream called before Start")
	}
	_, err := s.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        StreamName,
		Subjects:    []string{SubjectFilter},
		Retention:   jetstream.LimitsPolicy,
		Storage:     s.storage,
		Replicas:    1,
		MaxAge:      s.cfg.StreamMaxAge,
		Duplicates:  s.cfg.DupeWindow,
		AllowDirect: true,
	})
	if err != nil {
		return oops.Code("EVENTBUS_STREAM_DECLARE_FAILED").With("stream", StreamName).Wrap(err)
	}
	return nil
}

// Stop drains the in-process connection and shuts the server down.
//
// Order (mandatory for filestore consistency):
//  1. conn.Drain() — flushes pending publishes; Drain itself is bounded
//     by nats.DrainTimeout set on Connect (readyTimeout, 10s).
//  2. server.Shutdown() — signals shutdown.
//  3. server.WaitForShutdown() — blocks until state is flushed to disk.
//     Omitting this races the filestore on fast test teardown.
//
// The WaitForShutdown call runs in a goroutine so Stop honors ctx
// cancellation: a deadlined or cancelled ctx returns ctx.Err() while the
// server goroutine continues draining in the background. For embedded
// deployments this is the correct trade-off — the filestore will complete
// its write even after Stop returns; what we avoid is blocking a parent
// orchestrator shutdown past its own deadline.
//
// Idempotent: safe to call multiple times (e.g., explicit in test + t.Cleanup).
// codecov:ignore — non-error branches exercised by integration and E2E tests
func (s *Subsystem) Stop(ctx context.Context) error {
	// Stop the Prometheus exporter first: it holds an HTTP listener and a
	// scrape goroutine that queries the embedded NATS monitor endpoint.
	// Stopping it before Drain/Shutdown prevents scrape-in-flight errors
	// against a NATS server that's already tearing down its HTTP listener.
	if s.exporter != nil {
		s.exporter.Stop()
		s.exporter = nil
	}

	var drainErr error
	if s.conn != nil && !s.conn.IsClosed() {
		if err := s.conn.Drain(); err != nil && !errors.Is(err, nats.ErrConnectionClosed) {
			drainErr = err
		}
	}
	if s.server != nil {
		s.server.Shutdown()
		done := make(chan struct{})
		go func() {
			s.server.WaitForShutdown()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			// Fall through — server teardown continues in the background.
			// Prefer the caller's deadline over waiting for filestore flush.
		}
		s.server = nil
	}
	s.conn = nil
	s.js = nil
	if drainErr != nil {
		return oops.Code("EVENTBUS_DRAIN_FAILED").Wrap(drainErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return oops.Code("EVENTBUS_STOP_CTX_CANCELLED").Wrap(ctxErr)
	}
	return nil
}

// JS returns the JetStream context. Nil before Start / after Stop.
func (s *Subsystem) JS() jetstream.JetStream { return s.js }

// Conn returns the in-process NATS connection. Nil before Start / after Stop.
func (s *Subsystem) Conn() *nats.Conn { return s.conn }

// GameID returns the configured game id. Used by the plugin emitter to
// compose JetStream subjects (events.<game_id>.<...>).
func (s *Subsystem) GameID() string { return s.cfg.GameID }

// Config returns the subsystem's applied configuration. Used by the gRPC
// subsystem to construct the history reader (F4) with the same StreamMaxAge
// that was used to configure the JetStream EVENTS stream.
func (s *Subsystem) Config() Config { return s.cfg }

// resolveStoreDir resolves the StoreDir for this subsystem.
// Blank Config.StoreDir means "use xdg.DataDir() + /jetstream".
func (s *Subsystem) resolveStoreDir() (string, error) {
	if s.cfg.StoreDir != "" {
		return s.cfg.StoreDir, nil
	}
	baseDir, err := xdg.DataDir()
	if err != nil {
		return "", oops.Code("EVENTBUS_STOREDIR_FAILED").Wrap(err)
	}
	return filepath.Join(baseDir, "jetstream"), nil
}
