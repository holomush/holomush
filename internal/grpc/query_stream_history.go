// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access"
	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/cursor"
	plugins "github.com/holomush/holomush/internal/plugin"
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
// The client-supplied req.Stream is a domain-relative dot reference; it is
// qualified to a fully-qualified subject at entry (INV-EVENTBUS-18) and every gate
// below operates on the qualified value. Colon-style legacy refs fail to
// qualify and are rejected with InvalidArgument.
//
// Two-layer authorization (on the qualified subject):
//   - Private streams (events.<gid>.character.<id>, events.<gid>.scene.<id>.{ic,ooc}):
//     membership gate via sessionHasMembership (invariant I-17). This is a HARD
//     GATE, not a policy — the ABAC engine is NEVER consulted for private
//     streams, and there is no admin override.
//   - Public streams (events.<gid>.location.<id>, global, etc.): ABAC engine.Evaluate.
//
// Pure read — does not mutate session cursors (invariant I-13).
func (s *CoreServer) QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(
		ctx, "query stream history",
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
			// INV-PRIVACY-5 wire opacity: missing-session denial collapses to
			// STREAM_ACCESS_DENIED on the wire. denial_reason goes to slog
			// only; internal SESSION_NOT_FOUND must not leak to the client.
			slog.InfoContext(ctx, "stream access denied: session not found",
				"session_id", req.SessionId,
				"stream", req.Stream,
				"denial_reason", "session_not_found")
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", req.SessionId).With("stream", req.Stream).
				Errorf("not authorized to read stream")
		}
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	if info.IsExpired() {
		// INV-PRIVACY-5 wire opacity: expired-session denial collapses to
		// STREAM_ACCESS_DENIED on the wire. denial_reason goes to slog only.
		slog.InfoContext(ctx, "stream access denied: session expired",
			"session_id", req.SessionId,
			"stream", req.Stream,
			"denial_reason", "expired_session")
		return nil, oops.Code("STREAM_ACCESS_DENIED").
			With("session_id", req.SessionId).With("stream", req.Stream).
			Errorf("not authorized to read stream")
	}

	// Step 2: Validate stream and qualify to a fully-qualified dot subject.
	if req.Stream == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("stream is required")
	}
	// INV-EVENTBUS-18: the read path is dot-native. Qualify the client-supplied
	// domain-relative reference (e.g. "location.<id>") into a fully-qualified
	// subject (events.<gid>.location.<id>) up front; everything below — the
	// classifier switch, scope floor, ABAC resource, and bus fetch — operates
	// on the qualified value. Colon-style legacy refs fail Qualify and are
	// rejected fail-closed as InvalidArgument.
	qualified, qErr := eventbus.Qualify(s.currentGameID(), req.Stream)
	if qErr != nil {
		// Generic message (no inner error) — qErr may name internal token rules;
		// matches the ":stream is required" guard above and the §5.5 wire-opacity
		// contract. oops.Code maps to codes.InvalidArgument at the gRPC boundary.
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("invalid stream")
	}
	stream := string(qualified)

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

	// Step 4: Decode opaque cursor (if any). Empty = first page.
	var beforeSeq uint64
	var beforeID ulid.ULID
	// pluginCursorOwner is non-zero when the decoded cursor is OwnerPlugin.
	// It carries the plugin name for re-wrapping the returned event cursors.
	var pluginCursorOwner cursor.Owner
	if len(req.Cursor) > 0 {
		c, decodeErr := cursor.Decode(req.Cursor)
		if decodeErr != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid cursor: %v", decodeErr)
		}
		if c.Epoch != cursor.CurrentEpoch() {
			return nil, status.Errorf(codes.FailedPrecondition, "cursor stale: epoch %d vs current %d", c.Epoch, cursor.CurrentEpoch())
		}
		switch c.Owner.Kind {
		case cursor.OwnerHost:
			if c.Host == nil {
				return nil, status.Errorf(codes.InvalidArgument, "host cursor missing body")
			}
			beforeSeq = c.Host.Seq
			beforeID = c.Host.ID
		case cursor.OwnerPlugin:
			// Plugin-owned cursor: route to the plugin's QueryHistory. The
			// inner bytes (c.Plugin) are the raw ULID that the plugin stored
			// as its event cursor. Parse as ULID and forward as BeforeID.
			// Authorization (Step 5) still applies; the plugin performs its
			// own domain authz on top.
			if c.Owner.PluginName == "" {
				return nil, status.Errorf(codes.InvalidArgument, "plugin cursor missing plugin_name")
			}
			var innerID ulid.ULID
			if len(c.Plugin) == 16 {
				copy(innerID[:], c.Plugin)
			} else if len(c.Plugin) > 0 {
				// Non-16-byte inner: the plugin may have used a string ULID;
				// attempt string parse as a fallback.
				parsed, parseErr := ulid.Parse(string(c.Plugin))
				if parseErr != nil {
					return nil, status.Errorf(codes.InvalidArgument, "plugin cursor: invalid inner ULID bytes")
				}
				innerID = parsed
			}
			// Fall through to step 5 (authorization) then step 7 where we
			// route via fetchHistoryFramesFromBus. The Reader.QueryHistory
			// will delegate to PluginHistoryRouter for plugin-owned subjects.
			// beforeSeq stays 0 (plugins don't expose seq).
			beforeID = innerID
			// Stash plugin name for cursor re-wrap in the response builder.
			// We carry it via the decoded cursor owner so no extra field is
			// needed on the handler's local state.
			pluginCursorOwner = c.Owner
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unknown cursor owner kind")
		}
	}

	// Step 5: Authorization — three-way classifier (on the qualified subject).
	//   1. Private streams (events.<gid>.character.<id>, events.<gid>.scene.<id>.{ic,ooc}): membership gate (I-17).
	//   2. Location streams (events.<gid>.location.<id>): hard-gate via session.LocationID (INV-PRIVACY-1).
	//   3. Other public streams (global, system, …): ABAC engine.Evaluate.
	switch {
	case isPrivateStream(stream):
		// Validate scene stream format up-front so malformed scene streams
		// (e.g. invalid ULID in the sceneID segment) surface as INVALID_ARGUMENT
		// rather than STREAM_ACCESS_DENIED. Dot-style per INV-SCENE-1 / ADR holomush-s9nu.
		if isSceneStream(stream) {
			if _, keyErr := streamToFocusKey(stream); keyErr != nil {
				return nil, keyErr
			}
		}
		// Layer 1: Membership gate (I-17). ABAC is never consulted for
		// private streams — no policy override is possible.
		if !sessionHasMembership(info, stream) {
			slog.InfoContext(
				ctx, "stream access denied by I-17 membership gate",
				"session_id", req.SessionId,
				"character_id", info.CharacterID.String(),
				"stream", stream,
			)
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", req.SessionId).
				With("stream", stream).
				Errorf("not authorized to read stream")
		}
	case isLocationStream(stream):
		// Layer 2: Location hard-gate (INV-PRIVACY-1). The session must be currently
		// located in the requested location. staffOverride consults the ABAC
		// engine (read_unrestricted_history action on "stream:*") and returns
		// false if the engine is nil or evaluation fails (fail-closed).
		if !staffOverride(ctx, info, s.accessEngine) {
			if info.LocationID.String() != extractLocationID(stream) {
				slog.InfoContext(ctx, "stream access denied by location hard-gate",
					"session_id", req.SessionId, "denial_reason", "wrong_location",
					"character_id", info.CharacterID.String(),
					"session_location", info.LocationID.String(),
					"requested_stream", stream)
				return nil, oops.Code("STREAM_ACCESS_DENIED").
					With("session_id", req.SessionId).With("stream", stream).
					Errorf("not authorized to read stream")
			}
		}
	default:
		// Layer 3: ABAC policy for other public streams (global, system, …).
		if s.accessEngine == nil {
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("stream", stream).
				Errorf("access engine not configured")
		}
		accessReq, reqErr := accessTypes.NewAccessRequest(
			access.CharacterSubject(info.CharacterID.String()),
			accessTypes.ActionRead,
			"stream:"+stream,
			nil,
		)
		if reqErr != nil {
			return nil, oops.Code("INTERNAL").Wrap(reqErr)
		}
		decision, evalErr := s.accessEngine.Evaluate(ctx, accessReq)
		if evalErr != nil {
			return nil, oops.Code("INTERNAL").
				With("stream", stream).
				Wrap(evalErr)
		}
		if !decision.IsAllowed() {
			slog.InfoContext(
				ctx, "stream access denied by ABAC",
				"session_id", req.SessionId,
				"character_id", info.CharacterID.String(),
				"stream", stream,
				"policy_id", decision.PolicyID(),
			)
			return nil, oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", req.SessionId).
				With("stream", stream).
				Errorf("not authorized to read stream")
		}
	}

	// Step 6: Compute effective NotBefore = MAX(client-supplied, server-side scope floor).
	var notBefore time.Time
	if req.NotBeforeMs > 0 {
		notBefore = time.UnixMilli(req.NotBeforeMs).UTC()
	}
	scopeFloor := streamScopeFloor(info, stream)
	if scopeFloor.After(notBefore) {
		notBefore = scopeFloor
	}

	// Step 6b: NotAfter — cursor-bounded backfill (holomush-iu8j / fujt
	// Fix B). The web client sends Subscribe attach_moment_ms here so
	// backfill returns only events that existed before the live stream
	// attached, eliminating the connect-time replay/backfill race.
	// 0 = no upper bound (back-compat with legacy clients).
	var notAfter time.Time
	if req.NotAfterMs > 0 {
		notAfter = time.UnixMilli(req.NotAfterMs).UTC()
	}

	// Step 7: Fetch count+1 to detect has_more.
	// Delegate to the JetStream/PostgreSQL tier crossover reader (F4+).
	// Both paths produce ascending (oldest→newest) slices of count+1 events maximum.
	//
	// Caller MUST be derived from the authenticated session record only.
	// We never trust a client-supplied character_id from the request body.
	// The plugin-owned subject path forwards this through PluginAuditService
	// to enforce per-plugin membership (spec §4.2 + §4.3).
	caller := eventbus.Actor{
		Kind: eventbus.ActorKindCharacter,
		ID:   info.CharacterID,
	}

	// Build the typed authenticated identity for the hot-tier AuthGuard path.
	// Decision 2 (Phase 3b grounding doc): derived solely from the server-side
	// session record — never from client-supplied fields.
	historyIdentity, identityErr := s.buildCharacterIdentity(ctx, info.PlayerID.String(), info.CharacterID.String())
	if identityErr != nil {
		return nil, oops.Code("HISTORY_BINDING_LOOKUP_FAILED").Wrap(identityErr)
	}

	frames, fetchErr := fetchHistoryFramesFromBus(
		ctx, s.historyReader, s.identityRegistry, stream, count,
		notBefore, notAfter, beforeSeq, beforeID, caller, historyIdentity,
	)
	if fetchErr != nil {
		return nil, mapHistoryError(
			oops.Code("INTERNAL").With("stream", stream).Wrap(fetchErr),
			req.SessionId,
			stream,
		)
	}
	protoFrames := frames

	// If a plugin-owned cursor was decoded in Step 4, re-wrap each frame's
	// cursor as an OwnerPlugin token so the client's next page request can
	// be recognised as plugin-owned. fetchHistoryFramesFromBus stamps
	// OwnerHost cursors regardless of the subject owner; we fix that here.
	if pluginCursorOwner.Kind == cursor.OwnerPlugin {
		protoFrames = rewrapFrameCursorsForPlugin(protoFrames, pluginCursorOwner.PluginName)
	}

	// Step 8: Build response. Results are ascending (oldest→newest).
	// If we got more than count, the OLDEST (index 0) is the "has_more
	// indicator" — drop it, keep the newest `count` events.
	hasMore := len(protoFrames) > count
	if hasMore {
		protoFrames = protoFrames[len(protoFrames)-count:]
	}

	// Populate next_cursor from the last frame's cursor (oldest frame in
	// the page is the pagination anchor for the next backward read).
	var nextCursor []byte
	if hasMore && len(protoFrames) > 0 {
		nextCursor = protoFrames[0].GetCursor()
	}

	return &corev1.QueryStreamHistoryResponse{
		Meta:       responseMeta(requestID),
		Events:     protoFrames,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// mapHistoryError translates eventbus cursor errors and plugin-emitted
// gRPC status errors into the host's wire-level error vocabulary.
//
// sessionID and stream MUST be the request-scoped values from the
// QueryStreamHistoryRequest. They are attached to the oops chain on the
// PermissionDenied translation so server logs match the outer I-17 gate
// (see internal/grpc/query_stream_history.go:170-173).
func mapHistoryError(err error, sessionID, stream string) error {
	// gRPC status pass-through with opacity translation. The plugin emits
	// status.Error directly; the router preserves the code; we run this
	// dispatch FIRST so the existing cursor-error chain doesn't shadow it.
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.PermissionDenied:
			// Opaque: collapse plugin-boundary denial into the same oops
			// code the outer I-17 gate uses. Client cannot distinguish
			// "outer wall caught" from "plugin wall caught."
			return oops.Code("STREAM_ACCESS_DENIED").
				With("session_id", sessionID).
				With("stream", stream).
				Errorf("not authorized to read stream")
		case codes.InvalidArgument:
			// Preserves the plugin's gRPC code AND any status.WithDetails
			// proto messages it attached. NOTE: when err is a wrapped status
			// (the production shape per the call site at line 233), grpc's
			// status.FromError rewrites Status.Message to err.Error(),
			// which includes the outer oops chain text. That message-rewrite
			// is unchanged from PR #267's status.Errorf("%s", st.Message())
			// behavior (see spec §5 risk #3). This branch's contribution
			// is strictly Details preservation; message purity is a
			// separate concern.
			return st.Err() //nolint:wrapcheck // gRPC status errors pass through as-is to preserve WithDetails payloads.
		}
		// Other status codes (Internal, Unavailable, …) pass through to the
		// existing dispatch below, which falls through to default.
	}
	switch {
	case errors.Is(err, eventbus.ErrCursorInvalid):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	case errors.Is(err, eventbus.ErrCursorStale):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	case errors.Is(err, eventbus.ErrCursorLag):
		return status.Errorf(codes.Unavailable, "%v", err)
	default:
		return err // let oops wrap through
	}
}

// fetchHistoryFramesFromBus fetches count+1 events from the HistoryReader and
// returns them as proto EventFrames in ascending ULID order (oldest→newest),
// matching the legacy ReplayTail wire shape expected by the response builder.
//
// Direction is Backward (newest-first) so the reader returns the tail of the
// stream relative to beforeSeq/beforeID (or the absolute end when zero).
// The slice is reversed on return to restore ascending order.
//
// identity is passed through to HistoryQuery.Identity for the hot-tier
// AuthGuard path. Zero value is safe when Crypto.Enabled=false.
//
// reg is used by eventbusEventToEventFrame to resolve plugin/system ULIDs to
// display names. Nil is safe: non-character actors fall back to ULID-string form.
func fetchHistoryFramesFromBus(
	ctx context.Context,
	reader eventbus.HistoryReader,
	reg plugins.IdentityRegistry,
	qualifiedStream string,
	count int,
	notBefore time.Time,
	notAfter time.Time,
	beforeSeq uint64,
	beforeID ulid.ULID,
	caller eventbus.Actor,
	identity eventbus.SessionIdentity,
) ([]*corev1.EventFrame, error) {
	// qualifiedStream is already a fully-qualified dot subject (the caller ran
	// eventbus.Qualify at read entry — INV-EVENTBUS-18), so we construct the Subject
	// directly — no legacy colon translation on the read path.
	sub, err := eventbus.NewSubject(qualifiedStream)
	if err != nil {
		return nil, oops.With("stream", qualifiedStream).Wrap(err)
	}

	q := eventbus.HistoryQuery{
		Subject:   sub,
		Direction: eventbus.DirectionBackward,
		PageSize:  count + 1,
		NotBefore: notBefore,
		NotAfter:  notAfter,
		Caller:    caller,
		Identity:  identity,
	}
	if beforeSeq != 0 {
		q.BeforeSeq = beforeSeq
		q.BeforeID = beforeID
	} else if !beforeID.IsZero() {
		q.BeforeID = beforeID
	}

	stream, err := reader.QueryHistory(ctx, q)
	if err != nil {
		// Do NOT call mapHistoryError here — translation runs once, at the
		// outer QueryStreamHistory call site (see :233). Translating twice
		// produces an oops("INTERNAL").Wrap(oops("STREAM_ACCESS_DENIED")...)
		// chain whose top code is INTERNAL, breaking the §5.5 opacity contract.
		return nil, oops.With("subject", string(sub)).Wrap(err)
	}
	defer stream.Close() //nolint:errcheck // best-effort iterator close

	// Drain up to count+1 events. DirectionBackward gives newest-first;
	// we collect them and reverse below to restore ascending order.
	//
	// AUDIT_ONLY events (e.g. crypto.totp_*, crypto.policy_set) MUST NOT
	// reach client streams — symmetric with the live dispatchDelivery
	// filter at internal/grpc/server.go (~line 1019). Skip them while
	// draining so the asymmetry between live subscribe and history reads
	// cannot expose host-emit security audit content. Skipped events are
	// not counted toward count+1 — we keep pulling until count+1 client-
	// visible events accumulate or the stream EOFs.
	collected := make([]eventbus.Event, 0, count+1)
	for {
		e, nextErr := stream.Next(ctx)
		if nextErr != nil {
			if nextErr == io.EOF { //nolint:errorlint // io.EOF is a sentinel value
				break
			}
			return nil, oops.With("subject", string(sub)).Wrap(nextErr)
		}
		if e.Rendering != nil && e.Rendering.DisplayTarget == eventbus.EventChannelAuditOnly {
			continue
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
		// The frame's stream field is the already-qualified dot subject
		// (INV-EVENTBUS-18 / Task 6): return the event's subject directly rather
		// than translating back to a legacy colon form.
		frame := eventbusEventToEventFrame(collected[i], string(collected[i].Subject), reg)
		frame.Cursor = encodeEventCursor(collected[i])
		result[j] = frame
	}
	return result, nil
}

// encodeEventCursor encodes an eventbus.Event into an opaque cursor bytes
// value suitable for setting on EventFrame.Cursor and response NextCursor.
// Returns nil on encoding failure (non-fatal; client simply cannot paginate
// from this event).
func encodeEventCursor(ev eventbus.Event) []byte {
	b, err := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerHost},
		Host:    &cursor.HostCursor{Seq: ev.Seq, ID: ev.ID},
	})
	if err != nil {
		return nil
	}
	return b
}

// encodePluginEventCursor wraps a plugin's opaque inner cursor bytes inside a
// host OwnerPlugin token. The inner bytes are the ULID bytes the plugin stored
// as its event ID (or its own opaque cursor). The host treats them as opaque;
// on the next page request the handler decodes the token and forwards the
// inner bytes back to the plugin's QueryHistory as the Before cursor.
func encodePluginEventCursor(pluginName string, inner []byte) []byte {
	b, err := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerPlugin, PluginName: pluginName},
		Plugin:  inner,
	})
	if err != nil {
		return nil
	}
	return b
}

// rewrapFrameCursorsForPlugin replaces each EventFrame.Cursor in frames with
// a OwnerPlugin-wrapped token for the given plugin. The host-inner ULID bytes
// are extracted from the existing OwnerHost cursor set by fetchHistoryFramesFromBus.
// Frames whose existing cursor cannot be decoded are left with nil cursor
// (non-fatal — the client cannot paginate from that event).
func rewrapFrameCursorsForPlugin(frames []*corev1.EventFrame, pluginName string) []*corev1.EventFrame {
	if len(frames) == 0 || pluginName == "" {
		return frames
	}
	out := make([]*corev1.EventFrame, len(frames))
	for i, f := range frames {
		// Construct a new EventFrame rather than shallow-copying the proto
		// struct, which contains a sync.Mutex inside protoimpl.MessageState.
		var pluginCursor []byte
		if len(f.GetCursor()) > 0 {
			existing, decErr := cursor.Decode(f.GetCursor())
			if decErr == nil && existing.Host != nil {
				// Use the ULID bytes as the plugin inner cursor.
				id := existing.Host.ID
				pluginCursor = encodePluginEventCursor(pluginName, id[:])
			}
		}
		out[i] = &corev1.EventFrame{
			Id:                f.GetId(),
			Stream:            f.GetStream(),
			Type:              f.GetType(),
			Timestamp:         f.GetTimestamp(),
			ActorType:         f.GetActorType(),
			ActorId:           f.GetActorId(),
			Payload:           f.GetPayload(),
			Cursor:            pluginCursor,
			Rendering:         f.GetRendering(),
			MetadataOnly:      f.GetMetadataOnly(),
			NoPlaintextReason: f.GetNoPlaintextReason(),
		}
	}
	return out
}

// eventbusEventToEventFrame converts an eventbus.Event to a proto EventFrame.
// streamName is the fully-qualified dot subject (e.g.
// "events.<gid>.location.01ABC") set on the Stream field — the read path is
// dot-native per INV-EVENTBUS-18, so no colon translation is applied.
// Event.MetadataOnly (populated by the hot-tier AuthGuard on deny) is
// stamped into EventFrame.metadata_only (Phase 3b grounding doc Decision 4).
// Event.NoPlaintextReason is stamped into EventFrame.no_plaintext_reason
// (holomush-ojw1.6) to let clients distinguish causes.
// reg resolves plugin/system ULIDs to display names; nil falls back to
// ULID-string form (same behaviour as actorIDString in server.go).
func eventbusEventToEventFrame(e eventbus.Event, streamName string, reg plugins.IdentityRegistry) *corev1.EventFrame {
	actorID := actorIDString(e.Actor, reg)
	return &corev1.EventFrame{
		Id:                e.ID.String(),
		Stream:            streamName,
		Type:              string(e.Type),
		Timestamp:         timestamppb.New(e.Timestamp),
		ActorType:         e.Actor.Kind.String(),
		ActorId:           actorID,
		Payload:           e.Payload,
		Rendering:         eventbus.RenderingToProto(e.Rendering),
		MetadataOnly:      e.MetadataOnly,
		NoPlaintextReason: corev1.NoPlaintextReason(e.NoPlaintextReason),
	}
}
