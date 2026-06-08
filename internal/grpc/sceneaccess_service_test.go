// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
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
}

func (s *stubFocusCoordinator) JoinFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
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
