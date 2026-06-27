// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// fakeSettingsClient is a test double for pluginsdk.SettingsClient that lets
// tests drive GetSetting outcomes without a real host connection.
type fakeSettingsClient struct {
	values []string
	found  bool
	err    error
}

func (f *fakeSettingsClient) GetSetting(_ context.Context, _ pluginsdk.SettingScope, _, _ string) ([]string, bool, error) {
	return f.values, f.found, f.err
}

func (f *fakeSettingsClient) SetSetting(_ context.Context, _ pluginsdk.SettingScope, _, _ string, _ []string) error {
	return nil
}

// ── effectiveTaxonomy ────────────────────────────────────────────────────────

// TestEffectiveTaxonomyFallsBackToDefaultWhenGameUnset asserts that when the
// game-scope setting is not found, effectiveTaxonomy returns DefaultCWTaxonomy.
func TestEffectiveTaxonomyFallsBackToDefaultWhenGameUnset(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newFakeStore())
	svc.settings = &fakeSettingsClient{found: false}

	got := svc.effectiveTaxonomy(context.Background())

	assert.Equal(t, DefaultCWTaxonomy, got)
	assert.Contains(t, got, "violence")
	assert.Contains(t, got, "death")
}

// TestEffectiveTaxonomyFallsBackToDefaultWhenGetSettingErrors asserts that a
// GetSetting error (e.g. missing dispatch token) is silently swallowed and
// DefaultCWTaxonomy is returned.
func TestEffectiveTaxonomyFallsBackToDefaultWhenGetSettingErrors(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newFakeStore())
	svc.settings = &fakeSettingsClient{err: errors.New("no dispatch token")}

	got := svc.effectiveTaxonomy(context.Background())

	assert.Equal(t, DefaultCWTaxonomy, got)
}

// TestEffectiveTaxonomyFallsBackToDefaultWhenSettingsNil asserts that a nil
// settings client (late-wiring / test harness) returns DefaultCWTaxonomy.
func TestEffectiveTaxonomyFallsBackToDefaultWhenSettingsNil(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newFakeStore())
	// settings is nil (zero-value SceneServiceImpl)

	got := svc.effectiveTaxonomy(context.Background())

	assert.Equal(t, DefaultCWTaxonomy, got)
}

// TestEffectiveTaxonomyUsesGameOverrideWhenSet asserts that when the game-scope
// setting is present and non-empty, effectiveTaxonomy returns that custom list.
func TestEffectiveTaxonomyUsesGameOverrideWhenSet(t *testing.T) {
	t.Parallel()
	custom := []string{"custom-a", "custom-b"}
	svc := newTestService(t, newFakeStore())
	svc.settings = &fakeSettingsClient{values: custom, found: true}

	got := svc.effectiveTaxonomy(context.Background())

	assert.Equal(t, custom, got)
}

// TestIsUnexpectedSettingsError pins the classification that gates the WARN log
// on the CW resolution fail-open paths (effectiveTaxonomy, resolveBlockedCW):
// expected auth/not-found/missing-token outcomes are skipped quietly, while
// infrastructure failures are surfaced. The oops-wrapped cases are the
// safety-critical ones — the settings SDK client wraps the wire status in
// oops.With(...).Wrap(err), so the classifier MUST see the gRPC code through
// the wrap (host_service.go maps missing-token/foreign-principal to
// PermissionDenied at the boundary).
func TestIsUnexpectedSettingsError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error is not unexpected", nil, false},
		{"permission denied is expected (missing token / foreign principal)", status.Error(codes.PermissionDenied, "permission denied"), false},
		{"unauthenticated is expected", status.Error(codes.Unauthenticated, "no token"), false},
		{"invalid argument is expected (bad principal / scope)", status.Error(codes.InvalidArgument, "invalid principal_id"), false},
		{"not found is expected", status.Error(codes.NotFound, "missing"), false},
		{"internal is an unexpected infra error", status.Error(codes.Internal, "boom"), true},
		{"unavailable is an unexpected transport error", status.Error(codes.Unavailable, "transport"), true},
		{"plain non-status error is unexpected (e.g. misconfigured client)", errors.New("not configured"), true},
		{"oops-wrapped permission denied stays expected", oops.With("scope", "game").Wrap(status.Error(codes.PermissionDenied, "permission denied")), false},
		{"oops-wrapped internal stays unexpected", oops.With("scope", "game").Wrap(status.Error(codes.Internal, "boom")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isUnexpectedSettingsError(tt.err))
		})
	}
}

// ── CreateScene content_warnings validation ──────────────────────────────────

// TestCreateSceneRejectsUnknownContentWarning asserts that CreateScene returns
// InvalidArgument when a requested content warning is not in the effective taxonomy.
func TestCreateSceneRejectsUnknownContentWarning(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	// nil settings → DefaultCWTaxonomy; "unknown-cw" is not in it

	_, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId:     "char-1",
		Title:           "Test Scene",
		ContentWarnings: []string{"unknown-cw"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "unknown content warning")
}

// TestCreateScenePersistsValidContentWarnings asserts that CreateScene stores
// the validated content warnings on the row and returns them in the response.
// This pins the gap-fix: previously CreateScene hardcoded ContentWarnings: []string{}.
func TestCreateScenePersistsValidContentWarnings(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	// nil settings → DefaultCWTaxonomy; "violence" and "death" are in it

	resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId:     "char-1",
		Title:           "Test Scene",
		ContentWarnings: []string{"violence", "death"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp.GetScene())
	assert.ElementsMatch(t, []string{"violence", "death"}, resp.GetScene().GetContentWarnings(),
		"response MUST carry the persisted content warnings")

	// Verify the row was actually stored with the warnings (gap-fix pin).
	row, storeErr := store.Get(context.Background(), resp.GetScene().GetId())
	require.NoError(t, storeErr)
	assert.ElementsMatch(t, []string{"violence", "death"}, row.ContentWarnings,
		"stored row MUST carry the persisted content warnings, not []string{}")
}

// TestCreateSceneAcceptsEmptyContentWarnings asserts that CreateScene with no
// content warnings succeeds (empty slice is valid).
func TestCreateSceneAcceptsEmptyContentWarnings(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId:     "char-1",
		Title:           "Test Scene",
		ContentWarnings: []string{},
	})

	require.NoError(t, err)
	assert.Empty(t, resp.GetScene().GetContentWarnings())
}

// ── UpdateScene content_warnings validation ──────────────────────────────────

// TestUpdateSceneRejectsUnknownContentWarning asserts that UpdateScene returns
// InvalidArgument when a content warning in the update is not in the effective taxonomy.
func TestUpdateSceneRejectsUnknownContentWarning(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-cw-update",
		OwnerID:    "char-owner",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
		PoseOrder:  string(PoseOrderModeFree),
	}))
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	svc.SetHostEvaluator(allowEvaluator{})
	// nil settings → DefaultCWTaxonomy; "forbidden-cw" is not in it

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		SceneId:         "scene-cw-update",
		CharacterId:     "char-owner",
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"content_warnings"}},
		ContentWarnings: []string{"violence", "forbidden-cw"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "unknown content warning")

	// The rejected update MUST NOT have persisted anything: validation runs
	// before the store write, so the seeded row's (empty) content warnings
	// stay unchanged.
	got, getErr := store.Get(context.Background(), "scene-cw-update")
	require.NoError(t, getErr)
	assert.Empty(t, got.ContentWarnings, "rejected UpdateScene must not write content warnings")
}

// TestUpdateSceneAcceptsValidContentWarnings asserts that UpdateScene succeeds
// when all content warnings are in the effective taxonomy.
func TestUpdateSceneAcceptsValidContentWarnings(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-cw-valid",
		OwnerID:    "char-owner",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
		PoseOrder:  string(PoseOrderModeFree),
	}))
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	svc.SetHostEvaluator(allowEvaluator{})

	resp, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		SceneId:         "scene-cw-valid",
		CharacterId:     "char-owner",
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"content_warnings"}},
		ContentWarnings: []string{"self-harm", "abuse"},
	})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"self-harm", "abuse"}, resp.GetScene().GetContentWarnings())
}

// TestUpdateSceneWithCustomTaxonomyAcceptsCustomWarning asserts that UpdateScene
// accepts a CW that is in a game-override taxonomy but not DefaultCWTaxonomy.
func TestUpdateSceneWithCustomTaxonomyAcceptsCustomWarning(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-custom-cw",
		OwnerID:    "char-owner",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
		PoseOrder:  string(PoseOrderModeFree),
	}))
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	svc.SetHostEvaluator(allowEvaluator{})
	svc.settings = &fakeSettingsClient{values: []string{"custom-tag"}, found: true}

	resp, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		SceneId:         "scene-custom-cw",
		CharacterId:     "char-owner",
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"content_warnings"}},
		ContentWarnings: []string{"custom-tag"},
	})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"custom-tag"}, resp.GetScene().GetContentWarnings())
}
