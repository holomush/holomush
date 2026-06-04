// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/pquerna/otp/hotp"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/admin/approval"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/totp"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

// adminAuthEnv is the per-spec fixture: a fully-booted server (via
// runCoreWithDeps), a UDS-dialing AdminServiceClient, an independent
// query-only PG pool for events_audit assertions, and the secrets needed
// to compute valid TOTP codes for the seeded operators.
//
// Seeding shape (extended for T26 dual-control scenarios):
//   - alice (playerA): operator + admin — drives T25's Authenticate scenarios.
//     Locked at end of T25 lockout scenario; no T26 scenario depends on her
//     remaining unlocked.
//   - bob (playerB): admin (NOT operator) — target of T25's ResetTOTP scenario.
//     TOTP-cleared at end of T25 reset scenario; no T26 scenario depends on
//     him remaining enrolled.
//   - carol (playerC): operator + admin + TOTP enrolled — primary in T26
//     dual-control scenarios. Untouched by T25.
//   - dave (playerD): operator + admin + TOTP enrolled — second-op in T26
//     dual-control scenarios. Untouched by T25. His admin role is removed
//     mid-spec in the T26 INV-D16 scenario, so this scenario MUST run last
//     among T26 scenarios.
//
// All four are listed in cryptoConfig.Operators. Bob is intentionally
// NOT in Operators (T25's contract); carol and dave are.
type adminAuthEnv struct {
	ctx        context.Context
	cancelCtx  context.CancelFunc
	serverDone chan error

	gameID string

	playerA       ulid.ULID // alice — operator + admin role
	playerB       ulid.ULID // bob — admin role, NOT operator (target of ResetTOTP)
	playerC       ulid.ULID // carol — operator + admin role (T26 dual-control primary)
	playerD       ulid.ULID // dave — operator + admin role (T26 dual-control second-op)
	daveCharID    string    // dave's character (T26 role-removal scenario uses RoleStore.RemoveRole)
	alicePassword string
	bobPassword   string
	carolPassword string
	davePassword  string
	aliceSecret   string
	bobSecret     string
	carolSecret   string
	daveSecret    string

	socketPath string
	client     adminv1connect.AdminServiceClient

	queryPool *pgxpool.Pool

	// approvalRepo is constructed on queryPool and used by T26 dual-control
	// scenarios to Open admin_approvals rows. Open is the legitimate
	// primary-side step in the dual-control flow; the Approve RPC is exercised
	// through the production UDS surface unchanged. T25 scenarios do not use
	// this field.
	approvalRepo approval.Repo

	// roleStore is used by T26 to RemoveRole(RoleAdmin) from dave's character
	// mid-spec to exercise the Approve handler's INV-D16 runtime role re-check.
	roleStore store.RoleStore

	// rekeySceneContextType and rekeySceneContextID identify the scene context
	// pre-seeded with a v1 DEK for the T27 Rekey E2E scenario. Populated by
	// seedAdminRekeyDEK (admin_rekey_e2e_test.go) before server boot.
	rekeySceneContextType string
	rekeySceneContextID   string

	// carolSessionToken stores carol's most-recent session token so T27 can
	// reuse it rather than re-authenticating (which would fail with
	// "TOTP verify failed" due to TOTP replay prevention within the same
	// 30-second step window).
	carolSessionToken string
}

// Teardown cancels the server context, drains the boot goroutine, and closes
// the query pool. Per AfterEach.
func (e *adminAuthEnv) Teardown() {
	if e.cancelCtx != nil {
		e.cancelCtx()
	}
	if e.serverDone != nil {
		select {
		case <-e.serverDone:
		case <-time.After(15 * time.Second):
			GinkgoT().Logf("warning: server goroutine did not exit within 15s")
		}
	}
	if e.queryPool != nil {
		e.queryPool.Close()
	}
}

// authenticate is a thin wrapper around the AdminServiceClient.Authenticate
// RPC for spec readability. Returns the session_token on success.
func (e *adminAuthEnv) authenticate(username, password, totpCode string) (string, error) {
	resp, err := e.client.Authenticate(e.ctx, connect.NewRequest(&adminv1.AuthenticateRequest{
		Username: username,
		Password: password,
		TotpCode: totpCode,
	}))
	if err != nil {
		return "", err
	}
	return resp.Msg.GetSessionToken(), nil
}

// computeTOTP produces a fresh code for the given secret at the current step.
// Mirrors internal/totp/service.go::Verify (HOTP with step = unix/30).
func (e *adminAuthEnv) computeTOTP(secret string) string {
	now := time.Now().UTC()
	code, err := hotp.GenerateCode(secret, uint64(now.Unix()/30)) //nolint:gosec // G115: unix timestamp positive
	Expect(err).NotTo(HaveOccurred(), "hotp.GenerateCode")
	return code
}

// lockedEventCount returns the number of crypto.totp_locked rows in
// events_audit for the given player's subject. Used to assert INV-D14.
func (e *adminAuthEnv) lockedEventCount(playerID ulid.ULID) int {
	subj := totp.SubjectLocked(e.gameID, playerID.String())
	return e.eventCount(subj, totp.EventTypeLocked)
}

// clearedEventCount returns the number of crypto.totp_cleared rows for the
// given player. Used to assert INV-D14.
func (e *adminAuthEnv) clearedEventCount(playerID ulid.ULID) int {
	subj := totp.SubjectCleared(e.gameID, playerID.String())
	return e.eventCount(subj, totp.EventTypeCleared)
}

func (e *adminAuthEnv) eventCount(subject, eventType string) int {
	var count int
	err := e.queryPool.QueryRow(
		e.ctx,
		`SELECT COUNT(*) FROM events_audit WHERE subject = $1 AND type = $2`,
		subject, eventType,
	).Scan(&count)
	Expect(err).NotTo(HaveOccurred(), "events_audit COUNT(*)")
	return count
}

// connectErrCode extracts the connect.Code from a ConnectRPC error.
// connect.CodeOf walks the error chain via errors.As(*connect.Error).
func connectErrCode(err error) connect.Code {
	if err == nil {
		return connect.CodeUnknown
	}
	return connect.CodeOf(err)
}

// bootErrString safely renders an error or "<nil>" for the Eventually
// peek-at-server-done path during startup polling.
func bootErrString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// shortTempDir returns a directory whose path is short enough that
// filepath.Join(dir, "holomush", "admin.sock") fits inside the OS UDS
// sun_path limit (104 bytes on macOS / Darwin, 108 on Linux). t.TempDir()
// rooted under /var/folders/.../T/<TestName>/NNN/ on macOS frequently
// blows past 104 chars, producing "bind: invalid argument" when the admin
// socket subsystem tries to listen.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hmadm-")
	if err != nil {
		t.Fatalf("shortTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// setupAdminAuthEnv boots the full 14-subsystem server via runCoreWithDeps,
// pre-seeds players + characters + roles + TOTP enrollments, writes a KEK
// file, and returns a fixture wired to the live admin UDS.
func setupAdminAuthEnv(t *testing.T) *adminAuthEnv {
	t.Helper()

	// --- 1. Fresh PG database (template-cloned, migrations applied). ---
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	t.Setenv("DATABASE_URL", connStr)

	// --- 2. XDG dirs under shortTempDir() so the admin socket path fits
	//        inside the OS UDS sun_path limit. macOS tempdirs under
	//        /var/folders/... are too long for unix(7) bind.
	xdgRuntime := shortTempDir(t)
	xdgState := shortTempDir(t)
	xdgData := shortTempDir(t)
	xdgConfig := shortTempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", xdgRuntime)
	t.Setenv("XDG_STATE_HOME", xdgState)
	t.Setenv("XDG_DATA_HOME", xdgData)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)

	// runCoreWithDeps already migrates via the MigratorFactory; FreshDatabase
	// also pre-applies migrations. Either order works; setting auto-migrate
	// false keeps the migrator no-op rather than re-running.
	t.Setenv("HOLOMUSH_DB_AUTO_MIGRATE", "false")

	// --- 3. KEK file source — same construction as production
	//        (cmd_admin_totp_deps.go::buildKEKProviderFromConfig). The
	//        file MUST be the one runCoreWithDeps reads, so the
	//        BootstrapEnroll wrap_key_id matches the runtime KEK
	//        fingerprint (INV-CRYPTO-19 startup integrity check).
	kekFile := filepath.Join(shortTempDir(t), "master.key.enc")
	const kekPassphrase = "e2e-test-passphrase-not-for-prod"
	t.Setenv("HOLOMUSH_KEK_FILE", kekFile)
	t.Setenv("HOLOMUSH_KEK_PASSPHRASE", kekPassphrase)

	kekBytes := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(kekBytes)
	Expect(err).NotTo(HaveOccurred(), "rand.Read for KEK bytes")

	pf := func(_ context.Context) ([]byte, error) { return []byte(kekPassphrase), nil }
	src, err := kek.NewFileSource(kekFile, pf)
	Expect(err).NotTo(HaveOccurred(), "kek.NewFileSource")
	Expect(src.Persist(context.Background(), kekBytes)).To(Succeed(), "FileSource.Persist")

	// --- 4. Seed players, characters, character_roles via a temp pool.
	//        Use the auth.Argon2idHasher for password hashes and the same
	//        KEK file source for TOTP BootstrapEnroll so secrets wrap under
	//        the same KEK fingerprint runCoreWithDeps will load at boot.
	gameID := "main"
	playerA := ulid.Make()
	playerB := ulid.Make()
	playerC := ulid.Make()
	playerD := ulid.Make()
	const alicePassword = "alice-correct-horse-battery-staple"
	const bobPassword = "bob-tr0ub4dor-3-secure-pw"
	const carolPassword = "carol-secret-passphrase-for-e2e"
	const davePassword = "dave-other-secret-passphrase"

	hasher := auth.NewArgon2idHasher()
	aliceHash, err := hasher.Hash(alicePassword)
	Expect(err).NotTo(HaveOccurred(), "Hash alicePassword")
	bobHash, err := hasher.Hash(bobPassword)
	Expect(err).NotTo(HaveOccurred(), "Hash bobPassword")
	carolHash, err := hasher.Hash(carolPassword)
	Expect(err).NotTo(HaveOccurred(), "Hash carolPassword")
	daveHash, err := hasher.Hash(davePassword)
	Expect(err).NotTo(HaveOccurred(), "Hash davePassword")

	seedPool, err := pgxpool.New(context.Background(), connStr)
	Expect(err).NotTo(HaveOccurred(), "pgxpool.New for seed")

	seedCtx, seedCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer seedCancel()

	// Players.
	for _, row := range []struct {
		id       string
		username string
		hash     string
	}{
		{playerA.String(), "alice", aliceHash},
		{playerB.String(), "bob", bobHash},
		{playerC.String(), "carol", carolHash},
		{playerD.String(), "dave", daveHash},
	} {
		_, err = seedPool.Exec(seedCtx, `
			INSERT INTO players (id, username, password_hash, created_at, updated_at)
			VALUES ($1, $2, $3, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)`,
			row.id, row.username, row.hash)
		Expect(err).NotTo(HaveOccurred(), "INSERT players %s", row.username)
	}

	// Characters (one per player). character_id is also a ULID.
	aliceCharID := ulid.Make().String()
	bobCharID := ulid.Make().String()
	carolCharID := ulid.Make().String()
	daveCharID := ulid.Make().String()
	for _, row := range []struct {
		id       string
		playerID string
		name     string
	}{
		{aliceCharID, playerA.String(), "Alice"},
		{bobCharID, playerB.String(), "Bob"},
		{carolCharID, playerC.String(), "Carol"},
		{daveCharID, playerD.String(), "Dave"},
	} {
		_, err = seedPool.Exec(seedCtx, `
			INSERT INTO characters (id, player_id, name)
			VALUES ($1, $2, $3)`,
			row.id, row.playerID, row.name)
		Expect(err).NotTo(HaveOccurred(), "INSERT character %s", row.name)
	}

	// RoleAdmin on alice/carol/dave (step 5 of InGameCredentialsProvider).
	// Bob does NOT need RoleAdmin — he's only the target of ResetTOTP, and
	// his Authenticate attempt in T25 spec 3 fails at step 2 (DENY_NOT_ENROLLED)
	// before ever reaching the role check.
	rs := store.NewPostgresRoleStore(seedPool)
	for _, charID := range []string{aliceCharID, carolCharID, daveCharID} {
		Expect(rs.AddRole(seedCtx, charID, access.RoleAdmin)).To(Succeed(),
			"AddRole RoleAdmin on character %s", charID)
	}

	// TOTP enrollments — share the same KEK file source so wrap_key_id at
	// runtime matches what BootstrapEnroll persisted here.
	kekProvider, err := kek.NewLocalAEADProvider(seedCtx, src, seedPool)
	Expect(err).NotTo(HaveOccurred(), "kek.NewLocalAEADProvider for seed")

	totpRepo := totp.NewRepository(seedPool)
	totpSvc, err := totp.NewService(
		totp.Config{GameID: gameID},
		totpRepo, kekProvider, totp.NewRealClock(), hasher,
	)
	Expect(err).NotTo(HaveOccurred(), "totp.NewService for seed")

	// alice via BootstrapEnroll (uses the once-only "totp_v1" key).
	aliceBoot, err := totpSvc.BootstrapEnroll(seedCtx, playerA)
	Expect(err).NotTo(HaveOccurred(), "BootstrapEnroll alice")

	// bob, carol, dave via Enroll (the bootstrap key is consumed by alice).
	bobEnroll, err := totpSvc.Enroll(seedCtx, playerB)
	Expect(err).NotTo(HaveOccurred(), "Enroll bob")
	carolEnroll, err := totpSvc.Enroll(seedCtx, playerC)
	Expect(err).NotTo(HaveOccurred(), "Enroll carol")
	daveEnroll, err := totpSvc.Enroll(seedCtx, playerD)
	Expect(err).NotTo(HaveOccurred(), "Enroll dave")

	aliceSecret := aliceBoot.Enrollment.Secret
	bobSecret := bobEnroll.Enrollment.Secret
	carolSecret := carolEnroll.Enrollment.Secret
	daveSecret := daveEnroll.Enrollment.Secret
	Expect(aliceSecret).NotTo(BeEmpty())
	Expect(bobSecret).NotTo(BeEmpty())
	Expect(carolSecret).NotTo(BeEmpty())
	Expect(daveSecret).NotTo(BeEmpty())

	// T27 Rekey E2E — seed a v1 DEK for the scene context BEFORE boot.
	// Defined in admin_rekey_e2e_test.go. Uses the same kekProvider so
	// INV-CRYPTO-19 (wrap_key_id fingerprint) is satisfied at runtime.
	const rekeySceneCtxType = "scene"
	const rekeySceneCtxID = "e2e-rekey-t27"
	seedAdminRekeyDEK(seedCtx, seedPool, kekProvider, rekeySceneCtxType, rekeySceneCtxID)

	seedPool.Close()

	// --- 5. Build configs for runCoreWithDeps. ---
	cfg := &coreConfig{
		GRPCAddr:           "127.0.0.1:0",
		ControlAddr:        "127.0.0.1:0",
		MetricsAddr:        "", // disable observability HTTP
		LogFormat:          "json",
		DataDir:            xdgData, // plugin discovery resolves to xdgData/plugins (empty -> no plugins)
		GameID:             gameID,
		Setting:            "", // skip setting bootstrapper registration
		LuaTimeout:         1 * time.Second,
		LuaRegistryMaxSize: 65536,
	}

	// GuestStartLocation MUST be set when Setting="" because the bootstrap
	// subsystem otherwise fails with START_LOCATION_NOT_FOUND while resolving
	// starting_location_id from the (unbootstrapped) metadata store.
	// Hardcoded Nexus ULID matches cmd/holomush/core_test.go::defaultNexusULID.
	gameConfig := config.GameConfig{
		GuestStartLocation: "01HK153X0006AFVGQT61FPQX3S",
	}
	authConfig := config.DefaultAuthConfig()
	eventBusConfig := eventbus.Config{StoreDir: shortTempDir(t)}.Defaults()
	// Operators: alice (T25), carol (T26 primary), dave (T26 second-op).
	// Bob is intentionally omitted per T25's contract — he's the
	// admin-with-no-operator-cap counterexample for ResetTOTP.
	cryptoConfig := config.CryptoConfig{
		Operators: []string{
			playerA.String(),
			playerC.String(),
			playerD.String(),
		},
		DualControlRequired: []string{},
	}

	// --- 6. Run server in goroutine; wait for the admin socket to appear. ---
	ctx, cancel := context.WithCancel(context.Background())
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runCoreWithDeps(ctx, cfg, gameConfig, authConfig, eventBusConfig, cryptoConfig, config.DefaultLoggingConfig(), cmd, nil)
	}()

	socketPath := filepath.Join(xdgRuntime, "holomush", "admin.sock")
	Eventually(func() bool {
		// Surface a server-startup error promptly so we don't wait the full
		// 30s when boot fails. Non-blocking peek at serverDone.
		select {
		case bootErr := <-serverDone:
			Fail("server exited during startup: " + bootErrString(bootErr))
		default:
		}
		conn, dialErr := net.Dial("unix", socketPath)
		if dialErr != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, "30s", "100ms").Should(BeTrue(), "admin socket should be reachable at %s", socketPath)

	// --- 7. Build UDS-dialing client. Inlined because cmd/holomush's
	//        adminClientFromSocket already lives in this package. ---
	client := adminClientFromSocket(socketPath)

	// --- 8. Independent query pool for events_audit assertions and
	//        T26 dual-control direct mutations (force-expire, RemoveRole).
	queryPool, err := pgxpool.New(context.Background(), connStr)
	Expect(err).NotTo(HaveOccurred(), "pgxpool.New for query")

	approvalRepo := approval.NewPostgresRepo(queryPool, nil)
	roleStore := store.NewPostgresRoleStore(queryPool)

	return &adminAuthEnv{
		ctx:           ctx,
		cancelCtx:     cancel,
		serverDone:    serverDone,
		gameID:        gameID,
		playerA:       playerA,
		playerB:       playerB,
		playerC:       playerC,
		playerD:       playerD,
		daveCharID:    daveCharID,
		alicePassword: alicePassword,
		bobPassword:   bobPassword,
		carolPassword: carolPassword,
		davePassword:  davePassword,
		aliceSecret:   aliceSecret,
		bobSecret:     bobSecret,
		carolSecret:   carolSecret,
		daveSecret:    daveSecret,
		socketPath:    socketPath,
		client:        client,
		queryPool:     queryPool,
		approvalRepo:  approvalRepo,
		roleStore:     roleStore,
		// T27 Rekey E2E context (admin_rekey_e2e_test.go).
		rekeySceneContextType: rekeySceneCtxType,
		rekeySceneContextID:   rekeySceneCtxID,
	}
}

// One spec covering all three lifecycle scenarios sequentially.
//
// Why one spec: prometheus.DefaultRegisterer is process-wide
// (cluster.NewPillMetrics panics on duplicate-collector registration if
// runCoreWithDeps boots twice in the same process), and Ginkgo's Ordered
// + BeforeAll/AfterAll lifecycle is fragile in unobvious ways for tests
// that need a single long-lived server across nested It nodes (the
// AfterAll fires between specs in some configurations). Folding the
// three scenarios into one spec gives a single deterministic ordering
// — happy-path → admin-reset (clears bob, independent of alice) →
// lockout (terminal: locks alice) — with one boot, one teardown, and
// no Ginkgo-internal-lifecycle ambiguity.
var _ = Describe("Admin Authenticate Lifecycle (full-stack E2E)", func() {
	It("covers happy-path, admin-reset, and lockout against one running server", func() {
		env := setupAdminAuthEnv(adminAuthSuiteT)
		defer env.Teardown()

		By("scenario 1: returns session_token on happy path and emits no totp_locked")
		token1, err := env.authenticate("alice", env.alicePassword, env.computeTOTP(env.aliceSecret))
		Expect(err).NotTo(HaveOccurred(), "Authenticate happy path")
		Expect(token1).NotTo(BeEmpty(), "session token must be non-empty")
		// Brief settling window — audit projection drains EVENTS asynchronously,
		// but no totp_locked event should ever be produced on this path.
		Consistently(func() int {
			return env.lockedEventCount(env.playerA)
		}, "1s", "100ms").Should(Equal(0), "no crypto.totp_locked expected on happy path")

		By("scenario 2: ResetTOTP emits crypto.totp_cleared with cleared_by=admin_reset and the target can no longer authenticate")
		// Alice's existing session token is still valid; reuse it for the admin
		// reset call. (A fresh authenticate would race the 30s TOTP step window.)
		_, err = env.client.ResetTOTP(env.ctx, connect.NewRequest(&adminv1.ResetTOTPRequest{
			SessionToken:   token1,
			TargetPlayerId: env.playerB.String(),
		}))
		Expect(err).NotTo(HaveOccurred(), "ResetTOTP bob")

		Eventually(func() int {
			return env.clearedEventCount(env.playerB)
		}, "10s", "100ms").Should(Equal(1),
			"exactly one crypto.totp_cleared row for bob (INV-D14)")

		// Decode the projected envelope + payload and assert the
		// cleared_by field carries the admin-reset contract value. A
		// regression that emitted the event with the wrong cleared_by
		// would still satisfy the count check above; this read closes
		// that gap (CodeRabbit #1, INV-D14 contract).
		clearedSubject := totp.SubjectCleared(env.gameID, env.playerB.String())
		var envelopeBytes []byte
		err = env.queryPool.QueryRow(
			env.ctx,
			`SELECT envelope FROM events_audit
			  WHERE subject = $1 AND type = $2
			  ORDER BY js_seq DESC LIMIT 1`,
			clearedSubject, totp.EventTypeCleared,
		).Scan(&envelopeBytes)
		Expect(err).NotTo(HaveOccurred(), "fetch crypto.totp_cleared envelope")
		var clearedEv eventbusv1.Event
		Expect(proto.Unmarshal(envelopeBytes, &clearedEv)).To(Succeed(),
			"proto-unmarshal envelope")
		var clearedPayload totp.ClearedPayload
		Expect(json.Unmarshal(clearedEv.Payload, &clearedPayload)).To(Succeed(),
			"json-unmarshal ClearedPayload")
		Expect(clearedPayload.ClearedBy).To(Equal(totp.ClearReasonAdminReset),
			"crypto.totp_cleared cleared_by MUST be admin_reset for ResetTOTP path")

		// Bob can no longer authenticate: enrollment was cleared, so step 2 of
		// InGameCredentialsProvider returns DENY_NOT_ENROLLED, mapped to
		// connect.CodeFailedPrecondition.
		_, bobAuthErr := env.authenticate("bob", env.bobPassword, env.computeTOTP(env.bobSecret))
		Expect(bobAuthErr).To(HaveOccurred(), "bob auth must fail post-reset")
		Expect(connectErrCode(bobAuthErr)).To(Equal(connect.CodeFailedPrecondition),
			"DENY_NOT_ENROLLED -> connect.CodeFailedPrecondition")

		By("scenario 3: emits crypto.totp_locked on the 5th failed attempt and then rejects with DENY_LOCKED")
		// LockoutThreshold default is 5 (totp.Config.applyDefaults). The 5th
		// invalid attempt must transition NULL->non-NULL locked_until and the
		// AuditingService.Verify wrapper emits crypto.totp_locked. Regression-
		// lock for the IncrementFailedAttempts SQL `$3::TIMESTAMPTZ` cast bug
		// that was silently disabling lockout in production until T25 surfaced it.
		for i := 0; i < 5; i++ {
			_, attemptErr := env.authenticate("alice", env.alicePassword, "000000")
			Expect(attemptErr).To(HaveOccurred(), "attempt %d should fail", i+1)
		}
		// Confirm the DB-level lockout actually fired before chasing the
		// audit-projection roundtrip — separates "lockout never fired" from
		// "lockout fired but projection lagged" in the failure mode.
		Eventually(func() bool {
			// player_totp.locked_until is BIGINT epoch-ns (post-gfo6 Phase 4).
			var lockedUntil *pgnanos.Time
			err := env.queryPool.QueryRow(env.ctx,
				`SELECT locked_until FROM player_totp WHERE player_id = $1`,
				env.playerA.String()).Scan(&lockedUntil)
			return err == nil && lockedUntil != nil
		}, "5s", "100ms").Should(BeTrue(),
			"player_totp.locked_until MUST be set after 5 failed attempts")
		Eventually(func() int {
			return env.lockedEventCount(env.playerA)
		}, "10s", "100ms").Should(Equal(1),
			"exactly one crypto.totp_locked row must persist (INV-D14)")

		// Subsequent attempt with a valid TOTP code is rejected with DENY_LOCKED
		// (mapped to connect.CodeUnavailable per adminauth.denyCodeToConnect).
		_, lockedAuthErr := env.authenticate("alice", env.alicePassword, env.computeTOTP(env.aliceSecret))
		Expect(lockedAuthErr).To(HaveOccurred(), "Authenticate must fail while locked")
		Expect(connectErrCode(lockedAuthErr)).To(Equal(connect.CodeUnavailable),
			"DENY_LOCKED -> connect.CodeUnavailable")

		// =========================================================================
		// T26: Admin Approve / dual-control scenarios
		//
		// These run within the same boot as T25 (single-process constraint:
		// prometheus.DefaultRegisterer is process-wide) using the carol/dave
		// players seeded by setupAdminAuthEnv. T25's destructive scenarios above
		// have locked alice and TOTP-cleared bob; carol/dave are untouched and
		// drive every T26 scenario.
		//
		// Coverage:
		//   - Happy path: dave (second-op) approves carol's pending row.
		//   - DENY_DUAL_CONTROL_SELF (INV-D6): carol tries to approve her own row.
		//   - DENY_APPROVAL_EXPIRED (INV-D5): force-expire a row, then approve.
		//   - DENY_APPROVAL_ALREADY_APPROVED (INV-D7): approve once, then again.
		//   - DENY_NOT_ADMIN_ROLE (INV-D16): authenticate dave, RemoveRole, approve.
		//
		// Two intentional omissions, documented per Sub-Epic D plan §Task 26:
		//
		//   - DENY_NOT_OPERATOR (INV-D16 capability re-check): NOT reachable via
		//     the E2E surface. The Approve handler's defense-in-depth re-check
		//     fires after Authenticate, which itself requires crypto.operator
		//     capability (InGameCredentialsProvider step 4). The
		//     PlayerAttributeProvider's operator allow-list is captured at
		//     runCoreWithDeps boot time and is read-only thereafter (sub-epic B
		//     INV-B6: "no public mutation API"). There is no path through the
		//     production UDS surface where a session can be issued for a player
		//     who lacks crypto.operator at Approve time. Coverage of this branch
		//     lives in handler-level unit tests
		//     (internal/admin/approval/handler_test.go::TestApproveHandlerRejectsNotOperator).
		//
		//   - DENY_APPROVAL_ARGS_MISMATCH: NOT enforced by sub-epic D's Approve
		//     handler. Args-hash validation lands as part of sub-epic E's
		//     Rekey-proceed flow, where the primary reads back the approved row
		//     and recomputes the hash before proceeding. This E2E suite is
		//     bounded by sub-epic D's surface; the args-mismatch case will land
		//     in sub-epic E.
		// =========================================================================

		By("T26 scenario 1: dave approves carol's pending row -> approved_at + approved_by_player_id set")
		ridHappy := env.openApproval(env.playerC)
		daveToken, err := env.authenticate("dave", env.davePassword, env.computeTOTP(env.daveSecret))
		Expect(err).NotTo(HaveOccurred(), "Authenticate dave (T26 happy path)")
		Expect(daveToken).NotTo(BeEmpty(), "session token must be non-empty")

		Expect(env.approve(daveToken, ridHappy)).To(Succeed(),
			"dave approves carol's row (happy path)")

		approvedAt, approvedBy := env.approvalRow(ridHappy)
		Expect(approvedAt).NotTo(BeNil(),
			"approved_at MUST be set after successful Approve")
		Expect(approvedBy).To(Equal(env.playerD.String()),
			"approved_by_player_id MUST match second-op (dave)")

		By("T26 scenario 2: carol tries to approve her own row -> DENY_DUAL_CONTROL_SELF / CodeFailedPrecondition")
		ridSelf := env.openApproval(env.playerC)
		carolToken, err := env.authenticate("carol", env.carolPassword, env.computeTOTP(env.carolSecret))
		Expect(err).NotTo(HaveOccurred(), "Authenticate carol (for self-approval probe)")
		Expect(carolToken).NotTo(BeEmpty())
		// Store carol's token for T27 Rekey E2E reuse (in-memory session store;
		// not time-limited). Re-authenticating would fail with TOTP replay
		// prevention within the same 30-second step window.
		env.carolSessionToken = carolToken

		selfErr := env.approve(carolToken, ridSelf)
		Expect(selfErr).To(HaveOccurred(), "self-approval must fail")
		// connect.Code is the over-the-wire contract; the inner oops code
		// (DENY_DUAL_CONTROL_SELF, INV-D6) is server-internal taxonomy and is
		// covered by handler_test.go unit tests. ConnectRPC does not transmit
		// oops metadata to the client.
		Expect(connectErrCode(selfErr)).To(Equal(connect.CodeFailedPrecondition),
			"DENY_DUAL_CONTROL_SELF -> connect.CodeFailedPrecondition (INV-D6)")
		stillPendingAt, _ := env.approvalRow(ridSelf)
		Expect(stillPendingAt).To(BeNil(),
			"self-approval rejection MUST NOT mutate approved_at")

		By("T26 scenario 3: force-expired row -> DENY_APPROVAL_EXPIRED / CodeFailedPrecondition")
		ridExpired := env.openApproval(env.playerC)
		env.forceExpireApproval(ridExpired)

		// Reuse dave's session token from scenario 1 — still valid (in-memory
		// session store; not bound to TOTP step window).
		expiredErr := env.approve(daveToken, ridExpired)
		Expect(expiredErr).To(HaveOccurred(), "expired approval must fail")
		Expect(connectErrCode(expiredErr)).To(Equal(connect.CodeFailedPrecondition),
			"DENY_APPROVAL_EXPIRED -> connect.CodeFailedPrecondition (INV-D5)")

		By("T26 scenario 4: dave approves carol's row, then carol tries again -> DENY_APPROVAL_ALREADY_APPROVED / CodeFailedPrecondition")
		// Need a second-op who is NOT carol (primary) for the first approval —
		// dave fits. After dave's successful approval, carol (also operator+admin,
		// but the primary so DENY_DUAL_CONTROL_SELF would mask the row state) is
		// the wrong identity to probe with. Use dave's second attempt (self-op-
		// repeat is fine for already-approved differentiation: the UPDATE's
		// approved_at IS NULL predicate fails before primary == secondOp could).
		ridAlready := env.openApproval(env.playerC)
		Expect(env.approve(daveToken, ridAlready)).To(Succeed(),
			"dave approves carol's row first (T26 scenario 4)")

		alreadyErr := env.approve(daveToken, ridAlready)
		Expect(alreadyErr).To(HaveOccurred(),
			"second approval on already-approved row must fail")
		Expect(connectErrCode(alreadyErr)).To(Equal(connect.CodeFailedPrecondition),
			"DENY_APPROVAL_ALREADY_APPROVED -> connect.CodeFailedPrecondition (INV-D7)")

		By("T26 scenario 5 (LAST — destructive): RemoveRole on dave's character mid-session -> DENY_NOT_ADMIN_ROLE / CodePermissionDenied")
		// dave authenticated successfully in scenario 1 (admin role present at
		// the time, INV-D16 step 5 passed). Now we remove the role to simulate
		// out-of-band revocation. The Approve handler's runtime role re-check
		// MUST then reject dave's session even though the in-memory session
		// store still holds his token.
		Expect(env.roleStore.RemoveRole(env.ctx, env.daveCharID, access.RoleAdmin)).To(Succeed(),
			"RemoveRole RoleAdmin on dave's character")

		ridRoleRevoked := env.openApproval(env.playerC)
		roleErr := env.approve(daveToken, ridRoleRevoked)
		Expect(roleErr).To(HaveOccurred(),
			"approve after role revocation must fail")
		Expect(connectErrCode(roleErr)).To(Equal(connect.CodePermissionDenied),
			"DENY_NOT_ADMIN_ROLE -> connect.CodePermissionDenied (INV-D16 runtime role re-check)")
		rolePending, _ := env.approvalRow(ridRoleRevoked)
		Expect(rolePending).To(BeNil(),
			"role-rejected approval MUST NOT mutate approved_at")

		// =========================================================================
		// T27: AdminRekey production-boot E2E (bead jxo8.7.46)
		//
		// Runs within the same boot as T25/T26 (single-process prometheus
		// constraint). Uses carol as the operator (untouched by T25/T26 except
		// for T26 scenarios 1–4 which leave carol's session intact; T26 scenario
		// 5 only revokes dave's admin role, not carol's). The scene context
		// "e2e-rekey-t27" was pre-seeded with a v1 DEK by seedAdminRekeyDEK
		// in setupAdminAuthEnv (before server boot). Carol re-authenticates here
		// with a fresh TOTP code rather than reusing carolToken (which may have
		// aged past a TOTP step boundary).
		// =========================================================================
		runAdminRekeyScenario(env)

		// =========================================================================
		// F r8 (sub-epic F): AdminReadStream production-boot E2E scenarios
		// (bead jxo8.8.41 / R.17).
		//
		// Runs within the same boot as T25/T26/T27 (prometheus.DefaultRegisterer
		// is process-wide). Reuses env.carolSessionToken from T26 scenario 2 —
		// in-memory session store has no expiry, so the token survives all
		// prior scenarios. T26 scenario 5 revokes dave's admin role but does
		// NOT touch carol; she retains crypto.operator + admin.
		//
		// Scenarios driven by runAdminReadStreamScenarios (defined in
		// admin_read_stream_e2e_test.go):
		//   - F-E1:  happy path single context
		//   - F-E2:  whole-game wildcard with defaulted bounds
		//   - F-E13: multi-context k-way merge (global timestamp order)
		//   - F-E14: sensitive-content filter (INV-CRYPTO-65)
		//   - F-E17: classifier surface (INV-CRYPTO-62 producers)
		// =========================================================================
		runAdminReadStreamScenarios(env)
	})
})
