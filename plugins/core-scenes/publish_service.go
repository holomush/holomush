// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"fmt"
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
//  1. validate the caller_character_id;
//  2. read the header ONLY (no content_entries);
//  3. run the plugin-code IsParticipant gate — NO ABAC engine is consulted
//     (INV-P6-6: SceneServiceImpl has no policy engine to call);
//  4. read content ONLY after the gate passes, and only for PUBLISHED rows.
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

// StartScenePublish opens a publication attempt for an ended scene. The
// precondition ladder (spec §5) runs before any write: the scene must exist
// and be in the 'ended' state, must not already have a published archive
// (one-and-done) nor an active attempt, and must not have exhausted its
// publish-attempt budget. CreatePublishAttempt then seeds the COLLECTING row
// and frozen vote roster transactionally. Errors route through mapStoreErr so
// known oops codes map to their semantic gRPC status and unmapped errors
// funnel to internalErr (.claude/rules/grpc-errors.md).
func (s *SceneServiceImpl) StartScenePublish(ctx context.Context, req *scenev1.StartScenePublishRequest) (*scenev1.StartScenePublishResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.start_scene_publish",
		attribute.String("scene_id", req.GetSceneId()))
	defer span.End()

	callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	// store.Get returns a SCENE_NOT_FOUND oops error on miss (never nil,nil),
	// which mapStoreErr maps to codes.NotFound.
	scene, err := s.store.Get(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	if scene.State != string(SceneStateEnded) {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_INVALID_STATE").
			With("scene_id", req.GetSceneId()).With("current_state", scene.State).
			Errorf("scene must be in 'ended' state to publish"))
	}

	counts, err := s.store.CountAttempts(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	if counts.Published > 0 {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_ALREADY_PUBLISHED").With("scene_id", req.GetSceneId()).Errorf("scene already has a published archive"))
	}
	if counts.Active > 0 {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_ALREADY_ACTIVE").With("scene_id", req.GetSceneId()).Errorf("scene already has an active attempt"))
	}
	maxAttempts, err := s.store.GetSceneMaxPublishAttempts(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	if counts.Total >= maxAttempts {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_ATTEMPTS_EXHAUSTED").
			With("scene_id", req.GetSceneId()).With("max_attempts", maxAttempts).
			Errorf("scene has exhausted its publish-attempt budget"))
	}

	pub, err := s.store.CreatePublishAttempt(ctx, CreatePublishAttemptInput{
		SceneID:       req.GetSceneId(),
		AttemptNumber: counts.Total + 1,
		InitiatedBy:   callerID,
		VoteWindow:    s.cfg.DefaultVoteWindow,
		CoolOffWindow: s.cfg.DefaultCoolOffWindow,
		MaxAttempts:   maxAttempts,
	})
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	metricScenePublishAttemptResolved("started", "")
	if emitErr := s.events.emitPublishStarted(ctx, pub); emitErr != nil {
		slog.WarnContext(ctx, "publish-started emit failed", "err", emitErr.Error())
	}

	return &scenev1.StartScenePublishResponse{
		PublishedSceneId: pub.ID,
		AttemptNumber:    int32(pub.AttemptNumber), //nolint:gosec // attempt_number bounded by max_publish_attempts; never overflows int32
	}, nil
}

// publishRenderMime maps a download format to its MIME type. The key set is
// the authoritative supported-format list; a format absent here yields
// SCENE_PUBLISH_FORMAT_UNSUPPORTED.
var publishRenderMime = map[string]string{
	"markdown":   "text/markdown",
	"plain_text": "text/plain",
	"jsonl":      "application/jsonl",
}

// DownloadPublishedScene returns a published scene rendered in the requested
// format to a participant. It uses the SAME load-bearing INV-S9 / INV-P6-5
// ordering as GetPublishedScene: caller validation → format validation →
// header read (no content) → IsParticipant gate (NO ABAC) → PUBLISHED check →
// content read only on gate pass → render. A non-participant is denied with
// SCENE_PRIVACY_BOUNDARY_BLOCK (triple-signal) before any content read. Only
// PUBLISHED attempts are downloadable (spec §5). See spec §9.1, §10.
func (s *SceneServiceImpl) DownloadPublishedScene(ctx context.Context, req *scenev1.DownloadPublishedSceneRequest) (*scenev1.DownloadPublishedSceneResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.download_published_scene",
		attribute.String("published_scene_id", req.GetPublishedSceneId()),
		attribute.String("format", req.GetFormat()))
	defer span.End()

	callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	// Format is a resource-independent client error — validate it before any
	// resource read. (Returning FORMAT_UNSUPPORTED here leaks nothing about
	// the publication's existence to a non-participant.)
	mime, ok := publishRenderMime[req.GetFormat()]
	if !ok {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_FORMAT_UNSUPPORTED").
			With("format", req.GetFormat()).Errorf("unsupported download format"))
	}

	pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	if pub == nil {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_NOT_FOUND").
			Errorf("publication not found"))
	}

	// INV-S9 plugin-code participant gate (NO ABAC); deny BEFORE any content
	// read (INV-P6-5).
	isParticipant, err := s.store.IsParticipant(ctx, pub.SceneID, callerID)
	if err != nil {
		return nil, internalErr(ctx, err)
	}
	if !isParticipant {
		s.emitPrivacyBoundaryBlock(ctx, "DownloadPublishedScene", pub.SceneID, callerID, "not_participant")
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PRIVACY_BOUNDARY_BLOCK").
			Errorf("scene not accessible"))
	}

	// Only a PUBLISHED attempt has downloadable content.
	if pub.Status != StatusPublished {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_INVALID_STATE").
			With("status", string(pub.Status)).
			Errorf("only a published scene can be downloaded"))
	}

	entries, err := s.store.GetPublishedSceneContent(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, internalErr(ctx, err)
	}

	return &scenev1.DownloadPublishedSceneResponse{
		Content:  renderPublishedScene(req.GetFormat(), entries),
		MimeType: mime,
	}, nil
}

// renderPublishedScene renders content entries to bytes for the given format.
// PLACEHOLDER: the real per-format renderers land in Phase C (C1 markdown, C2
// plain_text, C3 jsonl) and replace this body inline. The format MUST already
// be validated against publishRenderMime by the caller. The placeholder is
// deterministic and non-empty so the download path is exercisable end-to-end
// before Phase C.
func renderPublishedScene(format string, entries []PublishedSceneEntry) []byte {
	return []byte(fmt.Sprintf("scene publication: %d entries (%s rendering pending — Phase C)", len(entries), format))
}
