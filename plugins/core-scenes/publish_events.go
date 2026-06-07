// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"time"

	"github.com/samber/oops"
	"google.golang.org/protobuf/encoding/protojson"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// publishEventEmitter is the production publishEventer (interface defined in
// publish_helpers.go). Wraps EventSink + sceneStorer + gameID; the store
// reference lets emitPublishStarted load the voter roster without requiring
// the handler to pass it explicitly.
//
// Every event emits on events.<game_id>.scene.<scene_id>.ic with
// Sensitive:false — all Phase 6 publication notice events carry
// sensitivity:never per the crypto.emits manifest.
type publishEventEmitter struct {
	sink   pluginsdk.EventSink
	store  sceneStorer
	gameID string
}

// newPublishEventEmitter returns a publishEventEmitter backed by the given
// sink, store, and gameID.
func newPublishEventEmitter(sink pluginsdk.EventSink, store sceneStorer, gameID string) *publishEventEmitter {
	return &publishEventEmitter{sink: sink, store: store, gameID: gameID}
}

func (e *publishEventEmitter) emitPublishStarted(ctx context.Context, pub *PublishedScene) error {
	voters, err := e.store.ListPublishVoters(ctx, pub.ID)
	if err != nil {
		return err //nolint:wrapcheck // store error passes through as-is; handler wraps at gRPC boundary
	}
	roster := make([]string, 0, len(voters))
	for _, v := range voters {
		roster = append(roster, v.CharacterID)
	}
	payload, err := protojson.Marshal(&scenev1.ScenePublishStartedEvent{
		AttemptId:            pub.ID,
		AttemptNumber:        int32(pub.AttemptNumber), //nolint:gosec // bounded; never overflows int32
		InitiatedBy:          pub.InitiatedBy,
		VoteWindowSeconds:    int64(pub.VoteWindow.Seconds()),
		CooloffWindowSeconds: int64(pub.CoolOffWindow.Seconds()),
		RosterCharacterIds:   roster,
	})
	if err != nil {
		return err //nolint:wrapcheck // protojson.Marshal error passes through; structurally impossible on well-formed messages
	}
	return e.sink.Emit(ctx, pluginsdk.EmitIntent{ //nolint:wrapcheck // EventSink error passes through as-is; caller decides logging
		Subject:   dotStyleSceneSubjectIC(e.gameID, pub.SceneID),
		Type:      pluginsdk.EventType("core-scenes:scene_publish_started"),
		Payload:   string(payload),
		Sensitive: false,
	})
}

func (e *publishEventEmitter) emitVoteCast(ctx context.Context, attemptID, characterID string, result *CastVoteResult) error {
	pub, err := e.store.GetPublishedSceneHeader(ctx, attemptID)
	if err != nil {
		return err //nolint:wrapcheck // store error passes through as-is; handler wraps at gRPC boundary
	}
	if pub == nil {
		return oops.Code("SCENE_PUBLISH_NOT_FOUND").With("attempt_id", attemptID).
			Errorf("publish attempt not found for event emission")
	}
	payload, err := protojson.Marshal(&scenev1.ScenePublishVoteCastEvent{
		AttemptId:   attemptID,
		CharacterId: characterID,
		Vote:        result.Vote,
		IsChange:    result.IsChange,
	})
	if err != nil {
		return err //nolint:wrapcheck // protojson.Marshal error passes through; structurally impossible on well-formed messages
	}
	return e.sink.Emit(ctx, pluginsdk.EmitIntent{ //nolint:wrapcheck // EventSink error passes through as-is; caller decides logging
		Subject:   dotStyleSceneSubjectIC(e.gameID, pub.SceneID),
		Type:      pluginsdk.EventType("core-scenes:scene_publish_vote_cast"),
		Payload:   string(payload),
		Sensitive: false,
	})
}

func (e *publishEventEmitter) emitCoolOffStarted(ctx context.Context, attemptID string, window time.Duration) error {
	pub, err := e.store.GetPublishedSceneHeader(ctx, attemptID)
	if err != nil {
		return err //nolint:wrapcheck // store error passes through as-is; handler wraps at gRPC boundary
	}
	if pub == nil {
		return oops.Code("SCENE_PUBLISH_NOT_FOUND").With("attempt_id", attemptID).
			Errorf("publish attempt not found for event emission")
	}
	// Compute the cool-off deadline from the persisted transition time so the
	// emitted deadline is deterministic under retry/delay rather than recomputed
	// from wall-clock at emit time. cooloff_started_at is always set for COOLOFF.
	cooloffEndsAtUnixNs := time.Now().Add(window).UnixNano() // pgnanos-exempt: proto event field (cooloff_ends_at_unix_ns); computed wire time, not a DB scan/insert seam
	if pub.CoolOffStartedAt != nil {
		cooloffEndsAtUnixNs = pub.CoolOffStartedAt.Time().Add(window).UnixNano() // pgnanos-exempt: same proto event field, derived from the persisted pgnanos.Time
	}
	payload, err := protojson.Marshal(&scenev1.ScenePublishCoolOffStartedEvent{
		AttemptId:           attemptID,
		CooloffEndsAtUnixNs: cooloffEndsAtUnixNs,
	})
	if err != nil {
		return err //nolint:wrapcheck // protojson.Marshal error passes through; structurally impossible on well-formed messages
	}
	return e.sink.Emit(ctx, pluginsdk.EmitIntent{ //nolint:wrapcheck // EventSink error passes through as-is; caller decides logging
		Subject:   dotStyleSceneSubjectIC(e.gameID, pub.SceneID),
		Type:      pluginsdk.EventType("core-scenes:scene_publish_cooloff_started"),
		Payload:   string(payload),
		Sensitive: false,
	})
}

func (e *publishEventEmitter) emitResolved(ctx context.Context, attemptID string, finalStatus PublishedSceneStatus, reason *PublishFailureReason, tally *VoteTally) error {
	pub, err := e.store.GetPublishedSceneHeader(ctx, attemptID)
	if err != nil {
		return err //nolint:wrapcheck // store error passes through as-is; handler wraps at gRPC boundary
	}
	if pub == nil {
		return oops.Code("SCENE_PUBLISH_NOT_FOUND").With("attempt_id", attemptID).
			Errorf("publish attempt not found for event emission")
	}
	reasonStr := ""
	if reason != nil {
		reasonStr = string(*reason)
	}
	var y, n, p int32
	if tally != nil {
		y = int32(tally.Yes)     //nolint:gosec // bounded; never overflows int32
		n = int32(tally.No)      //nolint:gosec // bounded; never overflows int32
		p = int32(tally.Pending) //nolint:gosec // bounded; never overflows int32
	}
	payload, err := protojson.Marshal(&scenev1.ScenePublishResolvedEvent{
		AttemptId:     attemptID,
		Outcome:       string(finalStatus),
		FailureReason: reasonStr,
		TallyYes:      y,
		TallyNo:       n,
		TallyPending:  p,
	})
	if err != nil {
		return err //nolint:wrapcheck // protojson.Marshal error passes through; structurally impossible on well-formed messages
	}
	return e.sink.Emit(ctx, pluginsdk.EmitIntent{ //nolint:wrapcheck // EventSink error passes through as-is; caller decides logging
		Subject:   dotStyleSceneSubjectIC(e.gameID, pub.SceneID),
		Type:      pluginsdk.EventType("core-scenes:scene_publish_resolved"),
		Payload:   string(payload),
		Sensitive: false,
	})
}

func (e *publishEventEmitter) emitWithdrawn(ctx context.Context, attemptID, withdrawnBy string) error {
	pub, err := e.store.GetPublishedSceneHeader(ctx, attemptID)
	if err != nil {
		return err //nolint:wrapcheck // store error passes through as-is; handler wraps at gRPC boundary
	}
	if pub == nil {
		return oops.Code("SCENE_PUBLISH_NOT_FOUND").With("attempt_id", attemptID).
			Errorf("publish attempt not found for event emission")
	}
	payload, err := protojson.Marshal(&scenev1.ScenePublishWithdrawnEvent{
		AttemptId:   attemptID,
		WithdrawnBy: withdrawnBy,
	})
	if err != nil {
		return err //nolint:wrapcheck // protojson.Marshal error passes through; structurally impossible on well-formed messages
	}
	return e.sink.Emit(ctx, pluginsdk.EmitIntent{ //nolint:wrapcheck // EventSink error passes through as-is; caller decides logging
		Subject:   dotStyleSceneSubjectIC(e.gameID, pub.SceneID),
		Type:      pluginsdk.EventType("core-scenes:scene_publish_withdrawn"),
		Payload:   string(payload),
		Sensitive: false,
	})
}

func (e *publishEventEmitter) emitAttemptsExtended(ctx context.Context, sceneID, adminID string, additional, newMax int) error {
	payload, err := protojson.Marshal(&scenev1.ScenePublishVoteAttemptsExtendedEvent{
		SceneId:    sceneID,
		AdminId:    adminID,
		Additional: int32(additional), //nolint:gosec // bounded; never overflows int32
		NewMax:     int32(newMax),     //nolint:gosec // bounded; never overflows int32
	})
	if err != nil {
		return err //nolint:wrapcheck // protojson.Marshal error passes through; structurally impossible on well-formed messages
	}
	return e.sink.Emit(ctx, pluginsdk.EmitIntent{ //nolint:wrapcheck // EventSink error passes through as-is; caller decides logging
		Subject:   dotStyleSceneSubjectIC(e.gameID, sceneID),
		Type:      pluginsdk.EventType("core-scenes:scene_publish_vote_attempts_extended"),
		Payload:   string(payload),
		Sensitive: false,
	})
}
