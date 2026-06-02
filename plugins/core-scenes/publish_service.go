// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"
	"time"

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
//
// dimension, scene_publish_privacy_block_total{reason}); only "not_participant"
// exists today, but future gate failures pass other reasons — keep it variable.
//
//nolint:unparam // reason is the §10 denial-reason taxonomy (a labeled metric
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
		InitiatedAtUnixNs:      pub.InitiatedAt.Time().UnixNano(), // pgnanos-exempt: proto int64 *_unix_ns wire field; serializing a pgnanos.Time, not a DB scan/insert seam (INV-STORE-2 targets the DB seam)
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
	return t.Time().UnixNano() // pgnanos-exempt: proto int64 *_unix_ns wire field; serializing a pgnanos.Time, not a DB scan/insert seam (INV-STORE-2 targets the DB seam)
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

	content, err := renderPublishedScene(req.GetFormat(), entries)
	if err != nil {
		return nil, internalErr(ctx, err)
	}
	return &scenev1.DownloadPublishedSceneResponse{
		Content:  content,
		MimeType: mime,
	}, nil
}

// renderPublishedScene renders content entries to bytes for the given format
// (markdown C1, plain_text C2, jsonl C3 — spec §12). The format MUST already be
// validated against publishRenderMime by the caller; the default case is a
// defensive guard for that contract. Only jsonl can error (json.Marshal), and
// that error is propagated rather than swallowed.
func renderPublishedScene(format string, entries []PublishedSceneEntry) ([]byte, error) {
	switch format {
	case "markdown":
		return []byte(renderMarkdown(entries)), nil
	case "plain_text":
		return []byte(renderPlainText(entries)), nil
	case "jsonl":
		return renderJSONL(entries)
	default:
		return nil, oops.Code("SCENE_PUBLISH_FORMAT_UNSUPPORTED").
			With("format", format).Errorf("unsupported download format")
	}
}

// GetPublicSceneArchive is the PUBLIC, unauthenticated read of a published
// scene. It is structurally separate from GetPublishedScene and shares no code
// path with it (ADR holomush-qd3r5): there is NO caller validation, NO
// participant gate, and NO ABAC. The only gate is status==PUBLISHED; every
// other case — a nonexistent id and any non-PUBLISHED attempt (COLLECTING /
// COOLOFF / ATTEMPT_FAILED) — returns the single opaque NOT_FOUND so a caller
// cannot infer that an attempt exists or is in progress (INV-P6-8). The public
// response carries only the published artifact (title, participant names,
// content, published_at) — never vote state or per-voter data (spec §5.1).
func (s *SceneServiceImpl) GetPublicSceneArchive(ctx context.Context, req *scenev1.GetPublicSceneArchiveRequest) (*scenev1.GetPublicSceneArchiveResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.get_public_scene_archive",
		attribute.String("published_scene_id", req.GetPublishedSceneId()))
	defer span.End()

	pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, internalErr(ctx, err)
	}
	// pub == nil is "missing" (GetPublishedSceneHeader returns nil,nil on miss);
	// any non-PUBLISHED status is treated identically. Both → opaque NOT_FOUND.
	if pub == nil || pub.Status != StatusPublished {
		return nil, publicArchiveNotFound()
	}

	entries, err := s.store.GetPublishedSceneContent(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, internalErr(ctx, err)
	}

	return assemblePublicResponse(pub, entries), nil
}

// assemblePublicResponse maps a PUBLISHED scene to the public archive response.
// It includes ONLY public-safe fields — no tally, no per-voter data, no
// failure_reason — per the §5.1 two-pair separation.
func assemblePublicResponse(pub *PublishedScene, entries []PublishedSceneEntry) *scenev1.GetPublicSceneArchiveResponse {
	resp := &scenev1.GetPublicSceneArchiveResponse{
		Id:                   pub.ID,
		ParticipantsSnapshot: pub.ParticipantsSnapshot,
		PublishedAtUnixNs:    unixNanoOrZero(pub.PublishedAt),
	}
	if pub.TitleSnapshot != nil {
		resp.TitleSnapshot = *pub.TitleSnapshot
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

// DownloadPublicSceneArchive is the PUBLIC, unauthenticated download of a
// published scene rendered to the requested format (markdown / plain_text /
// jsonl). Same status-gate + opacity contract as GetPublicSceneArchive
// (INV-P6-8): no caller validation, no participant gate, no ABAC; the only gate
// is status==PUBLISHED, and a missing id or any non-PUBLISHED attempt returns
// the single opaque NOT_FOUND. Shares the renderer code path with
// DownloadPublishedScene (spec §12). Format is validated first (a
// resource-independent client error that leaks nothing about existence).
func (s *SceneServiceImpl) DownloadPublicSceneArchive(ctx context.Context, req *scenev1.DownloadPublicSceneArchiveRequest) (*scenev1.DownloadPublicSceneArchiveResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.download_public_scene_archive",
		attribute.String("published_scene_id", req.GetPublishedSceneId()))
	defer span.End()

	mime, ok := publishRenderMime[req.GetFormat()]
	if !ok {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_FORMAT_UNSUPPORTED").
			With("format", req.GetFormat()).Errorf("unsupported download format"))
	}

	pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, internalErr(ctx, err)
	}
	// Public opacity (INV-P6-8): missing OR any non-PUBLISHED status → uniform
	// NOT_FOUND, identical to GetPublicSceneArchive.
	if pub == nil || pub.Status != StatusPublished {
		return nil, publicArchiveNotFound()
	}

	entries, err := s.store.GetPublishedSceneContent(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, internalErr(ctx, err)
	}

	content, err := renderPublishedScene(req.GetFormat(), entries)
	if err != nil {
		return nil, internalErr(ctx, err)
	}
	return &scenev1.DownloadPublicSceneArchiveResponse{
		Content:  content,
		MimeType: mime,
	}, nil
}

// ListScenePublishAttempts returns the audit list of a scene's publish
// attempts to a participant. Participant-gated (INV-S9, plugin-code, NO ABAC):
// caller validation → IsParticipant gate on the scene → list. The summaries
// carry NO content_entries (header only), so there is no content-read step;
// the gate still runs first so a non-participant cannot enumerate the attempts
// (SCENE_PRIVACY_BOUNDARY_BLOCK + triple-signal). See spec §5, §9.1.
func (s *SceneServiceImpl) ListScenePublishAttempts(ctx context.Context, req *scenev1.ListScenePublishAttemptsRequest) (*scenev1.ListScenePublishAttemptsResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.list_scene_publish_attempts",
		attribute.String("scene_id", req.GetSceneId()))
	defer span.End()

	callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	ok, err := s.store.IsParticipant(ctx, req.GetSceneId(), callerID)
	if err != nil {
		return nil, internalErr(ctx, err)
	}
	if !ok {
		s.emitPrivacyBoundaryBlock(ctx, "ListScenePublishAttempts", req.GetSceneId(), callerID, "not_participant")
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PRIVACY_BOUNDARY_BLOCK").
			Errorf("scene not accessible"))
	}

	attempts, err := s.store.ListSceneAttempts(ctx, req.GetSceneId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	return assembleAttemptsResponse(attempts), nil
}

// assembleAttemptsResponse maps attempt headers to the summary list. Summaries
// carry no content (id / attempt_number / status / failure_reason / timestamps).
func assembleAttemptsResponse(attempts []PublishedScene) *scenev1.ListScenePublishAttemptsResponse {
	resp := &scenev1.ListScenePublishAttemptsResponse{}
	for i := range attempts {
		a := &attempts[i]
		summary := &scenev1.PublishedSceneSummary{
			Id:                a.ID,
			AttemptNumber:     int32(a.AttemptNumber), //nolint:gosec // attempt_number bounded by max_publish_attempts; never overflows int32
			Status:            string(a.Status),
			InitiatedAtUnixNs: a.InitiatedAt.Time().UnixNano(), // pgnanos-exempt: proto int64 *_unix_ns wire field; serializing a pgnanos.Time, not a DB scan/insert seam (INV-STORE-2 targets the DB seam)
			ResolvedAtUnixNs:  unixNanoOrZero(a.ResolvedAt),
		}
		if a.FailureReason != nil {
			summary.FailureReason = string(*a.FailureReason)
		}
		resp.Attempts = append(resp.Attempts, summary)
	}
	return resp
}

// ExtendScenePublishVoteAttempts bumps a scene's max_publish_attempts budget by
// the requested amount and emits scene_publish_vote_attempts_extended. It is
// admin-only, but that is enforced by the host's ABAC `admin-extend-publish-
// attempts` policy at command dispatch (spec §8, "ABAC-gated, not name-gated").
// Like every other scene RPC, this handler trusts the host's authorization and
// performs NO in-plugin role check — admin is ABAC-gated, never a plugin-code
// gate (the inverse of INV-S9's hard privacy boundary, which is plugin-code by
// design). Adding an in-handler role check here would create a runtime
// privilege gradient and duplicate the policy engine.
func (s *SceneServiceImpl) ExtendScenePublishVoteAttempts(ctx context.Context, req *scenev1.ExtendScenePublishVoteAttemptsRequest) (*scenev1.ExtendScenePublishVoteAttemptsResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.extend_scene_publish_vote_attempts",
		attribute.String("scene_id", req.GetSceneId()))
	defer span.End()

	additional := int(req.GetAdditional())
	if additional <= 0 {
		// A non-positive extension is a client error: it would decrement or
		// no-op the budget. Reject before any write.
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_EXTEND_INVALID").
			With("additional", additional).Errorf("extension count must be positive"))
	}

	newMax, err := s.store.ExtendMaxPublishAttempts(ctx, req.GetSceneId(), additional)
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	// The budget bump is committed; the notice event is best-effort. Treating an
	// emit failure as fatal here would be worse than warn-and-continue: the
	// caller would retry a non-idempotent mutation and double-bump. Mirror
	// StartScenePublish's warn-and-continue.
	if emitErr := s.events.emitAttemptsExtended(ctx, req.GetSceneId(), req.GetCallerCharacterId(), additional, newMax); emitErr != nil {
		slog.WarnContext(ctx, "publish-vote-attempts-extended emit failed", "err", emitErr.Error())
	}
	return &scenev1.ExtendScenePublishVoteAttemptsResponse{
		NewMax: int32(newMax), //nolint:gosec // max_publish_attempts is a small budget; never overflows int32
	}, nil
}

// CastPublishSceneVote records a roster member's vote on an active publication
// attempt and runs the §4.3 resolution check, which may transition the attempt
// (COLLECTING→COOLOFF on all-yes, COLLECTING→ATTEMPT_FAILED on
// any-no-after-all-voted, or COOLOFF→COLLECTING on a flip to no). A vote on a
// terminal attempt is rejected. The vote is the durable effect; a failed
// resolution check or emit is logged but does NOT fail the cast — the next cast
// or scheduler tick re-evaluates.
func (s *SceneServiceImpl) CastPublishSceneVote(ctx context.Context, req *scenev1.CastPublishSceneVoteRequest) (*scenev1.CastPublishSceneVoteResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.cast_publish_scene_vote",
		attribute.String("published_scene_id", req.GetPublishedSceneId()))
	defer span.End()

	callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	if pub == nil {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_NOT_FOUND").
			Errorf("publication attempt not found"))
	}
	if pub.Status.IsTerminal() {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_INVALID_STATE").
			With("status", string(pub.Status)).
			Errorf("vote on a terminal publication attempt is rejected"))
	}

	result, err := s.store.CastVote(ctx, pub.ID, callerID, req.GetVote())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	// §4.3 resolution check. A failure here does NOT fail the vote — the vote is
	// already durable; the next cast or scheduler tick re-evaluates.
	if resErr := s.applyResolution(ctx, pub); resErr != nil {
		slog.WarnContext(ctx, "publish-vote resolution check failed",
			"err", resErr.Error(), "attempt_id", pub.ID)
	}

	metricScenePublishVoteCast(boolLabel(result.Vote), boolLabel(result.IsChange))
	if emitErr := s.events.emitVoteCast(ctx, pub.ID, callerID, result); emitErr != nil {
		slog.WarnContext(ctx, "vote-cast emit failed", "err", emitErr.Error(), "attempt_id", pub.ID)
	}

	return &scenev1.CastPublishSceneVoteResponse{IsChange: result.IsChange}, nil
}

// applyResolution evaluates the post-cast tally and applies any resulting
// transition (spec §4.3). pub carries the pre-cast status (the vote itself does
// not change status). From COLLECTING, ResolveFromTally decides; from COOLOFF a
// single no flips back to COLLECTING. COOLOFF→PUBLISHED is the snapshot
// pipeline's (C7), never triggered by a vote.
func (s *SceneServiceImpl) applyResolution(ctx context.Context, pub *PublishedScene) error {
	tally, err := s.store.TallyVotes(ctx, pub.ID)
	if err != nil {
		return err //nolint:wrapcheck // internal helper; the caller (CastPublishSceneVote) logs this best-effort error, never returns it across the gRPC boundary
	}
	switch pub.Status {
	case StatusCollecting:
		trig, ok := ResolveFromTally(*tally)
		if !ok {
			return nil
		}
		return s.applyTrigger(ctx, pub.ID, trig)
	case StatusCoolOff:
		if tally.No > 0 {
			return s.applyTrigger(ctx, pub.ID, TriggerFlipNo)
		}
	}
	return nil
}

// applyTrigger drives the DB state transition for an attempt: re-read the row,
// compute the next status via the pure NextStatus helper, and apply the side
// effects (TransitionStatus + metric + best-effort emit). Snapshot and
// cool-off-invariant triggers are NOT applicable here — they carry no
// trigger-derivable failure_reason and are applied by the snapshot pipeline
// (C7) directly.
func (s *SceneServiceImpl) applyTrigger(ctx context.Context, attemptID string, t PublishTrigger) error {
	pub, err := s.store.GetPublishedSceneHeader(ctx, attemptID)
	if err != nil {
		return err //nolint:wrapcheck // internal helper; the caller logs this best-effort error, never returns it across the gRPC boundary
	}
	if pub == nil {
		return oops.Code("SCENE_PUBLISH_NOT_FOUND").With("attempt_id", attemptID).
			Errorf("attempt vanished before transition")
	}
	next, ok := NextStatus(pub.Status, t)
	if !ok {
		return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").
			With("from", string(pub.Status)).With("trigger", string(t)).
			Errorf("illegal transition")
	}

	in := TransitionInput{To: next, Resolved: next.IsTerminal()}
	switch next {
	case StatusAttemptFailed:
		reason, hasReason := FailureReasonForTrigger(t)
		if !hasReason {
			return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").
				With("trigger", string(t)).
				Errorf("trigger carries no failure_reason; not applicable to applyTrigger")
		}
		in.FailureReason = &reason
	case StatusCoolOff:
		now := time.Now()
		in.SetCoolOffAt = &now
	case StatusCollecting:
		if pub.Status == StatusCoolOff {
			in.ClearCoolOff = true
		}
	}

	if err := s.store.TransitionStatus(ctx, attemptID, in); err != nil {
		return err //nolint:wrapcheck // internal helper; the caller logs this best-effort error, never returns it across the gRPC boundary
	}

	// Observability — best-effort; an emit failure logs but never rolls back
	// the committed transition (the DB is the source of truth).
	if next.IsTerminal() {
		reasonLabel := ""
		if in.FailureReason != nil {
			reasonLabel = string(*in.FailureReason)
		}
		metricScenePublishAttemptResolved(string(next), reasonLabel)
	}
	if next == StatusCoolOff {
		if emitErr := s.events.emitCoolOffStarted(ctx, attemptID, pub.CoolOffWindow); emitErr != nil {
			slog.WarnContext(ctx, "cooloff-started emit failed",
				"err", emitErr.Error(), "attempt_id", attemptID)
		}
	}
	if next.IsTerminal() {
		tally, tErr := s.store.TallyVotes(ctx, attemptID)
		if tErr != nil {
			slog.WarnContext(ctx, "tally for resolved emit failed",
				"err", tErr.Error(), "attempt_id", attemptID)
		}
		if emitErr := s.events.emitResolved(ctx, attemptID, next, in.FailureReason, tally); emitErr != nil {
			slog.WarnContext(ctx, "resolved emit failed",
				"err", emitErr.Error(), "attempt_id", attemptID)
		}
	}
	return nil
}

// WithdrawScenePublish lets the scene owner abandon an active publication
// attempt (COLLECTING or COOLOFF), transitioning it to ATTEMPT_FAILED with
// failure_reason WITHDRAWN (spec §4.1, §5). Owner-gated by the
// withdraw-publish-as-owner ABAC policy at command dispatch (spec §8). The
// handler ALSO performs a defense-in-depth owner check and returns
// SCENE_PUBLISH_NOT_OWNER (PermissionDenied) for a non-owner: unlike the
// admin-extend case (E1), the plugin holds the scene's owner attribute, so the
// check is feasible AND closes the direct-RPC gap (spec §5 documents
// SCENE_PUBLISH_NOT_OWNER for exactly this; the predicate is identical to the
// policy, so there is no drift). Emits scene_publish_withdrawn (carrying
// withdrawn_by) alongside the scene_publish_resolved that applyTrigger fires,
// so renderers can distinguish a withdrawal from a vote failure (spec §7).
func (s *SceneServiceImpl) WithdrawScenePublish(ctx context.Context, req *scenev1.WithdrawScenePublishRequest) (*scenev1.WithdrawScenePublishResponse, error) {
	ctx, span := startSpan(ctx, "scene.service.withdraw_scene_publish",
		attribute.String("published_scene_id", req.GetPublishedSceneId()))
	defer span.End()

	callerID, err := parseCallerCharacterID(req.GetCallerCharacterId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	pub, err := s.store.GetPublishedSceneHeader(ctx, req.GetPublishedSceneId())
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	if pub == nil {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_NOT_FOUND").
			Errorf("publication attempt not found"))
	}
	if pub.Status.IsTerminal() {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_INVALID_STATE").
			With("status", string(pub.Status)).
			Errorf("publication attempt is already terminal"))
	}

	scene, err := s.store.Get(ctx, pub.SceneID)
	if err != nil {
		return nil, mapStoreErr(ctx, err)
	}
	if scene.OwnerID != callerID {
		return nil, mapStoreErr(ctx, oops.Code("SCENE_PUBLISH_NOT_OWNER").
			With("scene_id", pub.SceneID).
			Errorf("only the scene owner may withdraw a publication attempt"))
	}

	if err := s.applyTrigger(ctx, pub.ID, TriggerWithdraw); err != nil {
		return nil, mapStoreErr(ctx, err)
	}

	if emitErr := s.events.emitWithdrawn(ctx, pub.ID, callerID); emitErr != nil {
		slog.WarnContext(ctx, "publish-withdrawn emit failed",
			"err", emitErr.Error(), "attempt_id", pub.ID)
	}

	return &scenev1.WithdrawScenePublishResponse{}, nil
}
