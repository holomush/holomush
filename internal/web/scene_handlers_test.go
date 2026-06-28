// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	fieldmaskpb "google.golang.org/protobuf/types/known/fieldmaskpb"

	holoGRPC "github.com/holomush/holomush/internal/grpc"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// TestSceneAccessClient_SatisfiedByGRPCClient verifies at compile time that
// *holoGRPC.Client implements the SceneAccessClient interface.
func TestSceneAccessClient_SatisfiedByGRPCClient(t *testing.T) {
	t.Helper()
	var _ SceneAccessClient = (*holoGRPC.Client)(nil)
}

// mockSceneAccessClient is a test double for SceneAccessClient.
type mockSceneAccessClient struct {
	listScenesReq  *sceneaccessv1.ListScenesForViewerRequest
	listScenesResp *sceneaccessv1.ListScenesForViewerResponse
	listScenesErr  error

	getSceneReq  *sceneaccessv1.GetSceneForViewerRequest
	getSceneResp *sceneaccessv1.GetSceneForViewerResponse
	getSceneErr  error

	listMyScenesReq  *sceneaccessv1.ListMyScenesRequest
	listMyScenesResp *sceneaccessv1.ListMyScenesResponse
	listMyScenesErr  error

	watchSceneReq  *sceneaccessv1.WatchSceneRequest
	watchSceneResp *sceneaccessv1.WatchSceneResponse
	watchSceneErr  error

	createSceneReq *sceneaccessv1.CreateSceneRequest
	createSceneErr error

	endSceneReq    *sceneaccessv1.EndSceneRequest
	endSceneErr    error
	pauseSceneReq  *sceneaccessv1.PauseSceneRequest
	pauseSceneErr  error
	resumeSceneReq *sceneaccessv1.ResumeSceneRequest
	resumeSceneErr error

	updateSceneReq *sceneaccessv1.UpdateSceneRequest
	updateSceneErr error

	exportSceneReq  *sceneaccessv1.ExportSceneRequest
	exportSceneResp *sceneaccessv1.ExportSceneResponse
	exportSceneErr  error

	setFocusReq  *sceneaccessv1.SetSceneFocusRequest
	setFocusResp *sceneaccessv1.SetSceneFocusResponse
	setFocusErr  error

	listPublishedReq  *sceneaccessv1.ListPublishedScenesRequest
	listPublishedResp *sceneaccessv1.ListPublishedScenesResponse
	listPublishedErr  error

	getArchiveReq  *sceneaccessv1.GetPublicSceneArchiveRequest
	getArchiveResp *sceneaccessv1.GetPublicSceneArchiveResponse
	getArchiveErr  error

	downloadArchiveReq  *sceneaccessv1.DownloadPublicSceneArchiveRequest
	downloadArchiveResp *sceneaccessv1.DownloadPublicSceneArchiveResponse
	downloadArchiveErr  error

	inviteToSceneReq *sceneaccessv1.InviteToSceneRequest
	inviteToSceneErr error

	kickFromSceneReq *sceneaccessv1.KickFromSceneRequest
	kickFromSceneErr error

	transferOwnershipReq *sceneaccessv1.TransferOwnershipRequest
	transferOwnershipErr error

	leaveSceneReq *sceneaccessv1.LeaveSceneRequest
	leaveSceneErr error
}

func (m *mockSceneAccessClient) ListScenesForViewer(_ context.Context, req *sceneaccessv1.ListScenesForViewerRequest) (*sceneaccessv1.ListScenesForViewerResponse, error) {
	m.listScenesReq = req
	return m.listScenesResp, m.listScenesErr
}

func (m *mockSceneAccessClient) GetSceneForViewer(_ context.Context, req *sceneaccessv1.GetSceneForViewerRequest) (*sceneaccessv1.GetSceneForViewerResponse, error) {
	m.getSceneReq = req
	return m.getSceneResp, m.getSceneErr
}

func (m *mockSceneAccessClient) ListMyScenes(_ context.Context, req *sceneaccessv1.ListMyScenesRequest) (*sceneaccessv1.ListMyScenesResponse, error) {
	m.listMyScenesReq = req
	return m.listMyScenesResp, m.listMyScenesErr
}

func (m *mockSceneAccessClient) WatchScene(_ context.Context, req *sceneaccessv1.WatchSceneRequest) (*sceneaccessv1.WatchSceneResponse, error) {
	m.watchSceneReq = req
	return m.watchSceneResp, m.watchSceneErr
}

func (m *mockSceneAccessClient) CreateScene(_ context.Context, req *sceneaccessv1.CreateSceneRequest) (*sceneaccessv1.CreateSceneResponse, error) {
	m.createSceneReq = req
	if m.createSceneErr != nil {
		return nil, m.createSceneErr
	}
	return &sceneaccessv1.CreateSceneResponse{Scene: &scenev1.SceneInfo{Id: "scene-123"}}, nil
}

func (m *mockSceneAccessClient) EndScene(_ context.Context, req *sceneaccessv1.EndSceneRequest) (*sceneaccessv1.EndSceneResponse, error) {
	m.endSceneReq = req
	if m.endSceneErr != nil {
		return nil, m.endSceneErr
	}
	return &sceneaccessv1.EndSceneResponse{Scene: &scenev1.SceneInfo{Id: "scene-123", State: "ended"}}, nil
}

func (m *mockSceneAccessClient) PauseScene(_ context.Context, req *sceneaccessv1.PauseSceneRequest) (*sceneaccessv1.PauseSceneResponse, error) {
	m.pauseSceneReq = req
	if m.pauseSceneErr != nil {
		return nil, m.pauseSceneErr
	}
	return &sceneaccessv1.PauseSceneResponse{Scene: &scenev1.SceneInfo{Id: "scene-123", State: "paused"}}, nil
}

func (m *mockSceneAccessClient) ResumeScene(_ context.Context, req *sceneaccessv1.ResumeSceneRequest) (*sceneaccessv1.ResumeSceneResponse, error) {
	m.resumeSceneReq = req
	if m.resumeSceneErr != nil {
		return nil, m.resumeSceneErr
	}
	return &sceneaccessv1.ResumeSceneResponse{Scene: &scenev1.SceneInfo{Id: "scene-123", State: "active"}}, nil
}

func (m *mockSceneAccessClient) UpdateScene(_ context.Context, req *sceneaccessv1.UpdateSceneRequest) (*sceneaccessv1.UpdateSceneResponse, error) {
	m.updateSceneReq = req
	if m.updateSceneErr != nil {
		return nil, m.updateSceneErr
	}
	return &sceneaccessv1.UpdateSceneResponse{Scene: &scenev1.SceneInfo{Id: "scene-123", Visibility: "private"}}, nil
}

func (m *mockSceneAccessClient) ExportScene(_ context.Context, req *sceneaccessv1.ExportSceneRequest) (*sceneaccessv1.ExportSceneResponse, error) {
	m.exportSceneReq = req
	return m.exportSceneResp, m.exportSceneErr
}

func (m *mockSceneAccessClient) SetSceneFocus(_ context.Context, req *sceneaccessv1.SetSceneFocusRequest) (*sceneaccessv1.SetSceneFocusResponse, error) {
	m.setFocusReq = req
	return m.setFocusResp, m.setFocusErr
}

func (m *mockSceneAccessClient) ListPublishedScenes(_ context.Context, req *sceneaccessv1.ListPublishedScenesRequest) (*sceneaccessv1.ListPublishedScenesResponse, error) {
	m.listPublishedReq = req
	return m.listPublishedResp, m.listPublishedErr
}

func (m *mockSceneAccessClient) GetPublicSceneArchive(_ context.Context, req *sceneaccessv1.GetPublicSceneArchiveRequest) (*sceneaccessv1.GetPublicSceneArchiveResponse, error) {
	m.getArchiveReq = req
	return m.getArchiveResp, m.getArchiveErr
}

func (m *mockSceneAccessClient) DownloadPublicSceneArchive(_ context.Context, req *sceneaccessv1.DownloadPublicSceneArchiveRequest) (*sceneaccessv1.DownloadPublicSceneArchiveResponse, error) {
	m.downloadArchiveReq = req
	return m.downloadArchiveResp, m.downloadArchiveErr
}

func (m *mockSceneAccessClient) InviteToScene(_ context.Context, req *sceneaccessv1.InviteToSceneRequest) (*sceneaccessv1.InviteToSceneResponse, error) {
	m.inviteToSceneReq = req
	if m.inviteToSceneErr != nil {
		return nil, m.inviteToSceneErr
	}
	return &sceneaccessv1.InviteToSceneResponse{}, nil
}

func (m *mockSceneAccessClient) KickFromScene(_ context.Context, req *sceneaccessv1.KickFromSceneRequest) (*sceneaccessv1.KickFromSceneResponse, error) {
	m.kickFromSceneReq = req
	if m.kickFromSceneErr != nil {
		return nil, m.kickFromSceneErr
	}
	return &sceneaccessv1.KickFromSceneResponse{}, nil
}

func (m *mockSceneAccessClient) TransferOwnership(_ context.Context, req *sceneaccessv1.TransferOwnershipRequest) (*sceneaccessv1.TransferOwnershipResponse, error) {
	m.transferOwnershipReq = req
	if m.transferOwnershipErr != nil {
		return nil, m.transferOwnershipErr
	}
	return &sceneaccessv1.TransferOwnershipResponse{}, nil
}

func (m *mockSceneAccessClient) LeaveScene(_ context.Context, req *sceneaccessv1.LeaveSceneRequest) (*sceneaccessv1.LeaveSceneResponse, error) {
	m.leaveSceneReq = req
	if m.leaveSceneErr != nil {
		return nil, m.leaveSceneErr
	}
	return &sceneaccessv1.LeaveSceneResponse{}, nil
}

// Publish-vote facade methods. Minimal stubs so mockSceneAccessClient satisfies
// the extended SceneAccessClient interface; the BFF publish-handler tests
// (holomush-5rh.24.37) enrich these with request capture as needed.
func (m *mockSceneAccessClient) StartScenePublish(_ context.Context, _ *sceneaccessv1.StartScenePublishRequest) (*sceneaccessv1.StartScenePublishResponse, error) {
	return &sceneaccessv1.StartScenePublishResponse{}, nil
}

func (m *mockSceneAccessClient) CastPublishSceneVote(_ context.Context, _ *sceneaccessv1.CastPublishSceneVoteRequest) (*sceneaccessv1.CastPublishSceneVoteResponse, error) {
	return &sceneaccessv1.CastPublishSceneVoteResponse{}, nil
}

func (m *mockSceneAccessClient) WithdrawScenePublish(_ context.Context, _ *sceneaccessv1.WithdrawScenePublishRequest) (*sceneaccessv1.WithdrawScenePublishResponse, error) {
	return &sceneaccessv1.WithdrawScenePublishResponse{}, nil
}

func (m *mockSceneAccessClient) GetPublishedScene(_ context.Context, _ *sceneaccessv1.GetPublishedSceneRequest) (*sceneaccessv1.GetPublishedSceneResponse, error) {
	return &sceneaccessv1.GetPublishedSceneResponse{}, nil
}

// --- WebListScenes ---

func TestWebListScenesForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	const token = "tok-list-scenes"
	sc := &mockSceneAccessClient{
		listScenesResp: &sceneaccessv1.ListScenesForViewerResponse{
			Scenes: []*scenev1.SceneInfo{{Id: "sc-01", Title: "Test Scene"}},
		},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebListScenesRequest{
		SessionId:   "sess-1",
		CharacterId: "char-1",
		Limit:       10,
		Offset:      5,
		Tags:        []string{"adventure"},
	})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebListScenes(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.listScenesReq)
	assert.Equal(t, "sess-1", sc.listScenesReq.GetSessionId())
	assert.Equal(t, token, sc.listScenesReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.listScenesReq.GetCharacterId())
	assert.Equal(t, int32(10), sc.listScenesReq.GetLimit())
	assert.Equal(t, int32(5), sc.listScenesReq.GetOffset())
	assert.Equal(t, []string{"adventure"}, sc.listScenesReq.GetTags())

	require.Len(t, resp.Msg.GetScenes(), 1)
	assert.Equal(t, "sc-01", resp.Msg.GetScenes()[0].GetId())
}

func TestWebListScenesPassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := status.Error(codes.Unauthenticated, "invalid token")
	sc := &mockSceneAccessClient{listScenesErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebListScenes(context.Background(),
		connect.NewRequest(&webv1.WebListScenesRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, facadeErr, err)
}

func TestWebListScenesReturnsUnimplementedWhenClientAbsent(t *testing.T) {
	h := NewHandler(&mockCoreClient{})
	_, err := h.WebListScenes(context.Background(),
		connect.NewRequest(&webv1.WebListScenesRequest{}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnimplemented, connectErr.Code())
}

// --- WebGetScene ---

func TestWebGetSceneForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	const token = "tok-get-scene"
	sc := &mockSceneAccessClient{
		getSceneResp: &sceneaccessv1.GetSceneForViewerResponse{
			Scene: &scenev1.SceneInfo{Id: "sc-02", Title: "Scene Two"},
		},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebGetSceneRequest{
		SessionId:   "sess-2",
		CharacterId: "char-2",
		SceneId:     "sc-02",
	})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebGetScene(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.getSceneReq)
	assert.Equal(t, "sess-2", sc.getSceneReq.GetSessionId())
	assert.Equal(t, token, sc.getSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-2", sc.getSceneReq.GetCharacterId())
	assert.Equal(t, "sc-02", sc.getSceneReq.GetSceneId())
	assert.Equal(t, "Scene Two", resp.Msg.GetScene().GetTitle())
}

func TestWebGetScenePassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := status.Error(codes.NotFound, "scene not found")
	sc := &mockSceneAccessClient{getSceneErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebGetScene(context.Background(),
		connect.NewRequest(&webv1.WebGetSceneRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, facadeErr, err)
}

// --- WebListMyScenes ---

func TestWebListMyScenesForwardsTokenAndCharacterIdToFacade(t *testing.T) {
	const token = "tok-list-my"
	sc := &mockSceneAccessClient{
		listMyScenesResp: &sceneaccessv1.ListMyScenesResponse{
			Scenes: []*scenev1.CharacterSceneInfo{{Scene: &scenev1.SceneInfo{Id: "sc-03"}, Role: "member"}},
		},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebListMyScenesRequest{
		SessionId:   "sess-3",
		CharacterId: "char-3",
	})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebListMyScenes(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.listMyScenesReq)
	assert.Equal(t, "sess-3", sc.listMyScenesReq.GetSessionId())
	assert.Equal(t, token, sc.listMyScenesReq.GetPlayerSessionToken())
	assert.Equal(t, "char-3", sc.listMyScenesReq.GetCharacterId())
	require.Len(t, resp.Msg.GetScenes(), 1)
	assert.Equal(t, "sc-03", resp.Msg.GetScenes()[0].GetScene().GetId())
}

func TestWebListMyScenesPassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := oops.Code("RPC_FAILED").Errorf("facade unavailable")
	sc := &mockSceneAccessClient{listMyScenesErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebListMyScenes(context.Background(),
		connect.NewRequest(&webv1.WebListMyScenesRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.ErrorIs(t, err, facadeErr)
}

// --- WebWatchScene ---

func TestWebWatchSceneForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	const token = "tok-watch"
	sc := &mockSceneAccessClient{
		watchSceneResp: &sceneaccessv1.WatchSceneResponse{
			Participant: &scenev1.ParticipantInfo{CharacterId: "char-4", Role: "observer"},
		},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebWatchSceneRequest{
		SessionId:   "sess-4",
		CharacterId: "char-4",
		SceneId:     "sc-04",
	})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebWatchScene(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.watchSceneReq)
	assert.Equal(t, "sess-4", sc.watchSceneReq.GetSessionId())
	assert.Equal(t, token, sc.watchSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-4", sc.watchSceneReq.GetCharacterId())
	assert.Equal(t, "sc-04", sc.watchSceneReq.GetSceneId())
	assert.Equal(t, "observer", resp.Msg.GetParticipant().GetRole())
}

func TestWebWatchScenePassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := status.Error(codes.FailedPrecondition, "no game session")
	sc := &mockSceneAccessClient{watchSceneErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebWatchScene(context.Background(),
		connect.NewRequest(&webv1.WebWatchSceneRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, facadeErr, err)
}

// --- WebCreateScene ---

func TestWebCreateSceneForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebCreateSceneRequest{
		SessionId: "sess-1", CharacterId: "char-1", Title: "The Manor", Description: "dusk",
	})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	resp, err := h.WebCreateScene(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "scene-123", resp.Msg.GetScene().GetId())
	require.NotNil(t, sc.createSceneReq)
	assert.Equal(t, "tok-abc", sc.createSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.createSceneReq.GetCharacterId())
	assert.Equal(t, "The Manor", sc.createSceneReq.GetTitle())
	assert.Equal(t, "dusk", sc.createSceneReq.GetDescription())
}

func TestWebCreateScenePassesStatusErrorThroughAsIs(t *testing.T) {
	wantErr := status.Error(codes.PermissionDenied, "guests cannot access scenes")
	sc := &mockSceneAccessClient{createSceneErr: wantErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebCreateSceneRequest{SessionId: "s", CharacterId: "c", Title: "X"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebCreateScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// --- WebEndScene ---

func TestWebEndSceneForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebEndSceneRequest{SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123"})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	resp, err := h.WebEndScene(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "ended", resp.Msg.GetScene().GetState())
	require.NotNil(t, sc.endSceneReq)
	assert.Equal(t, "tok-abc", sc.endSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.endSceneReq.GetCharacterId())
	assert.Equal(t, "scene-123", sc.endSceneReq.GetSceneId())
}

func TestWebEndScenePassesStatusErrorThroughAsIs(t *testing.T) {
	sc := &mockSceneAccessClient{endSceneErr: status.Error(codes.PermissionDenied, "not permitted to end this scene")}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebEndSceneRequest{SessionId: "s", CharacterId: "c", SceneId: "x"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebEndScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// --- WebPauseScene ---

func TestWebPauseSceneForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebPauseSceneRequest{SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123"})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	resp, err := h.WebPauseScene(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "paused", resp.Msg.GetScene().GetState())
	require.NotNil(t, sc.pauseSceneReq)
	assert.Equal(t, "tok-abc", sc.pauseSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.pauseSceneReq.GetCharacterId())
	assert.Equal(t, "scene-123", sc.pauseSceneReq.GetSceneId())
}

func TestWebPauseScenePassesStatusErrorThroughAsIs(t *testing.T) {
	sc := &mockSceneAccessClient{pauseSceneErr: status.Error(codes.PermissionDenied, "not permitted to pause this scene")}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebPauseSceneRequest{SessionId: "s", CharacterId: "c", SceneId: "x"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebPauseScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// --- WebResumeScene ---

func TestWebResumeSceneForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebResumeSceneRequest{SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123"})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	resp, err := h.WebResumeScene(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "active", resp.Msg.GetScene().GetState())
	require.NotNil(t, sc.resumeSceneReq)
	assert.Equal(t, "tok-abc", sc.resumeSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.resumeSceneReq.GetCharacterId())
	assert.Equal(t, "scene-123", sc.resumeSceneReq.GetSceneId())
}

func TestWebResumeScenePassesStatusErrorThroughAsIs(t *testing.T) {
	sc := &mockSceneAccessClient{resumeSceneErr: status.Error(codes.PermissionDenied, "not permitted to resume this scene")}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebResumeSceneRequest{SessionId: "s", CharacterId: "c", SceneId: "x"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebResumeScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// --- WebExportScene ---

func TestWebExportSceneForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	const token = "tok-export"
	sc := &mockSceneAccessClient{
		exportSceneResp: &sceneaccessv1.ExportSceneResponse{
			Content:  []byte("# Scene log"),
			MimeType: "text/markdown",
			Filename: "scene-01.md",
		},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebExportSceneRequest{
		SessionId:   "sess-5",
		CharacterId: "char-5",
		SceneId:     "sc-05",
		Format:      "markdown",
	})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebExportScene(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.exportSceneReq)
	assert.Equal(t, "sess-5", sc.exportSceneReq.GetSessionId())
	assert.Equal(t, token, sc.exportSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-5", sc.exportSceneReq.GetCharacterId())
	assert.Equal(t, "sc-05", sc.exportSceneReq.GetSceneId())
	assert.Equal(t, "markdown", sc.exportSceneReq.GetFormat())

	assert.Equal(t, []byte("# Scene log"), resp.Msg.GetContent())
	assert.Equal(t, "text/markdown", resp.Msg.GetMimeType())
	assert.Equal(t, "scene-01.md", resp.Msg.GetFilename())
}

func TestWebExportScenePassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := status.Error(codes.NotFound, "scene not found")
	sc := &mockSceneAccessClient{exportSceneErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebExportScene(context.Background(),
		connect.NewRequest(&webv1.WebExportSceneRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, facadeErr, err)
}

// --- WebSetSceneFocus ---

func TestWebSetSceneFocusForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	const token = "tok-focus"
	sc := &mockSceneAccessClient{
		setFocusResp: &sceneaccessv1.SetSceneFocusResponse{},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebSetSceneFocusRequest{
		SessionId:    "sess-6",
		ConnectionId: "conn-abc",
		SceneId:      "sc-06",
	})
	req.Header().Set(headerInjectSessionToken, token)

	_, err := h.WebSetSceneFocus(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.setFocusReq)
	assert.Equal(t, "sess-6", sc.setFocusReq.GetSessionId())
	assert.Equal(t, token, sc.setFocusReq.GetPlayerSessionToken())
	assert.Equal(t, "conn-abc", sc.setFocusReq.GetConnectionId())
	assert.Equal(t, "sc-06", sc.setFocusReq.GetSceneId())
}

func TestWebSetSceneFocusPassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := status.Error(codes.PermissionDenied, "connection not owned")
	sc := &mockSceneAccessClient{setFocusErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebSetSceneFocus(context.Background(),
		connect.NewRequest(&webv1.WebSetSceneFocusRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, facadeErr, err)
}

// --- WebListPublishedScenes ---

func TestWebListPublishedScenesForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	const token = "tok-published"
	sc := &mockSceneAccessClient{
		listPublishedResp: &sceneaccessv1.ListPublishedScenesResponse{
			Archives: []*scenev1.PublicSceneArchive{{Id: "pub-01"}},
		},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebListPublishedScenesRequest{
		SessionId: "sess-7",
		Limit:     20,
		Offset:    0,
		Tags:      []string{"canon"},
	})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebListPublishedScenes(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.listPublishedReq)
	assert.Equal(t, "sess-7", sc.listPublishedReq.GetSessionId())
	assert.Equal(t, token, sc.listPublishedReq.GetPlayerSessionToken())
	assert.Equal(t, int32(20), sc.listPublishedReq.GetLimit())
	assert.Equal(t, []string{"canon"}, sc.listPublishedReq.GetTags())
	require.Len(t, resp.Msg.GetArchives(), 1)
	assert.Equal(t, "pub-01", resp.Msg.GetArchives()[0].GetId())
}

func TestWebListPublishedScenesPassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := status.Error(codes.PermissionDenied, "guest denied")
	sc := &mockSceneAccessClient{listPublishedErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebListPublishedScenes(context.Background(),
		connect.NewRequest(&webv1.WebListPublishedScenesRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, facadeErr, err)
}

// --- WebGetPublicSceneArchive ---

func TestWebGetPublicSceneArchiveForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	const token = "tok-archive"
	sc := &mockSceneAccessClient{
		getArchiveResp: &sceneaccessv1.GetPublicSceneArchiveResponse{
			Id:                   "pub-02",
			TitleSnapshot:        "Epic Battle",
			ParticipantsSnapshot: []string{"Alice", "Bob"},
			PublishedAtUnixNs:    1234567890,
		},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebGetPublicSceneArchiveRequest{
		SessionId:        "sess-8",
		PublishedSceneId: "pub-02",
	})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebGetPublicSceneArchive(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.getArchiveReq)
	assert.Equal(t, "sess-8", sc.getArchiveReq.GetSessionId())
	assert.Equal(t, token, sc.getArchiveReq.GetPlayerSessionToken())
	assert.Equal(t, "pub-02", sc.getArchiveReq.GetPublishedSceneId())

	assert.Equal(t, "pub-02", resp.Msg.GetId())
	assert.Equal(t, "Epic Battle", resp.Msg.GetTitleSnapshot())
	assert.Equal(t, []string{"Alice", "Bob"}, resp.Msg.GetParticipantsSnapshot())
	assert.Equal(t, int64(1234567890), resp.Msg.GetPublishedAtUnixNs())
}

func TestWebGetPublicSceneArchivePassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := status.Error(codes.NotFound, "not published")
	sc := &mockSceneAccessClient{getArchiveErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebGetPublicSceneArchive(context.Background(),
		connect.NewRequest(&webv1.WebGetPublicSceneArchiveRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, facadeErr, err)
}

// --- WebDownloadPublicSceneArchive ---

func TestWebDownloadPublicSceneArchiveForwardsTokenAndOpFieldsToFacade(t *testing.T) {
	const token = "tok-download"
	sc := &mockSceneAccessClient{
		downloadArchiveResp: &sceneaccessv1.DownloadPublicSceneArchiveResponse{
			Content:  []byte("# Archive"),
			MimeType: "text/markdown",
		},
	}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebDownloadPublicSceneArchiveRequest{
		SessionId:        "sess-9",
		PublishedSceneId: "pub-03",
		Format:           "markdown",
	})
	req.Header().Set(headerInjectSessionToken, token)

	resp, err := h.WebDownloadPublicSceneArchive(context.Background(), req)
	require.NoError(t, err)

	require.NotNil(t, sc.downloadArchiveReq)
	assert.Equal(t, "sess-9", sc.downloadArchiveReq.GetSessionId())
	assert.Equal(t, token, sc.downloadArchiveReq.GetPlayerSessionToken())
	assert.Equal(t, "pub-03", sc.downloadArchiveReq.GetPublishedSceneId())
	assert.Equal(t, "markdown", sc.downloadArchiveReq.GetFormat())

	assert.Equal(t, []byte("# Archive"), resp.Msg.GetContent())
	assert.Equal(t, "text/markdown", resp.Msg.GetMimeType())
}

func TestWebDownloadPublicSceneArchivePassesStatusErrorThroughAsIs(t *testing.T) {
	facadeErr := status.Error(codes.NotFound, "not published")
	sc := &mockSceneAccessClient{downloadArchiveErr: facadeErr}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	_, err := h.WebDownloadPublicSceneArchive(context.Background(),
		connect.NewRequest(&webv1.WebDownloadPublicSceneArchiveRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, facadeErr, err)
}

// --- WebInviteToScene ---

func TestWebInviteToSceneForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebInviteToSceneRequest{
		SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123", TargetCharacterId: "char-eve",
	})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	_, err := h.WebInviteToScene(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, sc.inviteToSceneReq)
	assert.Equal(t, "tok-abc", sc.inviteToSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.inviteToSceneReq.GetCharacterId())
	assert.Equal(t, "scene-123", sc.inviteToSceneReq.GetSceneId())
	assert.Equal(t, "char-eve", sc.inviteToSceneReq.GetTargetCharacterId())
}

func TestWebInviteToScenePassesStatusErrorThroughAsIs(t *testing.T) {
	sc := &mockSceneAccessClient{inviteToSceneErr: status.Error(codes.PermissionDenied, "not permitted to invite")}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebInviteToSceneRequest{SessionId: "s", CharacterId: "c", SceneId: "x", TargetCharacterId: "t"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebInviteToScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// --- WebKickFromScene ---

func TestWebKickFromSceneForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebKickFromSceneRequest{
		SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123", TargetCharacterId: "char-bad",
	})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	_, err := h.WebKickFromScene(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, sc.kickFromSceneReq)
	assert.Equal(t, "tok-abc", sc.kickFromSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.kickFromSceneReq.GetCharacterId())
	assert.Equal(t, "scene-123", sc.kickFromSceneReq.GetSceneId())
	assert.Equal(t, "char-bad", sc.kickFromSceneReq.GetTargetCharacterId())
}

func TestWebKickFromScenePassesStatusErrorThroughAsIs(t *testing.T) {
	sc := &mockSceneAccessClient{kickFromSceneErr: status.Error(codes.PermissionDenied, "not permitted to kick")}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebKickFromSceneRequest{SessionId: "s", CharacterId: "c", SceneId: "x", TargetCharacterId: "t"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebKickFromScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// --- WebTransferOwnership ---

func TestWebTransferOwnershipForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebTransferOwnershipRequest{
		SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123", NewOwnerCharacterId: "char-heir",
	})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	_, err := h.WebTransferOwnership(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, sc.transferOwnershipReq)
	assert.Equal(t, "tok-abc", sc.transferOwnershipReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.transferOwnershipReq.GetCharacterId())
	assert.Equal(t, "scene-123", sc.transferOwnershipReq.GetSceneId())
	assert.Equal(t, "char-heir", sc.transferOwnershipReq.GetNewOwnerCharacterId())
}

func TestWebTransferOwnershipPassesStatusErrorThroughAsIs(t *testing.T) {
	sc := &mockSceneAccessClient{transferOwnershipErr: status.Error(codes.PermissionDenied, "not the owner")}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebTransferOwnershipRequest{SessionId: "s", CharacterId: "c", SceneId: "x", NewOwnerCharacterId: "h"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebTransferOwnership(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// --- WebLeaveScene ---

func TestWebLeaveSceneForwardsTokenAndFieldsToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebLeaveSceneRequest{
		SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123",
	})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	_, err := h.WebLeaveScene(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, sc.leaveSceneReq)
	assert.Equal(t, "tok-abc", sc.leaveSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.leaveSceneReq.GetCharacterId())
	assert.Equal(t, "scene-123", sc.leaveSceneReq.GetSceneId())
}

func TestWebLeaveScenePassesStatusErrorThroughAsIs(t *testing.T) {
	sc := &mockSceneAccessClient{leaveSceneErr: status.Error(codes.PermissionDenied, "not a member")}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebLeaveSceneRequest{SessionId: "s", CharacterId: "c", SceneId: "x"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebLeaveScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// --- WebUpdateScene ---

func TestWebUpdateSceneForwardsTokenFieldsAndMaskToFacade(t *testing.T) {
	sc := &mockSceneAccessClient{}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

	req := connect.NewRequest(&webv1.WebUpdateSceneRequest{
		SessionId: "sess-1", CharacterId: "char-1", SceneId: "scene-123",
		Visibility: "private", UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"visibility"}},
	})
	req.Header().Set(headerInjectSessionToken, "tok-abc")

	resp, err := h.WebUpdateScene(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "private", resp.Msg.GetScene().GetVisibility())
	require.NotNil(t, sc.updateSceneReq)
	assert.Equal(t, "tok-abc", sc.updateSceneReq.GetPlayerSessionToken())
	assert.Equal(t, "char-1", sc.updateSceneReq.GetCharacterId())
	assert.Equal(t, "scene-123", sc.updateSceneReq.GetSceneId())
	assert.Equal(t, []string{"visibility"}, sc.updateSceneReq.GetUpdateMask().GetPaths())
}

func TestWebUpdateScenePassesStatusErrorThroughAsIs(t *testing.T) {
	sc := &mockSceneAccessClient{updateSceneErr: status.Error(codes.PermissionDenied, "not permitted to update this scene")}
	h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))
	req := connect.NewRequest(&webv1.WebUpdateSceneRequest{SessionId: "s", CharacterId: "c", SceneId: "x"})
	req.Header().Set(headerInjectSessionToken, "tok")

	_, err := h.WebUpdateScene(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestWebUpdateSceneReturnsUnimplementedWhenClientAbsent(t *testing.T) {
	h := NewHandler(&mockCoreClient{})
	_, err := h.WebUpdateScene(context.Background(),
		connect.NewRequest(&webv1.WebUpdateSceneRequest{SessionId: "s"}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))
}
