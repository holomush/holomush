// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

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
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	holoFocus "github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/grpc/scenemocks"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
	"github.com/holomush/holomush/internal/world"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
)

// fakeNameResolver is a hand-rolled double for the unexported
// characterNameResolver interface (mockery does not generate mocks for
// unexported interfaces).
type fakeNameResolver struct {
	names map[ulid.ULID]string
	err   error
}

func (f *fakeNameResolver) Names(_ context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[ulid.ULID]string, len(ids))
	for _, id := range ids {
		if n, ok := f.names[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func TestWithCharacterNameResolverSetsTheField(t *testing.T) {
	srv := &SceneAccessServer{}
	r := &fakeNameResolver{}
	srv.WithCharacterNameResolver(r)
	assert.Same(t, r, srv.characterNameResolver)
}

func TestWithSceneDEKAdderSetsField(t *testing.T) {
	s := &SceneAccessServer{}
	s.WithSceneDEKAdder(stubSceneDEKAdder{})
	require.NotNil(t, s.dekAdder, "WithSceneDEKAdder must set the dekAdder field")
}

type stubSceneDEKAdder struct{}

func (stubSceneDEKAdder) EnsureParticipant(_ context.Context, _ dek.ContextID, _ dek.Participant) error {
	return nil
}

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
	setConnFocusErr  error
	joinFocusErr     error
	joinFocusCalls   int
	setConnFocusCall int
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
	s.setConnFocusCall++
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
			"CreateScene",
			func() error {
				_, err := srv.CreateScene(ctx, &sceneaccessv1.CreateSceneRequest{
					PlayerSessionToken: testSAToken, CharacterId: charID.String(), Title: "X",
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
	sceneMock.AssertNotCalled(t, "CreateScene")
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

// failingSceneDEKAdder is a sceneDEKAdder whose EnsureParticipant always fails,
// used to drive the fatal-seed branch of SetSceneFocus.
type failingSceneDEKAdder struct{}

func (failingSceneDEKAdder) EnsureParticipant(_ context.Context, _ dek.ContextID, _ dek.Participant) error {
	return oops.Code("DEK_ADD_FAILED").Errorf("seed failed")
}

// capturingSceneDEKAdder records the (ctxID, participant) passed to
// EnsureParticipant so tests can assert the seeded values.
type capturingSceneDEKAdder struct {
	called   bool
	gotCtxID dek.ContextID
	gotPart  dek.Participant
}

func (c *capturingSceneDEKAdder) EnsureParticipant(_ context.Context, ctxID dek.ContextID, p dek.Participant) error {
	c.called = true
	c.gotCtxID = ctxID
	c.gotPart = p
	return nil
}

// TestSetSceneFocusReturnsInternalWhenDEKSeedFails verifies the fatal-seed
// contract: when a dekAdder is attached and its EnsureParticipant fails, SetSceneFocus returns
// codes.Internal and MUST NOT proceed to SetConnectionFocus — a focused
// connection that cannot decrypt would receive blank (metadata-only) poses.
func TestSetSceneFocusReturnsInternalWhenDEKSeedFails(t *testing.T) {
	ctx := context.Background()
	coord := &stubFocusCoordinator{}
	sceneMock := scenemocks.NewMockSceneServiceClient(t)

	srv, charID, connID := buildSetSceneFocusServer(t, coord, sceneMock)
	srv.WithSceneDEKAdder(failingSceneDEKAdder{})

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

	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err),
		"a failed DEK seed MUST surface as Internal")
	require.Equal(t, 1, coord.joinFocusCalls,
		"the seed runs AFTER JoinFocus — JoinFocus must have been called once")
	require.Equal(t, 0, coord.setConnFocusCall,
		"a fatal DEK seed MUST refuse focus — SetConnectionFocus must not run")
}

// TestSetSceneFocusSeedsParticipantThenSetsConnectionFocus verifies the happy
// path with a dekAdder attached: the focusing character is seeded as a DEK
// participant on context {scene, sceneID} with a populated JoinedAt and
// AddedVia, and only then is SetConnectionFocus called.
func TestSetSceneFocusSeedsParticipantThenSetsConnectionFocus(t *testing.T) {
	ctx := context.Background()
	coord := &stubFocusCoordinator{}
	sceneMock := scenemocks.NewMockSceneServiceClient(t)

	srv, charID, connID := buildSetSceneFocusServer(t, coord, sceneMock)
	adder := &capturingSceneDEKAdder{}
	srv.WithSceneDEKAdder(adder)

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

	require.NoError(t, err)
	require.True(t, adder.called, "the seed MUST run on the focus path")
	require.Equal(t, dek.ContextID{Type: "scene", ID: sceneID.String()}, adder.gotCtxID,
		"the DEK context MUST be {scene, sceneID}")
	require.Equal(t, charID.String(), adder.gotPart.CharacterID,
		"the seeded participant MUST carry the focusing character's ID")
	require.False(t, adder.gotPart.JoinedAt.IsZero(),
		"the seeded participant MUST carry a JoinedAt timestamp")
	require.NotEmpty(t, adder.gotPart.AddedVia, "the seeded participant MUST carry an AddedVia provenance")
	require.WithinDuration(t, time.Now(), adder.gotPart.JoinedAt, time.Minute,
		"JoinedAt MUST be a recent UTC timestamp")
	require.Equal(t, 1, coord.setConnFocusCall,
		"a successful seed MUST proceed to SetConnectionFocus")
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

func TestSceneAccessCreateScene(t *testing.T) {
	ctx := context.Background()
	playerID := idgen.New()
	char := &world.Character{ID: idgen.New(), PlayerID: playerID, Name: "Alice"}
	ps := buildSATestPS(t, playerID)
	wantID := idgen.New().String()

	// nonGuest / guest resolve the session player as a registered account or a
	// guest, respectively; ownsAlice / noChars configure the owned-character set.
	nonGuest := func(t *testing.T) *authmocks.MockPlayerRepository {
		pr := authmocks.NewMockPlayerRepository(t)
		pr.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{ID: playerID, IsGuest: false}, nil).Maybe()
		return pr
	}
	guest := func(t *testing.T) *authmocks.MockPlayerRepository {
		pr := authmocks.NewMockPlayerRepository(t)
		pr.EXPECT().GetByID(mock.Anything, playerID).Return(&auth.Player{ID: playerID, IsGuest: true}, nil).Maybe()
		return pr
	}
	ownsAlice := func(t *testing.T) *authmocks.MockCharacterRepository {
		cr := authmocks.NewMockCharacterRepository(t)
		cr.EXPECT().ListByPlayer(mock.Anything, playerID).Return([]*world.Character{char}, nil).Maybe()
		return cr
	}
	noChars := func(t *testing.T) *authmocks.MockCharacterRepository {
		return authmocks.NewMockCharacterRepository(t)
	}
	noSceneCalls := func(t *testing.T) *scenemocks.MockSceneServiceClient {
		return scenemocks.NewMockSceneServiceClient(t)
	}

	tests := []struct {
		name       string
		playerRepo func(*testing.T) *authmocks.MockPlayerRepository
		charRepo   func(*testing.T) *authmocks.MockCharacterRepository
		sceneMock  func(*testing.T) *scenemocks.MockSceneServiceClient
		req        *sceneaccessv1.CreateSceneRequest
		check      func(t *testing.T, resp *sceneaccessv1.CreateSceneResponse, err error, sceneMock *scenemocks.MockSceneServiceClient)
	}{
		{
			// Verifies: INV-SCENE-63 — the facade forwards the SERVER-verified
			// character id (never the client's) to the plugin SceneService.
			name:       "owned character creates scene with verified id, title, description",
			playerRepo: nonGuest,
			charRepo:   ownsAlice,
			sceneMock: func(t *testing.T) *scenemocks.MockSceneServiceClient {
				sm := scenemocks.NewMockSceneServiceClient(t)
				sm.EXPECT().CreateScene(mock.Anything, mock.MatchedBy(func(req *scenev1.CreateSceneRequest) bool {
					return req.GetCharacterId() == char.ID.String() &&
						req.GetTitle() == "The Manor" && req.GetDescription() == "dusk"
				})).Return(&scenev1.CreateSceneResponse{Scene: &scenev1.SceneInfo{Id: wantID, Title: "The Manor"}}, nil).Once()
				return sm
			},
			req: &sceneaccessv1.CreateSceneRequest{
				PlayerSessionToken: testSAToken, CharacterId: char.ID.String(),
				Title: "The Manor", Description: "dusk",
			},
			check: func(t *testing.T, resp *sceneaccessv1.CreateSceneResponse, err error, _ *scenemocks.MockSceneServiceClient) {
				require.NoError(t, err)
				assert.Equal(t, wantID, resp.GetScene().GetId())
			},
		},
		{
			// Verifies: INV-SCENE-64 — guests are denied at the facade and the
			// downstream plugin is never reached.
			name:       "guest is denied and downstream is never called",
			playerRepo: guest,
			charRepo:   noChars,
			sceneMock:  noSceneCalls,
			req: &sceneaccessv1.CreateSceneRequest{
				PlayerSessionToken: testSAToken, CharacterId: char.ID.String(), Title: "X",
			},
			check: func(t *testing.T, _ *sceneaccessv1.CreateSceneResponse, err error, sceneMock *scenemocks.MockSceneServiceClient) {
				st, _ := status.FromError(err)
				assert.Equal(t, codes.PermissionDenied, st.Code())
				sceneMock.AssertNotCalled(t, "CreateScene")
			},
		},
		{
			// Verifies: INV-SCENE-63 — a character the player does not own is
			// rejected (NotFound) before any downstream call.
			name:       "character not owned returns NotFound",
			playerRepo: nonGuest,
			charRepo:   ownsAlice,
			sceneMock:  noSceneCalls,
			req: &sceneaccessv1.CreateSceneRequest{
				PlayerSessionToken: testSAToken, CharacterId: idgen.New().String(), Title: "X",
			},
			check: func(t *testing.T, _ *sceneaccessv1.CreateSceneResponse, err error, _ *scenemocks.MockSceneServiceClient) {
				st, _ := status.FromError(err)
				assert.Equal(t, codes.NotFound, st.Code())
			},
		},
		{
			// Spec §6: opaque error on downstream failure — the facade passes the
			// plugin's status error through unchanged (no double-wrap / opacity break).
			name:       "downstream failure passes the plugin status error through unchanged",
			playerRepo: nonGuest,
			charRepo:   ownsAlice,
			sceneMock: func(t *testing.T) *scenemocks.MockSceneServiceClient {
				sm := scenemocks.NewMockSceneServiceClient(t)
				sm.EXPECT().CreateScene(mock.Anything, mock.Anything).
					Return(nil, status.Error(codes.FailedPrecondition, "scene quota exceeded")).Once()
				return sm
			},
			req: &sceneaccessv1.CreateSceneRequest{
				PlayerSessionToken: testSAToken, CharacterId: char.ID.String(), Title: "X",
			},
			check: func(t *testing.T, _ *sceneaccessv1.CreateSceneResponse, err error, _ *scenemocks.MockSceneServiceClient) {
				require.Error(t, err)
				st, _ := status.FromError(err)
				assert.Equal(t, codes.FailedPrecondition, st.Code())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sceneMock := tt.sceneMock(t)
			srv := newTestSceneAccessServer(t, buildSASessionRepo(t, ps), tt.playerRepo(t), tt.charRepo(t),
				sessionmocks.NewMockStore(t), &stubFocusCoordinator{}, sceneMock, &stubPluginManager{})
			resp, err := srv.CreateScene(ctx, tt.req)
			tt.check(t, resp, err, sceneMock)
		})
	}
}
