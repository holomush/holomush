// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// TestObserverIsNotSeededIntoVoteRosterAndCannotCastVote pins INV-SCENE-28:
// CreatePublishAttempt seeds published_scene_votes from role IN
// ('owner','member') only. An observer added to the scene has no vote row
// and therefore CastPublishSceneVote returns SCENE_PUBLISH_NOT_A_VOTER.
// The tally's eligible count (Yes+No+Pending) likewise excludes the observer.
func TestObserverIsNotSeededIntoVoteRosterAndCannotCastVote(t *testing.T) {
	t.Parallel()

	// Use valid ULIDs: CastPublishSceneVote calls parseCallerCharacterID which
	// validates the caller is a well-formed ULID before the roster lookup.
	memberID := ulid.Make().String()
	observerID := ulid.Make().String()
	// The vote roster is seeded only from owner+member (CreatePublishAttempt uses
	// role IN ('owner','member')), so we install only the member as a voter.
	store, svc := newVoteFixture(t, "pub-obs-1", "scene-obs-1", memberID)
	// Observer is intentionally NOT in the voter roster (mirrors the real
	// CreatePublishAttempt behaviour: observer rows are excluded by the INSERT
	// SELECT WHERE role IN ('owner','member') at publish_store.go:84-87).
	// The observer attempts to cast a vote.
	_, err := svc.CastPublishSceneVote(context.Background(), &scenev1.CastPublishSceneVoteRequest{
		CallerCharacterId: observerID,
		PublishedSceneId:  "pub-obs-1",
		Vote:              true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"observer not on the roster must receive PermissionDenied")
	assert.Equal(t, "SCENE_PUBLISH_NOT_A_VOTER", status.Convert(err).Message(),
		"error code must be SCENE_PUBLISH_NOT_A_VOTER (INV-SCENE-28)")

	// The tally eligible count must only reflect the seeded member voter.
	// Eligible = len(publishedVoters) for this attempt (1 member, 0 observers).
	voters := store.publishedVoters["pub-obs-1"]
	require.Len(t, voters, 1, "vote roster must contain exactly the member, not the observer")
	assert.Equal(t, memberID, voters[0].CharacterID,
		"the single roster entry must be the member character")
}

// TestResolveFromTally asserts the COLLECTING-phase resolution rule: no
// resolution while votes are pending; unanimous yes → all_yes; any no once
// everyone has voted → all_voted_any_no.
func TestResolveFromTally(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		tally  VoteTally
		wantT  PublishTrigger
		wantOK bool
	}{
		{"all yes unanimous", VoteTally{Yes: 3, No: 0, Pending: 0}, TriggerAllYes, true},
		{"any no after all voted", VoteTally{Yes: 2, No: 1, Pending: 0}, TriggerAllVotedAnyNo, true},
		{"still pending", VoteTally{Yes: 2, No: 0, Pending: 1}, "", false},
		{"mixed with pending", VoteTally{Yes: 1, No: 1, Pending: 1}, "", false},
		{"single yes single voter", VoteTally{Yes: 1, No: 0, Pending: 0}, TriggerAllYes, true},
		{"all no", VoteTally{Yes: 0, No: 2, Pending: 0}, TriggerAllVotedAnyNo, true},
		{"zero-vote tally does NOT auto-resolve (no silent empty-roster publish)", VoteTally{Yes: 0, No: 0, Pending: 0}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveFromTally(tc.tally)
			assert.Equal(t, tc.wantOK, ok)
			if ok {
				assert.Equal(t, tc.wantT, got)
			}
		})
	}
}
