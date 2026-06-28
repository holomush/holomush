// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// TestWebSceneHandlersLogFacadeErrorsWithStructuredOopsFields verifies that every
// scene BFF handler logs facade-RPC failures via errutil.LogErrorContext (not a
// bare slog.ErrorContext), which extracts the oops error's Code() and Context()
// as TOP-LEVEL structured attributes for Loki/Sentry correlation.
//
// The discriminator is the top-level "code" attr: errutil.oopsAttrs emits it
// flat, whereas slog.ErrorContext(ctx, msg, "error", err) renders the oops
// LogValuer as a nested group under the (non-empty) "error" key, leaving
// "code" absent at top level. Asserting the flat "code" thus pins the
// migration for every handler in the table below.
func TestWebSceneHandlersLogFacadeErrorsWithStructuredOopsFields(t *testing.T) {
	const wantCode = "WEB_SCENE_FACADE_ERR"

	tests := []struct {
		name    string
		wantMsg string
		setErr  func(m *mockSceneAccessClient, err error)
		invoke  func(h *Handler) error
	}{
		{
			name:    "WebListScenes",
			wantMsg: "web: list scenes RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.listScenesErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebListScenes(context.Background(),
					connect.NewRequest(&webv1.WebListScenesRequest{SessionId: "sess-err"}))
				return err
			},
		},
		{
			name:    "WebGetScene",
			wantMsg: "web: get scene RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.getSceneErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebGetScene(context.Background(),
					connect.NewRequest(&webv1.WebGetSceneRequest{SessionId: "sess-err", SceneId: "sc-1"}))
				return err
			},
		},
		{
			name:    "WebListMyScenes",
			wantMsg: "web: list my scenes RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.listMyScenesErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebListMyScenes(context.Background(),
					connect.NewRequest(&webv1.WebListMyScenesRequest{SessionId: "sess-err"}))
				return err
			},
		},
		{
			name:    "WebWatchScene",
			wantMsg: "web: watch scene RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.watchSceneErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebWatchScene(context.Background(),
					connect.NewRequest(&webv1.WebWatchSceneRequest{SessionId: "sess-err", SceneId: "sc-1"}))
				return err
			},
		},
		{
			name:    "WebCreateScene",
			wantMsg: "web: create scene RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.createSceneErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebCreateScene(context.Background(),
					connect.NewRequest(&webv1.WebCreateSceneRequest{SessionId: "sess-err", Title: "t"}))
				return err
			},
		},
		{
			name:    "WebEndScene",
			wantMsg: "web: end scene RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.endSceneErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebEndScene(context.Background(),
					connect.NewRequest(&webv1.WebEndSceneRequest{SessionId: "sess-err", SceneId: "sc-1"}))
				return err
			},
		},
		{
			name:    "WebPauseScene",
			wantMsg: "web: pause scene RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.pauseSceneErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebPauseScene(context.Background(),
					connect.NewRequest(&webv1.WebPauseSceneRequest{SessionId: "sess-err", SceneId: "sc-1"}))
				return err
			},
		},
		{
			name:    "WebResumeScene",
			wantMsg: "web: resume scene RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.resumeSceneErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebResumeScene(context.Background(),
					connect.NewRequest(&webv1.WebResumeSceneRequest{SessionId: "sess-err", SceneId: "sc-1"}))
				return err
			},
		},
		{
			name:    "WebExportScene",
			wantMsg: "web: export scene RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.exportSceneErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebExportScene(context.Background(),
					connect.NewRequest(&webv1.WebExportSceneRequest{SessionId: "sess-err", SceneId: "sc-1"}))
				return err
			},
		},
		{
			name:    "WebSetSceneFocus",
			wantMsg: "web: set scene focus RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.setFocusErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebSetSceneFocus(context.Background(),
					connect.NewRequest(&webv1.WebSetSceneFocusRequest{SessionId: "sess-err", ConnectionId: "conn-1"}))
				return err
			},
		},
		{
			name:    "WebListPublishedScenes",
			wantMsg: "web: list published scenes RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.listPublishedErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebListPublishedScenes(context.Background(),
					connect.NewRequest(&webv1.WebListPublishedScenesRequest{SessionId: "sess-err"}))
				return err
			},
		},
		{
			name:    "WebGetPublicSceneArchive",
			wantMsg: "web: get public scene archive RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.getArchiveErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebGetPublicSceneArchive(context.Background(),
					connect.NewRequest(&webv1.WebGetPublicSceneArchiveRequest{SessionId: "sess-err", PublishedSceneId: "pub-1"}))
				return err
			},
		},
		{
			name:    "WebDownloadPublicSceneArchive",
			wantMsg: "web: download public scene archive RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.downloadArchiveErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebDownloadPublicSceneArchive(context.Background(),
					connect.NewRequest(&webv1.WebDownloadPublicSceneArchiveRequest{SessionId: "sess-err", PublishedSceneId: "pub-1"}))
				return err
			},
		},
		{
			name:    "WebStartScenePublish",
			wantMsg: "web: start scene publish RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.startScenePublishErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebStartScenePublish(context.Background(),
					connect.NewRequest(&webv1.WebStartScenePublishRequest{SessionId: "sess-err", SceneId: "sc-1"}))
				return err
			},
		},
		{
			name:    "WebCastPublishSceneVote",
			wantMsg: "web: cast publish vote RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.castPublishSceneVoteErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebCastPublishSceneVote(context.Background(),
					connect.NewRequest(&webv1.WebCastPublishSceneVoteRequest{SessionId: "sess-err", PublishedSceneId: "pub-1"}))
				return err
			},
		},
		{
			name:    "WebWithdrawScenePublish",
			wantMsg: "web: withdraw scene publish RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.withdrawScenePublishErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebWithdrawScenePublish(context.Background(),
					connect.NewRequest(&webv1.WebWithdrawScenePublishRequest{SessionId: "sess-err", PublishedSceneId: "pub-1"}))
				return err
			},
		},
		{
			name:    "WebGetPublishedScene",
			wantMsg: "web: get published scene RPC failed",
			setErr:  func(m *mockSceneAccessClient, err error) { m.getPublishedSceneErr = err },
			invoke: func(h *Handler) error {
				_, err := h.WebGetPublishedScene(context.Background(),
					connect.NewRequest(&webv1.WebGetPublishedSceneRequest{SessionId: "sess-err", PublishedSceneId: "pub-1"}))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" logs the facade error with a top-level oops code", func(t *testing.T) {
			facadeErr := oops.Code(wantCode).Errorf("facade unreachable")
			sc := &mockSceneAccessClient{}
			tt.setErr(sc, facadeErr)
			h := NewHandler(&mockCoreClient{}, WithSceneAccessClient(sc))

			// Capture the package-global logger: the BFF handlers log via
			// slog.Default(). Info-level JSON handler suppresses the entry-DebugContext
			// line, leaving exactly the one ERROR record.
			orig := slog.Default()
			var buf bytes.Buffer
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(orig) })

			err := tt.invoke(h)
			require.Error(t, err)

			var entry map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &entry),
				"handler must emit exactly one structured JSON log line; got: %q", buf.String())

			assert.Equal(t, "ERROR", entry["level"])
			assert.Equal(t, tt.wantMsg, entry["msg"])
			assert.Equal(t, wantCode, entry["code"],
				"oops code must be a top-level attr (errutil.LogErrorContext), not nested under error")
			assert.Equal(t, "sess-err", entry["session_id"],
				"handler's structured attrs must survive the migration")
		})
	}
}
