// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/core"
	holoFocus "github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/grpc/scenemocks"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
	"github.com/holomush/holomush/internal/world"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
)

const testSAToken = "test-scene-access-token"

// buildSATestPS returns a PlayerSession with a hashed token for the given playerID.
func buildSATestPS(t *testing.T, playerID ulid.ULID) *auth.PlayerSession {
	t.Helper()
	return &auth.PlayerSession{
		ID:        idgen.New(),
		PlayerID:  playerID,
		TokenHash: auth.HashSessionToken(testSAToken),
	}
}

// buildSASessionRepo returns a mock PlayerSessionRepository that returns ps on GetByTokenHash.
func buildSASessionRepo(t *testing.T, ps *auth.PlayerSession) *authmocks.MockPlayerSessionRepository {
	t.Helper()
	repo := authmocks.NewMockPlayerSessionRepository(t)
	repo.EXPECT().GetByTokenHash(mock.Anything, ps.TokenHash).Return(ps, nil).Maybe()
	repo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil).Maybe()
	return repo
}

// stubFocusCoordinator is a minimal Coordinator stub that records calls.
type stubFocusCoordinator struct {
	setConnFocusErr error
	joinFocusErr    error
	joinFocusCalls  int
}

func (s *stubFocusCoordinator) JoinFocus(_ context.Context, _ string, _ session.FocusKey) error {
	s.joinFocusCalls++
	return s.joinFocusErr
}

func (s *stubFocusCoordinator) LeaveFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
}

func (s *stubFocusCoordinator) LeaveFocusByTarget(_ context.Context, _ session.FocusKey) (session.LeaveByTargetResult, error) {
	return session.LeaveByTargetResult{}, nil
}

func (s *stubFocusCoordinator) PresentFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
}

func (s *stubFocusCoordinator) RestoreFocus(_ context.Context, _ string) (holoFocus.RestorePlan, error) {
	return holoFocus.RestorePlan{}, nil
}

func (s *stubFocusCoordinator) IsAnyConnFocused(_ context.Context, _, _ ulid.ULID) (bool, error) {
	return false, nil
}

func (s *stubFocusCoordinator) RestoreConnectionFocus(_ context.Context, _ string, _ ulid.ULID) error {
	return nil
}

func (s *stubFocusCoordinator) SetConnectionFocus(_ context.Context, _ ulid.ULID, _ *session.FocusKey, _ bool) (holoFocus.SetConnectionFocusResult, error) {
	return holoFocus.SetConnectionFocusResult{}, s.setConnFocusErr
}

func (s *stubFocusCoordinator) AutoFocusOnJoin(_ context.Context, _, _ ulid.ULID) (holoFocus.AutoFocusOnJoinResponse, error) {
	return holoFocus.AutoFocusOnJoinResponse{}, nil
}

func (s *stubFocusCoordinator) GetConnectionFocus(_ context.Context, _ ulid.ULID) (*session.FocusKey, error) {
	return nil, nil
}

// stubPluginManager is a minimal Manager stub for BeginServiceDispatch.
type stubPluginManager struct {
	// capturedActor is set by BeginServiceDispatch so tests can assert on it.
	capturedActor    core.Actor
	capturedPlayerID string
	dispatchErr      error
}

func (s *stubPluginManager) BeginServiceDispatch(ctx context.Context, _ string, actor core.Actor, ownerPlayerID string) (context.Context, func(), error) {
	s.capturedActor = actor
	s.capturedPlayerID = ownerPlayerID
	if s.dispatchErr != nil {
		return nil, nil, s.dispatchErr
	}
	return ctx, func() {}, nil
}

// newTestSceneAccessServer is the test constructor shared by all SA tests.
func newTestSceneAccessServer(
	t *testing.T,
	sessionRepo *authmocks.MockPlayerSessionRepository,
	playerRepo *authmocks.MockPlayerRepository,
	charRepo *authmocks.MockCharacterRepository,
	sessStore *sessionmocks.MockStore,
	coord holoFocus.Coordinator,
	sceneClient scenev1.SceneServiceClient,
	mgr sceneAccessPluginManager,
) *SceneAccessServer {
	t.Helper()
	return &SceneAccessServer{
		playerSessionRepo: sessionRepo,
		playerRepo:        playerRepo,
		charRepo:          charRepo,
		sessionStore:      sessStore,
		coordinator:       coord,
		sceneClient:       sceneClient,
		pluginManager:     mgr,
	}
}

// TestSceneAccessOverridesClientSuppliedCharacterWithOwnedAlt verifies that the
// facade enforces server-side identity resolution: the downstream call always
// receives the SERVER-VERIFIED character, never the raw client-supplied one.
//
// Verifies: INV-SCENE-63
func TestSceneAccessOverridesClientSuppliedCharacterWithOwnedAlt(t *testing.T) {
	ctx := context.Background()

	playerID := idgen.New()
	charA := &world.Character{ID: idgen.New(), PlayerID: playerID, Name: "Alice"}
	charB := &world.Character{ID: idgen.New(), PlayerID: playerID, Name: "Bob"}
	unownedCharID := idgen.New() // C — not owned by this player

	ps := buildSATestPS(t, playerID)

	player := &auth.Player{ID: playerID, IsGuest: false}
	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(player, nil).Maybe()

	psRepo := buildSASessionRepo(t, ps)

	charRepo := authmocks.NewMockCharacterRepository(t)
	// ListByPlayer returns charA and charB (player owns both, NOT unownedCharID).
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{charA, charB}, nil).Maybe()

	sessStore := sessionmocks.NewMockStore(t)
	sceneMock := scenemocks.NewMockSceneServiceClient(t)
	mgr := &stubPluginManager{}

	srv := newTestSceneAccessServer(t, psRepo, playerRepo, charRepo, sessStore, &stubFocusCoordinator{}, sceneMock, mgr)

	t.Run("not owned character returns NotFound", func(t *testing.T) {
		_, err := srv.ListScenesForViewer(ctx, &sceneaccessv1.ListScenesForViewerRequest{
			SessionId:          "ignored",
			PlayerSessionToken: testSAToken,
			CharacterId:        unownedCharID.String(),
		})
		require.Error(t, err)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.NotFound, st.Code(), "character not owned by player MUST return NotFound")
	})

	t.Run("owned character B reaches downstream with verified ID", func(t *testing.T) {
		sceneMock.EXPECT().ListScenes(mock.Anything, mock.MatchedBy(func(req *scenev1.ListScenesRequest) bool {
			return req.GetCharacterId() == charB.ID.String()
		})).Return(&scenev1.ListScenesResponse{}, nil).Once()

		_, err := srv.ListScenesForViewer(ctx, &sceneaccessv1.ListScenesForViewerRequest{
			SessionId:          "ignored",
			PlayerSessionToken: testSAToken,
			CharacterId:        charB.ID.String(),
		})
		require.NoError(t, err)
		// Assert the BeginServiceDispatch actor == verified character (INV-SCENE-63).
		assert.Equal(t, core.ActorCharacter, mgr.capturedActor.Kind, "dispatch actor kind MUST be character")
		assert.Equal(t, charB.ID.String(), mgr.capturedActor.ID, "dispatch actor ID MUST be the verified character, not the raw client value")
	})
}

// TestSceneAccessDeniesGuestPlayersEverywhere verifies that all 9 RPCs return
// codes.PermissionDenied for guest players without ever invoking the downstream
// SceneService client.
//
// Verifies: INV-SCENE-64
func TestSceneAccessDeniesGuestPlayersEverywhere(t *testing.T) {
	ctx := context.Background()

	playerID := idgen.New()
	guestPlayer := &auth.Player{ID: playerID, IsGuest: true}
	charID := idgen.New()

	ps := buildSATestPS(t, playerID)
	psRepo := buildSASessionRepo(t, ps)

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(guestPlayer, nil).Maybe()

	charRepo := authmocks.NewMockCharacterRepository(t)
	sessStore := sessionmocks.NewMockStore(t)
	// sceneClient should NEVER be called for a guest player.
	sceneMock := scenemocks.NewMockSceneServiceClient(t)
	mgr := &stubPluginManager{}

	srv := newTestSceneAccessServer(t, psRepo, playerRepo, charRepo, sessStore, &stubFocusCoordinator{}, sceneMock, mgr)

	connID := idgen.New()
	publishedSceneID := idgen.New()

	cases := []struct {
		name string
		call func() error
	}{
		{
			"ListScenesForViewer",
			func() error {
				_, err := srv.ListScenesForViewer(ctx, &sceneaccessv1.ListScenesForViewerRequest{
					PlayerSessionToken: testSAToken, CharacterId: charID.String(),
				})
				return err
			},
		},
		{
			"GetSceneForViewer",
			func() error {
				_, err := srv.GetSceneForViewer(ctx, &sceneaccessv1.GetSceneForViewerRequest{
					PlayerSessionToken: testSAToken, CharacterId: charID.String(), SceneId: idgen.New().String(),
				})
				return err
			},
		},
		{
			"ListMyScenes",
			func() error {
				_, err := srv.ListMyScenes(ctx, &sceneaccessv1.ListMyScenesRequest{
					PlayerSessionToken: testSAToken, CharacterId: charID.String(),
				})
				return err
			},
		},
		{
			"WatchScene",
			func() error {
				_, err := srv.WatchScene(ctx, &sceneaccessv1.WatchSceneRequest{
					PlayerSessionToken: testSAToken, CharacterId: charID.String(), SceneId: idgen.New().String(),
				})
				return err
			},
		},
		{
			"ExportScene",
			func() error {
				_, err := srv.ExportScene(ctx, &sceneaccessv1.ExportSceneRequest{
					PlayerSessionToken: testSAToken, CharacterId: charID.String(),
					SceneId: idgen.New().String(), Format: "markdown",
				})
				return err
			},
		},
		{
			"SetSceneFocus",
			func() error {
				_, err := srv.SetSceneFocus(ctx, &sceneaccessv1.SetSceneFocusRequest{
					PlayerSessionToken: testSAToken, ConnectionId: connID.String(),
				})
				return err
			},
		},
		{
			"ListPublishedScenes",
			func() error {
				_, err := srv.ListPublishedScenes(ctx, &sceneaccessv1.ListPublishedScenesRequest{
					PlayerSessionToken: testSAToken,
				})
				return err
			},
		},
		{
			"GetPublicSceneArchive",
			func() error {
				_, err := srv.GetPublicSceneArchive(ctx, &sceneaccessv1.GetPublicSceneArchiveRequest{
					PlayerSessionToken: testSAToken, PublishedSceneId: publishedSceneID.String(),
				})
				return err
			},
		},
		{
			"DownloadPublicSceneArchive",
			func() error {
				_, err := srv.DownloadPublicSceneArchive(ctx, &sceneaccessv1.DownloadPublicSceneArchiveRequest{
					PlayerSessionToken: testSAToken, PublishedSceneId: publishedSceneID.String(), Format: "markdown",
				})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_denies_guest", func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.PermissionDenied, st.Code(),
				"guest players MUST be denied PermissionDenied on %s", tc.name)
		})
	}
	// Downstream client MUST NOT have been invoked.
	sceneMock.AssertNotCalled(t, "ListScenes")
	sceneMock.AssertNotCalled(t, "GetScene")
	sceneMock.AssertNotCalled(t, "ListCharacterScenes")
	sceneMock.AssertNotCalled(t, "WatchScene")
	sceneMock.AssertNotCalled(t, "ExportSceneLog")
	sceneMock.AssertNotCalled(t, "ListPublishedScenes")
	sceneMock.AssertNotCalled(t, "GetPublicSceneArchive")
	sceneMock.AssertNotCalled(t, "DownloadPublicSceneArchive")
}

// TestWatchSceneRequiresExistingAltSession verifies that WatchScene returns
// codes.FailedPrecondition when the character has no game session (i.e. the
// player hasn't selected the character yet). The downstream SceneService MUST
// NOT be called.
func TestWatchSceneRequiresExistingAltSession(t *testing.T) {
	ctx := context.Background()

	playerID := idgen.New()
	charID := idgen.New()

	ps := buildSATestPS(t, playerID)
	psRepo := buildSASessionRepo(t, ps)

	player := &auth.Player{ID: playerID, IsGuest: false}
	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(player, nil).Maybe()

	charRepo := authmocks.NewMockCharacterRepository(t)
	ownedChar := &world.Character{ID: charID, PlayerID: playerID}
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{ownedChar}, nil).Maybe()

	sessStore := sessionmocks.NewMockStore(t)
	// FindByCharacter returns SESSION_NOT_FOUND.
	sessStore.EXPECT().FindByCharacter(mock.Anything, charID).Return(nil,
		oops.Code("SESSION_NOT_FOUND").Errorf("no session for character")).Maybe()

	sceneMock := scenemocks.NewMockSceneServiceClient(t)
	mgr := &stubPluginManager{}

	srv := newTestSceneAccessServer(t, psRepo, playerRepo, charRepo, sessStore, &stubFocusCoordinator{}, sceneMock, mgr)

	_, err := srv.WatchScene(ctx, &sceneaccessv1.WatchSceneRequest{
		PlayerSessionToken: testSAToken,
		CharacterId:        charID.String(),
		SceneId:            idgen.New().String(),
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code(),
		"SESSION_NOT_FOUND MUST become FailedPrecondition (select character first)")
	sceneMock.AssertNotCalled(t, "WatchScene")
}

// TestSceneAccessDispatchActorEqualsVerifiedCharacter pins the token+identity
// pairing invariant: BeginServiceDispatch MUST receive the server-verified
// character as its actor, not the raw client-supplied value. This is the
// abac-reviewer follow-up from holomush-5rh.8.21.
func TestSceneAccessDispatchActorEqualsVerifiedCharacter(t *testing.T) {
	ctx := context.Background()

	playerID := idgen.New()
	verifiedCharID := idgen.New()
	// The client supplies a DIFFERENT (unowned) character ID to try to spoof.
	spoofCharID := idgen.New()

	ps := buildSATestPS(t, playerID)
	psRepo := buildSASessionRepo(t, ps)

	player := &auth.Player{ID: playerID, IsGuest: false}
	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(player, nil).Maybe()

	charRepo := authmocks.NewMockCharacterRepository(t)
	ownedChar := &world.Character{ID: verifiedCharID, PlayerID: playerID}
	// Only verifiedCharID is owned; spoofCharID is not in the list.
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{ownedChar}, nil).Maybe()

	sessStore := sessionmocks.NewMockStore(t)
	sceneMock := scenemocks.NewMockSceneServiceClient(t)
	mgr := &stubPluginManager{}

	srv := newTestSceneAccessServer(t, psRepo, playerRepo, charRepo, sessStore, &stubFocusCoordinator{}, sceneMock, mgr)

	t.Run("spoof ID rejected before dispatch", func(t *testing.T) {
		_, err := srv.ListScenesForViewer(ctx, &sceneaccessv1.ListScenesForViewerRequest{
			PlayerSessionToken: testSAToken,
			CharacterId:        spoofCharID.String(),
		})
		require.Error(t, err)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.NotFound, st.Code())
		// mgr.capturedActor MUST remain zero — BeginServiceDispatch never called.
		assert.Equal(t, core.Actor{}, mgr.capturedActor,
			"BeginServiceDispatch MUST NOT be called when ownership check fails")
	})

	t.Run("verified char dispatch actor matches server-verified ID", func(t *testing.T) {
		sceneMock.EXPECT().ListScenes(mock.Anything, mock.Anything).Return(&scenev1.ListScenesResponse{}, nil).Once()

		_, err := srv.ListScenesForViewer(ctx, &sceneaccessv1.ListScenesForViewerRequest{
			PlayerSessionToken: testSAToken,
			CharacterId:        verifiedCharID.String(),
		})
		require.NoError(t, err)
		assert.Equal(t, core.ActorCharacter, mgr.capturedActor.Kind)
		assert.Equal(t, verifiedCharID.String(), mgr.capturedActor.ID,
			"dispatch actor MUST equal server-verified character, never spoof")
		assert.Equal(t, playerID.String(), mgr.capturedPlayerID,
			"dispatch ownerPlayerID MUST be the authenticated player")
	})
}

// buildSetSceneFocusServer wires a SceneAccessServer for SetSceneFocus tests:
// a non-guest player who owns exactly one character, whose game session has
// an auto-generated sessionID, and whose connection has the returned connID.
// Returns (server, charID, connID) — the two values callers need for requests.
func buildSetSceneFocusServer(
	t *testing.T,
	coord *stubFocusCoordinator,
	sceneMock scenev1.SceneServiceClient,
) (srv *SceneAccessServer, charID ulid.ULID, connID ulid.ULID) {
	t.Helper()
	playerID := idgen.New()
	charID = idgen.New()
	connID = idgen.New()
	sessionID := "sess-" + idgen.New().String()

	ps := buildSATestPS(t, playerID)
	psRepo := buildSASessionRepo(t, ps)

	player := &auth.Player{ID: playerID, IsGuest: false}
	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(player, nil).Maybe()

	ownedChar := &world.Character{ID: charID, PlayerID: playerID}
	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{ownedChar}, nil).Maybe()

	conn := &session.Connection{ID: connID, SessionID: sessionID, ClientType: "comms_hub"}
	gameSession := &session.Info{ID: sessionID, CharacterID: charID}

	sessStore := sessionmocks.NewMockStore(t)
	sessStore.EXPECT().GetConnection(mock.Anything, connID).Return(conn, nil).Maybe()
	sessStore.EXPECT().Get(mock.Anything, sessionID).Return(gameSession, nil).Maybe()

	mgr := &stubPluginManager{}
	srv = newTestSceneAccessServer(t, psRepo, playerRepo, charRepo, sessStore, coord, sceneMock, mgr)
	return
}

// TestSetSceneFocusDeniesNonParticipant verifies the privacy gate: a character
// that is not a participant of the target scene is rejected with PermissionDenied
// and JoinFocus is never called.
func TestSetSceneFocusDeniesNonParticipant(t *testing.T) {
	ctx := context.Background()
	coord := &stubFocusCoordinator{}
	sceneMock := scenemocks.NewMockSceneServiceClient(t)

	srv, _, connID := buildSetSceneFocusServer(t, coord, sceneMock)

	sceneID := idgen.New()
	// ListCharacterScenes returns a list that does NOT include sceneID.
	otherSceneID := idgen.New()
	otherScene := &scenev1.SceneInfo{Id: otherSceneID.String()}
	sceneMock.EXPECT().ListCharacterScenes(mock.Anything, mock.Anything).Return(
		&scenev1.ListCharacterScenesResponse{
			Scenes: []*scenev1.CharacterSceneInfo{
				{Scene: otherScene},
			},
		}, nil,
	).Once()

	_, err := srv.SetSceneFocus(ctx, &sceneaccessv1.SetSceneFocusRequest{
		PlayerSessionToken: testSAToken,
		ConnectionId:       connID.String(),
		SceneId:            sceneID.String(),
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code(),
		"non-participant MUST receive PermissionDenied")
	assert.Equal(t, 0, coord.joinFocusCalls,
		"JoinFocus MUST NOT be called when participation check fails")
}

// TestSetSceneFocusPropagatesInternalFromOwnershipInfraFailure verifies error
// fidelity (holomush-5rh.8.23): when the connection-ownership check fails due to
// an INFRA error (charRepo.ListByPlayer DB failure → ownedCharacter returns
// codes.Internal), SetSceneFocus propagates Internal rather than collapsing it
// into PermissionDenied — the denial code is reserved for a genuine not-owned
// connection, so an infra failure stays observable as a server error.
func TestSetSceneFocusPropagatesInternalFromOwnershipInfraFailure(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	charID := idgen.New()
	connID := idgen.New()
	sessionID := "sess-" + idgen.New().String()

	ps := buildSATestPS(t, playerID)
	psRepo := buildSASessionRepo(t, ps)

	player := &auth.Player{ID: playerID, IsGuest: false}
	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(player, nil).Maybe()

	// charRepo.ListByPlayer fails — ownedCharacter returns codes.Internal.
	charRepo := authmocks.NewMockCharacterRepository(t)
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return(nil, errors.New("db connection lost")).Maybe()

	conn := &session.Connection{ID: connID, SessionID: sessionID, ClientType: "comms_hub"}
	gameSession := &session.Info{ID: sessionID, CharacterID: charID}
	sessStore := sessionmocks.NewMockStore(t)
	sessStore.EXPECT().GetConnection(mock.Anything, connID).Return(conn, nil).Maybe()
	sessStore.EXPECT().Get(mock.Anything, sessionID).Return(gameSession, nil).Maybe()

	coord := &stubFocusCoordinator{}
	srv := newTestSceneAccessServer(t, psRepo, playerRepo, charRepo, sessStore, coord,
		scenemocks.NewMockSceneServiceClient(t), &stubPluginManager{})

	_, err := srv.SetSceneFocus(ctx, &sceneaccessv1.SetSceneFocusRequest{
		PlayerSessionToken: testSAToken,
		ConnectionId:       connID.String(),
		SceneId:            idgen.New().String(),
	})

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code(),
		"an infra failure in the ownership check MUST surface as Internal, not PermissionDenied")
	assert.Equal(t, 0, coord.joinFocusCalls, "JoinFocus MUST NOT be called when ownership check errors")
}

// TestSetSceneFocusJoinsFocusThenSetsConnectionFocus verifies the happy path for
// a participant: JoinFocus is called once, then SetConnectionFocus succeeds.
func TestSetSceneFocusJoinsFocusThenSetsConnectionFocus(t *testing.T) {
	ctx := context.Background()
	coord := &stubFocusCoordinator{}
	sceneMock := scenemocks.NewMockSceneServiceClient(t)

	srv, charID, connID := buildSetSceneFocusServer(t, coord, sceneMock)

	sceneID := idgen.New()
	// ListCharacterScenes returns a list that DOES include sceneID.
	sceneInfo := &scenev1.SceneInfo{Id: sceneID.String()}
	sceneMock.EXPECT().ListCharacterScenes(mock.Anything, mock.MatchedBy(func(req *scenev1.ListCharacterScenesRequest) bool {
		return req.GetCharacterId() == charID.String()
	})).Return(
		&scenev1.ListCharacterScenesResponse{
			Scenes: []*scenev1.CharacterSceneInfo{
				{Scene: sceneInfo},
			},
		}, nil,
	).Once()

	_, err := srv.SetSceneFocus(ctx, &sceneaccessv1.SetSceneFocusRequest{
		PlayerSessionToken: testSAToken,
		ConnectionId:       connID.String(),
		SceneId:            sceneID.String(),
	})

	require.NoError(t, err)
	assert.Equal(t, 1, coord.joinFocusCalls,
		"JoinFocus MUST be called exactly once for a valid participant")
}

// TestSetSceneFocusTreatsFocusAlreadyMemberAsSuccess verifies idempotency: when
// JoinFocus returns FOCUS_ALREADY_MEMBER the RPC succeeds (session already joined
// e.g. via terminal `scene join`).
func TestSetSceneFocusTreatsFocusAlreadyMemberAsSuccess(t *testing.T) {
	ctx := context.Background()
	coord := &stubFocusCoordinator{
		joinFocusErr: oops.Code("FOCUS_ALREADY_MEMBER").Errorf("already joined"),
	}
	sceneMock := scenemocks.NewMockSceneServiceClient(t)

	srv, charID, connID := buildSetSceneFocusServer(t, coord, sceneMock)

	sceneID := idgen.New()
	sceneInfo := &scenev1.SceneInfo{Id: sceneID.String()}
	sceneMock.EXPECT().ListCharacterScenes(mock.Anything, mock.MatchedBy(func(req *scenev1.ListCharacterScenesRequest) bool {
		return req.GetCharacterId() == charID.String()
	})).Return(
		&scenev1.ListCharacterScenesResponse{
			Scenes: []*scenev1.CharacterSceneInfo{
				{Scene: sceneInfo},
			},
		}, nil,
	).Once()

	_, err := srv.SetSceneFocus(ctx, &sceneaccessv1.SetSceneFocusRequest{
		PlayerSessionToken: testSAToken,
		ConnectionId:       connID.String(),
		SceneId:            sceneID.String(),
	})

	require.NoError(t, err, "FOCUS_ALREADY_MEMBER MUST be treated as success")
}

// TestSetSceneFocusClearToGridSkipsJoinFocus verifies that clearing focus
// (scene_id="") does NOT call JoinFocus or the participant check.
func TestSetSceneFocusClearToGridSkipsJoinFocus(t *testing.T) {
	ctx := context.Background()
	coord := &stubFocusCoordinator{}
	sceneMock := scenemocks.NewMockSceneServiceClient(t)

	srv, _, connID := buildSetSceneFocusServer(t, coord, sceneMock)

	_, err := srv.SetSceneFocus(ctx, &sceneaccessv1.SetSceneFocusRequest{
		PlayerSessionToken: testSAToken,
		ConnectionId:       connID.String(),
		SceneId:            "", // clear to grid
	})

	require.NoError(t, err)
	assert.Equal(t, 0, coord.joinFocusCalls,
		"JoinFocus MUST NOT be called when clearing focus to grid")
	sceneMock.AssertNotCalled(t, "ListCharacterScenes",
		"participant check MUST NOT run on clear-to-grid")
}

// --- name resolver tests ---

// stubSceneNameResolver is a minimal sceneNameResolver for unit tests.
type stubSceneNameResolver struct {
	names map[string]string
	err   error
}

func (s *stubSceneNameResolver) NamesByIDs(_ context.Context, _ []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.names, nil
}

// buildGetSceneServer returns a SceneAccessServer wired for GetSceneForViewer tests.
func buildGetSceneServer(
	t *testing.T,
	nr sceneNameResolver,
	sceneResp *scenev1.GetSceneResponse,
	sceneErr error,
) (*SceneAccessServer, ulid.ULID) {
	t.Helper()
	playerID := idgen.New()
	charID := idgen.New()

	ps := buildSATestPS(t, playerID)
	psRepo := buildSASessionRepo(t, ps)

	player := &auth.Player{ID: playerID, IsGuest: false}
	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(player, nil).Maybe()

	charRepo := authmocks.NewMockCharacterRepository(t)
	ownedChar := &world.Character{ID: charID, PlayerID: playerID}
	charRepo.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{ownedChar}, nil).Maybe()

	sessStore := sessionmocks.NewMockStore(t)
	mgr := &stubPluginManager{}

	sceneMock := scenemocks.NewMockSceneServiceClient(t)
	sceneMock.EXPECT().GetScene(mock.Anything, mock.Anything).Return(sceneResp, sceneErr).Maybe()

	srv := newTestSceneAccessServer(t, psRepo, playerRepo, charRepo, sessStore, &stubFocusCoordinator{}, sceneMock, mgr)
	if nr != nil {
		srv.WithSceneNameResolver(nr)
	}
	return srv, charID
}

// TestGetSceneForViewerRosterNamesPopulatedFromResolver verifies that when a
// name resolver is wired, the Participants and Observers in the response carry
// the resolved names instead of the raw character IDs.
func TestGetSceneForViewerRosterNamesPopulatedFromResolver(t *testing.T) {
	ctx := context.Background()

	part1ID := idgen.New().String()
	obs1ID := idgen.New().String()

	sceneResp := &scenev1.GetSceneResponse{
		Scene: &scenev1.SceneInfo{
			Id: idgen.New().String(),
			Participants: []*scenev1.ParticipantInfo{
				{CharacterId: part1ID, CharacterName: part1ID}, // plugin sets CharacterName=ID
			},
			Observers: []*scenev1.ParticipantInfo{
				{CharacterId: obs1ID, CharacterName: obs1ID},
			},
		},
	}
	nr := &stubSceneNameResolver{
		names: map[string]string{
			part1ID: "Alice",
			obs1ID:  "Bob",
		},
	}

	srv, charID := buildGetSceneServer(t, nr, sceneResp, nil)

	resp, err := srv.GetSceneForViewer(ctx, &sceneaccessv1.GetSceneForViewerRequest{
		PlayerSessionToken: testSAToken,
		CharacterId:        charID.String(),
		SceneId:            idgen.New().String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetScene().GetParticipants(), 1)
	require.Len(t, resp.GetScene().GetObservers(), 1)
	assert.Equal(t, "Alice", resp.GetScene().GetParticipants()[0].GetCharacterName(),
		"participant name MUST be resolved from the name resolver")
	assert.Equal(t, "Bob", resp.GetScene().GetObservers()[0].GetCharacterName(),
		"observer name MUST be resolved from the name resolver")
}

// TestGetSceneForViewerRosterFallsBackToIDOnResolverMiss verifies that a
// roster entry whose ID is absent from the resolver result retains the
// existing CharacterName value (the raw ID set by the plugin).
func TestGetSceneForViewerRosterFallsBackToIDOnResolverMiss(t *testing.T) {
	ctx := context.Background()

	knownID := idgen.New().String()
	unknownID := idgen.New().String()

	sceneResp := &scenev1.GetSceneResponse{
		Scene: &scenev1.SceneInfo{
			Id: idgen.New().String(),
			Participants: []*scenev1.ParticipantInfo{
				{CharacterId: knownID, CharacterName: knownID},
				{CharacterId: unknownID, CharacterName: unknownID}, // resolver will not return this
			},
		},
	}
	nr := &stubSceneNameResolver{
		names: map[string]string{
			knownID: "Alice", // unknownID intentionally absent
		},
	}

	srv, charID := buildGetSceneServer(t, nr, sceneResp, nil)

	resp, err := srv.GetSceneForViewer(ctx, &sceneaccessv1.GetSceneForViewerRequest{
		PlayerSessionToken: testSAToken,
		CharacterId:        charID.String(),
		SceneId:            idgen.New().String(),
	})
	require.NoError(t, err)
	parts := resp.GetScene().GetParticipants()
	require.Len(t, parts, 2)

	nameByID := make(map[string]string, len(parts))
	for _, p := range parts {
		nameByID[p.GetCharacterId()] = p.GetCharacterName()
	}
	assert.Equal(t, "Alice", nameByID[knownID], "resolved entry MUST carry the real name")
	assert.Equal(t, unknownID, nameByID[unknownID],
		"unresolved entry MUST retain its original CharacterName (the raw ID)")
}

// TestGetSceneForViewerRosterIsNonFatalOnResolverError verifies that a resolver
// error does not fail the RPC — the scene is still returned with the original
// CharacterName values (raw IDs) intact.
func TestGetSceneForViewerRosterIsNonFatalOnResolverError(t *testing.T) {
	ctx := context.Background()

	charIDInRoster := idgen.New().String()

	sceneResp := &scenev1.GetSceneResponse{
		Scene: &scenev1.SceneInfo{
			Id: idgen.New().String(),
			Participants: []*scenev1.ParticipantInfo{
				{CharacterId: charIDInRoster, CharacterName: charIDInRoster},
			},
		},
	}
	nr := &stubSceneNameResolver{
		err: errors.New("database connection failed"),
	}

	srv, charID := buildGetSceneServer(t, nr, sceneResp, nil)

	resp, err := srv.GetSceneForViewer(ctx, &sceneaccessv1.GetSceneForViewerRequest{
		PlayerSessionToken: testSAToken,
		CharacterId:        charID.String(),
		SceneId:            idgen.New().String(),
	})
	require.NoError(t, err, "resolver error MUST NOT fail the GetSceneForViewer RPC")
	require.Len(t, resp.GetScene().GetParticipants(), 1)
	assert.Equal(t, charIDInRoster, resp.GetScene().GetParticipants()[0].GetCharacterName(),
		"on resolver error the original CharacterName (the raw ID) MUST be preserved")
}
