// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// installAttempt returns a fakeStore setup func that seeds one attempt in the
// given status (scene id derived from the attempt id).
func installAttempt(id string, status PublishedSceneStatus) func(*fakeStore) {
	return func(f *fakeStore) {
		f.installPublishedAttempt(id, "scene-"+id, status)
	}
}

// newFakeStoreWithPublishedScene seeds a PUBLISHED attempt with content.
func newFakeStoreWithPublishedScene(id, sceneID string, entries []PublishedSceneEntry) *fakeStore {
	f := newFakeStore()
	f.installPublishedAttempt(id, sceneID, StatusPublished)
	title := "Published Test Scene"
	f.publishedScenes[id].TitleSnapshot = &title
	f.publishedScenes[id].ParticipantsSnapshot = []string{"Alice", "Bob"}
	f.publishedContent[id] = entries
	return f
}

// TestGetPublicSceneArchiveReturnsOpaqueNotFoundForNonReadableStates pins
// INV-P6-8: a nonexistent id and every non-PUBLISHED status return the SAME
// opaque NOT_FOUND (code + message), so a caller cannot infer that an attempt
// exists or is in progress.
func TestGetPublicSceneArchiveReturnsOpaqueNotFoundForNonReadableStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(*fakeStore)
		argID string
	}{
		{"returns NotFound for a nonexistent id", func(*fakeStore) {}, "missing-id"},
		{"returns NotFound for a COLLECTING attempt", installAttempt("pub-c1", StatusCollecting), "pub-c1"},
		{"returns NotFound for a COOLOFF attempt", installAttempt("pub-c2", StatusCoolOff), "pub-c2"},
		{"returns NotFound for an ATTEMPT_FAILED attempt", installAttempt("pub-c3", StatusAttemptFailed), "pub-c3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore()
			tc.setup(store)
			svc := NewSceneServiceImpl(store)

			_, err := svc.GetPublicSceneArchive(context.Background(), &scenev1.GetPublicSceneArchiveRequest{
				PublishedSceneId: tc.argID,
			})

			require.Error(t, err)
			assert.Equal(t, codes.NotFound, status.Code(err),
				"INV-P6-8: opaque NOT_FOUND for all non-PUBLISHED states")
			assert.Equal(t, "scene archive not found", status.Convert(err).Message(),
				"INV-P6-8: identical wire message across non-PUBLISHED states")
		})
	}
}

// TestGetPublicSceneArchiveReturnsContentForPublishedScene covers the only
// readable case: a PUBLISHED attempt returns its public artifact.
func TestGetPublicSceneArchiveReturnsContentForPublishedScene(t *testing.T) {
	t.Parallel()
	store := newFakeStoreWithPublishedScene("pub-pub", "scene-pub", []PublishedSceneEntry{
		{Speaker: "Alice", Kind: EntryKindSay, Content: "Hello."},
	})
	svc := NewSceneServiceImpl(store)

	resp, err := svc.GetPublicSceneArchive(context.Background(), &scenev1.GetPublicSceneArchiveRequest{
		PublishedSceneId: "pub-pub",
	})

	require.NoError(t, err)
	assert.Equal(t, "pub-pub", resp.GetId())
	assert.Equal(t, "Published Test Scene", resp.GetTitleSnapshot())
	assert.Equal(t, []string{"Alice", "Bob"}, resp.GetParticipantsSnapshot())
	require.Len(t, resp.GetContentEntries(), 1)
	assert.Equal(t, "Alice", resp.GetContentEntries()[0].GetSpeaker())
	assert.Equal(t, "say", resp.GetContentEntries()[0].GetKind())
}
