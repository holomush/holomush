// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/subjectxlate"
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

	// Step 0: Guard — historyReader must be configured.
	if s.historyReader == nil {
		return nil, oops.Code("INTERNAL").Errorf("history reader not configured")
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
	// Delegate to the JetStream/PostgreSQL tier crossover reader (F4+).
	// Both paths produce ascending (oldest→newest) slices of count+1 events maximum.
	frames, fetchErr := fetchHistoryFramesFromBus(ctx, s.historyReader, s.currentGameID(), req.Stream, count, notBefore, beforeID)
	if fetchErr != nil {
		return nil, oops.Code("INTERNAL").
			With("stream", req.Stream).
			Wrap(fetchErr)
	}
	protoFrames := frames

	// Step 8: Build response. Results are ascending (oldest→newest).
	// If we got more than count, the OLDEST (index 0) is the "has_more
	// indicator" — drop it, keep the newest `count` events.
	hasMore := len(protoFrames) > count
	if hasMore {
		protoFrames = protoFrames[len(protoFrames)-count:]
	}

	return &corev1.QueryStreamHistoryResponse{
		Meta:    responseMeta(requestID),
		Events:  protoFrames,
		HasMore: hasMore,
	}, nil
}

// fetchHistoryFramesFromBus fetches count+1 events from the HistoryReader and
// returns them as proto EventFrames in ascending ULID order (oldest→newest),
// matching the legacy ReplayTail wire shape expected by the response builder.
//
// Direction is Backward (newest-first) so the reader returns the tail of the
// stream relative to beforeID (or the absolute end when beforeID is zero).
// The slice is reversed on return to restore ascending order.
func fetchHistoryFramesFromBus(
	ctx context.Context,
	reader eventbus.HistoryReader,
	gameID, legacyStream string,
	count int,
	notBefore time.Time,
	beforeID ulid.ULID,
) ([]*corev1.EventFrame, error) {
	natsSubject, err := subjectxlate.Legacy(legacyStream, gameID)
	if err != nil {
		return nil, oops.With("stream", legacyStream).Wrap(err)
	}
	sub, err := eventbus.NewSubject(natsSubject)
	if err != nil {
		return nil, oops.With("stream", legacyStream).Wrap(err)
	}

	q := eventbus.HistoryQuery{
		Subject:   sub,
		Direction: eventbus.DirectionBackward,
		PageSize:  count + 1,
		NotBefore: notBefore,
	}
	if !beforeID.IsZero() {
		q.Before = beforeID
	}

	stream, err := reader.QueryHistory(ctx, q)
	if err != nil {
		return nil, oops.With("subject", string(sub)).Wrap(err)
	}
	defer stream.Close() //nolint:errcheck // best-effort iterator close

	// Drain up to count+1 events. DirectionBackward gives newest-first;
	// we collect them and reverse below to restore ascending order.
	collected := make([]eventbus.Event, 0, count+1)
	for {
		e, nextErr := stream.Next(ctx)
		if nextErr != nil {
			if nextErr == io.EOF { //nolint:errorlint // io.EOF is a sentinel value
				break
			}
			return nil, oops.With("subject", string(sub)).Wrap(nextErr)
		}
		collected = append(collected, e)
		if len(collected) >= count+1 {
			break
		}
	}

	// Convert bus events → proto frames. Reverse from newest-first
	// (Backward) to oldest-first to match the legacy ReplayTail wire shape.
	result := make([]*corev1.EventFrame, len(collected))
	for i := range collected {
		// Reverse index: collected[0] is newest; result[0] should be oldest.
		j := len(collected) - 1 - i
		legacyStreamName := subjectxlate.ToLegacy(string(collected[i].Subject), gameID)
		result[j] = eventbusEventToEventFrame(collected[i], legacyStreamName)
	}
	return result, nil
}

// eventbusEventToEventFrame converts an eventbus.Event to a proto EventFrame.
// legacyStreamName is the pre-translated colon-delimited stream name
// (e.g. "location:01ABC") that the web client expects in the Stream field.
func eventbusEventToEventFrame(e eventbus.Event, legacyStreamName string) *corev1.EventFrame {
	actorID := ""
	if e.Actor.ID != (ulid.ULID{}) {
		actorID = e.Actor.ID.String()
	}
	return &corev1.EventFrame{
		Id:        e.ID.String(),
		Stream:    legacyStreamName,
		Type:      string(e.Type),
		Timestamp: timestamppb.New(e.Timestamp),
		ActorType: e.Actor.Kind.String(),
		ActorId:   actorID,
		Payload:   e.Payload,
	}
}
