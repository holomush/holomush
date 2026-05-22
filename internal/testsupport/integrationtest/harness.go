// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package integrationtest provides a general-purpose integration-test
// harness that wraps a real in-process holomush stack — Postgres
// (testcontainers), embedded NATS JetStream, and the production CoreServer —
// so test files can express invariants against live gRPC handlers without
// mocking the access-control/event-delivery surface.
//
// Originally built for the holomush-iwzt history-scope privacy epic
// (formerly named "privacytest"); now also serves the holomush-5b2j presence
// snapshot tests, the holomush-e4qo location_state wire-format test, and
// future privacy/session/scene integration suites. Renamed to
// "integrationtest" to reflect this broader scope.
//
// Test packages that currently import this harness:
//
//   - test/integration/privacy/   (iwzt history-scope privacy invariants)
//   - test/integration/presence/  (5b2j presence snapshot semantics)
//
// Stack composition:
//
//   - Shared Postgres testcontainer with migrations applied + per-test DB
//   - Embedded NATS JetStream (in-memory, per-test isolation)
//   - Production CoreServer wired to the above via real options
//
// Default ABAC engine is allow-all (privacy tests focus on session/history
// gates, not role enforcement). Tests that need denial-path coverage pass
// WithPolicyEngine(policytest.DenyAllEngine()) — see iwzt.10 / iwzt.11 for
// usage.
//
// Helper categories:
//
//   - Real-path drivers (e.g., EmitDirectEvent, ConnectGuest, ConnectAuthed):
//     exercise actual production code paths.
//   - Test-only escape hatches (e.g., MoveTo, DeleteCharacter, DeleteSession,
//     ExpireSession, SetLocationArrivedAt): direct SQL mutations used to
//     produce state shapes that production paths can't easily generate from
//     a test (e.g., expired sessions, future-dated LocationArrivedAt, guest
//     character cleanup that production logout doesn't perform). Each helper
//     documents what it bypasses and why.
//
// Usage:
//
//	ts := integrationtest.Start(t)
//	defer ts.Stop()
//	sess := ts.ConnectGuest(ctx)
//	sess.SendCommand(ctx, "look")
//	sess.Logout(ctx)
//
// Build tag: integration. This package is never imported by production code.
package integrationtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventbus/history"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	"github.com/holomush/holomush/test/testutil"
)

// Server is the privacy-test harness wrapping a real in-process holomush
// stack (Postgres + NATS JetStream + CoreServer) for integration testing of
// holomush-iwzt history-scope privacy invariants.
//
// Nine downstream integration tasks (iwzt.9 and later) depend on this
// package. Methods that rely on iwzt-introduced RPCs or fields not yet
// implemented will panic via t.Fatalf with a TODO message directing the
// implementer to the relevant bead.
type Server struct {
	t *testing.T

	// pool is the shared Postgres connection pool.
	pool *pgxpool.Pool

	// stores / repos
	playerSessionStore *store.PostgresPlayerSessionStore
	playerRepo         *authpg.PlayerRepository
	charRepo           auth.CharacterRepository
	sessionStore       session.Store
	locRepo            *worldpg.LocationRepository

	// services
	authService *auth.Service
	guestSvc    *auth.GuestService

	// bus (embedded NATS JetStream)
	bus *eventbustest.Embedded

	// coreServer is the in-process CoreServer (no network transport).
	coreServer *holoGRPC.CoreServer

	// guestStartLocationID is the location all guests are placed into.
	guestStartLocationID ulid.ULID
}

// StartOption tunes Start construction. Tests pass options to override
// harness defaults (e.g., the ABAC policy engine).
type StartOption func(*startConfig)

// startConfig holds resolved Start options.
type startConfig struct {
	accessEngine types.AccessPolicyEngine
}

// WithPolicyEngine overrides the harness's default allow-all ABAC engine.
// Tests that need to exercise denial paths — e.g., the I-PRIV-1 hard-gate
// (iwzt.10) or the I-PRIV-5 wire-opacity meta-test (iwzt.11) — pass a
// stricter engine such as policytest.DenyAllEngine so staffOverride
// returns false and the hard-gate is exercised end-to-end.
func WithPolicyEngine(eng types.AccessPolicyEngine) StartOption {
	return func(c *startConfig) { c.accessEngine = eng }
}

// Start bootstraps a full in-process holomush stack and returns a Server.
// The caller MUST call Stop() (typically via defer) to release resources.
//
// The stack consists of:
//   - A shared Postgres testcontainer with migrations applied (per-test DB)
//   - An embedded NATS JetStream server (in-memory, per-test isolation)
//   - An in-process CoreServer wired to the above
//
// AllowAll ABAC engine is the default — privacy tests focus on session/
// history gates, not role enforcement. Pass WithPolicyEngine to override
// for tests that need denial-path coverage.
func Start(t *testing.T, opts ...StartOption) *Server {
	t.Helper()

	ctx := context.Background()

	// Postgres: shared container, fresh per-test database.
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	evStore, err := store.NewPostgresEventStore(ctx, connStr)
	require.NoError(t, err, "integrationtest.Start: open event store")
	t.Cleanup(evStore.Close)

	pool := evStore.Pool()

	// Stores and repos.
	playerSessionStore := store.NewPostgresPlayerSessionStore(pool)
	playerRepo := authpg.NewPlayerRepository(pool)
	hasher := auth.NewArgon2idHasher()

	authService, err := auth.NewAuthService(playerRepo, playerSessionStore, hasher)
	require.NoError(t, err, "integrationtest.Start: create auth service")

	worldCharRepo := worldpg.NewCharacterRepository(pool)
	charRepo := &authCharRepoAdapter{pool: pool, charRepo: worldCharRepo}
	sessionStoreInst := store.NewPostgresSessionStore(pool)
	locRepo := worldpg.NewLocationRepository(pool)

	// Guest start location: create a persistent location for guests.
	guestLocID := idgen.New()
	guestLoc := &world.Location{
		ID:           guestLocID,
		Name:         "Crossroads",
		Description:  "A well-travelled intersection.",
		Type:         world.LocationTypePersistent,
		ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
	}
	err = locRepo.Create(ctx, guestLoc)
	require.NoError(t, err, "integrationtest.Start: create guest start location")

	// GuestService wiring.
	guestNamer := naming.NewGemstoneElementTheme()
	guestBindingRepo := worldpg.NewBindingRepository(pool)
	guestTransactor := worldpg.NewTransactor(pool)
	guestSvc, err := auth.NewGuestService(
		telnet.NewGuestAuthenticator(guestNamer, guestLocID),
		playerRepo, charRepo, playerSessionStore,
		guestTransactor, guestBindingRepo,
	)
	require.NoError(t, err, "integrationtest.Start: create guest service")

	// Embedded NATS bus (in-memory, cleaned up via t.Cleanup).
	bus := eventbustest.New(t)

	// Resolve options. Default ABAC engine is allowAll (privacy tests focus
	// on session/history gates, not role enforcement). Tests that need
	// denial-path coverage override via WithPolicyEngine.
	cfg := &startConfig{accessEngine: &allowAllPolicyEngine{}}
	for _, opt := range opts {
		opt(cfg)
	}
	pe := cfg.accessEngine

	// Command dispatcher (minimal: no commands registered).
	dispatcher, err := command.NewDispatcher(command.NewRegistry(), pe)
	require.NoError(t, err, "integrationtest.Start: create command dispatcher")
	cmdServices := command.NewTestServices(command.ServicesConfig{Engine: pe})

	// Core engine with a no-op event appender.
	engine := core.NewEngine(&noopEventAppender{})

	// HistoryReader: minimal wiring against the embedded bus's JetStream
	// and the test Postgres pool. All crypto/audit/fence options are
	// nil-defaulted — the production newHistoryReader in
	// cmd/holomush/sub_grpc.go layers those on, but for privacy-invariant
	// tests the bare JS+Postgres tier is sufficient.
	historyReader := history.NewReader(bus.JS, pool, 30*24*time.Hour, time.Now)

	// CoreServer wired with all required subsystems.
	coreServer := holoGRPC.NewCoreServer(
		engine,
		sessionStoreInst,
		dispatcher,
		cmdServices,
		holoGRPC.WithAuthService(authService),
		holoGRPC.WithPlayerSessionRepo(playerSessionStore),
		holoGRPC.WithPlayerRepo(playerRepo),
		holoGRPC.WithCharacterRepo(charRepo),
		holoGRPC.WithCharacterNameResolver(holoGRPC.NewRepoCharacterNameResolver(worldCharRepo)),
		holoGRPC.WithSessionStore(sessionStoreInst),
		holoGRPC.WithGuestService(guestSvc),
		// Wire embedded bus subscriber so Subscribe calls succeed for
		// WaitForEvent / DrainEvents paths.
		holoGRPC.WithSubscriber(bus.Bus.Subscriber()),
		// HistoryReader powers QueryStreamHistory end-to-end so privacy
		// integration tests can exercise the full RPC path.
		holoGRPC.WithHistoryReader(historyReader),
		// AccessEngine drives staffOverride() in QueryStreamHistory; with
		// it unwired, every override check returns false (the nil-engine
		// short-circuit), defeating I-PRIV-6 tests. The harness uses
		// allowAllPolicyEngine so override semantics are exercised
		// without the operational complexity of seeded ABAC policies.
		holoGRPC.WithAccessEngine(pe),
	)

	return &Server{
		t:                    t,
		pool:                 pool,
		playerSessionStore:   playerSessionStore,
		playerRepo:           playerRepo,
		charRepo:             charRepo,
		sessionStore:         sessionStoreInst,
		locRepo:              locRepo,
		authService:          authService,
		guestSvc:             guestSvc,
		bus:                  bus,
		coreServer:           coreServer,
		guestStartLocationID: guestLocID,
	}
}

// Stop tears down the in-process stack. Idempotent.
// Postgres and NATS cleanup are handled by t.Cleanup() registered in Start.
func (s *Server) Stop() {
	// Resources cleaned up by t.Cleanup handlers registered in Start.
}

// NewLocation creates a fresh persistent location in the world and returns
// its ULID. Bypasses ABAC (direct repo write for test convenience).
func (s *Server) NewLocation(ctx context.Context) ulid.ULID {
	s.t.Helper()
	locID := idgen.New()
	loc := &world.Location{
		ID:           locID,
		Name:         "TestLoc_" + locID.String()[:8],
		Description:  "A test location.",
		Type:         world.LocationTypePersistent,
		ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
	}
	err := s.locRepo.Create(ctx, loc)
	require.NoError(s.t, err, "integrationtest.Server.NewLocation: create location")
	return loc.ID
}

// NewSceneWithoutMember creates a scene with no members and returns its ULID.
//
// TODO(iwzt-9): implement via FocusCoordinator once scene RPCs are wired.
func (s *Server) NewSceneWithoutMember(_ context.Context) ulid.ULID {
	s.t.Fatalf("integrationtest.Server.NewSceneWithoutMember: TODO iwzt-9 — scene RPCs not yet wired")
	return ulid.ULID{}
}

// ExpireSession directly marks a session row as expired in Postgres.
// Used by iwzt tests to force session-expiry scenarios.
func (s *Server) ExpireSession(ctx context.Context, sessionID string) {
	s.t.Helper()
	now := time.Now().UTC()
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET status = $1, expires_at = $2, updated_at = $2 WHERE id = $3`,
		string(session.StatusExpired), now, sessionID)
	require.NoError(s.t, err, "integrationtest.Server.ExpireSession")
	require.Equalf(s.t, int64(1), tag.RowsAffected(),
		"integrationtest.Server.ExpireSession: expected 1 row affected, got %d (sessionID=%s)", tag.RowsAffected(), sessionID)
}

// SetLocationArrivedAt directly mutates a session's location_arrived_at column
// in Postgres. Used by 5b2j tests to exercise floor-bypass semantics
// (I-PRES-2): the snapshot RPC reads sessionStore directly and is exempt from
// the I-PRIV-1 temporal floor, so manipulating this column should NOT affect
// ListFocusPresence's behavior.
func (s *Server) SetLocationArrivedAt(ctx context.Context, sessionID string, t time.Time) {
	s.t.Helper()
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET location_arrived_at = $1, updated_at = $1 WHERE id = $2`,
		t.UTC(), sessionID)
	require.NoError(s.t, err, "integrationtest.Server.SetLocationArrivedAt")
	require.Equalf(s.t, int64(1), tag.RowsAffected(),
		"integrationtest.Server.SetLocationArrivedAt: expected 1 row affected, got %d (sessionID=%s)", tag.RowsAffected(), sessionID)
}

// DeleteCharacter removes a character row + its FK-dependent rows from
// Postgres in dependency-safe order. Used by iwzt.21 (I-PRIV-2 guest
// name-reuse) to simulate guest-character cleanup that production logout
// does NOT currently perform — without this, the unique-name constraint on
// `characters.LOWER(name)` blocks any subsequent guest from drawing the
// same display name, defeating the name-reuse scenario.
//
// Production guest service relies on ExistsByName to retry-on-collision;
// this helper is test-only and MUST NOT be invoked from production paths.
func (s *Server) DeleteCharacter(ctx context.Context, charID ulid.ULID) {
	s.t.Helper()
	charIDStr := charID.String()

	// FK-safe order: dependent rows first (sessions, bindings, roles, owned
	// objects), then the character row. sessions for this character must be
	// gone before the character can be deleted; the test contract is that
	// Logout has already removed them, but DELETE is idempotent so we cover
	// that case too. objects.owner_id REFERENCES characters(id) defaults to
	// ON DELETE RESTRICT (per migrations/000001_baseline.up.sql), so any
	// character-owned objects would block the character DELETE without an
	// explicit pre-clean.
	for _, child := range []struct{ table, col string }{
		{"sessions", "character_id"},
		{"player_character_bindings", "character_id"},
		{"character_roles", "character_id"},
		{"objects", "owner_id"},
	} {
		_, err := s.pool.Exec(ctx, "DELETE FROM "+child.table+" WHERE "+child.col+" = $1", charIDStr)
		require.NoError(s.t, err, "integrationtest.Server.DeleteCharacter: clean %s", child.table)
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charIDStr)
	require.NoError(s.t, err, "integrationtest.Server.DeleteCharacter: delete characters")
	require.Equalf(s.t, int64(1), tag.RowsAffected(),
		"integrationtest.Server.DeleteCharacter: expected 1 row affected, got %d (charID=%s)",
		tag.RowsAffected(), charIDStr)
}

// ConnectGuest creates a guest player+character and opens a game session.
// The returned Session is ready for SendCommand / DrainEvents / Logout calls.
func (s *Server) ConnectGuest(ctx context.Context) *Session {
	s.t.Helper()

	resp, err := s.coreServer.CreateGuest(ctx, &corev1.CreateGuestRequest{})
	require.NoError(s.t, err, "integrationtest.ConnectGuest: CreateGuest RPC")
	require.True(s.t, resp.GetSuccess(), "integrationtest.ConnectGuest: CreateGuest failed: %s", resp.GetErrorMessage())

	rawToken := resp.GetPlayerSessionToken()
	charID, parseErr := ulid.Parse(resp.GetDefaultCharacterId())
	require.NoError(s.t, parseErr, "integrationtest.ConnectGuest: parse character ID")

	selResp, err := s.coreServer.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: rawToken,
		CharacterId:        charID.String(),
	})
	require.NoError(s.t, err, "integrationtest.ConnectGuest: SelectCharacter RPC")
	require.True(s.t, selResp.GetSuccess(),
		"integrationtest.ConnectGuest: SelectCharacter failed: %s", selResp.GetErrorMessage())

	// Hydrate session timestamps from the persisted row, NOT from time.Now() —
	// see the parallel block in ConnectAuthedWithRoles for the rationale.
	persisted, getErr := s.sessionStore.Get(ctx, selResp.GetSessionId())
	require.NoError(s.t, getErr, "integrationtest.ConnectGuest: read persisted session")

	return &Session{
		server:             s,
		SessionID:          selResp.GetSessionId(),
		CharacterID:        charID,
		CharacterName:      selResp.GetCharacterName(),
		LocationID:         s.guestStartLocationID,
		OriginalLocationID: s.guestStartLocationID,
		LocationArrivedAt:  persisted.LocationArrivedAt,
		SessionCreatedAt:   persisted.CreatedAt,
		LastReattachAt:     time.Time{},
		playerSessionToken: rawToken,
	}
}

// ConnectAuthed creates a named player+character and opens a game session.
// The character is placed at the server's guest start location.
func (s *Server) ConnectAuthed(ctx context.Context, charName string) *Session {
	return s.ConnectAuthedWithRoles(ctx, charName, nil)
}

// ConnectAuthedWithRoles creates a named player+character with the given
// roles and opens a game session. If roles is non-nil, each role is inserted
// into character_roles directly via Postgres (bypassing ABAC for harness
// convenience).
func (s *Server) ConnectAuthedWithRoles(ctx context.Context, charName string, roles []string) *Session {
	s.t.Helper()

	// Unique username per character name to avoid collisions across tests.
	username := charName + "_" + idgen.New().String()[:8]
	password := "TestPassword1!"

	// Register the player account.
	player, playerSession, rawToken, err := s.authService.CreatePlayer(ctx, username, password, "")
	require.NoError(s.t, err, "integrationtest.ConnectAuthedWithRoles: CreatePlayer")

	// Persist the player session so SelectCharacter can resolve the token.
	require.NoError(s.t, s.playerSessionStore.Create(ctx, playerSession),
		"integrationtest.ConnectAuthedWithRoles: persist player session")

	// Create the character directly (bypasses characterService wiring).
	startLocID := s.guestStartLocationID
	char, err := world.NewCharacter(player.ID, charName)
	require.NoError(s.t, err, "integrationtest.ConnectAuthedWithRoles: NewCharacter")
	char.LocationID = &startLocID
	// authCharRepoAdapter.Create delegates to worldpg.CharacterRepository.Create.
	require.NoError(s.t, s.charRepo.Create(ctx, char),
		"integrationtest.ConnectAuthedWithRoles: persist character")

	// Stamp roles into character_roles.
	for _, role := range roles {
		_, roleErr := s.pool.Exec(ctx,
			`INSERT INTO character_roles (character_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			char.ID.String(), role)
		require.NoError(s.t, roleErr, "integrationtest.ConnectAuthedWithRoles: insert role %q", role)
	}

	// Open a game session by selecting the character.
	selResp, err := s.coreServer.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: rawToken,
		CharacterId:        char.ID.String(),
	})
	require.NoError(s.t, err, "integrationtest.ConnectAuthedWithRoles: SelectCharacter RPC")
	require.True(s.t, selResp.GetSuccess(),
		"integrationtest.ConnectAuthedWithRoles: SelectCharacter failed: %s", selResp.GetErrorMessage())

	// Hydrate session timestamps from the persisted session row, NOT from
	// time.Now() — the server-side LocationArrivedAt drives the I-PRIV-1 /
	// I-PRIV-6 floor in QueryStreamHistory, so tests that assert against
	// it MUST see the canonical value (per CodeRabbit thread on PR #4048).
	persisted, getErr := s.sessionStore.Get(ctx, selResp.GetSessionId())
	require.NoError(s.t, getErr, "integrationtest.ConnectAuthedWithRoles: read persisted session")

	return &Session{
		server:             s,
		SessionID:          selResp.GetSessionId(),
		CharacterID:        char.ID,
		CharacterName:      selResp.GetCharacterName(),
		LocationID:         s.guestStartLocationID,
		OriginalLocationID: s.guestStartLocationID,
		LocationArrivedAt:  persisted.LocationArrivedAt,
		SessionCreatedAt:   persisted.CreatedAt,
		LastReattachAt:     time.Time{},
		playerSessionToken: rawToken,
	}
}

// AuthedPlayer returns an AuthedPlayer handle for multi-session continuity tests.
//
// TODO(iwzt-9): implement authed player creation.
func (s *Server) AuthedPlayer(_ context.Context, _ string) *AuthedPlayer {
	s.t.Fatalf("integrationtest.Server.AuthedPlayer: TODO iwzt-9 — authed player creation not yet wired")
	return nil
}

// --- internal helpers ---

// noopEventAppender satisfies core.EventAppender for tests that don't
// exercise event storage. Mirrors the pattern in test/integration/auth/.
type noopEventAppender struct{}

func (*noopEventAppender) Append(_ context.Context, _ core.Event) error { return nil }

var _ core.EventAppender = (*noopEventAppender)(nil)

// authCharRepoAdapter wraps *worldpg.CharacterRepository to satisfy
// auth.CharacterRepository. Mirrors test/integration/auth/auth_suite_test.go.
type authCharRepoAdapter struct {
	pool     *pgxpool.Pool
	charRepo *worldpg.CharacterRepository
}

func (a *authCharRepoAdapter) Create(ctx context.Context, char *world.Character) error {
	return a.charRepo.Create(ctx, char)
}

func (a *authCharRepoAdapter) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := a.pool.QueryRow(
		ctx,
		"SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))", name,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", name).Wrap(err)
	}
	return exists, nil
}

func (a *authCharRepoAdapter) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	var count int
	err := a.pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM characters WHERE player_id = $1", playerID.String(),
	).Scan(&count)
	if err != nil {
		return 0, oops.Code("CHARACTER_COUNT_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return count, nil
}

func (a *authCharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	rows, err := a.pool.Query(
		ctx,
		`SELECT id, player_id, name, description, location_id, created_at
		 FROM characters WHERE player_id = $1 ORDER BY name`, playerID.String(),
	)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	defer rows.Close()

	var chars []*world.Character
	for rows.Next() {
		var c world.Character
		var idStr, pidStr string
		var locStr *string
		if scanErr := rows.Scan(&idStr, &pidStr, &c.Name, &c.Description, &locStr, &c.CreatedAt); scanErr != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(scanErr)
		}
		var parseErr error
		c.ID, parseErr = ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "id").Wrap(parseErr)
		}
		c.PlayerID, parseErr = ulid.Parse(pidStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "player_id").Wrap(parseErr)
		}
		if locStr != nil {
			lid, locParseErr := ulid.Parse(*locStr)
			if locParseErr != nil {
				return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "location_id").Wrap(locParseErr)
			}
			c.LocationID = &lid
		}
		chars = append(chars, &c)
	}
	if rows.Err() != nil {
		return nil, oops.Code("CHARACTER_ROWS_FAILED").Wrap(rows.Err())
	}
	return chars, nil
}

var _ auth.CharacterRepository = (*authCharRepoAdapter)(nil)

// allowAllPolicyEngine is a minimal AccessPolicyEngine that grants every
// request. Used in the privacy-test harness so tests focus on session/history
// privacy gates rather than ABAC policy enforcement.
type allowAllPolicyEngine struct{}

func (*allowAllPolicyEngine) Evaluate(_ context.Context, _ types.AccessRequest) (types.Decision, error) {
	return types.NewDecision(types.EffectAllow, "harness-allow-all", ""), nil
}

func (*allowAllPolicyEngine) CanPerformAction(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

var _ types.AccessPolicyEngine = (*allowAllPolicyEngine)(nil)
