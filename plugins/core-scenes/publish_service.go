// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/pgnanos"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// GetPublishedScene returns a publication attempt's full state to a scene
// participant. It is the canonical INV-S9 participant-gated read, and its
// step ordering is LOAD-BEARING (INV-P6-5):
//
//	1. validate the caller_character_id;
//	2. read the header ONLY (no content_entries);
//	3. run the plugin-code IsParticipant gate — NO ABAC engine is consulted
//	   (INV-P6-6: SceneServiceImpl has no policy engine to call);
//	4. read content ONLY after the gate passes, and only for PUBLISHED rows.
//
// A non-participant is rejected with SCENE_PRIVACY_BOUNDARY_BLOCK BEFORE any
// content is read, and the triple-signal (slog WARN + metric + span error,
// but NO IC event) fires via emitPrivacyBoundaryBlock. See spec §9.1, §10.
func (s *SceneServiceImpl) GetPublishedScene(ctx context.Context, req *scenev1.GetPublishedSceneRequest) (*scenev1.GetPublishedSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.get_published_scene",
		attribute.String("published_scene_id", req.GetPublishedSceneId()))
	defer span.End()

	// Step 1 — caller validation.
	callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	// Step 2 — header read (NO content_entries).
	pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	if pub == nil {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_NOT_FOUND").
			Errorf("publication not found"))
	}

	// Step 3 — INV-S9 plugin-code participant gate. No ABAC engine is
	// consulted; the deny path runs BEFORE any content read (INV-P6-5).
	ok, err := s.store.IsParticipant(ctx, pub.SceneID, callerID)
	if err != nil {
		return nil, internalErr(ctx, err)
	}
	if !ok {
		s.emitPrivacyBoundaryBlock(ctx, "GetPublishedScene", pub.SceneID, callerID, "not_participant")
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PRIVACY_BOUNDARY_BLOCK").
			Errorf("scene not accessible"))
	}

	// Step 4 — content read, gated: only after the participant check and
	// only when the attempt has actually PUBLISHED.
	var entries []PublishedSceneEntry
	if pub.Status == StatusPublished {
		entries, err = s.store.GetPublishedSceneContent(ctx, req.GetPublishedSceneId())
		if err != nil {
			return nil, internalErr(ctx, err)
		}
	}

	tally, err := s.store.TallyVotes(ctx, pub.ID)
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	return assembleParticipantResponse(pub, entries, tally), nil
}

// emitPrivacyBoundaryBlock fires the spec §10 triple-signal for an INV-S9
// hard-privacy-boundary denial: a WARN log, a metric, and a span error. It
// deliberately emits NO IC event — a denial must not surface on any scene
// stream that could leak the attempt's existence to a non-participant.
func (s *SceneServiceImpl) emitPrivacyBoundaryBlock(ctx context.Context, operation, sceneID, callerID, reason string) {
	slog.WarnContext(ctx, "scene privacy boundary block",
		"operation", operation,
		"scene_id", sceneID,
		"caller_id", callerID,
		"denial_reason", reason,
		"code", "SCENE_PRIVACY_BOUNDARY_BLOCK")
	metricScenePublishPrivacyBlock(operation, reason)
	span := trace.SpanFromContext(ctx)
	span.SetStatus(otelcodes.Error, "denied")
	span.SetAttributes(attribute.String("deny.reason", reason))
}

// assembleParticipantResponse maps the participant-visible publication state
// to the wire response. entries is empty unless the attempt is PUBLISHED.
func assembleParticipantResponse(pub *PublishedScene, entries []PublishedSceneEntry, tally *VoteTally) *scenev1.GetPublishedSceneResponse {
	resp := &scenev1.GetPublishedSceneResponse{
		Id:                     pub.ID,
		SceneId:                pub.SceneID,
		AttemptNumber:          int32(pub.AttemptNumber), //nolint:gosec // attempt_number is bounded by max_publish_attempts (single digits); never overflows int32
		Status:                 string(pub.Status),
		ParticipantsSnapshot:   pub.ParticipantsSnapshot,
		InitiatedAtUnixNs:      pub.InitiatedAt.Time().UnixNano(), // pgnanos-exempt: proto int64 *_unix_ns wire field; serializing a pgnanos.Time, not a DB scan/insert seam (INV-TS-2 targets the DB seam)
		CooloffStartedAtUnixNs: unixNanoOrZero(pub.CoolOffStartedAt),
		ResolvedAtUnixNs:       unixNanoOrZero(pub.ResolvedAt),
		PublishedAtUnixNs:      unixNanoOrZero(pub.PublishedAt),
	}
	if pub.FailureReason != nil {
		resp.FailureReason = string(*pub.FailureReason)
	}
	if pub.TitleSnapshot != nil {
		resp.TitleSnapshot = *pub.TitleSnapshot
	}
	if tally != nil {
		resp.Tally = &scenev1.PublishedSceneVoteSummary{
			Yes:     int32(tally.Yes),     //nolint:gosec // vote counts are bounded by roster size; never overflows int32
			No:      int32(tally.No),      //nolint:gosec // vote counts are bounded by roster size; never overflows int32
			Pending: int32(tally.Pending), //nolint:gosec // vote counts are bounded by roster size; never overflows int32
		}
	}
	for _, e := range entries {
		resp.ContentEntries = append(resp.ContentEntries, &scenev1.PublishedSceneEntry{
			Speaker: e.Speaker,
			Kind:    string(e.Kind),
			Content: e.Content,
		})
	}
	return resp
}

// unixNanoOrZero renders a nullable epoch-nanosecond timestamp as int64,
// using 0 for an unset (nil) value.
func unixNanoOrZero(t *pgnanos.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Time().UnixNano() // pgnanos-exempt: proto int64 *_unix_ns wire field; serializing a pgnanos.Time, not a DB scan/insert seam (INV-TS-2 targets the DB seam)
}
