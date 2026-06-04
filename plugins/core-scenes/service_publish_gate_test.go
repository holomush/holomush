// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// contentTripwireStore embeds *fakeStore and counts GetPublishedSceneContent
// calls. The INV-SCENE-32 tripwire asserts the count stays at 0 when the
// participant gate denies a non-participant — i.e. content is NEVER read on
// the deny path.
type contentTripwireStore struct {
	*fakeStore
	contentReadCalls atomic.Int32
}

func (s *contentTripwireStore) GetPublishedSceneContent(ctx context.Context, id string) ([]PublishedSceneEntry, error) {
	s.contentReadCalls.Add(1)
	// Delegate to the embedded fake so seeded content flows through (the
	// counter still records the read for the INV-SCENE-32 gate-ordering tests).
	return s.fakeStore.GetPublishedSceneContent(ctx, id)
}

// TestGetPublishedSceneDeniesNonParticipantWithoutReadingContent is the
// load-bearing INV-SCENE-60 / INV-SCENE-32 tripwire: a non-participant is denied with
// SCENE_PRIVACY_BOUNDARY_BLOCK (PermissionDenied) BEFORE any content read.
func TestGetPublishedSceneDeniesNonParticipantWithoutReadingContent(t *testing.T) {
	t.Parallel()
	base := newFakeStore()
	owner := ulid.Make().String()
	member := ulid.Make().String()
	outsider := ulid.Make().String() // valid ULID, NOT on the roster
	base.installPublishedAttempt("pub-1", "scene-1", StatusPublished)
	base.installRoster("scene-1", owner, member)

	store := &contentTripwireStore{fakeStore: base}
	svc := newTestService(t, store)

	_, err := svc.GetPublishedScene(context.Background(), &scenev1.GetPublishedSceneRequest{
		CallerCharacterId: outsider,
		PublishedSceneId:  "pub-1",
	})

	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err),
		"non-participant must be denied with PermissionDenied")
	require.Equal(t, "SCENE_PRIVACY_BOUNDARY_BLOCK", status.Convert(err).Message())
	require.Equal(t, int32(0), store.contentReadCalls.Load(),
		"INV-SCENE-32 violation: content store was read before the participant gate denied")
}

// TestGetPublishedSceneAllowsParticipantToReadContent is the allow-path
// complement: a participant on a PUBLISHED attempt reads content exactly once.
func TestGetPublishedSceneAllowsParticipantToReadContent(t *testing.T) {
	t.Parallel()
	base := newFakeStore()
	owner := ulid.Make().String()
	base.installPublishedAttempt("pub-2", "scene-2", StatusPublished)
	base.installRoster("scene-2", owner)

	store := &contentTripwireStore{fakeStore: base}
	svc := newTestService(t, store)

	resp, err := svc.GetPublishedScene(context.Background(), &scenev1.GetPublishedSceneRequest{
		CallerCharacterId: owner,
		PublishedSceneId:  "pub-2",
	})

	require.NoError(t, err)
	require.Equal(t, "pub-2", resp.GetId())
	require.Equal(t, int32(1), store.contentReadCalls.Load(),
		"a participant on a PUBLISHED attempt must trigger exactly one content read")
}

// TestGetPublishedSceneDenialEmitsPrivacyBoundaryWarn locks the §10
// triple-signal's forensic record: a non-participant denial MUST emit the
// WARN log (the only durable trace of a boundary-block attempt — there is no
// IC event by design). NOT parallel: it swaps the global slog default to
// capture output. (The metric leg is a no-op stub until the binary-plugin
// metrics pipeline lands in D6; the span leg needs an OTel recorder harness —
// both deferred. The WARN is the observable security signal today.)
func TestGetPublishedSceneDenialEmitsPrivacyBoundaryWarn(t *testing.T) {
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	base := newFakeStore()
	owner := ulid.Make().String()
	outsider := ulid.Make().String()
	base.installPublishedAttempt("pub-3", "scene-3", StatusPublished)
	base.installRoster("scene-3", owner)
	svc := newTestService(t, base)

	_, err := svc.GetPublishedScene(context.Background(), &scenev1.GetPublishedSceneRequest{
		CallerCharacterId: outsider,
		PublishedSceneId:  "pub-3",
	})
	require.Error(t, err)

	out := buf.String()
	assert.Contains(t, out, "scene privacy boundary block", "the §10 triple-signal WARN MUST fire on an INV-SCENE-60 denial")
	assert.Contains(t, out, "SCENE_PRIVACY_BOUNDARY_BLOCK")
	assert.Contains(t, out, "not_participant")
	assert.Contains(t, out, "scene-3", "the WARN must record the affected scene")
}

// TestPublicationServiceFileImportsNoABACPolicyPackage enforces INV-SCENE-33
// structurally: the participant-gated publication handler file must not import
// ANY host access/ABAC package — the gate is plugin-code, never the engine.
func TestPublicationServiceFileImportsNoABACPolicyPackage(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("publish_service.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "publish_service.go", src, parser.ImportsOnly)
	require.NoError(t, err)
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		require.NotContains(t, path, "internal/access",
			"INV-SCENE-33 violation: publish_service.go imports access/ABAC package %q; the participant gate must be plugin-code only", path)
	}
}

// TestPublicationServiceTypeHasNoABACEngineField enforces INV-SCENE-33 at runtime:
// SceneServiceImpl carries no field whose type names an ABAC policy/engine, so
// the participant-gated handlers physically cannot consult the engine.
func TestPublicationServiceTypeHasNoABACEngineField(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(SceneServiceImpl{})
	for i := range typ.NumField() {
		field := typ.Field(i)
		typeName := strings.ToLower(field.Type.String())
		require.NotContains(t, typeName, "policy",
			"INV-SCENE-33 violation: SceneServiceImpl.%s carries ABAC policy type %s", field.Name, field.Type.String())
		require.NotContains(t, typeName, "engine",
			"INV-SCENE-33 violation: SceneServiceImpl.%s carries ABAC engine type %s", field.Name, field.Type.String())
	}
}

// TestDownloadPublishedSceneDeniesNonParticipantWithoutReadingContent applies
// the B5 INV-SCENE-32 tripwire to the download path: a non-participant is denied
// before any content read.
func TestDownloadPublishedSceneDeniesNonParticipantWithoutReadingContent(t *testing.T) {
	t.Parallel()
	base := newFakeStore()
	owner := ulid.Make().String()
	outsider := ulid.Make().String()
	base.installPublishedAttempt("pub-d1", "scene-d1", StatusPublished)
	base.installRoster("scene-d1", owner)
	store := &contentTripwireStore{fakeStore: base}
	svc := newTestService(t, store)

	_, err := svc.DownloadPublishedScene(context.Background(), &scenev1.DownloadPublishedSceneRequest{
		CallerCharacterId: outsider,
		PublishedSceneId:  "pub-d1",
		Format:            "markdown",
	})

	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Equal(t, "SCENE_PRIVACY_BOUNDARY_BLOCK", status.Convert(err).Message())
	require.Equal(t, int32(0), store.contentReadCalls.Load(),
		"INV-SCENE-32 violation: content read before the participant gate denied")
}

// TestDownloadPublishedSceneRendersForParticipant covers the allow path: a
// participant on a PUBLISHED attempt gets non-empty rendered bytes + the
// format's MIME type, after exactly one content read.
func TestDownloadPublishedSceneRendersForParticipant(t *testing.T) {
	t.Parallel()
	base := newFakeStore()
	owner := ulid.Make().String()
	base.installPublishedAttempt("pub-d2", "scene-d2", StatusPublished)
	base.publishedContent["pub-d2"] = []PublishedSceneEntry{
		{Speaker: "Alice", Kind: EntryKindSay, Content: "Hello."},
	}
	base.installRoster("scene-d2", owner)
	store := &contentTripwireStore{fakeStore: base}
	svc := newTestService(t, store)

	resp, err := svc.DownloadPublishedScene(context.Background(), &scenev1.DownloadPublishedSceneRequest{
		CallerCharacterId: owner,
		PublishedSceneId:  "pub-d2",
		Format:            "jsonl",
	})

	require.NoError(t, err)
	require.Equal(t, `{"speaker":"Alice","kind":"say","content":"Hello."}`+"\n", string(resp.GetContent()),
		"jsonl download renders the seeded content (was a placeholder before C5 wired the renderer)")
	require.Equal(t, "application/jsonl", resp.GetMimeType())
	require.Equal(t, int32(1), store.contentReadCalls.Load())
}

// TestDownloadPublishedSceneRejectsUnsupportedFormat: an unknown format is an
// InvalidArgument client error, rejected before any content read.
func TestDownloadPublishedSceneRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()
	base := newFakeStore()
	owner := ulid.Make().String()
	base.installPublishedAttempt("pub-d3", "scene-d3", StatusPublished)
	base.installRoster("scene-d3", owner)
	store := &contentTripwireStore{fakeStore: base}
	svc := newTestService(t, store)

	_, err := svc.DownloadPublishedScene(context.Background(), &scenev1.DownloadPublishedSceneRequest{
		CallerCharacterId: owner,
		PublishedSceneId:  "pub-d3",
		Format:            "pdf",
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "SCENE_PUBLISH_FORMAT_UNSUPPORTED", status.Convert(err).Message())
	require.Equal(t, int32(0), store.contentReadCalls.Load(),
		"format is validated before any content read")
}

// TestDownloadPublishedSceneRejectsNonPublishedAttempt: only PUBLISHED
// attempts are downloadable; a COLLECTING attempt yields INVALID_STATE.
func TestDownloadPublishedSceneRejectsNonPublishedAttempt(t *testing.T) {
	t.Parallel()
	base := newFakeStore()
	owner := ulid.Make().String()
	base.installPublishedAttempt("pub-d4", "scene-d4", StatusCollecting)
	base.installRoster("scene-d4", owner)
	store := &contentTripwireStore{fakeStore: base}
	svc := newTestService(t, store)

	_, err := svc.DownloadPublishedScene(context.Background(), &scenev1.DownloadPublishedSceneRequest{
		CallerCharacterId: owner,
		PublishedSceneId:  "pub-d4",
		Format:            "markdown",
	})

	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "SCENE_PUBLISH_INVALID_STATE", status.Convert(err).Message())
	require.Equal(t, int32(0), store.contentReadCalls.Load(),
		"a non-published attempt must not reach the content read")
}

// TestListScenePublishAttemptsDeniesNonParticipant is the INV-SCENE-60 gate on the
// audit list: a non-participant cannot enumerate a scene's publish attempts
// (the list itself is participant-only, even though summaries carry no content).
func TestListScenePublishAttemptsDeniesNonParticipant(t *testing.T) {
	t.Parallel()
	base := newFakeStore()
	owner := ulid.Make().String()
	outsider := ulid.Make().String()
	base.installPublishedAttempt("pub-l1", "scene-l1", StatusPublished)
	base.installRoster("scene-l1", owner)
	svc := newTestService(t, base)

	_, err := svc.ListScenePublishAttempts(context.Background(), &scenev1.ListScenePublishAttemptsRequest{
		CallerCharacterId: outsider,
		SceneId:           "scene-l1",
	})

	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err),
		"a non-participant must not be able to enumerate publish attempts")
	require.Equal(t, "SCENE_PRIVACY_BOUNDARY_BLOCK", status.Convert(err).Message())
}

// TestListScenePublishAttemptsReturnsSummariesForParticipant is the allow path:
// a participant receives every attempt as a content-free summary, ordered by
// attempt_number, with failure_reason populated only on a failed attempt.
func TestListScenePublishAttemptsReturnsSummariesForParticipant(t *testing.T) {
	t.Parallel()
	base := newFakeStore()
	owner := ulid.Make().String()
	base.installPublishedAttempt("pub-l2a", "scene-l2", StatusAttemptFailed)
	base.installPublishedAttempt("pub-l2b", "scene-l2", StatusPublished)
	base.installRoster("scene-l2", owner)
	// Distinguish the two attempts: #1 failed (ANY_NO), #2 published.
	reason := FailureAnyNo
	base.publishedScenes["pub-l2a"].AttemptNumber = 1
	base.publishedScenes["pub-l2a"].FailureReason = &reason
	base.publishedScenes["pub-l2b"].AttemptNumber = 2

	svc := newTestService(t, base)
	resp, err := svc.ListScenePublishAttempts(context.Background(), &scenev1.ListScenePublishAttemptsRequest{
		CallerCharacterId: owner,
		SceneId:           "scene-l2",
	})

	require.NoError(t, err)
	require.Len(t, resp.GetAttempts(), 2)
	first, second := resp.GetAttempts()[0], resp.GetAttempts()[1]
	assert.Equal(t, int32(1), first.GetAttemptNumber())
	assert.Equal(t, string(StatusAttemptFailed), first.GetStatus())
	assert.Equal(t, string(FailureAnyNo), first.GetFailureReason())
	assert.Equal(t, int32(2), second.GetAttemptNumber())
	assert.Equal(t, string(StatusPublished), second.GetStatus())
	assert.Empty(t, second.GetFailureReason(), "a published attempt has no failure_reason")
}
