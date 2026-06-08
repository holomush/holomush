// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// recordingExportDecryptor is a recording snapshotDecryptor fake for the
// export unit suite. It records per-call batch sizes (so the non-participant
// arm can assert the decrypt seam was never touched) and resolves each row's
// plaintext by row ID, echoing the integration suite's fakeSnapshotDecryptor
// outcome shapes. It also records every AuditRow.Subject it receives so tests
// can assert the AAD-critical subject is propagated correctly (Blocker 1).
type recordingExportDecryptor struct {
	// calls records the per-call batch sizes.
	calls []int
	// plaintextByID maps a row's string(Id) to the plaintext to return.
	plaintextByID map[string][]byte
	// subjects records every Subject field seen across all DecryptOwnAuditRows calls.
	subjects []string
}

func (f *recordingExportDecryptor) DecryptOwnAuditRows(_ context.Context, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error) {
	f.calls = append(f.calls, len(rows))
	out := make([]*pluginv1.RowResult, len(rows))
	for i, r := range rows {
		f.subjects = append(f.subjects, r.GetSubject())
		out[i] = &pluginv1.RowResult{
			Id:      r.GetId(),
			Outcome: &pluginv1.RowResult_Plaintext{Plaintext: f.plaintextByID[string(r.GetId())]},
		}
	}
	return out, nil
}

// newExportFixture seeds a scene with an owner and returns the store, service,
// decryptor, and the generated owner/scene IDs. Tests adjust the roster and
// log rows per arm.
func newExportFixture(t *testing.T) (*fakeStore, *SceneServiceImpl, *recordingExportDecryptor, string, string) {
	t.Helper()
	store := newFakeStore()
	ownerID := ulid.Make().String()
	sceneID := ulid.Make().String()
	store.scenes[sceneID] = &SceneRow{
		ID:      sceneID,
		Title:   "Tea at the Manor",
		OwnerID: ownerID,
		State:   string(SceneStateActive),
	}
	store.installRoster(sceneID, ownerID)
	dec := &recordingExportDecryptor{plaintextByID: map[string][]byte{}}
	svc := newTestService(t, store)
	svc.SetSnapshotDecryptor(dec)
	return store, svc, dec, ownerID, sceneID
}

// installExportLogRow appends an IC log row and registers its plaintext with
// the decryptor fake.
func installExportLogRow(store *fakeStore, dec *recordingExportDecryptor, id, eventType string, plaintext []byte) {
	store.exportLogRows = append(store.exportLogRows, LogRow{
		ID:    []byte(id),
		Type:  eventType,
		Codec: "identity",
	})
	dec.plaintextByID[id] = plaintext
}

func TestExportSceneLogDeniesNonParticipantBeforeTouchingDecryptSeam(t *testing.T) {
	t.Parallel()
	store, svc, dec, _, sceneID := newExportFixture(t)
	installExportLogRow(store, dec, "row-1", "core-scenes:scene_pose",
		[]byte(`{"actor_id":"someone","text":"waves"}`))
	outsiderID := ulid.Make().String()

	_, err := svc.ExportSceneLog(context.Background(), &scenev1.ExportSceneLogRequest{
		CharacterId: outsiderID,
		SceneId:     sceneID,
		Format:      "markdown",
	})

	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err),
		"non-participant must be denied with PermissionDenied")
	require.Equal(t, "SCENE_EXPORT_NOT_PARTICIPANT", status.Convert(err).Message())
	require.Empty(t, dec.calls,
		"the decrypt seam must never be invoked for a non-participant")
}

func TestExportSceneLogAllowsObserverParticipant(t *testing.T) {
	t.Parallel()
	store, svc, dec, _, sceneID := newExportFixture(t)
	observerID := ulid.Make().String()
	// A REAL observer participant row — role "observer", not owner/member.
	store.participants[sceneID][observerID] = "observer"
	installExportLogRow(store, dec, "row-1", "core-scenes:scene_pose",
		[]byte(`{"actor_id":"Aria","text":"pours the tea."}`))

	resp, err := svc.ExportSceneLog(context.Background(), &scenev1.ExportSceneLogRequest{
		CharacterId: observerID,
		SceneId:     sceneID,
		Format:      "markdown",
	})

	require.NoError(t, err, "an observer participant row (any role) passes the export gate")
	assert.Contains(t, string(resp.GetContent()), "pours the tea.")
}

func TestExportSceneLogRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()
	_, svc, dec, ownerID, sceneID := newExportFixture(t)

	_, err := svc.ExportSceneLog(context.Background(), &scenev1.ExportSceneLogRequest{
		CharacterId: ownerID,
		SceneId:     sceneID,
		Format:      "html",
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "SCENE_EXPORT_BAD_FORMAT", status.Convert(err).Message())
	require.Empty(t, dec.calls)
}

func TestExportSceneLogRendersJSONLLineForEachDecryptedRow(t *testing.T) {
	t.Parallel()
	store, svc, dec, ownerID, sceneID := newExportFixture(t)
	installExportLogRow(store, dec, "row-1", "core-scenes:scene_pose",
		[]byte(`{"actor_id":"Aria","text":"pours the tea."}`))
	installExportLogRow(store, dec, "row-2", "core-scenes:scene_say",
		[]byte(`{"actor_id":"Bex","text":"Lovely."}`))

	resp, err := svc.ExportSceneLog(context.Background(), &scenev1.ExportSceneLogRequest{
		CharacterId: ownerID,
		SceneId:     sceneID,
		Format:      "jsonl",
	})

	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(resp.GetContent()), "\n"), "\n")
	require.Len(t, lines, 2, "two decrypted rows render as two JSONL lines")
	assert.JSONEq(t, `{"speaker":"Aria","kind":"pose","content":"pours the tea."}`, lines[0])
	assert.JSONEq(t, `{"speaker":"Bex","kind":"say","content":"Lovely."}`, lines[1])
	assert.Equal(t, "application/jsonl", resp.GetMimeType())
	assert.Equal(t, "tea-at-the-manor.jsonl", resp.GetFilename())
}

func TestExportSceneLogRendersMarkdownWithSlugifiedFilename(t *testing.T) {
	t.Parallel()
	store, svc, dec, ownerID, sceneID := newExportFixture(t)
	installExportLogRow(store, dec, "row-1", "core-scenes:scene_pose",
		[]byte(`{"actor_id":"Aria","text":"pours the tea."}`))
	installExportLogRow(store, dec, "row-2", "core-scenes:scene_emit",
		[]byte(`{"actor_id":"","text":"The kettle whistles."}`))

	resp, err := svc.ExportSceneLog(context.Background(), &scenev1.ExportSceneLogRequest{
		CharacterId: ownerID,
		SceneId:     sceneID,
		Format:      "markdown",
	})

	require.NoError(t, err)
	content := string(resp.GetContent())
	assert.Contains(t, content, "**Aria** pours the tea.")
	assert.Contains(t, content, "_The kettle whistles._")
	assert.Equal(t, "text/markdown", resp.GetMimeType())
	assert.Equal(t, "tea-at-the-manor.md", resp.GetFilename())
}

func TestExportSceneLogReturnsSentinelDocumentForEmptyLog(t *testing.T) {
	t.Parallel()
	_, svc, dec, ownerID, sceneID := newExportFixture(t)

	resp, err := svc.ExportSceneLog(context.Background(), &scenev1.ExportSceneLogRequest{
		CharacterId: ownerID,
		SceneId:     sceneID,
		Format:      "markdown",
	})

	// Empty-log precedent follows the renderers' empty-entry contracts
	// (renderMarkdown's sentinel line / renderJSONL's zero records), matching
	// DownloadPublishedScene: an empty scene is a document, not an error.
	require.NoError(t, err)
	assert.Equal(t, "_No content was recorded for this scene._\n", string(resp.GetContent()))
	assert.Empty(t, dec.calls, "no rows means the decrypt seam is never invoked")
}

// TestExportSceneLogPropagatesFullICSubjectToDecryptSeam pins the AAD-critical
// subject propagation that the empty-subject bug (Blocker 1) masked. Every
// AuditRow passed to DecryptOwnAuditRows MUST carry the full dot-style IC
// subject ("events.<gameID>.scene.<sceneID>.ic") — both OwnerMap.Resolve and
// the AEAD tag-check bind to this exact string.
func TestExportSceneLogPropagatesFullICSubjectToDecryptSeam(t *testing.T) {
	t.Parallel()
	store, svc, dec, ownerID, sceneID := newExportFixture(t)
	installExportLogRow(store, dec, "row-1", "core-scenes:scene_pose",
		[]byte(`{"actor_id":"Aria","text":"waves."}`))
	installExportLogRow(store, dec, "row-2", "core-scenes:scene_say",
		[]byte(`{"actor_id":"Bex","text":"Hello."}`))

	// newTestService uses NewSceneServiceImpl which sets gameID = "main".
	wantSubject := "events.main.scene." + sceneID + ".ic"

	_, err := svc.ExportSceneLog(context.Background(), &scenev1.ExportSceneLogRequest{
		CharacterId: ownerID,
		SceneId:     sceneID,
		Format:      "markdown",
	})
	require.NoError(t, err)

	// Every row passed to the decrypt seam must carry the full IC subject.
	require.NotEmpty(t, dec.subjects, "decrypt seam must have been invoked")
	for i, got := range dec.subjects {
		assert.Equal(t, wantSubject, got,
			"AuditRow[%d].Subject must be the full IC subject for AAD/DEK lookup", i)
	}
	// Also assert the store received the same subject for the SQL query.
	assert.Equal(t, wantSubject, store.exportLogSubject,
		"ReadSceneLogForExport must receive the full IC subject (not a bare sceneID)")
}
