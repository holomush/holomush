// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	holoFocus "github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
)

// sceneAccessPluginManager is the narrow interface SceneAccessServer needs from
// the plugin manager — only BeginServiceDispatch.
type sceneAccessPluginManager interface {
	BeginServiceDispatch(ctx context.Context, pluginName string, actor core.Actor, ownerPlayerID string) (context.Context, func(), error)
}

// sceneDEKAdder seeds a character as a DEK participant so the AuthGuard's
// hot-tier checkCharacter branch permits this session to decrypt sensitive
// scene events (e.g. scene_pose). The genesis-safe form: it mints the scene
// DEK (seeded with the character) when none exists yet — the first reader to
// focus a never-posed scene (INV-CRYPTO-121). Satisfied by dek.Manager.
type sceneDEKAdder interface {
	EnsureParticipant(ctx context.Context, ctxID dek.ContextID, p dek.Participant) error
}

// SceneAccessServer is the host-side facade that owns player authentication,
// server-side identity resolution (INV-SCENE-63), and guest-player rejection
// (INV-SCENE-64) for all scene-surface RPCs. It wraps the plugin SceneService,
// ensuring that every downstream call carries a server-verified, player-owned
// character identity.
type SceneAccessServer struct {
	sceneaccessv1.UnimplementedSceneAccessServiceServer

	playerSessionRepo auth.PlayerSessionRepository
	playerRepo        auth.PlayerRepository
	charRepo          auth.CharacterRepository
	sessionStore      session.Store
	coordinator       holoFocus.Coordinator
	sceneClient       scenev1.SceneServiceClient
	pluginManager     sceneAccessPluginManager

	// dekAdder is optional. When non-nil, SetSceneFocus seeds the focusing
	// character as a DEK participant after the participation gate passes, so
	// the AuthGuard permits decryption of sensitive scene events. nil disables
	// seeding (KEK-less deployments / tests).
	dekAdder sceneDEKAdder

	// characterNameResolver resolves participant/observer display names by ID
	// for GetSceneForViewer roster enrichment. Optional: nil leaves rosters
	// with raw ULIDs (best-effort). Mirrors CoreServer's resolver (5b2j).
	characterNameResolver characterNameResolver
}

// NewSceneAccessServer constructs a SceneAccessServer. All fields are required;
// a nil sceneClient disables the service (returns Unimplemented for all RPCs).
func NewSceneAccessServer(
	playerSessionRepo auth.PlayerSessionRepository,
	playerRepo auth.PlayerRepository,
	charRepo auth.CharacterRepository,
	sessionStore session.Store,
	coordinator holoFocus.Coordinator,
	sceneClient scenev1.SceneServiceClient,
	pluginManager sceneAccessPluginManager,
) *SceneAccessServer {
	return &SceneAccessServer{
		playerSessionRepo: playerSessionRepo,
		playerRepo:        playerRepo,
		charRepo:          charRepo,
		sessionStore:      sessionStore,
		coordinator:       coordinator,
		sceneClient:       sceneClient,
		pluginManager:     pluginManager,
	}
}

// WithSceneDEKAdder attaches a DEK participant adder. Call after construction;
// when set, SetSceneFocus seeds the focusing character as a DEK participant
// (fatal on failure).
func (s *SceneAccessServer) WithSceneDEKAdder(a sceneDEKAdder) {
	s.dekAdder = a
}

// WithCharacterNameResolver attaches the roster name resolver. Call after
// construction; when set, GetSceneForViewer overwrites ParticipantInfo
// CharacterName with the resolved display name (ULID fallback on a miss).
func (s *SceneAccessServer) WithCharacterNameResolver(r characterNameResolver) {
	s.characterNameResolver = r
}

// ownedCharacter verifies that charIDStr is a valid ULID and is owned by
// playerID. Returns (verified *world.Character, nil) on success or
// (nil, codes.NotFound) when the character is absent from the player's list.
func (s *SceneAccessServer) ownedCharacter(ctx context.Context, playerID ulid.ULID, charIDStr string) (*world.Character, error) {
	charID, err := ulid.Parse(charIDStr)
	if err != nil {
		return nil, status.Error(codes.NotFound, "character not found") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	chars, err := s.charRepo.ListByPlayer(ctx, playerID)
	if err != nil {
		slog.ErrorContext(ctx, "scene access: list characters failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	for _, c := range chars {
		if c.ID == charID {
			return c, nil
		}
	}
	return nil, status.Error(codes.NotFound, "character not found") //nolint:wrapcheck // gRPC status error at handler boundary
}

// resolveAndGate resolves the player session from rawToken, loads the player,
// and enforces the guest gate (INV-SCENE-64). Returns the validated PlayerSession
// or a gRPC status error.
func (s *SceneAccessServer) resolveAndGate(ctx context.Context, rawToken string) (*auth.PlayerSession, error) {
	ps, err := resolvePlayerSessionWithRepo(ctx, s.playerSessionRepo, rawToken)
	if err != nil {
		if oe, ok := oops.AsOops(err); ok && oe.Code() == "NOT_CONFIGURED" {
			return nil, status.Error(codes.Unimplemented, "player session service not configured") //nolint:wrapcheck // gRPC status error at handler boundary
		}
		return nil, status.Error(codes.Unauthenticated, "unauthenticated") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	player, err := s.playerRepo.GetByID(ctx, ps.PlayerID)
	if err != nil {
		slog.ErrorContext(ctx, "scene access: player lookup failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	if player.IsGuest {
		return nil, status.Error(codes.PermissionDenied, "guests cannot access scenes") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	return ps, nil
}

// beginDispatch wraps BeginServiceDispatch for the verified character actor.
// Returns the enriched ctx + release func, or a gRPC status error.
func (s *SceneAccessServer) beginDispatch(ctx context.Context, verifiedChar *world.Character, playerID ulid.ULID) (context.Context, func(), error) {
	actor := core.Actor{Kind: core.ActorCharacter, ID: verifiedChar.ID.String()}
	dctx, release, err := s.pluginManager.BeginServiceDispatch(ctx, "core-scenes", actor, playerID.String())
	if err != nil {
		slog.ErrorContext(ctx, "scene access: BeginServiceDispatch failed", "error", err)
		return nil, nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	return dctx, release, nil
}

// ListScenesForViewer returns the public scene board for the verified character.
func (s *SceneAccessServer) ListScenesForViewer(ctx context.Context, req *sceneaccessv1.ListScenesForViewerRequest) (*sceneaccessv1.ListScenesForViewerResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.ListScenes(dctx, &scenev1.ListScenesRequest{
		CharacterId:            char.ID.String(),
		Limit:                  req.GetLimit(),
		Offset:                 req.GetOffset(),
		Tags:                   req.GetTags(),
		ExcludeContentWarnings: req.GetExcludeContentWarnings(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.ListScenesForViewerResponse{Scenes: resp.GetScenes()}, nil
}

// GetSceneForViewer loads one scene for the verified character.
func (s *SceneAccessServer) GetSceneForViewer(ctx context.Context, req *sceneaccessv1.GetSceneForViewerRequest) (*sceneaccessv1.GetSceneForViewerResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.GetScene(dctx, &scenev1.GetSceneRequest{
		CharacterId: char.ID.String(),
		SceneId:     req.GetSceneId(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	scene := resp.GetScene()
	s.resolveRosterNames(ctx, scene)
	return &sceneaccessv1.GetSceneForViewerResponse{Scene: scene}, nil
}

// resolveRosterNames overwrites participant + observer CharacterName fields with
// resolved display names, in place. Best-effort: nil resolver, parse failure, a
// resolver error, or a missing id all leave the raw ULID. The scene gate already
// authorized this roster (plugin GetScene privacy gate), so name resolution is
// downstream display — no per-character ABAC (mirrors ListFocusPresence).
func (s *SceneAccessServer) resolveRosterNames(ctx context.Context, scene *scenev1.SceneInfo) {
	if s.characterNameResolver == nil || scene == nil {
		return
	}
	roster := append(append([]*scenev1.ParticipantInfo{}, scene.GetParticipants()...), scene.GetObservers()...)
	if len(roster) == 0 {
		return
	}
	ids := make([]ulid.ULID, 0, len(roster))
	for _, p := range roster {
		id, err := ulid.Parse(p.GetCharacterId())
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	names, err := s.characterNameResolver.Names(ctx, ids)
	if err != nil {
		// Log via slog.ErrorContext like ListFocusPresence (list_focus_presence.go:161-162).
		// DELIBERATE divergence: presence HARD-FAILS (INTERNAL) on resolver error;
		// the scene roster DEGRADES — keep the ULIDs, never fail GetSceneForViewer.
		slog.ErrorContext(ctx, "scene roster name resolution failed", "error", err, "scene_id", scene.GetId())
		return
	}
	for _, p := range roster {
		id, err := ulid.Parse(p.GetCharacterId())
		if err != nil {
			continue
		}
		if n, ok := names[id]; ok && n != "" {
			p.CharacterName = n
		}
	}
}

// ListMyScenes returns the verified character's scene participations.
func (s *SceneAccessServer) ListMyScenes(ctx context.Context, req *sceneaccessv1.ListMyScenesRequest) (*sceneaccessv1.ListMyScenesResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.ListCharacterScenes(dctx, &scenev1.ListCharacterScenesRequest{
		CharacterId: char.ID.String(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.ListMyScenesResponse{Scenes: resp.GetScenes()}, nil
}

// WatchScene auto-joins the verified character as observer, requiring an
// existing game session (FailedPrecondition if none — select character first).
func (s *SceneAccessServer) WatchScene(ctx context.Context, req *sceneaccessv1.WatchSceneRequest) (*sceneaccessv1.WatchSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}

	// Require an existing game session for the character (WatchScene piggybacks on
	// SelectCharacter; the session_id is forwarded to the plugin so JoinFocus can
	// register the FocusMembership).
	gameSession, err := s.sessionStore.FindByCharacter(ctx, char.ID)
	if err != nil {
		if oe, ok := oops.AsOops(err); ok && oe.Code() == "SESSION_NOT_FOUND" {
			return nil, status.Error(codes.FailedPrecondition, "no game session — select character first") //nolint:wrapcheck // gRPC status error at handler boundary
		}
		slog.ErrorContext(ctx, "scene access: FindByCharacter failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.WatchScene(dctx, &scenev1.WatchSceneRequest{
		CharacterId: char.ID.String(),
		SceneId:     req.GetSceneId(),
		SessionId:   gameSession.ID,
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.WatchSceneResponse{Participant: resp.GetParticipant()}, nil
}

// CreateScene creates a new scene owned by the verified player's owned character
// and returns its metadata. Unlike WatchScene it requires no existing game
// session — creation does not touch focus or sessions. resolveAndGate enforces
// the guest gate (INV-SCENE-64); ownedCharacter enforces ownership (INV-SCENE-63).
func (s *SceneAccessServer) CreateScene(ctx context.Context, req *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.CreateScene(dctx, &scenev1.CreateSceneRequest{
		CharacterId: char.ID.String(),
		Title:       req.GetTitle(),
		Description: req.GetDescription(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.CreateSceneResponse{Scene: resp.GetScene()}, nil
}

// EndScene resolves the verified owner from the player session and forwards an
// EndScene call to the plugin SceneService (which self-enforces the ABAC `end`
// policy, INV-SCENE-65). resolveAndGate enforces the guest gate (INV-SCENE-64);
// ownedCharacter enforces ownership (INV-SCENE-63).
func (s *SceneAccessServer) EndScene(ctx context.Context, req *sceneaccessv1.EndSceneRequest) (*sceneaccessv1.EndSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.EndScene(dctx, &scenev1.EndSceneRequest{
		CharacterId: char.ID.String(),
		SceneId:     req.GetSceneId(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.EndSceneResponse{Scene: resp.GetScene()}, nil
}

// PauseScene resolves the verified owner from the player session and forwards a
// PauseScene call to the plugin SceneService (which self-enforces the ABAC `pause`
// policy, INV-SCENE-65). resolveAndGate enforces the guest gate (INV-SCENE-64);
// ownedCharacter enforces ownership (INV-SCENE-63).
func (s *SceneAccessServer) PauseScene(ctx context.Context, req *sceneaccessv1.PauseSceneRequest) (*sceneaccessv1.PauseSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.PauseScene(dctx, &scenev1.PauseSceneRequest{
		CharacterId: char.ID.String(),
		SceneId:     req.GetSceneId(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.PauseSceneResponse{Scene: resp.GetScene()}, nil
}

// ResumeScene resolves the verified participant from the player session and
// forwards a ResumeScene call to the plugin SceneService (which self-enforces
// the ABAC `resume` policy — participant-wide, INV-SCENE-65). resolveAndGate
// enforces the guest gate (INV-SCENE-64); ownedCharacter enforces ownership
// (INV-SCENE-63).
func (s *SceneAccessServer) ResumeScene(ctx context.Context, req *sceneaccessv1.ResumeSceneRequest) (*sceneaccessv1.ResumeSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.ResumeScene(dctx, &scenev1.ResumeSceneRequest{
		CharacterId: char.ID.String(),
		SceneId:     req.GetSceneId(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.ResumeSceneResponse{Scene: resp.GetScene()}, nil
}

// InviteToScene resolves the verified inviter from the player session and
// forwards to the plugin SceneService (which self-enforces the participant-wide
// `invite` policy, INV-SCENE-65). resolveAndGate enforces the guest gate
// (INV-SCENE-64); ownedCharacter enforces ownership of the acting alt (INV-SCENE-63).
func (s *SceneAccessServer) InviteToScene(ctx context.Context, req *sceneaccessv1.InviteToSceneRequest) (*sceneaccessv1.InviteToSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	if _, err := s.sceneClient.InviteToScene(dctx, &scenev1.InviteToSceneRequest{
		CharacterId:       char.ID.String(),
		SceneId:           req.GetSceneId(),
		TargetCharacterId: req.GetTargetCharacterId(),
	}); err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.InviteToSceneResponse{}, nil
}

// KickFromScene resolves the verified owner from the player session and
// forwards to the plugin SceneService (which self-enforces the owner-only
// `kick` policy, INV-SCENE-65). resolveAndGate enforces the guest gate
// (INV-SCENE-64); ownedCharacter enforces ownership of the acting alt (INV-SCENE-63).
func (s *SceneAccessServer) KickFromScene(ctx context.Context, req *sceneaccessv1.KickFromSceneRequest) (*sceneaccessv1.KickFromSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	if _, err := s.sceneClient.KickFromScene(dctx, &scenev1.KickFromSceneRequest{
		CharacterId:       char.ID.String(),
		SceneId:           req.GetSceneId(),
		TargetCharacterId: req.GetTargetCharacterId(),
	}); err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.KickFromSceneResponse{}, nil
}

// TransferOwnership resolves the verified current owner from the player session
// and forwards to the plugin SceneService (which self-enforces the owner-only
// `transfer-ownership` policy, INV-SCENE-65). resolveAndGate enforces the guest
// gate (INV-SCENE-64); ownedCharacter enforces ownership of the acting alt
// (INV-SCENE-63).
func (s *SceneAccessServer) TransferOwnership(ctx context.Context, req *sceneaccessv1.TransferOwnershipRequest) (*sceneaccessv1.TransferOwnershipResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	if _, err := s.sceneClient.TransferOwnership(dctx, &scenev1.TransferOwnershipRequest{
		CharacterId:         char.ID.String(),
		SceneId:             req.GetSceneId(),
		NewOwnerCharacterId: req.GetNewOwnerCharacterId(),
	}); err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.TransferOwnershipResponse{}, nil
}

// LeaveScene resolves the verified participant from the player session and
// forwards to the plugin SceneService (which self-enforces the participant
// `leave` policy; the owner cannot leave — INV-SCENE-65). resolveAndGate
// enforces the guest gate (INV-SCENE-64); ownedCharacter enforces ownership of
// the acting alt (INV-SCENE-63).
func (s *SceneAccessServer) LeaveScene(ctx context.Context, req *sceneaccessv1.LeaveSceneRequest) (*sceneaccessv1.LeaveSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	if _, err := s.sceneClient.LeaveScene(dctx, &scenev1.LeaveSceneRequest{
		CharacterId: char.ID.String(),
		SceneId:     req.GetSceneId(),
	}); err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.LeaveSceneResponse{}, nil
}

// ExportScene renders the verified character's scene IC log.
func (s *SceneAccessServer) ExportScene(ctx context.Context, req *sceneaccessv1.ExportSceneRequest) (*sceneaccessv1.ExportSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.ExportSceneLog(dctx, &scenev1.ExportSceneLogRequest{
		CharacterId: char.ID.String(),
		SceneId:     req.GetSceneId(),
		Format:      req.GetFormat(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.ExportSceneResponse{
		Content:  resp.GetContent(),
		MimeType: resp.GetMimeType(),
		Filename: resp.GetFilename(),
	}, nil
}

// SetSceneFocus sets per-connection focus for a web-portal connection. The
// facade verifies that the connection belongs to a game session owned by one
// of the player's characters (INV-SCENE-63), then:
//   - when setting a non-nil scene focus: verifies the character is a participant
//     of that scene (privacy gate — focusing a scene the char has no row in would
//     subscribe the session to its streams), then calls JoinFocus idempotently, then
//     calls SetConnectionFocus (which now succeeds because the membership exists);
//   - when clearing focus (scene_id=""): calls SetConnectionFocus directly (no
//     JoinFocus needed; membership is irrelevant for the grid).
func (s *SceneAccessServer) SetSceneFocus(ctx context.Context, req *sceneaccessv1.SetSceneFocusRequest) (*sceneaccessv1.SetSceneFocusResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}

	connID, err := ulid.Parse(req.GetConnectionId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid connection_id") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	// Look up the connection to find its session, then verify character ownership.
	conn, err := s.sessionStore.GetConnection(ctx, connID)
	if err != nil {
		if oe, ok := oops.AsOops(err); ok && oe.Code() == "CONNECTION_NOT_FOUND" {
			return nil, status.Error(codes.NotFound, "connection not found") //nolint:wrapcheck // gRPC status error at handler boundary
		}
		slog.ErrorContext(ctx, "scene access: GetConnection failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	gameSession, err := s.sessionStore.Get(ctx, conn.SessionID)
	if err != nil {
		slog.ErrorContext(ctx, "scene access: session lookup for connection failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	// Verify the session's character is owned by this player (INV-SCENE-63).
	// ownedCharacter returns Internal on an infra failure (ListByPlayer) and
	// NotFound when the character is genuinely not owned. Propagate the infra
	// failure as-is for observability; only the not-owned case collapses to the
	// connection-ownership denial.
	char, err := s.ownedCharacter(ctx, ps.PlayerID, gameSession.CharacterID.String())
	if err != nil {
		if status.Code(err) == codes.Internal {
			return nil, err
		}
		return nil, status.Error(codes.PermissionDenied, "connection does not belong to your character") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	// Build the focus key (nil = clear to grid; non-nil = scene focus).
	var focusKey *session.FocusKey
	if sceneIDStr := req.GetSceneId(); sceneIDStr != "" {
		sceneID, parseErr := ulid.Parse(sceneIDStr)
		if parseErr != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid scene_id") //nolint:wrapcheck // gRPC status error at handler boundary
		}
		fk := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
		focusKey = &fk

		// Privacy gate: verify the character has a participant row in the target
		// scene before establishing focus membership. JoinFocus subscribes the
		// session to the scene's streams — focusing a scene the char has no row
		// in would leak its events to an unauthorized session.
		dctx, release, dispatchErr := s.beginDispatch(ctx, char, ps.PlayerID)
		if dispatchErr != nil {
			return nil, dispatchErr
		}
		defer release()

		myScenes, listErr := s.sceneClient.ListCharacterScenes(dctx, &scenev1.ListCharacterScenesRequest{
			CharacterId: char.ID.String(),
		})
		if listErr != nil {
			slog.ErrorContext(ctx, "scene access: SetSceneFocus participant check failed", "error", listErr)
			return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
		}
		isParticipant := false
		for _, info := range myScenes.GetScenes() {
			if info.GetScene().GetId() == sceneIDStr {
				isParticipant = true
				break
			}
		}
		if !isParticipant {
			return nil, status.Error(codes.PermissionDenied, "not a participant of this scene") //nolint:wrapcheck // gRPC status error at handler boundary
		}

		// Establish focus membership idempotently so SetConnectionFocus (which
		// gates on FocusMemberships — INV-SCENE-14) succeeds for fresh comms_hub
		// sessions that have not yet called JoinFocus.
		// FOCUS_ALREADY_MEMBER is success: session is already a member (e.g.
		// joined via `scene join` on the terminal). Mirror the precedent at
		// plugins/core-scenes/service.go WatchScene and commands.go handleJoin.
		joinErr := s.coordinator.JoinFocus(ctx, gameSession.ID, fk)
		if joinErr != nil {
			var oe oops.OopsError
			if !errors.As(joinErr, &oe) || oe.Code() != "FOCUS_ALREADY_MEMBER" {
				slog.ErrorContext(ctx, "scene access: SetSceneFocus JoinFocus failed", "error", joinErr)
				return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
			}
			// FOCUS_ALREADY_MEMBER — membership already present; proceed to SetConnectionFocus.
		}

		// Seed the character as a DEK participant so the AuthGuard hot-tier
		// permits this session to decrypt sensitive scene events (scene_pose,
		// scene_say, scene_emit, scene_ooc). Genesis-safe: mints the scene DEK
		// seeded with this reader if none exists yet (first focus precedes
		// first pose). FATAL: if the seed fails the connection MUST NOT be
		// focused — a focused connection that cannot decrypt would receive
		// blank (metadata-only) poses. Refusing focus surfaces the error so the
		// client retries; EnsureParticipant is idempotent so retry is a safe
		// no-op. (INV-CRYPTO-121. Invariant: a connection is focused on a scene
		// only if its character can decrypt that scene.)
		if s.dekAdder != nil {
			ctxID := dek.ContextID{Type: "scene", ID: sceneIDStr}
			addErr := s.dekAdder.EnsureParticipant(ctx, ctxID, dek.Participant{
				PlayerID:    ps.PlayerID.String(),
				CharacterID: gameSession.CharacterID.String(),
				JoinedAt:    time.Now().UTC(),
				AddedVia:    "sceneaccess.SetSceneFocus",
			})
			if addErr != nil {
				slog.ErrorContext(ctx, "scene access: SetSceneFocus DEK seed failed",
					"scene_id", sceneIDStr,
					"character_id", gameSession.CharacterID.String(),
					"error", addErr)
				return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
			}
		}
	}

	_, err = s.coordinator.SetConnectionFocus(ctx, connID, focusKey, false)
	if err != nil {
		slog.ErrorContext(ctx, "scene access: SetConnectionFocus failed", "error", err)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}
	return &sceneaccessv1.SetSceneFocusResponse{}, nil
}

// ListPublishedScenes pages through public PUBLISHED scene archives. Guest gate
// is enforced (INV-SCENE-64); no character identity is required.
func (s *SceneAccessServer) ListPublishedScenes(ctx context.Context, req *sceneaccessv1.ListPublishedScenesRequest) (*sceneaccessv1.ListPublishedScenesResponse, error) {
	if _, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken()); err != nil {
		return nil, err
	}

	resp, err := s.sceneClient.ListPublishedScenes(ctx, &scenev1.ListPublishedScenesRequest{
		Limit:  req.GetLimit(),
		Offset: req.GetOffset(),
		Tags:   req.GetTags(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.ListPublishedScenesResponse{Archives: resp.GetArchives()}, nil
}

// GetPublicSceneArchive reads a PUBLISHED scene archive (INV-SCENE-35). Guest
// gate enforced (INV-SCENE-64); no character identity required.
func (s *SceneAccessServer) GetPublicSceneArchive(ctx context.Context, req *sceneaccessv1.GetPublicSceneArchiveRequest) (*sceneaccessv1.GetPublicSceneArchiveResponse, error) {
	if _, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken()); err != nil {
		return nil, err
	}

	resp, err := s.sceneClient.GetPublicSceneArchive(ctx, &scenev1.GetPublicSceneArchiveRequest{
		PublishedSceneId: req.GetPublishedSceneId(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.GetPublicSceneArchiveResponse{
		Id:                   resp.GetId(),
		TitleSnapshot:        resp.GetTitleSnapshot(),
		ParticipantsSnapshot: resp.GetParticipantsSnapshot(),
		ContentEntries:       resp.GetContentEntries(),
		PublishedAtUnixNs:    resp.GetPublishedAtUnixNs(),
	}, nil
}

// UpdateScene resolves the verified owner from the player session and forwards an
// UpdateScene call to the plugin SceneService (which self-enforces the ABAC
// `update` policy, owner-only, INV-SCENE-65, and applies only the masked fields).
// resolveAndGate enforces the guest gate (INV-SCENE-64); ownedCharacter enforces
// ownership (INV-SCENE-63).
func (s *SceneAccessServer) UpdateScene(ctx context.Context, req *sceneaccessv1.UpdateSceneRequest) (*sceneaccessv1.UpdateSceneResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)
	if err != nil {
		return nil, err
	}
	defer release()

	resp, err := s.sceneClient.UpdateScene(dctx, &scenev1.UpdateSceneRequest{
		CharacterId:     char.ID.String(),
		SceneId:         req.GetSceneId(),
		Title:           req.GetTitle(),
		Description:     req.GetDescription(),
		Visibility:      req.GetVisibility(),
		PoseOrderMode:   req.GetPoseOrderMode(),
		Tags:            req.GetTags(),
		ContentWarnings: req.GetContentWarnings(),
		UpdateMask:      req.GetUpdateMask(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.UpdateSceneResponse{Scene: resp.GetScene()}, nil
}

// DownloadPublicSceneArchive returns a PUBLISHED scene archive in the requested
// format (INV-SCENE-35). Guest gate enforced (INV-SCENE-64).
func (s *SceneAccessServer) DownloadPublicSceneArchive(ctx context.Context, req *sceneaccessv1.DownloadPublicSceneArchiveRequest) (*sceneaccessv1.DownloadPublicSceneArchiveResponse, error) {
	if _, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken()); err != nil {
		return nil, err
	}

	resp, err := s.sceneClient.DownloadPublicSceneArchive(ctx, &scenev1.DownloadPublicSceneArchiveRequest{
		PublishedSceneId: req.GetPublishedSceneId(),
		Format:           req.GetFormat(),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return &sceneaccessv1.DownloadPublicSceneArchiveResponse{
		Content:  resp.GetContent(),
		MimeType: resp.GetMimeType(),
	}, nil
}
