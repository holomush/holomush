// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package list_session_streams_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// newTestSession returns a session.Info pre-populated with sensible defaults
// for ListSessionStreams integration tests. The RPC does not consult focus
// memberships or cursors, so defaults are minimal. PlayerID is supplied so
// the bd-jv7z ownership gate can compare against a seeded PlayerSession.
func newTestSession(id string, playerID, charID, locID ulid.ULID) *session.Info {
	now := time.Now().UTC()
	return &session.Info{
		ID:             id,
		PlayerID:       playerID,
		CharacterID:    charID,
		CharacterName:  "Alice",
		LocationID:     locID,
		IsGuest:        false,
		Status:         session.StatusActive,
		GridPresent:    true,
		EventCursors:   map[string]ulid.ULID{},
		CommandHistory: []string{},
		TTLSeconds:     300,
		MaxHistory:     50,
		CreatedAt:      now,
	}
}

// seedPlayerSession inserts a minimal `players` row for playerID (required
// by the player_sessions FK constraint), creates a real PlayerSession row
// owned by that player, and returns the plaintext token. Callers pass the
// token as ListSessionStreamsRequest.PlayerSessionToken so the bd-jv7z
// ownership gate in CoreServer.ListSessionStreams accepts the request.
func seedPlayerSession(ctx context.Context, playerID ulid.ULID) string {
	// Minimal player row — only id + unique username + non-null
	// password_hash are required by the schema. The password hash is
	// never validated here; the token is what we exercise.
	_, err := env.pool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'unused')`,
		playerID.String(), "lss_"+playerID.String())
	Expect(err).NotTo(HaveOccurred())

	raw := make([]byte, auth.SessionTokenBytes)
	_, err = rand.Read(raw)
	Expect(err).NotTo(HaveOccurred())
	token := hex.EncodeToString(raw)

	ps, err := auth.NewPlayerSession(playerID, auth.HashSessionToken(token), "", "", auth.PlayerSessionTTL)
	Expect(err).NotTo(HaveOccurred())
	Expect(env.playerSessionStore.Create(ctx, ps)).To(Succeed())
	return token
}

// newCoreServerForTest constructs a real CoreServer wired to the suite's
// PostgresEventStore and PostgresSessionStore. ListSessionStreams only needs
// the session store + focusCoordinator (nil here, so the fallback path runs).
//
// The dispatcher is required by NewCoreServer but is never invoked by
// ListSessionStreams, so we register an empty command registry.
func newCoreServerForTest() *grpcpkg.CoreServer {
	coreEngine := core.NewEngine(env.eventStore)

	reg := command.NewRegistry()
	cmdSvc := command.NewTestServices(command.ServicesConfig{
		Engine:  policytest.AllowAllEngine(),
		Session: env.sessionStore,
		Events:  env.eventStore,
	})
	dispatcher, err := command.NewDispatcher(reg, policytest.AllowAllEngine())
	Expect(err).NotTo(HaveOccurred())

	return grpcpkg.NewCoreServer(
		coreEngine,
		env.sessionStore,
		dispatcher,
		cmdSvc,
		grpcpkg.WithEventStore(env.eventStore),
		grpcpkg.WithAccessEngine(policytest.AllowAllEngine()),
		grpcpkg.WithPlayerSessionRepo(env.playerSessionStore),
	)
}

var _ = Describe("CoreService.ListSessionStreams", func() {
	var (
		testCtx    context.Context
		testCancel context.CancelFunc
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithTimeout(context.Background(), 30*time.Second)
		cleanupTestData(testCtx, env.pool)
	})

	AfterEach(func() {
		if testCancel != nil {
			testCancel()
		}
	})

	It("returns character and location streams for a fresh session via the fallback path", func() {
		// Arrange: real session persisted to Postgres; focusCoordinator is nil
		// on this server, so the handler runs the Subscribe-parity fallback
		// that emits character + location streams.
		playerID := ulid.Make()
		charID := ulid.Make()
		locID := ulid.Make()
		sessionID := core.NewULID().String()

		server := newCoreServerForTest()
		Expect(env.sessionStore.Set(testCtx, sessionID, newTestSession(sessionID, playerID, charID, locID))).To(Succeed())
		token := seedPlayerSession(testCtx, playerID)

		// Act.
		resp, err := server.ListSessionStreams(testCtx, &corev1.ListSessionStreamsRequest{
			SessionId:          sessionID,
			PlayerSessionToken: token,
		})

		// Assert: both ambient streams present, encoded with the canonical prefixes.
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.GetStreams()).To(ContainElement(world.CharacterStream(charID)))
		Expect(resp.GetStreams()).To(ContainElement(world.LocationStream(locID)))
		// Sanity-check the exact prefix format callers rely on.
		Expect(resp.GetStreams()).To(ContainElement("character:" + charID.String()))
		Expect(resp.GetStreams()).To(ContainElement("location:" + locID.String()))
	})

	It("rejects a request with empty session_id", func() {
		server := newCoreServerForTest()

		_, err := server.ListSessionStreams(testCtx, &corev1.ListSessionStreamsRequest{})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("session_id is required"))
	})

	It("rejects a request with a missing player session token (bd-jv7z ownership gate)", func() {
		playerID := ulid.Make()
		charID := ulid.Make()
		locID := ulid.Make()
		sessionID := core.NewULID().String()

		server := newCoreServerForTest()
		Expect(env.sessionStore.Set(testCtx, sessionID, newTestSession(sessionID, playerID, charID, locID))).To(Succeed())

		_, err := server.ListSessionStreams(testCtx, &corev1.ListSessionStreamsRequest{
			SessionId: sessionID,
			// PlayerSessionToken intentionally empty.
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("session not found"))
	})
})
