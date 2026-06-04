// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/holomush/holomush/internal/pgnanos"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// fakeStore is an in-memory sceneStorer used by service unit tests. It
// supports configurable error injection so tests can exercise the error
// branches of the service layer.
type fakeStore struct {
	scenes                    map[string]*SceneRow
	participants              map[string]map[string]string     // sceneID → characterID → role
	publishedScenes           map[string]*PublishedScene       // Phase 6: published_scene_id → attempt
	publishedContent          map[string][]PublishedSceneEntry // Phase 6: published_scene_id → content entries
	publishedVoters           map[string][]PublishedSceneVote  // Phase 6: published_scene_id → voter rows
	attemptCounts             map[string]AttemptCounts         // Phase 6: sceneID → attempt counts
	maxPublishAttempts        map[string]int                   // Phase 6: sceneID → budget
	createdAttempts           []*PublishedScene                // Phase 6: records CreatePublishAttempt calls
	createErr                 error
	createWithOwnerErr        error
	getErr                    error
	addParticipantErr         error
	listScenesForCharacterErr error
	// ListBoard control fields (iokti.12).
	listBoardRows []*SceneRow
	listBoardErr  error
	listBoardGot  *BoardQuery // records the last query received
}

type recordingEventSink struct {
	intents []pluginsdk.EmitIntent
	err     error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		scenes:             make(map[string]*SceneRow),
		participants:       make(map[string]map[string]string),
		publishedScenes:    make(map[string]*PublishedScene),
		publishedContent:   make(map[string][]PublishedSceneEntry),
		publishedVoters:    make(map[string][]PublishedSceneVote),
		attemptCounts:      make(map[string]AttemptCounts),
		maxPublishAttempts: make(map[string]int),
	}
}

// installRoster seeds a scene's participant roster: ownerID as "owner" and
// each memberID as "member". Used by Phase 6 gate tests.
func (f *fakeStore) installRoster(sceneID, ownerID string, memberIDs ...string) {
	roster := map[string]string{ownerID: "owner"}
	for _, m := range memberIDs {
		roster[m] = "member"
	}
	f.participants[sceneID] = roster
}

// installPublishedAttempt seeds a published_scenes row in the given status.
func (f *fakeStore) installPublishedAttempt(id, sceneID string, status PublishedSceneStatus) {
	f.publishedScenes[id] = &PublishedScene{
		ID:                  id,
		SceneID:             sceneID,
		AttemptNumber:       1,
		Status:              status,
		InitiatedBy:         "system",
		InitiatedAt:         pgnanos.From(time.Now()),
		VoteWindow:          7 * 24 * time.Hour,
		CoolOffWindow:       30 * time.Minute,
		MaxAttemptsSnapshot: 3,
	}
}

// GetPublishedSceneHeader returns the installed attempt (nil, nil when
// absent — mirroring the production not-found contract).
func (f *fakeStore) GetPublishedSceneHeader(_ context.Context, id string) (*PublishedScene, error) {
	return f.publishedScenes[id], nil
}

// GetPublishedSceneContent returns the installed content entries for an
// attempt (nil when none). The INV-SCENE-32 tripwire test overrides this on an
// embedding type to count calls.
func (f *fakeStore) GetPublishedSceneContent(_ context.Context, id string) ([]PublishedSceneEntry, error) {
	return f.publishedContent[id], nil
}

// installVoters seeds the voter roster for a published attempt. Used by
// Phase D event-emitter tests that need emitPublishStarted to return a
// non-empty roster.
func (f *fakeStore) installVoters(publishedSceneID string, characterIDs ...string) {
	voters := make([]PublishedSceneVote, 0, len(characterIDs))
	for _, cid := range characterIDs {
		voters = append(voters, PublishedSceneVote{
			PublishedSceneID: publishedSceneID,
			CharacterID:      cid,
		})
	}
	f.publishedVoters[publishedSceneID] = voters
}

// ListPublishVoters returns the seeded voter rows for an attempt.
func (f *fakeStore) ListPublishVoters(_ context.Context, publishedSceneID string) ([]PublishedSceneVote, error) {
	return f.publishedVoters[publishedSceneID], nil
}

// ListSceneAttempts returns all installed attempts for a scene, ordered by
// attempt_number (mirroring the store's ORDER BY).
func (f *fakeStore) ListSceneAttempts(_ context.Context, sceneID string) ([]PublishedScene, error) {
	var out []PublishedScene
	for _, pub := range f.publishedScenes {
		if pub.SceneID == sceneID {
			out = append(out, *pub)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AttemptNumber < out[j].AttemptNumber })
	return out, nil
}

// TallyVotes counts the seeded roster rows: nil vote → Pending, *true → Yes,
// *false → No. Stateful so the B3 state-machine tests can drive resolution.
func (f *fakeStore) TallyVotes(_ context.Context, publishedSceneID string) (*VoteTally, error) {
	var t VoteTally
	for _, v := range f.publishedVoters[publishedSceneID] {
		switch {
		case v.Vote == nil:
			t.Pending++
		case *v.Vote:
			t.Yes++
		default:
			t.No++
		}
	}
	return &t, nil
}

// CastVote upserts a roster member's vote on the in-memory roster, mirroring
// the store's is_change semantics. A non-roster character is rejected with
// SCENE_PUBLISH_NOT_A_VOTER. Mirroring the real store (holomush-wn612), a vote
// on a terminal attempt is rejected with SCENE_PUBLISH_INVALID_STATE before the
// roster check — status-before-roster ordering. Attempts not installed via
// installPublishedAttempt are treated as live (the looser fake contract used by
// fixtures that seed only a voter roster).
func (f *fakeStore) CastVote(_ context.Context, publishedSceneID, characterID string, vote bool) (*CastVoteResult, error) {
	if pub, ok := f.publishedScenes[publishedSceneID]; ok && pub.Status.IsTerminal() {
		return nil, oops.Code("SCENE_PUBLISH_INVALID_STATE").
			With("published_scene_id", publishedSceneID).
			With("status", string(pub.Status)).
			Errorf("vote on a terminal publication attempt is rejected")
	}
	rows := f.publishedVoters[publishedSceneID]
	for i := range rows {
		if rows[i].CharacterID == characterID {
			isChange := rows[i].Vote != nil && *rows[i].Vote != vote
			v := vote
			rows[i].Vote = &v
			return &CastVoteResult{Vote: vote, IsChange: isChange}, nil
		}
	}
	return nil, oops.Code("SCENE_PUBLISH_NOT_A_VOTER").
		With("character_id", characterID).Errorf("character is not on the voter roster")
}

// TransitionStatus applies a state-machine transition to the in-memory attempt,
// setting the side-effect fields. Legality is enforced by applyTrigger's
// NextStatus check before this is called, so the fake applies unconditionally.
func (f *fakeStore) TransitionStatus(_ context.Context, id string, in TransitionInput) error {
	pub, ok := f.publishedScenes[id]
	if !ok {
		return oops.Code("SCENE_PUBLISH_NOT_FOUND").With("id", id).Errorf("attempt not found")
	}
	pub.Status = in.To
	if in.FailureReason != nil {
		pub.FailureReason = in.FailureReason
	}
	if in.SetCoolOffAt != nil {
		ts := pgnanos.From(*in.SetCoolOffAt)
		pub.CoolOffStartedAt = &ts
	}
	if in.ClearCoolOff {
		pub.CoolOffStartedAt = nil
	}
	if in.Resolved {
		now := pgnanos.From(time.Now())
		pub.ResolvedAt = &now
	}
	return nil
}

// CountAttempts returns the configured per-scene attempt counts (zero value
// when unset — i.e. no prior attempts).
func (f *fakeStore) CountAttempts(_ context.Context, sceneID string) (AttemptCounts, error) {
	return f.attemptCounts[sceneID], nil
}

// GetSceneMaxPublishAttempts returns the configured budget (zero when unset).
func (f *fakeStore) GetSceneMaxPublishAttempts(_ context.Context, sceneID string) (int, error) {
	return f.maxPublishAttempts[sceneID], nil
}

// ExtendMaxPublishAttempts bumps the configured budget and returns the new value.
func (f *fakeStore) ExtendMaxPublishAttempts(_ context.Context, sceneID string, additional int) (int, error) {
	f.maxPublishAttempts[sceneID] += additional
	return f.maxPublishAttempts[sceneID], nil
}

// CreatePublishAttempt records the call and returns a synthetic COLLECTING
// attempt — the handler-unit-test concern is precondition logic, not roster
// seeding (A5 integration-tests cover that).
func (f *fakeStore) CreatePublishAttempt(_ context.Context, in CreatePublishAttemptInput) (*PublishedScene, error) {
	pub := &PublishedScene{
		ID:            "pub-" + in.SceneID,
		SceneID:       in.SceneID,
		AttemptNumber: in.AttemptNumber,
		Status:        StatusCollecting,
		InitiatedBy:   in.InitiatedBy,
	}
	f.createdAttempts = append(f.createdAttempts, pub)
	return pub, nil
}

func (s *recordingEventSink) Emit(_ context.Context, intent pluginsdk.EmitIntent) error {
	if s.err != nil {
		return s.err
	}
	s.intents = append(s.intents, intent)
	return nil
}

func (f *fakeStore) Create(_ context.Context, row *SceneRow) error {
	if f.createErr != nil {
		return f.createErr
	}
	if _, exists := f.scenes[row.ID]; exists {
		return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Errorf("duplicate")
	}
	cp := *row
	f.scenes[row.ID] = &cp
	return nil
}

func (f *fakeStore) CreateWithOwner(ctx context.Context, row *SceneRow) error {
	if f.createWithOwnerErr != nil {
		return f.createWithOwnerErr
	}
	if err := f.Create(ctx, row); err != nil {
		return err
	}
	if f.participants[row.ID] == nil {
		f.participants[row.ID] = make(map[string]string)
	}
	f.participants[row.ID][row.OwnerID] = "owner"
	return nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*SceneRow, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	return row, nil
}

func (f *fakeStore) GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error) {
	row, err := f.Get(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	var participants, invitees []string
	for cid, role := range f.participants[id] {
		switch role {
		case "owner", "member":
			participants = append(participants, cid)
		case "invited":
			invitees = append(invitees, cid)
		}
	}
	return row, participants, invitees, nil
}

func (f *fakeStore) AddParticipant(_ context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error) {
	if f.addParticipantErr != nil {
		return nil, OpNoChange, f.addParticipantErr
	}
	scene, ok := f.scenes[sceneID]
	if !ok {
		return nil, OpNoChange, oops.Code("SCENE_NOT_FOUND").With("scene_id", sceneID).Errorf("not found")
	}
	if scene.State != string(SceneStateActive) && scene.State != string(SceneStatePaused) {
		return nil, OpNoChange, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).With("current_state", scene.State).Errorf("cannot join")
	}
	if f.participants[sceneID] == nil {
		f.participants[sceneID] = make(map[string]string)
	}
	existing, exists := f.participants[sceneID][characterID]
	if exists {
		if existing == "invited" {
			f.participants[sceneID][characterID] = "member"
			return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: "member"}, OpPromoted, nil
		}
		return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: existing}, OpNoChange, nil
	}
	if scene.Visibility == string(SceneVisibilityPrivate) {
		return nil, OpNoChange, oops.Code("SCENE_JOIN_NOT_INVITED").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("not invited")
	}
	f.participants[sceneID][characterID] = "member"
	return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: "member"}, OpInserted, nil
}

func (f *fakeStore) RemoveParticipant(_ context.Context, sceneID, characterID string) (*ParticipantRow, error) {
	role, exists := f.participants[sceneID][characterID]
	if !exists {
		return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("not found")
	}
	if role == "owner" {
		return nil, oops.Code("SCENE_OWNER_CANNOT_LEAVE").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("owners cannot leave")
	}
	delete(f.participants[sceneID], characterID)
	return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: role}, nil
}

func (f *fakeStore) InviteParticipant(_ context.Context, sceneID, _, targetID string) (*ParticipantRow, error) {
	if f.participants[sceneID] == nil {
		f.participants[sceneID] = make(map[string]string)
	}
	if existing, ok := f.participants[sceneID][targetID]; ok {
		if existing == "invited" {
			return &ParticipantRow{SceneID: sceneID, CharacterID: targetID, Role: "invited"}, nil
		}
		return nil, oops.Code("SCENE_INVITE_TARGET_ALREADY_MEMBER").
			With("scene_id", sceneID).With("target_id", targetID).Errorf("already %s", existing)
	}
	f.participants[sceneID][targetID] = "invited"
	return &ParticipantRow{SceneID: sceneID, CharacterID: targetID, Role: "invited"}, nil
}

func (f *fakeStore) KickParticipant(_ context.Context, sceneID, _, targetID string) (*ParticipantRow, error) {
	role, exists := f.participants[sceneID][targetID]
	if !exists {
		return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
			With("scene_id", sceneID).With("target_id", targetID).Errorf("not found")
	}
	if role == "owner" {
		return nil, oops.Code("SCENE_KICK_FORBIDDEN").
			With("scene_id", sceneID).With("target_id", targetID).Errorf("cannot kick owner")
	}
	delete(f.participants[sceneID], targetID)
	return &ParticipantRow{SceneID: sceneID, CharacterID: targetID, Role: role}, nil
}

func (f *fakeStore) TransferOwnership(_ context.Context, sceneID, currentOwnerID, newOwnerID string) error {
	if currentOwnerID == newOwnerID {
		return nil
	}
	scene, ok := f.scenes[sceneID]
	if !ok {
		return oops.Code("SCENE_NOT_FOUND").With("scene_id", sceneID).Errorf("not found")
	}
	if scene.OwnerID != currentOwnerID {
		return oops.Code("SCENE_NOT_OWNER").With("scene_id", sceneID).Errorf("not owner")
	}
	if scene.State != string(SceneStateActive) && scene.State != string(SceneStatePaused) {
		return oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).With("current_state", scene.State).Errorf("wrong state")
	}
	if f.participants[sceneID][newOwnerID] != "member" {
		return oops.Code("SCENE_TRANSFER_TARGET_NOT_MEMBER").
			With("scene_id", sceneID).With("target_id", newOwnerID).Errorf("not member")
	}
	f.participants[sceneID][currentOwnerID] = "member"
	f.participants[sceneID][newOwnerID] = "owner"
	scene.OwnerID = newOwnerID
	return nil
}

func (f *fakeStore) ListParticipants(_ context.Context, sceneID string) ([]ParticipantRow, error) {
	out := make([]ParticipantRow, 0, len(f.participants[sceneID]))
	for cid, role := range f.participants[sceneID] {
		out = append(out, ParticipantRow{SceneID: sceneID, CharacterID: cid, Role: role})
	}
	return out, nil
}

func (f *fakeStore) GetParticipant(_ context.Context, sceneID, characterID string) (*ParticipantRow, error) {
	role, ok := f.participants[sceneID][characterID]
	if !ok {
		return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
			With("scene_id", sceneID).With("character_id", characterID).Errorf("not found")
	}
	return &ParticipantRow{SceneID: sceneID, CharacterID: characterID, Role: role}, nil
}

func (f *fakeStore) IsParticipant(_ context.Context, sceneID, characterID string) (bool, error) {
	role, ok := f.participants[sceneID][characterID]
	if !ok {
		return false, nil
	}
	return role == "owner" || role == "member", nil
}

func (f *fakeStore) ListParticipantsWithPoseMeta(_ context.Context, sceneID string) (ParticipantsWithPoseMeta, error) {
	var result ParticipantsWithPoseMeta
	for cid, role := range f.participants[sceneID] {
		if role == "owner" || role == "member" {
			result.Participants = append(result.Participants, ParticipantWithPoseMeta{
				CharacterID: cid,
			})
		}
	}
	return result, nil
}

// ListScenesForCharacter mirrors the production query's role + state
// filter: only owner/member rows in active/paused scenes count. Failure
// can be injected via fakeStore.listScenesForCharacterErr.
func (f *fakeStore) ListScenesForCharacter(_ context.Context, characterID string) ([]string, error) {
	if f.listScenesForCharacterErr != nil {
		return nil, f.listScenesForCharacterErr
	}
	var ids []string
	for sceneID, members := range f.participants {
		role, ok := members[characterID]
		if !ok {
			continue
		}
		if role != "owner" && role != "member" {
			continue
		}
		// Mirror the production query's state filter: only active or paused
		// scenes count toward single-membership inference.
		scene, sceneOK := f.scenes[sceneID]
		if !sceneOK {
			continue
		}
		if scene.State != string(SceneStateActive) && scene.State != string(SceneStatePaused) {
			continue
		}
		ids = append(ids, sceneID)
	}
	return ids, nil
}

// ListBoard records the query it received and returns the configured rows/err.
// Satisfies sceneStorer for iokti.12 unit tests.
func (f *fakeStore) ListBoard(_ context.Context, q BoardQuery) ([]*SceneRow, error) {
	got := q
	f.listBoardGot = &got
	if f.listBoardErr != nil {
		return nil, f.listBoardErr
	}
	return f.listBoardRows, nil
}

func (f *fakeStore) End(_ context.Context, id string) (*SceneRow, error) {
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	if row.State != string(SceneStateActive) && row.State != string(SceneStatePaused) {
		return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", id).
			With("op", "end").
			With("current_state", row.State).
			Errorf("cannot end")
	}
	row.State = string(SceneStateEnded)
	endedAt := pgnanos.From(time.Now().UTC())
	row.EndedAt = &endedAt
	cp := *row
	return &cp, nil
}

func (f *fakeStore) Pause(_ context.Context, id string) (*SceneRow, error) {
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	if row.State != string(SceneStateActive) {
		return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", id).
			With("op", "pause").
			With("current_state", row.State).
			Errorf("cannot pause")
	}
	row.State = string(SceneStatePaused)
	cp := *row
	return &cp, nil
}

func (f *fakeStore) Resume(_ context.Context, id string) (*SceneRow, error) {
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	if row.State != string(SceneStatePaused) {
		return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", id).
			With("op", "resume").
			With("current_state", row.State).
			Errorf("cannot resume")
	}
	row.State = string(SceneStateActive)
	cp := *row
	return &cp, nil
}

func (f *fakeStore) Update(_ context.Context, id string, update *SceneUpdate) (*SceneRow, error) {
	row, ok := f.scenes[id]
	if !ok {
		return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Errorf("not found")
	}
	if update == nil || !update.HasChanges() {
		// No-op: return a copy of the current row, mirroring the real
		// store's "no-op returns current state" contract.
		cp := *row
		return &cp, nil
	}
	if row.State != string(SceneStateActive) && row.State != string(SceneStatePaused) {
		return nil, oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", id).
			With("op", "update").
			With("current_state", row.State).
			Errorf("cannot update")
	}
	if update.Title != nil {
		row.Title = *update.Title
	}
	if update.Description != nil {
		row.Description = *update.Description
	}
	if update.Visibility != nil {
		row.Visibility = *update.Visibility
	}
	if update.PoseOrder != nil {
		row.PoseOrder = *update.PoseOrder
	}
	if update.LocationID != nil {
		if *update.LocationID == "" {
			row.LocationID = nil
		} else {
			loc := *update.LocationID
			row.LocationID = &loc
		}
	}
	if update.UpdateContentWarnings {
		row.ContentWarnings = update.ContentWarnings
	}
	if update.UpdateTags {
		row.Tags = update.Tags
	}
	cp := *row
	return &cp, nil
}

// ── C7 snapshot-pipeline interface stubs ──────────────────────────────────
// The snapshot pipeline (runSnapshot) is tx-scoped and exercised exclusively by
// publish_snapshot_integration_test.go against a real Postgres + real read-back
// decryptor. These fakeStore stubs exist only to satisfy the sceneStorer
// interface for the unit suite; they are never invoked by a unit test.

func (f *fakeStore) SnapshotPool() *pgxpool.Pool { return nil }

func (f *fakeStore) LockForSnapshot(_ context.Context, _ pgx.Tx, id string) (*PublishedScene, error) {
	return nil, oops.Code("SCENE_PUBLISH_NOT_FOUND").With("id", id).Errorf("fakeStore: LockForSnapshot not supported")
}

func (f *fakeStore) TallyVotesTx(ctx context.Context, _ pgx.Tx, publishedSceneID string) (*VoteTally, error) {
	return f.TallyVotes(ctx, publishedSceneID)
}

func (f *fakeStore) ReadSceneLogForSnapshot(_ context.Context, _ pgx.Tx, _ string) ([]LogRow, error) {
	return nil, nil
}

func (f *fakeStore) ReadSceneMetaForSnapshot(_ context.Context, _ pgx.Tx, _ string) (SnapshotSceneMeta, error) {
	return SnapshotSceneMeta{}, nil
}

func (f *fakeStore) MarkPublished(_ context.Context, _ pgx.Tx, _ string, _ MarkPublishedInput) error {
	return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").Errorf("fakeStore: MarkPublished not supported")
}

func (f *fakeStore) ArchiveSceneStateForPublish(_ context.Context, _ pgx.Tx, _ string) (bool, error) {
	return false, nil
}

func (f *fakeStore) FailAttemptTx(_ context.Context, _ pgx.Tx, _ string, _ PublishFailureReason) error {
	return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").Errorf("fakeStore: FailAttemptTx not supported")
}

func TestSceneServiceCreateScenePersistsTitleAndOwnerWhenRequestIsValid(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})

	resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "  Tea at the Manor  ",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetScene())
	assert.False(t, strings.HasPrefix(resp.GetScene().GetId(), "scene-"),
		"scene id is a bare ULID (holomush-y5inx)")
	_, idErr := ulid.Parse(resp.GetScene().GetId())
	assert.NoError(t, idErr, "scene id parses as a bare ULID")
	assert.Equal(t, "Tea at the Manor", resp.GetScene().GetTitle(), "title should be trimmed")
	assert.Equal(t, "char-alice", resp.GetScene().GetOwnerId())
	assert.Equal(t, string(SceneStateActive), resp.GetScene().GetState())
	assert.Equal(t, string(SceneVisibilityOpen), resp.GetScene().GetVisibility())
}

func TestSceneServiceCreateSceneDefaultsVisibilityToOpenWhenRequestOmitsIt(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})

	resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "Open Scene",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetScene())
	assert.Equal(t, string(SceneVisibilityOpen), resp.GetScene().GetVisibility())
}

func TestSceneServiceCreateScenePersistsPrivateVisibilityWhenRequestSpecifiesIt(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})

	resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "Secret Gathering",
		Visibility:  string(SceneVisibilityPrivate),
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetScene())
	assert.Equal(t, string(SceneVisibilityPrivate), resp.GetScene().GetVisibility(),
		"private visibility from request must be stored and returned")

	// Verify the row in the store also has private visibility.
	var stored *SceneRow
	for _, row := range store.scenes {
		stored = row
	}
	require.NotNil(t, stored)
	assert.Equal(t, string(SceneVisibilityPrivate), stored.Visibility,
		"persisted row must carry private visibility, not the default")
}

func TestSceneServiceCreateSceneRejectsWhitespaceOnlyTitle(t *testing.T) {
	svc := newTestService(t, newFakeStore())

	_, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "   ",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "whitespace-only")
}

func TestSceneServiceCreateSceneReturnsInternalWhenStoreFails(t *testing.T) {
	store := newFakeStore()
	store.createErr = oops.Code("SCENE_CREATE_FAILED").Errorf("boom")
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})

	_, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "Tea",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestSceneServiceCreateSceneEmitsLifecycleEventWhenEventSinkConfigured(t *testing.T) {
	store := newFakeStore()
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "Tea",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetScene())

	require.Len(t, sink.intents, 1)
	assert.Equal(t, dotStyleSceneSubject("main", resp.GetScene().GetId()), sink.intents[0].Subject)
	assert.Equal(t, pluginsdk.HostEventTypeSystem, sink.intents[0].Type)
	assert.Contains(t, sink.intents[0].Payload, `"kind":"scene.lifecycle.created"`)
	assert.Contains(t, sink.intents[0].Payload, `"scene_id":"`+resp.GetScene().GetId()+`"`)
}

func TestSceneServiceCreateSceneFailsWhenEventSinkIsMissing(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)

	_, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "Tea",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "failed to prepare scene event")
	assert.Empty(t, store.scenes)
}

func TestSceneServiceCreateSceneReturnsInternalWhenEventEmitFailsAfterPersist(t *testing.T) {
	store := newFakeStore()
	sink := &recordingEventSink{err: errors.New("boom")}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		CharacterId: "char-alice",
		Title:       "Tea",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "failed to emit scene event")
	require.Len(t, store.scenes, 1)
}

func TestSceneServiceSceneCreatedIntentReturnsZeroValueForNilRow(t *testing.T) {
	svc := newTestService(t, newFakeStore())

	intent, err := svc.sceneCreatedIntent(nil)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.EmitIntent{}, intent)
}

func TestSceneServiceSceneCreatedIntentBuildsLifecyclePayload(t *testing.T) {
	svc := newTestService(t, newFakeStore())

	intent, err := svc.sceneCreatedIntent(&SceneRow{
		ID:      "scene-123",
		OwnerID: "char-alice",
		Title:   "Tea",
	})
	require.NoError(t, err)
	assert.Equal(t, dotStyleSceneSubject("main", "scene-123"), intent.Subject)
	assert.Equal(t, pluginsdk.HostEventTypeSystem, intent.Type)
	assert.Contains(t, intent.Payload, `"kind":"scene.lifecycle.created"`)
	assert.Contains(t, intent.Payload, `"scene_id":"scene-123"`)
	assert.Contains(t, intent.Payload, `"owner_id":"char-alice"`)
}

func TestSceneServiceGetSceneReturnsSceneWhenItExists(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-known"] = &SceneRow{
		ID:         "scene-known",
		Title:      "Existing",
		OwnerID:    "char-alice",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := newTestService(t, store)

	resp, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-known",
	})
	require.NoError(t, err)
	assert.Equal(t, "scene-known", resp.GetScene().GetId())
	assert.Equal(t, "Existing", resp.GetScene().GetTitle())
}

func TestSceneServiceGetSceneReturnsNotFoundWhenSceneIsMissing(t *testing.T) {
	svc := newTestService(t, newFakeStore())

	_, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-missing",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceGetSceneReturnsInternalForUnknownStoreError(t *testing.T) {
	store := newFakeStore()
	store.getErr = errors.New("connection refused")
	svc := newTestService(t, store)

	_, err := svc.GetScene(context.Background(), &scenev1.GetSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-x",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestSceneServiceEndSceneTransitionsScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		Title:      "Test",
		OwnerID:    "char-alice",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := newTestService(t, store)

	resp, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "scene-1", resp.GetScene().GetId())
	assert.Equal(t, string(SceneStateEnded), resp.GetScene().GetState())
}

func TestSceneServiceEndSceneReturnsNotFoundForMissingScene(t *testing.T) {
	svc := newTestService(t, newFakeStore())

	_, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceEndSceneReturnsFailedPreconditionForEndedScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-ended"] = &SceneRow{
		ID:    "scene-ended",
		State: string(SceneStateEnded),
	}
	svc := newTestService(t, store)

	_, err := svc.EndScene(context.Background(), &scenev1.EndSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-ended",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServicePauseSceneTransitionsScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := newTestService(t, store)

	resp, err := svc.PauseScene(context.Background(), &scenev1.PauseSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
	})
	require.NoError(t, err)
	assert.Equal(t, string(SceneStatePaused), resp.GetScene().GetState())
}

func TestSceneServicePauseSceneReturnsNotFoundForMissingScene(t *testing.T) {
	svc := newTestService(t, newFakeStore())

	_, err := svc.PauseScene(context.Background(), &scenev1.PauseSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServicePauseSceneReturnsFailedPreconditionForAlreadyPausedScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-paused"] = &SceneRow{
		ID:    "scene-paused",
		State: string(SceneStatePaused),
	}
	svc := newTestService(t, store)

	_, err := svc.PauseScene(context.Background(), &scenev1.PauseSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-paused",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceResumeSceneTransitionsScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		State:      string(SceneStatePaused),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := newTestService(t, store)

	resp, err := svc.ResumeScene(context.Background(), &scenev1.ResumeSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
	})
	require.NoError(t, err)
	assert.Equal(t, string(SceneStateActive), resp.GetScene().GetState())
}

func TestSceneServiceResumeSceneReturnsNotFoundForMissingScene(t *testing.T) {
	svc := newTestService(t, newFakeStore())

	_, err := svc.ResumeScene(context.Background(), &scenev1.ResumeSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceResumeSceneReturnsFailedPreconditionForActiveScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-active"] = &SceneRow{
		ID:    "scene-active",
		State: string(SceneStateActive),
	}
	svc := newTestService(t, store)

	_, err := svc.ResumeScene(context.Background(), &scenev1.ResumeSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-active",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceUpdateSceneAppliesTitleChange(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		Title:      "Original",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := newTestService(t, store)

	resp, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
		Title:       "Updated",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated", resp.GetScene().GetTitle())
}

func TestSceneServiceUpdateSceneRejectsEndedScene(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-ended"] = &SceneRow{
		ID:    "scene-ended",
		State: string(SceneStateEnded),
	}
	svc := newTestService(t, store)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-ended",
		Title:       "Try",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceUpdateSceneAppliesContentWarnings(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:              "scene-1",
		Title:           "T",
		State:           string(SceneStateActive),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{"violence"},
	}
	svc := newTestService(t, store)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId:     "char-alice",
		SceneId:         "scene-1",
		ContentWarnings: []string{"violence", "death"},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"content_warnings"}},
	})
	require.NoError(t, err)
	got := store.scenes["scene-1"]
	assert.ElementsMatch(t, []string{"violence", "death"}, got.ContentWarnings)
}

func TestSceneServiceUpdateSceneRejectsEmptyTitleInMask(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:         "scene-1",
		Title:      "Original",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	svc := newTestService(t, store)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
		Title:       "   ",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "title")
}

func TestSceneServiceUpdateSceneRejectsUnknownMaskPath(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-1"] = &SceneRow{
		ID:    "scene-1",
		State: string(SceneStateActive),
	}
	svc := newTestService(t, store)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-1",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"owner_id"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "unknown update_mask path")
}

func TestSceneServiceUpdateSceneEmptyMaskIsNoOp(t *testing.T) {
	// Clients commonly send either an omitted UpdateMask (nil) or an
	// explicit empty FieldMask with no paths. Both MUST be treated as a
	// no-op. Table-driven so a future serialization form can be added
	// without duplicating the fixture setup.
	cases := []struct {
		name string
		mask *fieldmaskpb.FieldMask
	}{
		{"nil update_mask is a no-op", nil},
		{"explicit empty update_mask paths is a no-op", &fieldmaskpb.FieldMask{Paths: []string{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			store.scenes["scene-1"] = &SceneRow{
				ID:         "scene-1",
				Title:      "Unchanged",
				State:      string(SceneStateActive),
				Visibility: string(SceneVisibilityOpen),
			}
			svc := newTestService(t, store)

			resp, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
				CharacterId: "char-alice",
				SceneId:     "scene-1",
				UpdateMask:  tc.mask,
			})
			require.NoError(t, err)
			assert.Equal(t, "Unchanged", resp.GetScene().GetTitle())
			// Store row must also be untouched — the no-op path MUST NOT
			// emit any mutation to the fake store.
			assert.Equal(t, "Unchanged", store.scenes["scene-1"].Title)
		})
	}
}

func TestSceneServiceJoinSceneInsertsMemberAndReturnsSuccess(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := newTestService(t, store)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob",
		SceneId:     "scene-js-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "member", store.participants["scene-js-1"]["char-bob"])
}

func TestSceneServiceJoinSceneMapsNotInvitedToPermissionDenied(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-priv", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	svc := newTestService(t, store)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-js-priv",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestSceneServiceJoinSceneMapsNotFoundToNotFound(t *testing.T) {
	svc := newTestService(t, newFakeStore())
	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceJoinSceneMapsTransitionForbiddenToFailedPrecondition(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-js-ended", OwnerID: "char-alice",
		State: string(SceneStateEnded), Visibility: string(SceneVisibilityOpen),
	}))
	svc := newTestService(t, store)
	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		CharacterId: "char-bob", SceneId: "scene-js-ended",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceLeaveSceneRejectsOwnerWithFailedPrecondition(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-ls-owner", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := newTestService(t, store)

	_, err := svc.LeaveScene(context.Background(), &scenev1.LeaveSceneRequest{
		CharacterId: "char-alice",
		SceneId:     "scene-ls-owner",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "owners cannot leave")
}

func TestSceneServiceLeaveSceneRemovesMember(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-ls-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-ls-1", "char-bob")
	require.NoError(t, err)
	svc := newTestService(t, store)

	_, err = svc.LeaveScene(context.Background(), &scenev1.LeaveSceneRequest{
		CharacterId: "char-bob",
		SceneId:     "scene-ls-1",
	})
	require.NoError(t, err)
	_, exists := store.participants["scene-ls-1"]["char-bob"]
	assert.False(t, exists)
}

func TestSceneServiceInviteToSceneCallsStore(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-its-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	svc := newTestService(t, store)

	_, err := svc.InviteToScene(context.Background(), &scenev1.InviteToSceneRequest{
		CharacterId:       "char-alice",
		SceneId:           "scene-its-1",
		TargetCharacterId: "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, "invited", store.participants["scene-its-1"]["char-bob"])
}

func TestSceneServiceKickFromSceneRemovesMember(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-kfs-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-kfs-1", "char-bob")
	require.NoError(t, err)
	svc := newTestService(t, store)

	_, err = svc.KickFromScene(context.Background(), &scenev1.KickFromSceneRequest{
		CharacterId:       "char-alice",
		SceneId:           "scene-kfs-1",
		TargetCharacterId: "char-bob",
	})
	require.NoError(t, err)
	_, exists := store.participants["scene-kfs-1"]["char-bob"]
	assert.False(t, exists)
}

func TestSceneServiceKickFromSceneRejectsKickingOwner(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-kfs-owner", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := newTestService(t, store)

	_, err := svc.KickFromScene(context.Background(), &scenev1.KickFromSceneRequest{
		CharacterId:       "char-alice",
		SceneId:           "scene-kfs-owner",
		TargetCharacterId: "char-alice",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestSceneServiceTransferOwnershipUpdatesOwner(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-tos-1", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-tos-1", "char-bob")
	require.NoError(t, err)
	svc := newTestService(t, store)

	_, err = svc.TransferOwnership(context.Background(), &scenev1.TransferOwnershipRequest{
		CharacterId:         "char-alice",
		SceneId:             "scene-tos-1",
		NewOwnerCharacterId: "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, "char-bob", store.scenes["scene-tos-1"].OwnerID)
}

func TestSceneServiceTransferOwnershipRejectsNonMemberTargetWithFailedPrecondition(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-tos-nm", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := newTestService(t, store)

	_, err := svc.TransferOwnership(context.Background(), &scenev1.TransferOwnershipRequest{
		CharacterId:         "char-alice",
		SceneId:             "scene-tos-nm",
		NewOwnerCharacterId: "char-bob",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// --- Error-path coverage for membership service handlers ---

func TestSceneServiceLeaveSceneReturnsNotFoundForMissingScene(t *testing.T) {
	svc := newTestService(t, newFakeStore())

	_, err := svc.LeaveScene(context.Background(), &scenev1.LeaveSceneRequest{
		CharacterId: "char-bob",
		SceneId:     "scene-does-not-exist",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceLeaveSceneReturnsNotFoundForNonParticipant(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-ls-np", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := newTestService(t, store)

	_, err := svc.LeaveScene(context.Background(), &scenev1.LeaveSceneRequest{
		CharacterId: "char-stranger",
		SceneId:     "scene-ls-np",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSceneServiceInviteToSceneReturnsAlreadyExistsForMember(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-inv-ae", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-inv-ae", "char-bob")
	require.NoError(t, err)
	svc := newTestService(t, store)

	_, err = svc.InviteToScene(context.Background(), &scenev1.InviteToSceneRequest{
		CharacterId:       "char-alice",
		SceneId:           "scene-inv-ae",
		TargetCharacterId: "char-bob",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.AlreadyExists, st.Code())
}

func TestSceneServiceKickFromSceneReturnsNotFoundForNonParticipant(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-kick-np", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	svc := newTestService(t, store)

	_, err := svc.KickFromScene(context.Background(), &scenev1.KickFromSceneRequest{
		CharacterId:       "char-alice",
		SceneId:           "scene-kick-np",
		TargetCharacterId: "char-stranger",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

// findIntentByType returns the first EmitIntent with the given Type, or nil.
func findIntentByType(intents []pluginsdk.EmitIntent, eventType string) *pluginsdk.EmitIntent {
	for i := range intents {
		if string(intents[i].Type) == eventType {
			return &intents[i]
		}
	}
	return nil
}

func TestJoinScene_EmitsSceneJoinIC_OnInsert(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-join-emit", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		SceneId:     "scene-join-emit",
		CharacterId: "char-bob",
	})
	require.NoError(t, err)

	found := findIntentByType(sink.intents, "scene_join_ic")
	require.NotNil(t, found, "JoinScene MUST auto-emit scene_join_ic on OpInserted")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-join-emit"), found.Subject)
	assert.False(t, found.Sensitive, "scene_join_ic is sensitivity:never")
	assert.Contains(t, found.Payload, `"actor_id":"char-bob"`)
	assert.Contains(t, found.Payload, `"scene_id":"scene-join-emit"`)
	assert.Contains(t, found.Payload, `"from_role":"none"`)
}

func TestJoinScene_EmitsSceneJoinIC_OnPromote(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-join-promote", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	// Pre-seed char-bob as invited so AddParticipant returns OpPromoted.
	store.participants["scene-join-promote"]["char-bob"] = "invited"
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		SceneId:     "scene-join-promote",
		CharacterId: "char-bob",
	})
	require.NoError(t, err)

	found := findIntentByType(sink.intents, "scene_join_ic")
	require.NotNil(t, found, "JoinScene MUST auto-emit scene_join_ic on OpPromoted")
	assert.Contains(t, found.Payload, `"from_role":"invited"`)
}

func TestJoinScene_NoEmit_OnNoChange(t *testing.T) {
	t.Parallel()
	// Idempotent retry: already-member join returns OpNoChange and MUST NOT
	// emit a duplicate scene_join_ic.
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-join-idempotent", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	// char-alice is already owner (inserted by CreateWithOwner).
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
		SceneId:     "scene-join-idempotent",
		CharacterId: "char-alice", // already owner → OpNoChange
	})
	require.NoError(t, err)

	joinCount := 0
	for _, it := range sink.intents {
		if string(it.Type) == "scene_join_ic" {
			joinCount++
		}
	}
	assert.Equal(t, 0, joinCount, "idempotent join MUST NOT emit scene_join_ic")
}

func TestLeaveScene_EmitsSceneLeaveIC_ReasonLeft(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-leave-emit", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	// Pre-seed char-bob as a member so RemoveParticipant succeeds.
	store.participants["scene-leave-emit"]["char-bob"] = "member"
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.LeaveScene(context.Background(), &scenev1.LeaveSceneRequest{
		SceneId:     "scene-leave-emit",
		CharacterId: "char-bob",
	})
	require.NoError(t, err)

	found := findIntentByType(sink.intents, "scene_leave_ic")
	require.NotNil(t, found, "LeaveScene MUST auto-emit scene_leave_ic")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-leave-emit"), found.Subject)
	assert.False(t, found.Sensitive, "scene_leave_ic is sensitivity:never")
	assert.Contains(t, found.Payload, `"actor_id":"char-bob"`)
	assert.Contains(t, found.Payload, `"scene_id":"scene-leave-emit"`)
	assert.Contains(t, found.Payload, `"reason":"left"`)
	assert.NotContains(t, found.Payload, `"removed_by"`, "voluntary leave MUST NOT include removed_by")
}

func TestKickFromScene_EmitsSceneLeaveIC_ReasonKicked(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-kick-emit", OwnerID: "char-owner",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	// Pre-seed char-target as a member so KickParticipant succeeds.
	store.participants["scene-kick-emit"]["char-target"] = "member"
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.KickFromScene(context.Background(), &scenev1.KickFromSceneRequest{
		SceneId:           "scene-kick-emit",
		CharacterId:       "char-owner",  // kicker
		TargetCharacterId: "char-target", // who gets kicked
	})
	require.NoError(t, err)

	found := findIntentByType(sink.intents, "scene_leave_ic")
	require.NotNil(t, found, "KickFromScene MUST auto-emit scene_leave_ic")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-kick-emit"), found.Subject)
	assert.False(t, found.Sensitive, "scene_leave_ic is sensitivity:never")
	assert.Contains(t, found.Payload, `"actor_id":"char-target"`, "actor_id is the TARGET of the kick")
	assert.Contains(t, found.Payload, `"scene_id":"scene-kick-emit"`)
	assert.Contains(t, found.Payload, `"reason":"kicked"`)
	assert.Contains(t, found.Payload, `"removed_by":"char-owner"`)
}

func TestUpdateScene_EmitsPoseOrderChangedIC_OnModeChange(t *testing.T) {
	t.Parallel()
	// Scene starts with pose_order_mode = "free" (CreateScene default);
	// owner updates it to "strict". Expect scene_pose_order_changed_ic.
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-mode-change",
		OwnerID:    "char-owner",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
		PoseOrder:  string(PoseOrderModeFree),
	}))
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		SceneId:       "scene-mode-change",
		CharacterId:   "char-owner",
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"pose_order_mode"}},
		PoseOrderMode: "strict",
	})
	require.NoError(t, err)

	found := findIntentByType(sink.intents, "scene_pose_order_changed_ic")
	require.NotNil(t, found, "UpdateScene MUST auto-emit scene_pose_order_changed_ic on mode change")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-mode-change"), found.Subject)
	assert.False(t, found.Sensitive, "scene_pose_order_changed_ic is sensitivity:never")
	assert.Contains(t, found.Payload, `"old_mode":"free"`)
	assert.Contains(t, found.Payload, `"new_mode":"strict"`)
	assert.Contains(t, found.Payload, `"actor_id":"char-owner"`)
	assert.Contains(t, found.Payload, `"scene_id":"scene-mode-change"`)
}

func TestUpdateScene_NoEmit_OnNoModeChange(t *testing.T) {
	t.Parallel()
	// Mask includes pose_order_mode but the value is the same as current.
	// No-op update MUST NOT emit a spurious notice.
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-mode-noop",
		OwnerID:    "char-owner",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
		PoseOrder:  string(PoseOrderModeFree),
	}))
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		SceneId:       "scene-mode-noop",
		CharacterId:   "char-owner",
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"pose_order_mode"}},
		PoseOrderMode: "free", // same as current — no-op
	})
	require.NoError(t, err)

	count := 0
	for _, it := range sink.intents {
		if it.Type == "scene_pose_order_changed_ic" {
			count++
		}
	}
	assert.Equal(t, 0, count, "no-op mode update MUST NOT emit scene_pose_order_changed_ic")
}

func TestUpdateScene_NoEmit_OnNonModeUpdate(t *testing.T) {
	t.Parallel()
	// Mask does NOT include pose_order_mode (only updates title).
	// MUST NOT emit scene_pose_order_changed_ic.
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-other-update",
		OwnerID:    "char-owner",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
		PoseOrder:  string(PoseOrderModeFree),
	}))
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	_, err := svc.UpdateScene(context.Background(), &scenev1.UpdateSceneRequest{
		SceneId:     "scene-other-update",
		CharacterId: "char-owner",
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
		Title:       "New Title",
	})
	require.NoError(t, err)

	count := 0
	for _, it := range sink.intents {
		if it.Type == "scene_pose_order_changed_ic" {
			count++
		}
	}
	assert.Equal(t, 0, count, "non-mode update MUST NOT emit scene_pose_order_changed_ic")
}

// TestGetPoseOrder_NotParticipant_PermissionDenied pins the INV-SCENE-60
// plugin-code gate: a caller who is NOT a current participant of the
// scene MUST receive PermissionDenied, NOT NotFound. The gate fires
// before any scene-existence check so the error path does not leak
// scene existence to non-members.
func TestGetPoseOrder_NotParticipant_PermissionDenied(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-gpo-perm",
		OwnerID:    "char-owner",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
		PoseOrder:  string(PoseOrderModeFree),
	}))
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})

	_, err := svc.GetPoseOrder(context.Background(), &scenev1.GetPoseOrderRequest{
		SceneId:     "scene-gpo-perm",
		CharacterId: "char-bob", // not in participants map
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code(),
		"non-participant MUST receive PermissionDenied (INV-SCENE-60 gate)")
}

// TestGetPoseOrder_InvitedRole_PermissionDenied pins INV-SCENE-60's
// "invited" exclusion: an invited (but not yet accepted) character
// is NOT a member of the scene for pose-order purposes and MUST be
// rejected by the gate. fakeStore.IsParticipant treats invited as
// non-member, matching the production store's role filter
// (sceneStorer.IsParticipant docstring).
func TestGetPoseOrder_InvitedRole_PermissionDenied(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-gpo-invited",
		OwnerID:    "char-owner",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityPrivate),
		PoseOrder:  string(PoseOrderModeStrict),
	}))
	// Mark char-bob as invited (not member).
	_, err := store.InviteParticipant(context.Background(), "scene-gpo-invited", "char-owner", "char-bob")
	require.NoError(t, err)

	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})

	_, err = svc.GetPoseOrder(context.Background(), &scenev1.GetPoseOrderRequest{
		SceneId:     "scene-gpo-invited",
		CharacterId: "char-bob",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code(),
		"invited-but-not-member MUST receive PermissionDenied (INV-SCENE-60 gate excludes invited role)")
}

// TestGetPoseOrder_NoScene_PermissionDenied pins the
// scene-existence-non-disclosure aspect of INV-SCENE-60: a request for a
// non-existent scene from a non-participant returns PermissionDenied
// (not NotFound). The IsParticipant gate runs before Get, so the
// response collapses both "scene does not exist" and "you are not a
// member" into the same security-aware error code.
func TestGetPoseOrder_NoScene_PermissionDenied(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newFakeStore())
	svc.SetEventSink(&recordingEventSink{})

	_, err := svc.GetPoseOrder(context.Background(), &scenev1.GetPoseOrderRequest{
		SceneId:     "scene-nonexistent",
		CharacterId: "any-character",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code(),
		"non-existent scene MUST be indistinguishable from non-membership (INV-SCENE-60)")
}

// TestGetPoseOrder_HappyPath_FreeMode verifies the composition:
// IsParticipant (T4) → Get + ListParticipantsWithPoseMeta (T5) →
// Compute (T9) → proto wire form. A fresh scene has total_pose_count
// of zero; in free mode every participant is eligible.
func TestGetPoseOrder_HappyPath_FreeMode(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-gpo-happy",
		OwnerID:    "char-alice",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
		PoseOrder:  string(PoseOrderModeFree),
	}))
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})

	resp, err := svc.GetPoseOrder(context.Background(), &scenev1.GetPoseOrderRequest{
		SceneId:     "scene-gpo-happy",
		CharacterId: "char-alice",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, string(PoseOrderModeFree), resp.GetMode())
	assert.Equal(t, uint32(0), resp.GetTotalPoseCount(),
		"fresh scene has zero total_pose_count")
	require.Len(t, resp.GetEntries(), 1, "owner is the sole participant")
	entry := resp.GetEntries()[0]
	assert.Equal(t, "char-alice", entry.GetCharacterId())
	// Names map is empty for Phase 4 → Compute renders missing names
	// as the character_id (poseorder.go::resolveName).
	assert.Equal(t, "char-alice", entry.GetCharacterName(),
		"empty names map yields character_id fallback")
	assert.True(t, entry.GetEligible(), "free mode: every participant is eligible")
	assert.Nil(t, entry.GetLastPosedAt(), "never-posed: last_posed_at is nil")
}

// TestGetPoseOrder_StoreError_Internal verifies that an unexpected
// store error from the participant check is surfaced as Internal,
// not silently mapped to PermissionDenied.
func TestGetPoseOrder_StoreError_Internal(t *testing.T) {
	t.Parallel()
	store := &erroringIsParticipantStore{
		fakeStore: newFakeStore(),
		err:       errors.New("db down"),
	}
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})

	_, err := svc.GetPoseOrder(context.Background(), &scenev1.GetPoseOrderRequest{
		SceneId:     "scene-gpo-storeerr",
		CharacterId: "char-alice",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code(),
		"store failure on IsParticipant MUST map to Internal, not PermissionDenied")
}

// erroringIsParticipantStore wraps fakeStore so a single test can
// inject an IsParticipant error without polluting the shared
// fakeStore type.
type erroringIsParticipantStore struct {
	*fakeStore
	err error
}

func (e *erroringIsParticipantStore) IsParticipant(_ context.Context, _, _ string) (bool, error) {
	return false, e.err
}

func TestNewSceneIDReturnsBareULIDWithoutPrefix(t *testing.T) {
	id, err := newSceneID()
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(id, "scene-"),
		"scene id must be a bare ULID, not a scene- prefixed string (holomush-y5inx)")
	parsed, perr := ulid.Parse(id)
	require.NoError(t, perr, "scene id must parse as a bare ULID")
	assert.Equal(t, id, parsed.String(), "round-trip: stored id equals its ULID string form")
}

// ── ListScenes unit tests (iokti.12) ─────────────────────────────────────────

func TestListScenesMapsRequestFieldsToBoardQueryAndReturnsSceneInfos(t *testing.T) {
	store := newFakeStore()
	now := time.Now().UTC()
	store.listBoardRows = []*SceneRow{
		{
			ID:              "scene-a",
			Title:           "Alpha Scene",
			OwnerID:         "owner-1",
			State:           string(SceneStateActive),
			Visibility:      "open",
			PoseOrder:       string(PoseOrderModeFree),
			ContentWarnings: []string{},
			Tags:            []string{"plot"},
			CreatedAt:       pgnanos.From(now),
		},
		{
			ID:              "scene-b",
			Title:           "Beta Scene",
			OwnerID:         "owner-2",
			State:           string(SceneStatePaused),
			Visibility:      "open",
			PoseOrder:       string(PoseOrderModeFree),
			ContentWarnings: []string{},
			Tags:            []string{"plot", "action"},
			CreatedAt:       pgnanos.From(now),
		},
	}

	svc := newTestService(t, store)
	resp, err := svc.ListScenes(context.Background(), &scenev1.ListScenesRequest{
		Limit:  10,
		Offset: 5,
		Tags:   []string{"plot"},
	})
	require.NoError(t, err)

	// Verify BoardQuery was built correctly from request fields.
	require.NotNil(t, store.listBoardGot, "ListBoard must have been called")
	assert.Equal(t, 10, store.listBoardGot.Limit)
	assert.Equal(t, 5, store.listBoardGot.Offset)
	assert.Equal(t, []string{"plot"}, store.listBoardGot.Tags)

	// Verify rows were mapped to SceneInfo.
	require.Len(t, resp.GetScenes(), 2)
	assert.Equal(t, "scene-a", resp.GetScenes()[0].GetId())
	assert.Equal(t, "Alpha Scene", resp.GetScenes()[0].GetTitle())
	assert.Equal(t, "scene-b", resp.GetScenes()[1].GetId())
}

func TestListScenesRequestValidationAllowsEmptyIdentityFields(t *testing.T) {
	// iokti review .1: character_id (5) and player_id (6) carry min_len=1 but
	// are optional filters. IGNORE_IF_ZERO_VALUE must let an anonymous browse
	// (both omitted) pass protovalidate so the request reaches the handler;
	// without it the interceptor would reject every board query that omits an
	// identity. Populated values still pass min_len.
	v, err := protovalidate.New()
	require.NoError(t, err)

	require.NoError(t, v.Validate(&scenev1.ListScenesRequest{
		Limit: 10,
		Tags:  []string{"plot"},
	}), "anonymous browse with empty character_id/player_id must validate")

	require.NoError(t, v.Validate(&scenev1.ListScenesRequest{
		CharacterId: "char-1",
		PlayerId:    "player-1",
	}), "populated identity fields must validate")
}

func TestListScenesForwardsExcludeContentWarningsToBoardQuery(t *testing.T) {
	// ExcludeContentWarnings, CharacterId, PlayerId are wired in iokti.13 — the
	// handler passes the blocked-CW union to BoardQuery.BlockedCW. With no
	// settings client wired (nil), resolveBlockedCW returns the per-query
	// excludes alone, so BlockedCW MUST carry exactly the request's
	// ExcludeContentWarnings; Limit still flows through unchanged.
	store := newFakeStore()
	store.listBoardRows = []*SceneRow{}

	svc := newTestService(t, store)
	_, err := svc.ListScenes(context.Background(), &scenev1.ListScenesRequest{
		Limit:                  3,
		ExcludeContentWarnings: []string{"violence"},
		CharacterId:            "char-1",
		PlayerId:               "player-1",
	})
	require.NoError(t, err)

	require.NotNil(t, store.listBoardGot)
	assert.Equal(t, 3, store.listBoardGot.Limit)
	assert.Equal(t, []string{"violence"}, store.listBoardGot.BlockedCW,
		"per-query ExcludeContentWarnings must be forwarded as BoardQuery.BlockedCW")
}

func TestListScenesStoreErrorReturnsInternalWithoutLeakingDetails(t *testing.T) {
	store := newFakeStore()
	store.listBoardErr = errors.New("db exploded: secret connection string info")

	svc := newTestService(t, store)
	_, err := svc.ListScenes(context.Background(), &scenev1.ListScenesRequest{})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok, "error must be a gRPC status")
	assert.Equal(t, codes.Internal, st.Code())
	// The inner error detail MUST NOT leak into the status message.
	assert.NotContains(t, st.Message(), "db exploded",
		"internal error detail must not be included in gRPC status message")
	assert.NotContains(t, st.Message(), "secret connection string",
		"internal error detail must not be included in gRPC status message")
}

// ── resolveBlockedCW + ListScenes CW union tests (iokti.13) ─────────────────

// scopedFakeSettingsClient is a per-scope fakeSettingsClient for iokti.13
// tests. It maps (scope, principalID) → (values, found, err) so each scope
// read can return a distinct result, mirroring the three single-scope reads
// that resolveBlockedCW performs.
type scopedFakeSettingsClient struct {
	// byScope maps SettingScope → per-scope outcome.
	byScope map[pluginsdk.SettingScope]scopedFakeOutcome
}

type scopedFakeOutcome struct {
	values []string
	found  bool
	err    error
}

func (f *scopedFakeSettingsClient) GetSetting(_ context.Context, scope pluginsdk.SettingScope, _, _ string) ([]string, bool, error) {
	if out, ok := f.byScope[scope]; ok {
		return out.values, out.found, out.err
	}
	return nil, false, nil
}

func (f *scopedFakeSettingsClient) SetSetting(_ context.Context, _ pluginsdk.SettingScope, _, _ string, _ []string) error {
	return nil
}

// TestResolveBlockedCWUnionsAllScopesPlusExclude asserts that resolveBlockedCW
// accumulates blocks from GAME, PLAYER, and CHARACTER scopes plus the per-query
// exclude list into a single deduplicated union.
func TestResolveBlockedCWUnionsAllScopesPlusExclude(t *testing.T) {
	svc := newTestService(t, newFakeStore())
	svc.settings = &scopedFakeSettingsClient{
		byScope: map[pluginsdk.SettingScope]scopedFakeOutcome{
			pluginsdk.SettingScopeGame:      {values: []string{"a"}, found: true},
			pluginsdk.SettingScopePlayer:    {values: []string{"b"}, found: true},
			pluginsdk.SettingScopeCharacter: {values: []string{"c"}, found: true},
		},
	}

	req := &scenev1.ListScenesRequest{
		PlayerId:               "player-1",
		CharacterId:            "char-1",
		ExcludeContentWarnings: []string{"d"},
	}
	got := svc.resolveBlockedCW(context.Background(), req)
	sort.Strings(got)

	assert.Equal(t, []string{"a", "b", "c", "d"}, got)
}

// TestResolveBlockedCWDeduplicatesAcrossScopes asserts that a CW tag that
// appears in multiple scopes is only present once in the returned union.
func TestResolveBlockedCWDeduplicatesAcrossScopes(t *testing.T) {
	svc := newTestService(t, newFakeStore())
	svc.settings = &scopedFakeSettingsClient{
		byScope: map[pluginsdk.SettingScope]scopedFakeOutcome{
			pluginsdk.SettingScopeGame:      {values: []string{"violence", "death"}, found: true},
			pluginsdk.SettingScopePlayer:    {values: []string{"death", "abuse"}, found: true},
			pluginsdk.SettingScopeCharacter: {values: []string{"violence"}, found: true},
		},
	}

	req := &scenev1.ListScenesRequest{
		PlayerId:    "player-1",
		CharacterId: "char-1",
	}
	got := svc.resolveBlockedCW(context.Background(), req)
	sort.Strings(got)

	assert.Equal(t, []string{"abuse", "death", "violence"}, got)
}

// TestResolveBlockedCWSkipsDeniedScopeWithoutBoardFailure asserts that a
// settings read error for one scope is silently skipped; other scopes still
// contribute their blocks and no error is propagated.
func TestResolveBlockedCWSkipsDeniedScopeWithoutBoardFailure(t *testing.T) {
	svc := newTestService(t, newFakeStore())
	svc.settings = &scopedFakeSettingsClient{
		byScope: map[pluginsdk.SettingScope]scopedFakeOutcome{
			pluginsdk.SettingScopeGame:      {values: []string{"a"}, found: true},
			pluginsdk.SettingScopePlayer:    {err: errors.New("ownership denied")},
			pluginsdk.SettingScopeCharacter: {values: []string{"c"}, found: true},
		},
	}

	req := &scenev1.ListScenesRequest{
		PlayerId:    "player-x",
		CharacterId: "char-1",
	}
	got := svc.resolveBlockedCW(context.Background(), req)
	sort.Strings(got)

	// PLAYER scope error is skipped; GAME and CHARACTER blocks still apply.
	assert.Equal(t, []string{"a", "c"}, got)
}

// TestResolveBlockedCWNilSettingsClientUsesExcludeOnly asserts that when the
// settings client is nil, resolveBlockedCW returns only the per-query exclude
// list (no scope reads attempted).
func TestResolveBlockedCWNilSettingsClientUsesExcludeOnly(t *testing.T) {
	svc := newTestService(t, newFakeStore())
	// settings is nil — no SetSettingsClient call.

	req := &scenev1.ListScenesRequest{
		PlayerId:               "player-1",
		CharacterId:            "char-1",
		ExcludeContentWarnings: []string{"violence", "death"},
	}
	got := svc.resolveBlockedCW(context.Background(), req)
	sort.Strings(got)

	assert.Equal(t, []string{"death", "violence"}, got)
}

// TestListScenesExcludesUnionOfBlockedCWsAndKeepsNonOverlappingScene asserts
// that ListScenes passes the resolved blocked-CW union to BoardQuery.BlockedCW,
// and that a scene with a blocked CW tag is excluded while a scene without
// any overlap is kept. The kept scene still carries its content_warnings (INV-SCENE-56).
func TestListScenesExcludesUnionOfBlockedCWsAndKeepsNonOverlappingScene(t *testing.T) {
	store := newFakeStore()
	// fakeStore.ListBoard records the BlockedCW it received but does NOT filter
	// rows itself — the SQL exclusion is tested at the integration tier.
	// Here we assert that the correct BlockedCW set reaches the store.
	store.listBoardRows = []*SceneRow{
		{
			ID:              "scene-safe",
			Title:           "Safe Scene",
			OwnerID:         "owner-1",
			State:           string(SceneStateActive),
			Visibility:      "open",
			PoseOrder:       string(PoseOrderModeFree),
			ContentWarnings: []string{"romance"},
			Tags:            []string{},
			CreatedAt:       pgnanos.From(time.Now().UTC()),
		},
	}

	svc := newTestService(t, store)
	svc.settings = &scopedFakeSettingsClient{
		byScope: map[pluginsdk.SettingScope]scopedFakeOutcome{
			pluginsdk.SettingScopePlayer: {values: []string{"death"}, found: true},
		},
	}

	resp, err := svc.ListScenes(context.Background(), &scenev1.ListScenesRequest{
		PlayerId:    "player-1",
		CharacterId: "char-1",
	})
	require.NoError(t, err)

	// The blocked union {death} must have been passed to the store.
	require.NotNil(t, store.listBoardGot)
	assert.Equal(t, []string{"death"}, store.listBoardGot.BlockedCW)

	// The kept scene still carries its content_warnings (INV-SCENE-56).
	require.Len(t, resp.GetScenes(), 1)
	assert.Equal(t, "scene-safe", resp.GetScenes()[0].GetId())
	assert.Equal(t, []string{"romance"}, resp.GetScenes()[0].GetContentWarnings(),
		"INV-SCENE-56: content_warnings must not be stripped from the board response")
}

// TestListScenesPassesEmptyBlockedCWWhenNoBlocksAreConfigured asserts that
// when no scopes return blocks and no exclude_content_warnings is set, the
// BoardQuery.BlockedCW is nil/empty (no exclusion applied).
func TestListScenesPassesEmptyBlockedCWWhenNoBlocksAreConfigured(t *testing.T) {
	store := newFakeStore()
	store.listBoardRows = []*SceneRow{}

	svc := newTestService(t, store)
	svc.settings = &scopedFakeSettingsClient{byScope: map[pluginsdk.SettingScope]scopedFakeOutcome{}}

	_, err := svc.ListScenes(context.Background(), &scenev1.ListScenesRequest{
		PlayerId:    "player-1",
		CharacterId: "char-1",
	})
	require.NoError(t, err)

	require.NotNil(t, store.listBoardGot)
	assert.Empty(t, store.listBoardGot.BlockedCW,
		"no blocks configured → BlockedCW must be empty so IS NULL skips the filter")
}
