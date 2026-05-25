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
// calls. The INV-P6-5 tripwire asserts the count stays at 0 when the
// participant gate denies a non-participant — i.e. content is NEVER read on
// the deny path.
type contentTripwireStore struct {
	*fakeStore
	contentReadCalls atomic.Int32
}

func (s *contentTripwireStore) GetPublishedSceneContent(_ context.Context, _ string) ([]PublishedSceneEntry, error) {
	s.contentReadCalls.Add(1)
	return nil, nil
}

// TestGetPublishedSceneDeniesNonParticipantWithoutReadingContent is the
// load-bearing INV-S9 / INV-P6-5 tripwire: a non-participant is denied with
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
	svc := NewSceneServiceImpl(store)

	_, err := svc.GetPublishedScene(context.Background(), &scenev1.GetPublishedSceneRequest{
		CallerCharacterId: outsider,
		PublishedSceneId:  "pub-1",
	})

	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err),
		"non-participant must be denied with PermissionDenied")
	require.Equal(t, "SCENE_PRIVACY_BOUNDARY_BLOCK", status.Convert(err).Message())
	require.Equal(t, int32(0), store.contentReadCalls.Load(),
		"INV-P6-5 violation: content store was read before the participant gate denied")
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
	svc := NewSceneServiceImpl(store)

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
	svc := NewSceneServiceImpl(base)

	_, err := svc.GetPublishedScene(context.Background(), &scenev1.GetPublishedSceneRequest{
		CallerCharacterId: outsider,
		PublishedSceneId:  "pub-3",
	})
	require.Error(t, err)

	out := buf.String()
	assert.Contains(t, out, "scene privacy boundary block", "the §10 triple-signal WARN MUST fire on an INV-S9 denial")
	assert.Contains(t, out, "SCENE_PRIVACY_BOUNDARY_BLOCK")
	assert.Contains(t, out, "not_participant")
	assert.Contains(t, out, "scene-3", "the WARN must record the affected scene")
}

// TestPublicationServiceFileImportsNoABACPolicyPackage enforces INV-P6-6
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
			"INV-P6-6 violation: publish_service.go imports access/ABAC package %q; the participant gate must be plugin-code only", path)
	}
}

// TestPublicationServiceTypeHasNoABACEngineField enforces INV-P6-6 at runtime:
// SceneServiceImpl carries no field whose type names an ABAC policy/engine, so
// the participant-gated handlers physically cannot consult the engine.
func TestPublicationServiceTypeHasNoABACEngineField(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(SceneServiceImpl{})
	for i := range typ.NumField() {
		field := typ.Field(i)
		typeName := strings.ToLower(field.Type.String())
		require.NotContains(t, typeName, "policy",
			"INV-P6-6 violation: SceneServiceImpl.%s carries ABAC policy type %s", field.Name, field.Type.String())
		require.NotContains(t, typeName, "engine",
			"INV-P6-6 violation: SceneServiceImpl.%s carries ABAC engine type %s", field.Name, field.Type.String())
	}
}
