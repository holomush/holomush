// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// WebListScenes proxies to SceneAccessService.ListScenesForViewer. The gateway
// reads the player_session_token from the X-Session-Token cookie header
// (injected by CookieMiddleware) and forwards it to the facade together with
// all op-fields from the request body. Authorization and identity resolution
// are owned entirely by the facade.
func (h *Handler) WebListScenes(ctx context.Context, req *connect.Request[webv1.WebListScenesRequest]) (*connect.Response[webv1.WebListScenesResponse], error) {
	slog.DebugContext(ctx, "web: WebListScenes", "session_id", req.Msg.GetSessionId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.ListScenesForViewer(rpcCtx, &sceneaccessv1.ListScenesForViewerRequest{
		SessionId:              req.Msg.GetSessionId(),
		PlayerSessionToken:     token,
		CharacterId:            req.Msg.GetCharacterId(),
		Limit:                  req.Msg.GetLimit(),
		Offset:                 req.Msg.GetOffset(),
		Tags:                   req.Msg.GetTags(),
		ExcludeContentWarnings: req.Msg.GetExcludeContentWarnings(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list scenes RPC failed", err, "session_id", req.Msg.GetSessionId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebListScenesResponse{
		Scenes: resp.GetScenes(),
	}), nil
}

// WebGetScene proxies to SceneAccessService.GetSceneForViewer. The gateway
// reads the player_session_token from the X-Session-Token cookie header and
// forwards it to the facade together with the scene_id and character_id.
// Authorization and identity resolution are owned entirely by the facade.
func (h *Handler) WebGetScene(ctx context.Context, req *connect.Request[webv1.WebGetSceneRequest]) (*connect.Response[webv1.WebGetSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebGetScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.GetSceneForViewer(rpcCtx, &sceneaccessv1.GetSceneForViewerRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: get scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebGetSceneResponse{
		Scene: resp.GetScene(),
	}), nil
}

// WebListMyScenes proxies to SceneAccessService.ListMyScenes. The gateway
// reads the player_session_token from the X-Session-Token cookie header and
// forwards it with character_id. Authorization and identity resolution are
// owned entirely by the facade.
func (h *Handler) WebListMyScenes(ctx context.Context, req *connect.Request[webv1.WebListMyScenesRequest]) (*connect.Response[webv1.WebListMyScenesResponse], error) {
	slog.DebugContext(ctx, "web: WebListMyScenes", "session_id", req.Msg.GetSessionId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.ListMyScenes(rpcCtx, &sceneaccessv1.ListMyScenesRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list my scenes RPC failed", err, "session_id", req.Msg.GetSessionId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebListMyScenesResponse{
		Scenes: resp.GetScenes(),
	}), nil
}

// WebWatchScene proxies to SceneAccessService.WatchScene. The gateway reads
// the player_session_token from the X-Session-Token cookie header and forwards
// it with character_id and scene_id. Authorization and identity resolution are
// owned entirely by the facade.
func (h *Handler) WebWatchScene(ctx context.Context, req *connect.Request[webv1.WebWatchSceneRequest]) (*connect.Response[webv1.WebWatchSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebWatchScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.WatchScene(rpcCtx, &sceneaccessv1.WatchSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: watch scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebWatchSceneResponse{
		Participant: resp.GetParticipant(),
	}), nil
}

// WebCreateScene proxies to SceneAccessService.CreateScene. The gateway reads
// the player_session_token from the X-Session-Token cookie header and forwards
// it with character_id, title, and description. Authorization and identity
// resolution are owned entirely by the facade.
func (h *Handler) WebCreateScene(ctx context.Context, req *connect.Request[webv1.WebCreateSceneRequest]) (*connect.Response[webv1.WebCreateSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebCreateScene", "session_id", req.Msg.GetSessionId(), "character_id", req.Msg.GetCharacterId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.CreateScene(rpcCtx, &sceneaccessv1.CreateSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		Title:              req.Msg.GetTitle(),
		Description:        req.Msg.GetDescription(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: create scene RPC failed", err, "session_id", req.Msg.GetSessionId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebCreateSceneResponse{Scene: resp.GetScene()}), nil
}

// WebEndScene proxies to SceneAccessService.EndScene. The gateway reads the
// player_session_token from the X-Session-Token cookie header and forwards it
// with character_id and scene_id. Authorization is owned by the facade.
func (h *Handler) WebEndScene(ctx context.Context, req *connect.Request[webv1.WebEndSceneRequest]) (*connect.Response[webv1.WebEndSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebEndScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.EndScene(rpcCtx, &sceneaccessv1.EndSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: end scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebEndSceneResponse{Scene: resp.GetScene()}), nil
}

// WebPauseScene proxies to SceneAccessService.PauseScene. The gateway reads the
// player_session_token from the X-Session-Token cookie header and forwards it
// with character_id and scene_id. Authorization is owned by the facade.
func (h *Handler) WebPauseScene(ctx context.Context, req *connect.Request[webv1.WebPauseSceneRequest]) (*connect.Response[webv1.WebPauseSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebPauseScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.PauseScene(rpcCtx, &sceneaccessv1.PauseSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: pause scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebPauseSceneResponse{Scene: resp.GetScene()}), nil
}

// WebResumeScene proxies to SceneAccessService.ResumeScene. The gateway reads
// the player_session_token from the X-Session-Token cookie header and forwards
// it with character_id and scene_id. Authorization is owned by the facade.
func (h *Handler) WebResumeScene(ctx context.Context, req *connect.Request[webv1.WebResumeSceneRequest]) (*connect.Response[webv1.WebResumeSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebResumeScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.ResumeScene(rpcCtx, &sceneaccessv1.ResumeSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: resume scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebResumeSceneResponse{Scene: resp.GetScene()}), nil
}

// WebUpdateScene proxies to SceneAccessService.UpdateScene. The gateway reads the
// player_session_token from the X-Session-Token cookie header and forwards it
// with character_id, scene_id, the editable fields, and update_mask.
// Authorization (owner-only) is owned by the facade.
func (h *Handler) WebUpdateScene(ctx context.Context, req *connect.Request[webv1.WebUpdateSceneRequest]) (*connect.Response[webv1.WebUpdateSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebUpdateScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.UpdateScene(rpcCtx, &sceneaccessv1.UpdateSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
		Title:              req.Msg.GetTitle(),
		Description:        req.Msg.GetDescription(),
		Visibility:         req.Msg.GetVisibility(),
		PoseOrderMode:      req.Msg.GetPoseOrderMode(),
		Tags:               req.Msg.GetTags(),
		ContentWarnings:    req.Msg.GetContentWarnings(),
		UpdateMask:         req.Msg.GetUpdateMask(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: update scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebUpdateSceneResponse{Scene: resp.GetScene()}), nil
}

// WebExportScene proxies to SceneAccessService.ExportScene. The gateway reads
// the player_session_token from the X-Session-Token cookie header and forwards
// it with scene_id, character_id, and format. Authorization and identity
// resolution are owned entirely by the facade.
func (h *Handler) WebExportScene(ctx context.Context, req *connect.Request[webv1.WebExportSceneRequest]) (*connect.Response[webv1.WebExportSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebExportScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.ExportScene(rpcCtx, &sceneaccessv1.ExportSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
		Format:             req.Msg.GetFormat(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: export scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebExportSceneResponse{
		Content:  resp.GetContent(),
		MimeType: resp.GetMimeType(),
		Filename: resp.GetFilename(),
	}), nil
}

// WebSetSceneFocus proxies to SceneAccessService.SetSceneFocus. The gateway
// reads the player_session_token from the X-Session-Token cookie header and
// forwards it with connection_id and scene_id. Authorization is owned entirely
// by the facade.
func (h *Handler) WebSetSceneFocus(ctx context.Context, req *connect.Request[webv1.WebSetSceneFocusRequest]) (*connect.Response[webv1.WebSetSceneFocusResponse], error) {
	slog.DebugContext(ctx, "web: WebSetSceneFocus", "session_id", req.Msg.GetSessionId(), "connection_id", req.Msg.GetConnectionId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	_, err := h.sceneAccess.SetSceneFocus(rpcCtx, &sceneaccessv1.SetSceneFocusRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		ConnectionId:       req.Msg.GetConnectionId(),
		SceneId:            req.Msg.GetSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: set scene focus RPC failed", err, "session_id", req.Msg.GetSessionId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebSetSceneFocusResponse{}), nil
}

// WebListPublishedScenes proxies to SceneAccessService.ListPublishedScenes.
// The gateway reads the player_session_token from the X-Session-Token cookie
// header and forwards it with pagination and tag-filter fields. Authorization
// (guest denial) is owned entirely by the facade.
func (h *Handler) WebListPublishedScenes(ctx context.Context, req *connect.Request[webv1.WebListPublishedScenesRequest]) (*connect.Response[webv1.WebListPublishedScenesResponse], error) {
	slog.DebugContext(ctx, "web: WebListPublishedScenes", "session_id", req.Msg.GetSessionId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.ListPublishedScenes(rpcCtx, &sceneaccessv1.ListPublishedScenesRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		Limit:              req.Msg.GetLimit(),
		Offset:             req.Msg.GetOffset(),
		Tags:               req.Msg.GetTags(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list published scenes RPC failed", err, "session_id", req.Msg.GetSessionId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebListPublishedScenesResponse{
		Archives: resp.GetArchives(),
	}), nil
}

// WebGetPublicSceneArchive proxies to SceneAccessService.GetPublicSceneArchive.
// The gateway reads the player_session_token from the X-Session-Token cookie
// header and forwards it with the published_scene_id. Authorization (guest
// denial, PUBLISHED-only gate) is owned entirely by the facade.
func (h *Handler) WebGetPublicSceneArchive(ctx context.Context, req *connect.Request[webv1.WebGetPublicSceneArchiveRequest]) (*connect.Response[webv1.WebGetPublicSceneArchiveResponse], error) {
	slog.DebugContext(ctx, "web: WebGetPublicSceneArchive", "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.GetPublicSceneArchive(rpcCtx, &sceneaccessv1.GetPublicSceneArchiveRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		PublishedSceneId:   req.Msg.GetPublishedSceneId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: get public scene archive RPC failed", err, "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebGetPublicSceneArchiveResponse{
		Id:                   resp.GetId(),
		TitleSnapshot:        resp.GetTitleSnapshot(),
		ParticipantsSnapshot: resp.GetParticipantsSnapshot(),
		ContentEntries:       resp.GetContentEntries(),
		PublishedAtUnixNs:    resp.GetPublishedAtUnixNs(),
	}), nil
}

// WebDownloadPublicSceneArchive proxies to
// SceneAccessService.DownloadPublicSceneArchive. The gateway reads the
// player_session_token from the X-Session-Token cookie header and forwards it
// with published_scene_id and format. Authorization (guest denial,
// PUBLISHED-only gate) is owned entirely by the facade.
func (h *Handler) WebDownloadPublicSceneArchive(ctx context.Context, req *connect.Request[webv1.WebDownloadPublicSceneArchiveRequest]) (*connect.Response[webv1.WebDownloadPublicSceneArchiveResponse], error) {
	slog.DebugContext(ctx, "web: WebDownloadPublicSceneArchive", "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.sceneAccess.DownloadPublicSceneArchive(rpcCtx, &sceneaccessv1.DownloadPublicSceneArchiveRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		PublishedSceneId:   req.Msg.GetPublishedSceneId(),
		Format:             req.Msg.GetFormat(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: download public scene archive RPC failed", err, "session_id", req.Msg.GetSessionId(), "published_scene_id", req.Msg.GetPublishedSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebDownloadPublicSceneArchiveResponse{
		Content:  resp.GetContent(),
		MimeType: resp.GetMimeType(),
	}), nil
}

// WebInviteToScene proxies to SceneAccessService.InviteToScene. The session
// token is read from the X-Session-Token header; the facade verifies identity,
// guest rejection, and scene-ownership before adding the invitee.
func (h *Handler) WebInviteToScene(ctx context.Context, req *connect.Request[webv1.WebInviteToSceneRequest]) (*connect.Response[webv1.WebInviteToSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebInviteToScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if _, err := h.sceneAccess.InviteToScene(rpcCtx, &sceneaccessv1.InviteToSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
		TargetCharacterId:  req.Msg.GetTargetCharacterId(),
	}); err != nil {
		errutil.LogErrorContext(ctx, "web: invite to scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebInviteToSceneResponse{}), nil
}

// WebKickFromScene proxies to SceneAccessService.KickFromScene. The session
// token is read from the X-Session-Token header; the facade verifies ownership
// before removing the target member.
func (h *Handler) WebKickFromScene(ctx context.Context, req *connect.Request[webv1.WebKickFromSceneRequest]) (*connect.Response[webv1.WebKickFromSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebKickFromScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if _, err := h.sceneAccess.KickFromScene(rpcCtx, &sceneaccessv1.KickFromSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
		TargetCharacterId:  req.Msg.GetTargetCharacterId(),
	}); err != nil {
		errutil.LogErrorContext(ctx, "web: kick from scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebKickFromSceneResponse{}), nil
}

// WebTransferOwnership proxies to SceneAccessService.TransferOwnership. The
// session token is read from the X-Session-Token header; the facade verifies
// the caller is the current owner and that the heir is an existing member.
func (h *Handler) WebTransferOwnership(ctx context.Context, req *connect.Request[webv1.WebTransferOwnershipRequest]) (*connect.Response[webv1.WebTransferOwnershipResponse], error) {
	slog.DebugContext(ctx, "web: WebTransferOwnership", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if _, err := h.sceneAccess.TransferOwnership(rpcCtx, &sceneaccessv1.TransferOwnershipRequest{
		SessionId:           req.Msg.GetSessionId(),
		PlayerSessionToken:  token,
		CharacterId:         req.Msg.GetCharacterId(),
		SceneId:             req.Msg.GetSceneId(),
		NewOwnerCharacterId: req.Msg.GetNewOwnerCharacterId(),
	}); err != nil {
		errutil.LogErrorContext(ctx, "web: transfer ownership RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebTransferOwnershipResponse{}), nil
}

// WebLeaveScene proxies to SceneAccessService.LeaveScene. The session token is
// read from the X-Session-Token header; the facade verifies membership and
// rejects the scene owner (who must transfer ownership first).
func (h *Handler) WebLeaveScene(ctx context.Context, req *connect.Request[webv1.WebLeaveSceneRequest]) (*connect.Response[webv1.WebLeaveSceneResponse], error) {
	slog.DebugContext(ctx, "web: WebLeaveScene", "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())

	if h.sceneAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("scene access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if _, err := h.sceneAccess.LeaveScene(rpcCtx, &sceneaccessv1.LeaveSceneRequest{
		SessionId:          req.Msg.GetSessionId(),
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		SceneId:            req.Msg.GetSceneId(),
	}); err != nil {
		errutil.LogErrorContext(ctx, "web: leave scene RPC failed", err, "session_id", req.Msg.GetSessionId(), "scene_id", req.Msg.GetSceneId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebLeaveSceneResponse{}), nil
}
