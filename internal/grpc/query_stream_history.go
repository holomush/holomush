// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

const (
	// defaultHistoryPageSize is the page size used when the client requests 0.
	defaultHistoryPageSize = 150
	// maxHistoryPageSize is the server-side cap on per-page count.
	maxHistoryPageSize = 500
)

// QueryStreamHistory implements CoreServiceServer.QueryStreamHistory.
//
// Two-layer authorization:
//   - Private streams (character:*, scene:*:ic, scene:*:ooc): membership gate
//     via sessionHasMembership (invariant I-17). This is a HARD GATE, not a
//     policy — the ABAC engine is NEVER consulted for private streams, and
//     there is no admin override.
//   - Public streams (location:*, global, etc.): ABAC engine.Evaluate.
//
// Pure read — does not mutate session cursors (invariant I-13).
func (s *CoreServer) QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(ctx, "query stream history",
		"request_id", requestID,
		"session_id", req.SessionId,
		"stream", req.Stream,
	)

	// Step 0: Guard — eventStore must be configured.
	if s.eventStore == nil {
		return nil, oops.Code("INTERNAL").Errorf("event store not configured")
	}

	// Step 1: Validate session_id and load session.
	if req.SessionId == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id is required")
	}
	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		if oopsErr, ok := oops.AsOops(err); ok && oopsErr.Code() == "SESSION_NOT_FOUND" {
			return nil, oops.Code("SESSION_NOT_FOUND").
				With("session_id", req.SessionId).
				Errorf("session not found")
		}
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	if info.IsExpired() {
		return nil, oops.Code("SESSION_EXPIRED").
			With("session_id", req.SessionId).
			Errorf("session expired")
	}

	// Step 2: Validate stream.
	if req.Stream == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("stream is required")
	}

	// Step 3: Normalize count.
	count := int(req.Count)
	if count < 0 {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("count must be non-negative")
	}
	if count == 0 {
		count = defaultHistoryPageSize
	}
	if count > maxHistoryPageSize {
		count = maxHistoryPageSize
	}

	// Step 4: Parse before_id (empty = no cursor).
	var beforeID ulid.ULID
	if req.BeforeId != "" {
		parsed, parseErr := ulid.Parse(req.BeforeId)
		if parseErr != nil {
			return nil, oops.Code("INVALID_ARGUMENT").
				With("before_id", req.BeforeId).
				Errorf("before_id must be a valid ULID")
		}
		beforeID = parsed
	}

	// Step 5: Authorization — two-layer model.
	if isPrivateStream(req.Stream) {
		// Validate scene stream format up-front so malformed scene streams
		// surface as INVALID_ARGUMENT rather than STREAM_ACCESS_DENIED.
		if strings.HasPrefix(req.Stream, "scene:") {
			if _, keyErr := streamToFocusKey(req.Stream); keyErr != nil {
				return nil, keyErr
			}
		}
		// Layer 1: Membership gate (I-17). ABAC is never consulted for
		// private streams — no policy override is possible.
		if !sessionHasMembership(info, req.Stream) {
			slog.InfoContext(ctx, "stream access denied by I-17 membership gate",
				"session_id", req.SessionId,
				"character_id", info.CharacterID.String(),
				"stream", req.Stream,
			)
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", req.SessionId).
				With("stream", req.Stream).
				Errorf("not authorized to read stream")
		}
	} else {
		// Layer 2: ABAC policy for public streams.
		if s.accessEngine == nil {
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("stream", req.Stream).
				Errorf("access engine not configured")
		}
		accessReq, reqErr := accessTypes.NewAccessRequest(
			"character:"+info.CharacterID.String(),
			accessTypes.ActionRead,
			"stream:"+req.Stream,
		)
		if reqErr != nil {
			return nil, oops.Code("INTERNAL").Wrap(reqErr)
		}
		decision, evalErr := s.accessEngine.Evaluate(ctx, accessReq)
		if evalErr != nil {
			return nil, oops.Code("INTERNAL").
				With("stream", req.Stream).
				Wrap(evalErr)
		}
		if !decision.IsAllowed() {
			slog.InfoContext(ctx, "stream access denied by ABAC",
				"session_id", req.SessionId,
				"character_id", info.CharacterID.String(),
				"stream", req.Stream,
				"policy_id", decision.PolicyID(),
			)
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", req.SessionId).
				With("stream", req.Stream).
				Errorf("not authorized to read stream")
		}
	}

	// Step 6: Parse not_before.
	var notBefore time.Time
	if req.NotBeforeMs > 0 {
		notBefore = time.UnixMilli(req.NotBeforeMs).UTC()
	}

	// Step 7: Fetch count+1 to detect has_more.
	events, fetchErr := s.eventStore.ReplayTail(ctx, req.Stream, count+1, notBefore, beforeID)
	if fetchErr != nil {
		return nil, oops.Code("INTERNAL").
			With("stream", req.Stream).
			Wrap(fetchErr)
	}

	// Step 8: Build response. ReplayTail returns up to count+1 newest events
	// in ascending order. If we got more than count, the OLDEST (index 0) is
	// the "has_more indicator" — drop it, keep the newest `count` events.
	hasMore := len(events) > count
	if hasMore {
		events = events[len(events)-count:]
	}

	protoEvents := make([]*corev1.EventFrame, 0, len(events))
	for _, e := range events {
		protoEvents = append(protoEvents, coreEventToEventFrame(e))
	}

	return &corev1.QueryStreamHistoryResponse{
		Meta:    responseMeta(requestID),
		Events:  protoEvents,
		HasMore: hasMore,
	}, nil
}

// coreEventToEventFrame converts a core.Event to a proto EventFrame (bare,
// not wrapped in SubscribeResponse). Actor.Kind.String() matches the
// representation used by eventToProto for Subscribe responses.
func coreEventToEventFrame(e core.Event) *corev1.EventFrame {
	return &corev1.EventFrame{
		Id:        e.ID.String(),
		Stream:    e.Stream,
		Type:      string(e.Type),
		Timestamp: timestamppb.New(e.Timestamp),
		ActorType: e.Actor.Kind.String(),
		ActorId:   e.Actor.ID,
		Payload:   e.Payload,
	}
}
