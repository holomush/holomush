// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
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

// DependsOn returns [SubsystemDatabase] (07-09 item 7, round 7 BLOCKER 1) —
// GameIDProvider resolves the DB-backed gameID at Start, so the database
// subsystem must have run InitGameID first. Acyclic: Database has no deps.
//
// DO NOT add SubsystemCryptoChainVerifier here. The verifier's own DependsOn
// includes SubsystemEventBus (internal/eventbus/audit/chain/verifier_subsystem.go)
// because its chain-handler set is built from THIS subsystem's Publisher():
// cryptoWiring's readStreamW wiring (cmd/holomush/core.go, buildReadStreamWiring
// in cmd/holomush/readstream_wiring.go:118) hard-requires an AuditPublisher
// derived from eventBusSub.Publisher() (cmd/holomush/core.go:~735) — a nil
// Publisher makes buildReadStreamWiring return an empty wiring, silently
// unregistering operator_read from the boot gate. So the bus must start
// BEFORE the verifier can know which chains it verifies; adding the reverse
// edge here would close EventBus -> CryptoChainVerifier -> EventBus, a cycle
// internal/lifecycle/orchestrator.go's topoSort refuses at boot (07-10
// MEDIUM-11 — this is the false ordering claim that used to live at
// cmd/holomush/core.go as a comment; the real order is pinned as a test in
// cmd/holomush/core_topo_order_test.go, not asserted here or there in prose).
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}
}

// Prepare establishes the NATS transport for the configured mode, obtains a
// JetStream context, and declares the EVENTS stream (D-03 provision policy).
// In embedded mode it brings the in-process NATS server up; in external mode
// (CLUSTER-01) it dials the configured cluster with creds/TLS and fails closed
// at boot if unreachable (D-02). Any failure mid-way rolls back earlier state
// (shutdown server if any, close conn).
//
// The ENTIRE Start body — including `go s.server.Start()` — belongs in
// Prepare (D-13.3 row 2): the embedded server sets DontListen: true, so it
// binds no socket and is reachable only in-process via
// nats.InProcessServer(s.server); server-start and client-connect are one
// atomic call with no seam between them. The NATS Prometheus monitor's
// operator-facing HTTP port (HTTPPort: s.cfg.MonitorPort, set in
// connectEmbedded) DOES bind here — that is D-13.0's exception 1
// (observability, not domain traffic), the same class as the observability
// server that already starts before StartAll today. Do NOT move any of this
// to Activate: audit's own Prepare requires this live JetStream context
// (it fails closed with AUDIT_DEP_NOT_STARTED otherwise).
// codecov:ignore — rollback branches exercised by integration and E2E tests
func (s *Subsystem) Prepare(ctx context.Context) error {
	if s.conn != nil {
		return nil // already prepared (embedded or external)
	}

	// Resolve the gameID provider once, before connect()/EnsureStream (07-09
	// item 7, round 7 BLOCKER 1 + round 8 correction). An empty resolve
	// keeps whatever Defaults()-substituted (or koanf-loaded) value is
	// already in s.cfg.GameID — the same "empty resolve keeps the default"
	// rule used by the DLQ subject / relay gameID rows.
	if s.cfg.GameIDProvider != nil {
		if resolved := s.cfg.GameIDProvider(); resolved != "" {
			s.cfg.GameID = resolved
		}
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

	// CLUSTER-02 (D-13c): in external mode the server authenticates as the
	// single `holomush-server` account scoped to events.>/audit.>/internal.>/
	// _INBOX.>. Self-verify that our own account is not over-scoped and
	// refuse to boot if it can reach beyond the granted prefixes (fail
	// closed). Skipped in embedded mode, which has no account model (its
	// default-open permissions would always look over-scoped). The
	// complementary external proof that other principals are locked out is
	// deploy/nats/verify-scoping.sh. Moved in from cmd/holomush (07-09,
	// design table row :475) — this package owns its own connection, so the
	// check belongs in its own boot path rather than a caller-side
	// post-Start step.
	if s.cfg.Mode == ModeExternal {
		if scopeErr := VerifyAccountScoping(ctx, s.conn); scopeErr != nil {
			s.rollbackStart(conn)
			return oops.Code("EVENTBUS_SCOPE_CHECK_FAILED").
				With("operation", "verify_account_scoping").
				Wrap(scopeErr)
		}
	}
	return nil
}

// Activate is a no-op — publishing happens through consumers, not here
// (D-13.3 row 2). The entire acquisition sequence, including bringing the
// embedded server up, already ran in Prepare; documented here so nobody
// "corrects" this by moving work into Activate.
func (s *Subsystem) Activate(_ context.Context) error {
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

// EnsureStream reconciles the EVENTS stream per the provision policy (D-03).
//
// When provision is enabled (the default), it idempotently creates or updates
// the stream — the same code path in embedded and external mode. When an
// operator sets provision:false (a locked-down cluster whose server account
// lacks $JS.API stream-admin permissions), it instead VERIFIES the stream
// exists with the expected config and fails closed on mismatch via
// EVENTBUS_STREAM_CONFIG_MISMATCH — the server MUST NOT attempt stream admin.
func (s *Subsystem) EnsureStream(ctx context.Context) error {
	if s.js == nil {
		return oops.Code("EVENTBUS_NOT_STARTED").Errorf("EnsureStream called before Prepare")
	}

	desired := s.desiredStreamConfig()

	if !s.cfg.IsProvision() {
		return s.verifyStream(ctx, desired)
	}

	_, err := s.js.CreateOrUpdateStream(ctx, desired)
	if err != nil {
		return oops.Code("EVENTBUS_STREAM_DECLARE_FAILED").With("stream", StreamName).Wrap(err)
	}
	return nil
}

// desiredStreamConfig is the EVENTS stream config this subsystem owns. It is the
// single source of truth for both the provision (create/update) and the
// provision:false (verify) paths, so the two can never drift.
func (s *Subsystem) desiredStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:        StreamName,
		Subjects:    []string{SubjectFilter},
		Retention:   jetstream.LimitsPolicy,
		Storage:     s.storage,
		Replicas:    1,
		MaxAge:      s.cfg.StreamMaxAge,
		Duplicates:  s.cfg.DupeWindow,
		AllowDirect: true,
	}
}

// verifyStream implements the provision:false path (D-03): it looks the stream
// up and compares the config fields this subsystem owns against desired,
// failing closed with EVENTBUS_STREAM_CONFIG_MISMATCH if the stream is absent
// or its config drifts. It never creates or mutates the stream.
func (s *Subsystem) verifyStream(ctx context.Context, desired jetstream.StreamConfig) error {
	stream, err := s.js.Stream(ctx, StreamName)
	if err != nil {
		// Absent (ErrStreamNotFound) or unreadable in provision:false mode: the
		// server MUST NOT create it — fail closed rather than provision (D-03).
		return oops.Code("EVENTBUS_STREAM_CONFIG_MISMATCH").
			With("stream", StreamName).
			With("reason", "stream not found").
			Wrap(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return oops.Code("EVENTBUS_STREAM_CONFIG_MISMATCH").
			With("stream", StreamName).
			With("reason", "stream info unavailable").
			Wrap(err)
	}
	if field := streamConfigMismatch(desired, info.Config); field != "" {
		return oops.Code("EVENTBUS_STREAM_CONFIG_MISMATCH").
			With("stream", StreamName).
			With("field", field).
			Errorf("existing EVENTS stream config differs from desired (provision:false)")
	}
	return nil
}

// streamConfigMismatch returns the name of the first owned config field that
// differs between desired and got, or "" if they match. It compares every field
// this subsystem declares as its contract in desiredStreamConfig (Subjects,
// Retention, Storage, MaxAge, Duplicates, AllowDirect) — server-managed fields
// (e.g. Replicas on a real cluster, placement) are not this subsystem's contract
// to enforce. Storage is durability-critical: a stream provisioned with Memory
// storage instead of File would silently pass provision:false verification and
// lose the audit event stream on restart, so it MUST fail closed (D-03).
func streamConfigMismatch(desired, got jetstream.StreamConfig) string {
	switch {
	case !slices.Equal(desired.Subjects, got.Subjects):
		return "subjects"
	case desired.Retention != got.Retention:
		return "retention"
	case desired.Storage != got.Storage:
		return "storage"
	case desired.MaxAge != got.MaxAge:
		return "max_age"
	case desired.Duplicates != got.Duplicates:
		return "duplicates"
	case desired.AllowDirect != got.AllowDirect:
		return "allow_direct"
	default:
		return ""
	}
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
		srv := s.server
		srv.Shutdown()
		done := make(chan struct{})
		go func(srv *server.Server, done chan struct{}) {
			srv.WaitForShutdown()
			close(done)
		}(srv, done)
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
