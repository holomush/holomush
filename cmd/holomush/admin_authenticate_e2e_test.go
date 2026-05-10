// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
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

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/totp"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
	"github.com/holomush/holomush/test/testutil"
)

// adminAuthEnv is the per-spec fixture: a fully-booted server (via
// runCoreWithDeps), a UDS-dialing AdminServiceClient, an independent
// query-only PG pool for events_audit assertions, and the secrets needed
// to compute valid TOTP codes for both seeded operators.
type adminAuthEnv struct {
	ctx        context.Context
	cancelCtx  context.CancelFunc
	serverDone chan error

	gameID string

	playerA       ulid.ULID // alice — operator + admin role
	playerB       ulid.ULID // bob — admin role, NOT operator (target of ResetTOTP)
	alicePassword string
	bobPassword   string
	aliceSecret   string
	bobSecret     string

	socketPath string
	client     adminv1connect.AdminServiceClient

	queryPool *pgxpool.Pool
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
	//        fingerprint (INV-33 startup integrity check).
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
	const alicePassword = "alice-correct-horse-battery-staple"
	const bobPassword = "bob-tr0ub4dor-3-secure-pw"

	hasher := auth.NewArgon2idHasher()
	aliceHash, err := hasher.Hash(alicePassword)
	Expect(err).NotTo(HaveOccurred(), "Hash alicePassword")
	bobHash, err := hasher.Hash(bobPassword)
	Expect(err).NotTo(HaveOccurred(), "Hash bobPassword")

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
	} {
		_, err = seedPool.Exec(seedCtx, `
			INSERT INTO players (id, username, password_hash, created_at, updated_at)
			VALUES ($1, $2, $3, now(), now())`,
			row.id, row.username, row.hash)
		Expect(err).NotTo(HaveOccurred(), "INSERT players %s", row.username)
	}

	// Characters (one per player). character_id is also a ULID.
	aliceCharID := ulid.Make().String()
	bobCharID := ulid.Make().String()
	for _, row := range []struct {
		id       string
		playerID string
		name     string
	}{
		{aliceCharID, playerA.String(), "Alice"},
		{bobCharID, playerB.String(), "Bob"},
	} {
		_, err = seedPool.Exec(seedCtx, `
			INSERT INTO characters (id, player_id, name)
			VALUES ($1, $2, $3)`,
			row.id, row.playerID, row.name)
		Expect(err).NotTo(HaveOccurred(), "INSERT character %s", row.name)
	}

	// RoleAdmin on alice's character (step 5 of InGameCredentialsProvider).
	// Bob does NOT need RoleAdmin — he's only the target of ResetTOTP, and
	// his Authenticate attempt in spec 3 fails at step 2 (DENY_NOT_ENROLLED)
	// before ever reaching the role check.
	rs := store.NewPostgresRoleStore(seedPool)
	Expect(rs.AddRole(seedCtx, aliceCharID, access.RoleAdmin)).To(Succeed(),
		"AddRole RoleAdmin on alice's character")

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

	// bob via Enroll (the bootstrap key has been consumed by alice).
	bobEnroll, err := totpSvc.Enroll(seedCtx, playerB)
	Expect(err).NotTo(HaveOccurred(), "Enroll bob")

	aliceSecret := aliceBoot.Enrollment.Secret
	bobSecret := bobEnroll.Enrollment.Secret
	Expect(aliceSecret).NotTo(BeEmpty())
	Expect(bobSecret).NotTo(BeEmpty())

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
	cryptoConfig := config.CryptoConfig{
		Operators:           []string{playerA.String()}, // alice only
		DualControlRequired: []string{},
	}

	// --- 6. Run server in goroutine; wait for the admin socket to appear. ---
	ctx, cancel := context.WithCancel(context.Background())
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runCoreWithDeps(ctx, cfg, gameConfig, authConfig, eventBusConfig, cryptoConfig, cmd, nil)
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

	// --- 8. Independent query pool for events_audit assertions. ---
	queryPool, err := pgxpool.New(context.Background(), connStr)
	Expect(err).NotTo(HaveOccurred(), "pgxpool.New for query")

	return &adminAuthEnv{
		ctx:           ctx,
		cancelCtx:     cancel,
		serverDone:    serverDone,
		gameID:        gameID,
		playerA:       playerA,
		playerB:       playerB,
		alicePassword: alicePassword,
		bobPassword:   bobPassword,
		aliceSecret:   aliceSecret,
		bobSecret:     bobSecret,
		socketPath:    socketPath,
		client:        client,
		queryPool:     queryPool,
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
			var lockedUntil *time.Time
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
	})
})
