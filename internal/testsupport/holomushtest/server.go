// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package holomushtest provides a lightweight test-server harness used by
// rekey E2E integration tests. It wires the DEK manager, Rekey orchestrator,
// audit-chain verifier, checkpoint sweep subsystem, and admin UDS server in
// a single in-process fixture backed by real Postgres and embedded NATS —
// without spinning up the full production binary.
//
// This package is imported only by integration test binaries (build tag:
// integration). It MUST NOT be imported by production code.
//
// Design choice: the harness constructs dek.Manager itself rather than
// depending on production wiring (cmd/holomush). This lets E2E tests run
// before the production-wiring bead's follow-up lands and keeps the test-
// server free of cmd/ import cycles.
package holomushtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
	"github.com/holomush/holomush/test/testutil"
)

// PlayerCreds holds the credentials seeded for a test operator.
type PlayerCreds struct {
	PlayerID string
	Username string
	Password string
}

// ServerConfig parameterises a test server instance.
type ServerConfig struct {
	// MemberID is the cluster member identifier (e.g. "member-1").
	MemberID string
	// Game is the logical game identifier (e.g. "g1").
	Game string
	// PG is a pre-opened pgx pool (shared between replicas).
	PG *pgxpool.Pool
	// NATSURL is the embedded NATS connection URL returned by StartEmbeddedNATS.
	// Stored for documentation / future use; the slim harness does not use NATS.
	NATSURL string
}

// Server is a slim in-process test server wiring the rekey crypto subsystems.
// It exposes the internal components needed by E2E specs via accessor methods.
type Server struct {
	cfg       ServerConfig
	udsPath   string
	pool      *pgxpool.Pool
	dekMgr    dek.Manager
	orch      *dek.Orchestrator
	ckptRepo  *dek.CheckpointRepo
	chainRepo chain.Repo
	verifier  chain.Verifier
	adminSrv  *socket.Server
}

// UDSPath returns the path of the admin Unix domain socket.
func (s *Server) UDSPath() string { return s.udsPath }

// GetRekeyOrchestrator returns the orchestrator for direct phase driving by
// test scenarios (e.g. SetBatchHookForTest for crash-resume tests).
func (s *Server) GetRekeyOrchestrator() *dek.Orchestrator { return s.orch }

// GetCheckpointRepo returns the CheckpointRepo for direct DB assertions.
func (s *Server) GetCheckpointRepo() *dek.CheckpointRepo { return s.ckptRepo }

// GetAuditChainVerifier returns the chain Verifier for INV-CRYPTO-101/INV-CRYPTO-102 assertions.
func (s *Server) GetAuditChainVerifier() chain.Verifier { return s.verifier }

// GetDEKManager returns the DEK manager for seeding fixture DEK rows.
func (s *Server) GetDEKManager() dek.Manager { return s.dekMgr }

// GetPool returns the shared Postgres pool for direct SQL assertions.
func (s *Server) GetPool() *pgxpool.Pool { return s.pool }

// GetChainRepo returns the auditchain Repo for direct chain assertions.
func (s *Server) GetChainRepo() chain.Repo { return s.chainRepo }

// VerifierForChain returns a Verifier scoped to the given handler's Repo.
// The returned Verifier is the same instance used by boot-time verification.
func (s *Server) VerifierForChain(_ chain.Handler) chain.Verifier { return s.verifier }

// EmitPolicySet emits one crypto.policy_set event for policyName with ops as
// the required_op_kinds slice. Used by E2E tests to simulate a policy edit
// that extends the policy_set chain. The event is inserted directly into
// events_audit (bypassing NATS) via the directSQLPublisher adapter.
func (s *Server) EmitPolicySet(policyName string, ops []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pub := &directSQLPublisher{pool: s.pool}
	deps := policy.EmitDeps{
		GameID:          s.cfg.Game,
		ServerStartULID: ulid.Make().String(),
		ServerIdentity:  "holomushtest@emit-policy-set",
		Pool:            s.pool,
		Publisher:       pub,
		Clock:           wallClock{},
		Config:          policy.CryptoEffectiveConfig{DualControlRequired: ops},
	}
	return policy.EmitCurrentSnapshot(ctx, deps, policyName)
}

// EditDualControlRequired emits a new crypto.policy_set event for
// "dual_control_required" with the given op-kinds list, simulating a
// runtime policy edit that extends the chain. This is equivalent to an
// operator changing the dual_control_required setting on a live server.
func (s *Server) EditDualControlRequired(ops []string) error {
	return s.EmitPolicySet("dual_control_required", ops)
}

// RestartToReloadPolicy is a no-op in the in-process test harness. The chain
// verifier reads from the DB on every call, so no restart is needed to pick up
// a new policy_set row emitted by EditDualControlRequired. The method exists so
// E2E spec bodies read naturally (matching what a real server would require).
func (s *Server) RestartToReloadPolicy() {
	// No-op: the in-process verifier reads from the DB on each VerifyScope call.
}

// Shutdown stops the admin UDS server gracefully.
func (s *Server) Shutdown() {
	if s.adminSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.adminSrv.Stop(ctx) //nolint:errcheck // best-effort test cleanup
	}
}

// AdminClient is a thin wrapper around the generated ConnectRPC admin client
// providing convenience helpers for E2E specs.
type AdminClient struct {
	pool            *pgxpool.Pool
	client          adminv1connect.AdminServiceClient
	t               *testing.T
	defaultPlayerID string // set by SetDefaultPlayerID; used by Rekey/RekeyStatus/RekeyList helpers
}

// Raw returns the underlying AdminServiceClient for specs that need full
// control over request construction (e.g. testing error paths).
func (c *AdminClient) Raw() adminv1connect.AdminServiceClient { return c.client }

// SetDefaultPlayerID sets the player ID used as the session token in the
// convenience Rekey/RekeyStatus/RekeyList wrapper methods. Must be called
// after SeedAdminPlayer to wire the primary operator's player ID.
func (c *AdminClient) SetDefaultPlayerID(playerID string) {
	c.defaultPlayerID = playerID
}

// SeedAdminPlayer creates a player + character in the database and returns
// PlayerCreds for use in E2E specs.
//
// The password parameter is stored verbatim in PlayerCreds.Password; the
// harness does not hash it into the players table (E2E tests bypass the TOTP
// auth flow and use the session token directly via noopRekeySessionStore).
func (c *AdminClient) SeedAdminPlayer(playerID, username, password string) PlayerCreds {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := c.pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash, created_at, updated_at)
		 VALUES ($1, $2, 'harness-hash-not-used', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		 ON CONFLICT (id) DO NOTHING`,
		playerID, username)
	require.NoError(c.t, err, "SeedAdminPlayer: insert player %s", username)

	charID := playerID + "CHAR"
	_, err = c.pool.Exec(ctx,
		`INSERT INTO characters (id, player_id, name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (id) DO NOTHING`,
		charID, playerID, username)
	require.NoError(c.t, err, "SeedAdminPlayer: insert character for %s", username)

	return PlayerCreds{PlayerID: playerID, Username: username, Password: password}
}

// StartEmbeddedNATS starts an in-memory NATS+JetStream server and returns
// the NATS connection URL. Cleanup is registered on t.Cleanup.
func StartEmbeddedNATS(t *testing.T) string {
	t.Helper()
	bus := eventbustest.New(t)
	if bus.Conn == nil {
		return ""
	}
	return bus.Conn.ConnectedUrl()
}

// StartPG returns a pgxpool.Pool bound to a fresh per-test database on the
// process-wide shared Postgres testcontainer (migrations already applied via
// the template DB). The container outlives any single test; per-test
// databases are dropped on t.Cleanup, and the pool is closed on t.Cleanup.
func StartPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return startPGPool(t)
}

// StartServer boots a slim crypto test server sharing the given PG pool.
// Cleanup (Shutdown) is registered on t.Cleanup.
func StartServer(t *testing.T, cfg ServerConfig) *Server {
	t.Helper()
	s := buildServer(t, cfg)
	t.Cleanup(s.Shutdown)
	return s
}

// NewAdminClient constructs an AdminClient that dials the admin UDS at udsPath.
func NewAdminClient(pool *pgxpool.Pool, udsPath string, t *testing.T) *AdminClient {
	t.Helper()
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", udsPath)
			},
		},
	}
	return &AdminClient{
		pool:   pool,
		client: adminv1connect.NewAdminServiceClient(httpClient, "http://admin"),
		t:      t,
	}
}

// Authenticate performs a session token acquisition via the admin UDS.
func (c *AdminClient) Authenticate(username, password, totpCode string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.Authenticate(ctx, connect.NewRequest(&adminv1.AuthenticateRequest{
		Username: username,
		Password: password,
		TotpCode: totpCode,
	}))
	if err != nil {
		return "", err
	}
	return resp.Msg.GetSessionToken(), nil
}

// RekeyResult holds the outcome of a successful Rekey stream.
type RekeyResult struct {
	requestID    []byte
	auditEventID []byte
}

// RequestID returns the raw 16-byte request_id from the completed event.
func (r RekeyResult) RequestID() []byte { return r.requestID }

// AuditEventID returns the raw 16-byte audit_event_id from the completed event.
func (r RekeyResult) AuditEventID() []byte { return r.auditEventID }

// Rekey calls AdminService.Rekey over the UDS using the default player ID set
// by SetDefaultPlayerID as the session token (noopRekeySessionStore echoes it
// back as the player ID). Returns when the stream terminates with
// RekeyCompleted or RekeyError.
//
// Callers MUST call SetDefaultPlayerID before invoking this method.
func (c *AdminClient) Rekey(ctx dek.ContextID, justification string) (RekeyResult, error) {
	tctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	playerID := c.defaultPlayerID
	if playerID == "" {
		return RekeyResult{}, rekeyClientError("AdminClient.Rekey: defaultPlayerID not set — call SetDefaultPlayerID first")
	}

	stream, err := c.client.Rekey(tctx, connect.NewRequest(&adminv1.RekeyRequest{
		SessionToken:  playerID,
		ContextType:   ctx.Type,
		ContextId:     ctx.ID,
		Justification: justification,
	}))
	if err != nil {
		return RekeyResult{}, err
	}
	for stream.Receive() {
		msg := stream.Msg()
		switch ev := msg.Event.(type) {
		case *adminv1.RekeyProgress_Completed:
			return RekeyResult{
				requestID:    ev.Completed.GetRequestId(),
				auditEventID: ev.Completed.GetAuditEventId(),
			}, nil
		case *adminv1.RekeyProgress_Error:
			return RekeyResult{}, connect.NewError(
				connect.CodeInternal,
				rekeyClientError(ev.Error.GetCode()+": "+ev.Error.GetMessage()),
			)
		}
	}
	if err := stream.Err(); err != nil {
		return RekeyResult{}, err
	}
	return RekeyResult{}, connect.NewError(connect.CodeInternal,
		rekeyClientError("Rekey stream closed without terminal event"))
}

// rekeyClientError is an error type for the AdminClient convenience wrappers.
type rekeyClientError string

func (e rekeyClientError) Error() string { return string(e) }

// RekeyStatus calls AdminService.RekeyStatus over the UDS using the default
// player ID set by SetDefaultPlayerID. rid is the 16-byte request_id.
func (c *AdminClient) RekeyStatus(rid []byte) (*adminv1.RekeyStatusResponse, error) {
	tctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	playerID := c.defaultPlayerID
	if playerID == "" {
		return nil, rekeyClientError("AdminClient.RekeyStatus: defaultPlayerID not set — call SetDefaultPlayerID first")
	}

	resp, err := c.client.RekeyStatus(tctx, connect.NewRequest(&adminv1.RekeyStatusRequest{
		SessionToken: playerID,
		RequestId:    rid,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// RekeyList calls AdminService.RekeyList over the UDS and drains the stream.
// includeTerminal maps to the proto --include-terminal flag; contextPattern
// is an empty string or SQL LIKE pattern matched against context_id.
func (c *AdminClient) RekeyList(includeTerminal bool, contextPattern string) ([]*adminv1.RekeyStatusResponse, error) {
	tctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	playerID := c.defaultPlayerID
	if playerID == "" {
		return nil, rekeyClientError("AdminClient.RekeyList: defaultPlayerID not set — call SetDefaultPlayerID first")
	}

	req := &adminv1.RekeyListRequest{
		SessionToken:    playerID,
		IncludeTerminal: includeTerminal,
	}
	if contextPattern != "" {
		req.ContextPattern = &contextPattern
	}

	stream, err := c.client.RekeyList(tctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}

	var rows []*adminv1.RekeyStatusResponse
	for stream.Receive() {
		rows = append(rows, stream.Msg())
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// --- internal helpers ---

// buildServer constructs and starts the slim crypto test server.
func buildServer(t *testing.T, cfg ServerConfig) *Server {
	t.Helper()

	pool := cfg.PG
	require.NotNil(t, pool, "ServerConfig.PG must be non-nil")

	gameID := cfg.Game
	if gameID == "" {
		gameID = "g1"
	}

	// Wire crypto material: KEK + DEK manager.
	provider := newKEKProvider(t, cfg.MemberID)
	dekStore := dek.NewStore(pool)
	cacheConfig := dek.CacheConfig{Capacity: 64, TTL: time.Minute}
	mgr, err := dek.NewManager(
		provider, dekStore,
		dek.NewCache(cacheConfig), dek.NewParticipantsCache(cacheConfig),
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&noopBindingResolver{},
	)
	require.NoError(t, err, "holomushtest.buildServer: dek.NewManager")

	// Wire audit chain.
	chainRepo := chain.NewPostgresRepo(pool)
	chainEmitter := chain.NewEmitter(chainRepo)

	// Wire rekey audit emitter using a direct-SQL publisher.
	rekeyPub := &testAuditPublisher{pool: pool}
	auditEmitter := dek.NewRekeyAuditEmitter(chainEmitter, rekeyPub)

	// Wire orchestrator.
	dek.SetGameIDForTest(gameID)
	ckptRepo := dek.NewCheckpointRepo(pool)
	policyHandler := policy.PolicySetHandlerFor(gameID)
	policyHashSrc := dek.NewAuditChainPolicyHashSource(chainRepo, policyHandler)
	orch := dek.NewOrchestrator(dekStore, ckptRepo, policyHashSrc, mgr)
	orch.SetMaterialResolver(mgr.(dek.MaterialResolver)) //nolint:forcetypeassert // *manager satisfies MaterialResolver
	orch.SetAuditEmitter(auditEmitter)
	orch.SetDestroyer(mgr) //nolint:forcetypeassert // *manager satisfies DEKDestroyer
	orch.SetPhase5Coordinator(&noopPhase5Coordinator{})

	// Chain verifier (used for assertion helpers).
	verifier := chain.NewVerifier(chainRepo)

	// Admin UDS server with rekey handler wired.
	udsPath, udsLockPath := makeUDSPaths(t)

	rekeyHandler := socket.NewRekeyHandler(
		&noopRekeySessionStore{},
		&grantAllSubjectResolver{},
		&grantAllRoleChecker{},
		&orchestratorRunnerAdapter{orch: orch},
		&rekeyAbortAdapter{repo: ckptRepo, emitter: auditEmitter},
		&checkpointReaderAdapter{repo: ckptRepo},
	)
	rekeyConnectHandler := socket.NewRekeyConnectHandler(rekeyHandler, func(err error) error { return err })

	adminSrv := socket.NewServer(socket.Config{
		SocketPath:   udsPath,
		LockPath:     udsLockPath,
		Version:      "test",
		RekeyHandler: rekeyConnectHandler,
	})

	errCh, startErr := adminSrv.Start()
	require.NoError(t, startErr, "holomushtest.buildServer: admin socket start")
	go func() {
		for range errCh { //nolint:revive // drain error channel
		}
	}()

	return &Server{
		cfg:       cfg,
		udsPath:   udsPath,
		pool:      pool,
		dekMgr:    mgr,
		orch:      orch,
		ckptRepo:  ckptRepo,
		chainRepo: chainRepo,
		verifier:  verifier,
		adminSrv:  adminSrv,
	}
}

// startPGPool opens a pgxpool.Pool against a fresh per-test database
// template-cloned from testutil.SharedPostgres. The Postgres container is
// process-scope (started once per test binary); the database itself is
// dropped on t.Cleanup, and the pool is closed on t.Cleanup.
func startPGPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err, "holomushtest.startPGPool: open pool")
	t.Cleanup(pool.Close)

	return pool
}

// newKEKProvider mints a random KEK and wires a LocalAEADProvider for tests.
func newKEKProvider(t *testing.T, memberID string) kek.Provider {
	t.Helper()
	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	require.NoError(t, err)

	envName := "HOLOMUSH_TEST_KEK_" + sanitizeEnvName(memberID)
	t.Setenv(envName, hex.EncodeToString(kekBytes))
	src := kek.NewEnvSource(envName, false)
	p, err := kek.NewLocalAEADProviderForUnitTest(context.Background(), src)
	require.NoError(t, err)
	return p
}

// makeUDSPaths returns a short-pathed UDS socket and lock path to fit within
// the OS sun_path limit (104 bytes on macOS, 108 bytes on Linux).
func makeUDSPaths(t *testing.T) (socketPath, lockPath string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "hmtest-")
	require.NoError(t, err, "makeUDSPaths: MkdirTemp")
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // test cleanup
	return filepath.Join(dir, "admin.sock"), filepath.Join(dir, "admin.lock")
}

// sanitizeEnvName converts an arbitrary string into a safe env-var suffix.
func sanitizeEnvName(s string) string {
	out := make([]byte, 0, len(s))
	for _, b := range []byte(s) {
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
			out = append(out, b)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

// --- stub / adapter types ---

// noopBindingResolver satisfies dek.BindingResolver for tests that don't
// exercise the binding lookup path.
type noopBindingResolver struct{}

func (*noopBindingResolver) CurrentWithPlayer(_ context.Context, _ string) (bindingID, playerID string, err error) {
	return "noop-binding", "", nil
}

// testAuditPublisher satisfies dek.AuditPublisher for the in-process harness.
// It inserts audit event rows directly into events_audit so chain verifier
// and assertion helpers can read them.
type testAuditPublisher struct {
	pool *pgxpool.Pool
}

func (p *testAuditPublisher) PublishAudit(
	ctx context.Context,
	subject, evType string,
	payload []byte,
) (ulid.ULID, error) {
	id := ulid.Make()
	// events_audit.timestamp is BIGINT-ns post-gfo6 (INV-STORE-1); use the SQL-side
	// BIGINT-ns expression rather than TIMESTAMPTZ now().
	_, err := p.pool.Exec(ctx,
		`INSERT INTO events_audit
		   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		 VALUES ($1, $2, $3, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, 'system', $4, 1, 'identity', 0, '{}'::jsonb)
		 ON CONFLICT (id) DO NOTHING`,
		id[:], subject, evType, payload)
	return id, err
}

// noopPhase5Coordinator satisfies dek.Phase5Coordinator for harnesses that
// don't test cluster-level Phase 5 invalidation. All members report ack.
type noopPhase5Coordinator struct{}

func (*noopPhase5Coordinator) RequestInvalidation(
	_ context.Context,
	_ dek.ContextID,
	_ string,
	_, _ uint32,
) error {
	return nil // no missing members → full ack
}

// noopRekeySessionStore satisfies socket.RekeySessionStore.
// The token is echoed as the player ID so E2E specs can pass the player ID
// directly as the session token.
type noopRekeySessionStore struct{}

func (*noopRekeySessionStore) GetOperatorSession(token string) (socket.OperatorSession, error) {
	return socket.OperatorSession{
		PlayerID:     token,
		TOTPVerified: true,
	}, nil
}

// grantAllSubjectResolver satisfies access.SubjectResolver by granting the
// crypto.operator capability for every check. E2E specs are not testing the
// capability check layer; they need it to pass.
type grantAllSubjectResolver struct{}

func (*grantAllSubjectResolver) ResolveSubjectAttributes(
	_ context.Context,
	_ string, _ string,
) (*types.AttributeBags, error) {
	bags := types.NewAttributeBags()
	bags.Subject[access.PlayerGrantsAttribute] = []string{"crypto.operator"}
	return bags, nil
}

// grantAllRoleChecker satisfies socket.OperatorRoleChecker by granting all
// role checks.
type grantAllRoleChecker struct{}

func (*grantAllRoleChecker) PlayerHasRole(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

// orchestratorRunnerAdapter adapts *dek.Orchestrator to socket.OrchestratorRunner.
// Mirrors productionOrchestratorRunner in cmd/holomush/crypto_rekey_wiring.go.
type orchestratorRunnerAdapter struct {
	orch *dek.Orchestrator
}

// Run delegates to Orchestrator.RunByRequestID when req.RequestID is non-zero
// (explicit RekeyResume RPC path) or Orchestrator.Run otherwise
// (fresh-start / auto-resume path).
func (a *orchestratorRunnerAdapter) Run(ctx context.Context, req socket.RekeyRunRequest) (socket.RekeyRunOutcome, error) {
	operator := dek.OperatorIdentity{
		PlayerID: req.Operator.PlayerID,
	}

	var (
		outcome dek.RekeyOutcome
		err     error
	)

	if req.RequestID != ([16]byte{}) {
		// Explicit-resume path (RekeyResume RPC).
		rid := dek.RequestID(req.RequestID)
		outcome, err = a.orch.RunByRequestID(ctx, rid, dek.RekeyRequest{
			Operator:     operator,
			ForceDestroy: req.ForceDestroy,
		})
	} else {
		// Fresh-start / auto-resume path (Rekey RPC).
		outcome, err = a.orch.Run(ctx, dek.RekeyRequest{
			ContextType:   req.ContextType,
			ContextID:     req.ContextID,
			Justification: req.Justification,
			Operator:      operator,
			ForceDestroy:  req.ForceDestroy,
		})
	}
	if err != nil {
		return socket.RekeyRunOutcome{}, err
	}
	return socket.RekeyRunOutcome{
		RequestID:        [16]byte(outcome.RequestID),
		AuditEventID:     [16]byte(outcome.AuditEventID),
		Phase3RowCount:   outcome.Phase3RowCount,
		Phase5Attempts:   outcome.Phase5Attempts,
		ForceDestroyUsed: outcome.ForceDestroyUsed,
		Resumed:          outcome.Resumed,
		DurationMs:       outcome.DurationMs,
	}, nil
}

// rekeyAbortAdapter satisfies socket.RekeyAbortRunner by delegating to
// CheckpointRepo.MarkAborted and RekeyAuditEmitter.Emit (INV-CRYPTO-104).
type rekeyAbortAdapter struct {
	repo    *dek.CheckpointRepo
	emitter *dek.RekeyAuditEmitter
}

func (a *rekeyAbortAdapter) RunAbort(ctx context.Context, req socket.RekeyAbortRequest) (socket.RekeyAbortOutcome, error) {
	rid := dek.RequestID(req.RequestID)
	abortReason := "operator:" + req.PlayerID
	if err := a.repo.MarkAborted(ctx, rid, abortReason); err != nil {
		return socket.RekeyAbortOutcome{}, err
	}

	ckpt, err := a.repo.Get(ctx, rid)
	if err != nil {
		return socket.RekeyAbortOutcome{}, err
	}

	// Emit a minimal abort audit event (INV-CRYPTO-104). The payload carries
	// only the fields available at abort time.
	payload := dek.RekeyAuditPayload{
		RequestID:       rid.String(),
		Context:         dek.RekeyAuditContext{Type: ckpt.ContextType, ID: ckpt.ContextID},
		OldDEK:          dek.RekeyAuditDEK{ID: ckpt.OldDEKID},
		PrimaryOperator: dek.RekeyAuditOp{PlayerID: req.PlayerID},
		Justification:   "aborted by operator",
		StartedAt:       ckpt.StartedAt,
		CompletedAt:     time.Now().UTC(),
		SpecVersion:     "abort",
	}
	auditID, _, emitErr := a.emitter.Emit(ctx, payload)
	if emitErr != nil {
		// Non-fatal: abort is committed; audit emit is best-effort.
		auditID = ulid.ULID{}
	}

	abortedAt := time.Now().UTC()
	if ckpt.AbortedAt != nil {
		abortedAt = *ckpt.AbortedAt
	}
	return socket.RekeyAbortOutcome{
		AbortedAt:    abortedAt,
		AuditEventID: [16]byte(auditID),
	}, nil
}

// checkpointReaderAdapter adapts *dek.CheckpointRepo to socket.CheckpointStatusReader.
type checkpointReaderAdapter struct {
	repo *dek.CheckpointRepo
}

func (a *checkpointReaderAdapter) GetCheckpoint(ctx context.Context, rid [16]byte) (socket.CheckpointView, error) {
	ckpt, err := a.repo.Get(ctx, dek.RequestID(rid))
	if err != nil {
		return socket.CheckpointView{}, err
	}
	return ckptToView(rid, ckpt), nil
}

func (a *checkpointReaderAdapter) ListCheckpoints(ctx context.Context, filter socket.CheckpointListFilter) ([]socket.CheckpointView, error) {
	rows, err := a.repo.ListFiltered(ctx, dek.CheckpointListFilter{
		IncludeTerminal: filter.IncludeTerminal,
		ContextPattern:  filter.ContextPattern,
		Since:           filter.Since,
		Limit:           filter.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]socket.CheckpointView, 0, len(rows))
	for _, ckpt := range rows {
		out = append(out, ckptToView([16]byte(ckpt.RequestID), ckpt))
	}
	return out, nil
}

func ckptToView(rid [16]byte, ckpt dek.Checkpoint) socket.CheckpointView {
	members, _ := ckpt.Phase5MissingMembers() //nolint:errcheck // nil on decode failure is safe; callers treat nil == empty
	return socket.CheckpointView{
		RequestID:            rid,
		ContextType:          ckpt.ContextType,
		ContextID:            ckpt.ContextID,
		Status:               string(ckpt.Status),
		PrimaryPlayerID:      ckpt.PrimaryPlayerID,
		StartedAt:            ckpt.StartedAt,
		LastHeartbeatAt:      ckpt.LastHeartbeatAt,
		CompletedAt:          ckpt.CompletedAt,
		Phase5AttemptCount:   ckpt.Phase5AttemptCount,
		Phase5MissingMembers: members,
		ForceDestroy:         ckpt.ForceDestroy,
		OldDEKID:             ckpt.OldDEKID,
		NewDEKID:             ckpt.NewDEKID,
	}
}

// directSQLPublisher satisfies eventbus.Publisher for test-harness policy emit
// calls. It inserts the event payload directly into events_audit so the chain
// verifier can read it without a live NATS connection.
//
// decodePolicyPayloadJSON tries proto.Unmarshal first, then falls back to raw
// JSON. Storing the raw JSON payload directly triggers the fallback path, which
// is sufficient for test correctness.
type directSQLPublisher struct {
	pool *pgxpool.Pool
}

func (p *directSQLPublisher) Publish(ctx context.Context, ev eventbus.Event) error {
	// Store ev.Payload (raw JSON body) directly as the envelope column.
	// The policy chain verifier's decodePolicyPayloadJSON falls back to raw JSON
	// when proto.Unmarshal fails, so this path is correct for test usage.
	// events_audit.timestamp is BIGINT-ns post-gfo6 (INV-STORE-1); SQL-side
	// expression mirrors the migration's DEFAULT.
	_, err := p.pool.Exec(ctx,
		`INSERT INTO events_audit
		   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		 VALUES (gen_random_bytes(16), $1, $2, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, 'system', $3, 1, 'identity',
		         (SELECT COALESCE(MAX(js_seq), 0) + 1 FROM events_audit), '{}'::jsonb)`,
		string(ev.Subject), string(ev.Type), ev.Payload)
	return err
}

// wallClock satisfies policy.Clock using real wall time.
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// ensure directSQLPublisher satisfies eventbus.Publisher at compile time.
var _ eventbus.Publisher = (*directSQLPublisher)(nil)
