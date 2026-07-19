// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// SessionIdentityBuilder returns the typed bus-side identity for a session's
// subscriber. CoreServer supplies its own buildCharacterIdentity here; the seam
// exists because that helper is shared with QueryStreamHistory and therefore
// belongs to no single extracted unit. A nil builder yields the zero
// (passthrough) identity, matching buildCharacterIdentity's own unwired path.
type SessionIdentityBuilder func(ctx context.Context, playerID, characterID string) (eventbus.SessionIdentity, error)

// SessionLivenessRecomputer applies the canonical connection-count →
// session-liveness transition for a session. CoreServer supplies its own
// recomputeSessionLiveness here; the seam exists because that helper is shared
// with Disconnect and the lease sweep. A nil recomputer is a no-op.
type SessionLivenessRecomputer func(ctx context.Context, sessionID string) error

// SubscribeDeps carries the collaborators the subscribe/stream-delivery cluster
// actually uses. It is a struct rather than a positional parameter list because
// a dozen same-shaped interface parameters is an established defect shape in
// this repo (arch-review LOW-8, filed against productionSubsystems).
//
// Every field is optional at construction time. The handler resolves nil
// collaborators at call time with exactly the semantics CoreServer had — see
// the field comments, which are the only written record of each nil default's
// fail direction.
type SubscribeDeps struct {
	// SessionStore is the session-state source of truth: ownership validation,
	// reattach CAS, connection registration, and the per-event filter-at-delivery
	// re-read all go through it.
	SessionStore session.Store

	// PlayerSessionRepo backs the auth.ValidateSessionOwnership preamble that
	// gates stream open (SECURITY bd-jv7z).
	PlayerSessionRepo auth.PlayerSessionRepository

	// Subscriber opens the per-session durable JetStream consumer. Nil causes
	// Subscribe to fail early with NOT_CONFIGURED.
	Subscriber eventbus.Subscriber

	// FocusCoordinator manages session focus memberships and replay policy.
	FocusCoordinator focus.Coordinator

	// StreamContributor collects plugin-contributed stream names for a session.
	StreamContributor SessionStreamContributor

	// StreamRegistry routes mid-session stream-control updates to the live
	// Subscribe loop.
	StreamRegistry *SessionStreamRegistry

	// WorldQuerier backs the locationFollower's synthetic location_state frames.
	WorldQuerier WorldQuerier

	// VerbRegistry validates event types on the locationFollower's synthetic path.
	VerbRegistry *core.VerbRegistry

	// IdentityRegistry resolves plugin/system ULIDs to display names for the
	// gRPC wire (actorIDString). Nil means non-character actors fall back to
	// ULID-string form.
	IdentityRegistry plugins.IdentityRegistry

	// SceneMute optionally suppresses the SCENE_ACTIVITY badge downgrade for a
	// non-focused member whose GLOBAL notify preference is off or who has muted
	// the scene. Nil (unwired) or any returned error fails OPEN — the badge is
	// delivered, since mute/notify-pref are preferences, not access control, and
	// the downgraded frame is already content-free (INV-SCENE-62).
	SceneMute SceneMuteChecker

	// GameID returns the current game id used to qualify domain-relative dot
	// stream references (e.g. "character.01ABC") into fully-qualified JetStream
	// subjects (e.g. "events.main.character.01ABC") via eventbus.Qualify.
	// Colon-style references are rejected, not translated. Defaults to "main".
	GameID GameIDProvider

	// BuildIdentity and RecomputeLiveness are the two helpers this cluster
	// shares with clusters that have not been extracted yet (QueryStreamHistory
	// and Disconnect respectively). They are injected as narrow function values
	// rather than reached through a parent pointer (D-02).
	BuildIdentity     SessionIdentityBuilder
	RecomputeLiveness SessionLivenessRecomputer
}

// SubscribeHandler owns CoreService.Subscribe and its live delivery loop.
// CoreServer delegates to it and holds no stream logic of its own.
type SubscribeHandler struct {
	sessionStore      session.Store
	playerSessionRepo auth.PlayerSessionRepository
	subscriber        eventbus.Subscriber
	focusCoordinator  focus.Coordinator
	streamContributor SessionStreamContributor
	streamRegistry    *SessionStreamRegistry
	worldQuerier      WorldQuerier
	verbRegistry      *core.VerbRegistry

	// identityRegistry resolves plugin/system ULIDs to display names for the
	// gRPC wire (actorIDString). Nil means non-character actors fall back to
	// ULID-string form.
	identityRegistry plugins.IdentityRegistry

	// sceneMute optionally suppresses the SCENE_ACTIVITY badge downgrade for a
	// non-focused member whose GLOBAL notify preference is off or who has muted
	// the scene. Nil (unwired) or any returned error fails OPEN — the badge is
	// delivered, since mute/notify-pref are preferences, not access control, and
	// the downgraded frame is already content-free (INV-SCENE-62).
	sceneMute SceneMuteChecker

	gameID            GameIDProvider
	buildIdentity     SessionIdentityBuilder
	recomputeLiveness SessionLivenessRecomputer
}

// NewSubscribeHandler constructs the handler from its own collaborators only.
// No parent pointer is accepted or retained (D-02) — this is what makes the
// unit constructible from an external test package.
func NewSubscribeHandler(deps SubscribeDeps) *SubscribeHandler {
	return &SubscribeHandler{
		sessionStore:      deps.SessionStore,
		playerSessionRepo: deps.PlayerSessionRepo,
		subscriber:        deps.Subscriber,
		focusCoordinator:  deps.FocusCoordinator,
		streamContributor: deps.StreamContributor,
		streamRegistry:    deps.StreamRegistry,
		worldQuerier:      deps.WorldQuerier,
		verbRegistry:      deps.VerbRegistry,
		identityRegistry:  deps.IdentityRegistry,
		sceneMute:         deps.SceneMute,
		gameID:            deps.GameID,
		buildIdentity:     deps.BuildIdentity,
		recomputeLiveness: deps.RecomputeLiveness,
	}
}

// currentGameID returns the configured game id, falling back to "main".
func (h *SubscribeHandler) currentGameID() string {
	if h.gameID != nil {
		if g := h.gameID(); g != "" {
			return g
		}
	}
	return "main"
}

// buildSessionIdentity applies the injected identity builder, treating a nil
// builder as the zero (passthrough) identity — the same result
// buildCharacterIdentity returns when no binding repo is wired.
func (h *SubscribeHandler) buildSessionIdentity(ctx context.Context, playerID, characterID string) (eventbus.SessionIdentity, error) {
	if h.buildIdentity == nil {
		return eventbus.SessionIdentity{}, nil
	}
	return h.buildIdentity(ctx, playerID, characterID)
}

// toProtoSubscribeResponse maps an eventbus.Event and its MetadataOnly flag
// to a gRPC SubscribeResponse frame. metadataOnly comes from
// Delivery.MetadataOnly() and is stamped onto EventFrame.metadata_only
// (Phase 3b grounding doc Decision 4). ev.NoPlaintextReason is stamped into
// EventFrame.no_plaintext_reason (holomush-ojw1.6). The stream field carries
// the fully-qualified JetStream subject (e.g. "events.main.character.01ABC")
// as-is: producers and clients exchange domain-relative dot references and the
// server qualifies on the way in (holomush-rops). The web client dedups history
// by event ID and renders live frames without matching by stream, so the
// relative-send / qualified-receive asymmetry is intentional.
func (h *SubscribeHandler) toProtoSubscribeResponse(ev eventbus.Event, metadataOnly bool) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Event{
			Event: &corev1.EventFrame{
				Id:                ev.ID.String(),
				Stream:            string(ev.Subject),
				Type:              string(ev.Type),
				Timestamp:         timestamppb.New(ev.Timestamp),
				ActorType:         ev.Actor.Kind.String(),
				ActorId:           actorIDString(ev.Actor, h.identityRegistry),
				Payload:           ev.Payload,
				Cursor:            encodeEventCursor(ev),
				Rendering:         eventbus.RenderingToProto(ev.Rendering),
				MetadataOnly:      metadataOnly,
				NoPlaintextReason: corev1.NoPlaintextReason(ev.NoPlaintextReason),
			},
		},
	}
}

// computeInitialFilters qualifies a focus restore plan's stream list into
// bus-side subjects. Each domain-relative dot reference is qualified to an
// events.<game>. subject (via eventbus.Qualify); subjects that already look
// JetStream-native pass through unchanged. References that fail to qualify
// (e.g. colon-style or malformed) are logged and dropped so a single bad
// stream can't brick the Subscribe handshake.
func (h *SubscribeHandler) computeInitialFilters(ctx context.Context, plan focus.RestorePlan) []eventbus.Subject {
	gameID := h.currentGameID()
	out := make([]eventbus.Subject, 0, len(plan.Streams))
	for _, sm := range plan.Streams {
		sub, err := h.toSubject(gameID, sm.Stream)
		if err != nil {
			slog.WarnContext(ctx, "skipping stream with invalid subject translation",
				"stream", sm.Stream, "error", err)
			continue
		}
		out = append(out, sub)
	}
	return out
}

// qualifyStreamSubject qualifies a domain-relative stream reference (e.g.
// "location.01ABC") against the game id via eventbus.Qualify, which validates
// the result against eventbus.NewSubject. A colon-style reference no longer
// qualifies and is rejected here (holomush-rops).
//
// Free function rather than a method because CoreServer.emitCommandResponse
// (the command cluster, extracted in 08-05) is a second caller and must not
// reach through SubscribeHandler to get at it. The body is byte-identical to
// the former (*CoreServer).toSubject.
func qualifyStreamSubject(gameID, streamName string) (eventbus.Subject, error) {
	sub, err := eventbus.Qualify(gameID, streamName)
	if err != nil {
		return "", oops.With("stream", streamName).Wrap(err)
	}
	return sub, nil
}

// toSubject is the cluster-local spelling of qualifyStreamSubject.
func (h *SubscribeHandler) toSubject(gameID, streamName string) (eventbus.Subject, error) {
	return qualifyStreamSubject(gameID, streamName)
}

// Subscribe opens a stream of events for the session.
//
// SECURITY (bd-jv7z): Before opening the stream, the caller's
// player_session_token is validated against the target session via
// auth.ValidateSessionOwnership. Any failure — missing/invalid token,
// expired token, unknown session, or ownership mismatch — collapses to
// the enumeration-safe SESSION_NOT_FOUND error. This closes the IDOR
// surface where one player could subscribe to another player's event
// stream. Ongoing revocation (session deletion, cap eviction)
// propagates via the existing control-frame mechanism — validation runs
// once at stream open.
//
// Post-F3 live-loop architecture: the handler opens a durable JetStream
// session consumer via eventbus.Subscriber.OpenSession and pumps every
// delivery through toProtoSubscribeResponse → grpcStream.Send → Ack.
// Cursor resume is JS-native (durable consumer acked-seq). Mid-session
// filter changes use SessionStream.SetFilters which JS applies atomically
// while preserving the cursor.
func (h *SubscribeHandler) Subscribe(req *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.SubscribeResponse]) error {
	ctx := stream.Context()
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	// Phase-level spans on the Subscribe handler (holomush-87qu). The
	// otelgrpc interceptor's RPC span is the parent; each phase below
	// is a sibling child span scoped to a single piece of pre-replay
	// work. The dominator of the 8-10s "syncing" window observed in
	// holomush-87qu is unknown by code reading alone — these spans let
	// Tempo / Grafana show which phase carries the time.
	subscribeSpan := trace.SpanFromContext(ctx)
	subscribeSpan.SetAttributes(attribute.String("subscribe.session_id", req.SessionId))
	if requestID != "" {
		// Skip emitting the attribute on Meta-less requests so a Tempo
		// search by `subscribe.request_id=""` doesn't match every empty
		// caller.
		subscribeSpan.SetAttributes(attribute.String("subscribe.request_id", requestID))
	}

	slog.DebugContext(
		ctx, "subscribe request",
		"request_id", requestID,
		"session_id", req.SessionId,
	)

	if h.subscriber == nil {
		return oops.Code("NOT_CONFIGURED").Errorf("event bus subscriber not configured")
	}

	// Validate session ownership before any other work. Enumeration-safe:
	// every failure mode collapses to the same SESSION_NOT_FOUND error.
	validateCtx, validateSpan := tracer.Start(ctx, "subscribe.validate_ownership")
	if _, err := auth.ValidateSessionOwnership(
		validateCtx,
		h.playerSessionRepo,
		h.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		recordSpanError(validateSpan, err)
		validateSpan.End()
		slog.DebugContext(
			ctx, "subscribe session ownership validation failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return subscribeSessionNotFound(req.SessionId)
	}
	validateSpan.End()

	getCtx, getSpan := tracer.Start(ctx, "subscribe.session_get")
	info, err := h.sessionStore.Get(getCtx, req.SessionId)
	if err != nil {
		recordSpanError(getSpan, err)
		getSpan.End()
		return subscribeSessionNotFound(req.SessionId)
	}
	getSpan.End()

	// Control channel + stream-registry registration MUST happen before
	// RestoreFocus / OpenSession (CodeRabbit PR #4191): a focus-change RPC
	// firing in that ~130-line window would persist successfully but its
	// SendToConnection would fail with CONNECTION_NOT_REGISTERED, leaving
	// the new subscriber on stale filters. The ctrlCh buffer (16) absorbs
	// any inbound updates until runSubscribeLoop drains them after
	// OpenSession completes.
	ctrlCh := make(chan sessionStreamUpdate, 16)
	if h.streamRegistry != nil {
		h.streamRegistry.Register(info.ID, ctrlCh)
		defer h.streamRegistry.Deregister(info.ID, ctrlCh)
	}

	// Connection registration (bd-j2xj). Only fires when the caller supplies
	// a connection_id + client_type. Gate removal uses a fresh context so
	// stream context cancellation does not abort cleanup.
	if req.GetConnectionId() != "" {
		connID, parseErr := ulid.Parse(req.GetConnectionId())
		if parseErr != nil || req.GetClientType() == "" {
			return oops.Code("SUBSCRIBE_INVALID_CONNECTION").
				With("session_id", req.GetSessionId()).
				Errorf("subscribe: connection_id must be a valid ULID and client_type must be set")
		}
		conn := &session.Connection{
			ID:          connID,
			SessionID:   req.GetSessionId(),
			ClientType:  req.GetClientType(),
			Streams:     []string{},
			ConnectedAt: time.Now(),
		}
		addCtx, addSpan := tracer.Start(ctx, "subscribe.add_connection",
			trace.WithAttributes(attribute.String("connection.id", connID.String())))
		if addErr := h.sessionStore.AddConnection(addCtx, conn); addErr != nil {
			recordSpanError(addSpan, addErr)
			addSpan.End()
			return oops.Code("SUBSCRIBE_ADD_CONNECTION_FAILED").
				With("session_id", req.GetSessionId()).
				With("connection_id", req.GetConnectionId()).
				Wrap(addErr)
		}
		addSpan.End()
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
			defer cancel()
			if rmErr := h.sessionStore.RemoveConnection(cleanupCtx, connID); rmErr != nil {
				slog.WarnContext(
					ctx, "subscribe: failed to remove connection on stream close",
					"connection_id", connID.String(),
					"session_id", req.GetSessionId(),
					"error", rmErr,
				)
			}
		}()
		// Per-Connection routing (INV-SCENE-24): register in the per-Connection
		// map immediately after AddConnection so T14-T18 coordinators can
		// route to exactly one connection. The session-wide Register above
		// has already armed ctrlCh.
		if h.streamRegistry != nil {
			h.streamRegistry.RegisterConnection(info.ID, connID, ctrlCh)
			defer h.streamRegistry.DeregisterConnection(info.ID, connID, ctrlCh)
		}

		// RestoreConnectionFocus (D-08): a reconnecting telnet member whose
		// session PresentingFocus was a scene has its fresh per-connection
		// FocusKey repopulated so it receives live scene content rather than a
		// badge downgrade. Gated on PresentingFocus != nil (Assumption A2 /
		// Pitfall 5): an unconditional restore would clobber a web tab's
		// per-tab focus, and PresentingFocus is the telnet single-pane
		// reconnect signal. The primitive itself validates FocusMemberships
		// (INV-SCENE-18) and grid-falls-back when membership was revoked mid-
		// disconnect — which also blocks a swapped-in character (D-09) from
		// inheriting a prior character's scene focus. Best-effort: a restore
		// failure is logged but MUST NOT fail the Subscribe.
		if h.focusCoordinator != nil && info.PresentingFocus != nil {
			rcfCtx, rcfSpan := tracer.Start(ctx, "subscribe.restore_connection_focus",
				trace.WithAttributes(attribute.String("connection.id", connID.String())))
			if rcfErr := h.focusCoordinator.RestoreConnectionFocus(rcfCtx, req.GetSessionId(), connID); rcfErr != nil {
				recordSpanError(rcfSpan, rcfErr)
				slog.WarnContext(
					ctx, "subscribe: restore connection focus failed (non-fatal)",
					"session_id", req.GetSessionId(),
					"connection_id", connID.String(),
					"error", rcfErr,
				)
			}
			rcfSpan.End()
		}
	}

	// Reattach if detached.
	if info.Status == session.StatusDetached {
		reattachCtx, reattachSpan := tracer.Start(ctx, "subscribe.reattach_cas")
		ok, casErr := h.sessionStore.ReattachCAS(reattachCtx, req.SessionId)
		if casErr != nil {
			recordSpanError(reattachSpan, casErr)
			reattachSpan.End()
			return oops.Code("SESSION_REATTACH_FAILED").With("session_id", req.SessionId).Wrap(casErr)
		}
		reattachSpan.SetAttributes(attribute.Bool("reattach.won_cas", ok))
		reattachSpan.End()
		if !ok {
			return oops.Code("SESSION_REATTACH_LOST").
				With("session_id", req.SessionId).
				Errorf("session reattach CAS lost — another handler won the race")
		}
		// Transport reattach: the session row was held open across a
		// transport disconnect. The session is the SAME continuing
		// session — its LocationArrivedAt MUST NOT be reset. Per spec §2
		// (post-2026-05-17 amendment): only session-create and
		// character-move advance the floor. See SelectCharacter reattach
		// branch for the matching commentary.
	}

	// Recompute grid_present now that a connection exists (and after
	// ReattachCAS has already transitioned a formerly-detached session to
	// active). This restores grid_present=true when a reattaching session
	// was left at grid_present=false after the prior Disconnect (I-LIVE-3).
	// Placed here — after ReattachCAS — so that recomputeSessionLiveness
	// reads status=active and only needs to fix grid_present, rather than
	// racing with ReattachCAS over the detached→active transition.
	if req.GetConnectionId() != "" && h.recomputeLiveness != nil {
		if liveErr := h.recomputeLiveness(ctx, req.GetSessionId()); liveErr != nil {
			slog.WarnContext(ctx, "subscribe: failed to recompute session liveness after add_connection",
				"session_id", req.GetSessionId(), "error", liveErr)
		}
	}

	// RestoreFocus — produces the full stream list and replay modes.
	var plan focus.RestorePlan
	if h.focusCoordinator != nil {
		restoreCtx, restoreSpan := tracer.Start(ctx, "subscribe.restore_focus")
		p, planErr := h.focusCoordinator.RestoreFocus(restoreCtx, req.SessionId)
		if planErr != nil {
			recordSpanError(restoreSpan, planErr)
			slog.WarnContext(ctx, "restoreFocus failed, falling back to empty plan",
				"session_id", req.SessionId, "error", planErr)
		}
		restoreSpan.SetAttributes(attribute.Int("restore.stream_count", len(p.Streams)))
		restoreSpan.End()
		plan = p
	}

	// Ensure ambient streams are present even without a coordinator.
	if len(plan.Streams) == 0 {
		plan.Streams = append(
			plan.Streams,
			focus.StreamWithMode{Stream: world.CharacterStream(info.CharacterID), Mode: focus.ReplayModeFromCursor},
		)
		if !info.LocationID.IsZero() {
			plan.Streams = append(
				plan.Streams,
				focus.StreamWithMode{Stream: world.LocationStream(info.LocationID), Mode: focus.ReplayModeFromCursor},
			)
		}
		if h.streamContributor != nil {
			playerID := ""
			if !info.PlayerID.IsZero() {
				playerID = info.PlayerID.String()
			}
			pluginStreams := h.streamContributor.QuerySessionStreams(ctx, plugins.SessionStreamsRequest{
				CharacterID: info.CharacterID.String(),
				PlayerID:    playerID,
				SessionID:   info.ID,
			})
			for _, ps := range pluginStreams {
				plan.Streams = append(
					plan.Streams,
					focus.StreamWithMode{Stream: ps, Mode: focus.ReplayModeFromCursor},
				)
			}
		}
	}

	// Active filter set — mutated by location-following and mid-session
	// ctrl updates. Both paths funnel into SessionStream.SetFilters.
	filters := h.computeInitialFilters(ctx, plan)

	// Build the typed authenticated identity for this session's subscriber.
	// Decision 2 (Phase 3b grounding doc): the gRPC handler is the
	// authentication boundary; identity is derived solely from the
	// server-side session record — never from client-supplied fields.
	sessionIdentity, identityErr := h.buildSessionIdentity(ctx, info.PlayerID.String(), info.CharacterID.String())
	if identityErr != nil {
		return oops.Code("SUBSCRIBE_BINDING_LOOKUP_FAILED").Wrap(identityErr)
	}

	// Compute minFloor across all subscribed subjects per holomush-iwzt §6.2 Tier 1.
	// See maxStreamScopeFloor in scope_floor.go for MAX semantics + the legacy-vs-NATS
	// subject format gap that currently keeps minFloor at zero for production filters.
	filterStrings := make([]string, len(filters))
	for i, subj := range filters {
		filterStrings[i] = string(subj)
	}
	minFloor := maxStreamScopeFloor(info, filterStrings)

	// Open the bus session. JS preserves the durable consumer's cursor
	// across reconnect, so events not acked last time get redelivered
	// automatically; there is no explicit replay phase. This span covers
	// the JetStream LookupConsumer + CreateOrUpdateConsumer round trips
	// — known to be a non-trivial chunk of connect latency under load
	// (see holomush-l015 for the warmup-race surface on the audit-side
	// counterpart).
	openCtx, openSpan := tracer.Start(ctx, "subscribe.bus_open_session",
		trace.WithAttributes(attribute.Int("filters.count", len(filters))))
	busStream, subErr := h.subscriber.OpenSession(openCtx, req.SessionId, sessionIdentity, filters, minFloor)
	if subErr != nil {
		recordSpanError(openSpan, subErr)
		openSpan.End()
		return oops.Code("SUBSCRIBE_FAILED").With("session_id", req.SessionId).Wrap(subErr)
	}
	openSpan.End()

	// Capture the Subscribe-attach moment IMMEDIATELY after OpenSession
	// returns (holomush-iu8j). This is the latest server timestamp that
	// bounds what backfill MIGHT need to deliver: events with a
	// timestamp later than attachMoment are guaranteed to go through
	// the live Subscribe stream (the durable consumer is now wired and
	// receiving), so backfill never has to cover them. The client
	// passes this value back on WebQueryStreamHistory as not_after_ms
	// to eliminate the connect-time replay/backfill race (fujt Fix B).
	//
	// Server-side time source: avoids the clock-skew risk a
	// client-computed value would introduce. Both publish-side
	// timestamps and this attach moment come from the same process
	// clock.
	attachMomentMs := time.Now().UTC().UnixMilli()
	defer func() {
		if closeErr := busStream.Close(); closeErr != nil {
			slog.WarnContext(ctx, "bus stream close failed", "session_id", req.SessionId, "error", closeErr)
		}
	}()

	// filterSet tracks the current subject set so mid-session mutations
	// can diff additions/removals without the caller having to enumerate.
	filterSet := make(map[eventbus.Subject]struct{}, len(filters))
	for _, f := range filters {
		filterSet[f] = struct{}{}
	}

	// locationFollower translates character move events into atomic filter
	// swaps against the bus session.
	locStreamName := ""
	if !info.LocationID.IsZero() {
		locStreamName = world.LocationStream(info.LocationID)
	}
	lf := &locationFollower{
		characterID:   info.CharacterID,
		currentLocID:  info.LocationID,
		worldQuerier:  h.worldQuerier,
		sessionStore:  h.sessionStore,
		locStreamName: locStreamName,
		updateFilters: h.makeFilterUpdater(busStream, filterSet),
		verbRegistry:  h.verbRegistry,
	}
	syntheticCtx, syntheticSpan := tracer.Start(ctx, "subscribe.send_synthetic")
	if sendErr := lf.sendSynthetic(syntheticCtx, stream); sendErr != nil {
		recordSpanError(syntheticSpan, sendErr)
		syntheticSpan.End()
		return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
	}
	syntheticSpan.End()

	// (ctrlCh + Register/RegisterConnection were hoisted above to close
	// the focus-snapshot race window per CodeRabbit PR #4191 review.)

	// REPLAY_COMPLETE is emitted immediately — the bus handles replay
	// transparently by redelivering from the durable's acked-seq. Clients
	// that gate UI on this signal still see it at the expected point.
	// This is the latency-budget boundary for holomush-87qu: time from
	// Subscribe RPC entry to this Send is what the user perceives as the
	// 'syncing' window on the server side.
	if err := stream.Send(replayCompleteFrame(attachMomentMs)); err != nil {
		return oops.With("session_id", req.SessionId).Wrap(err)
	}
	subscribeSpan.AddEvent("subscribe.replay_complete_sent")

	// Resolve the connection's current FocusKey for badge downgrade.
	// connID is only non-zero when connection_id was supplied in the request.
	var connID *ulid.ULID
	if req.GetConnectionId() != "" {
		if parsed, parseErr := ulid.Parse(req.GetConnectionId()); parseErr == nil {
			connID = &parsed
		}
	}

	return h.runSubscribeLoop(ctx, info, busStream, filterSet, stream, lf, ctrlCh, connID)
}

// runSubscribeLoop is the post-REPLAY_COMPLETE live pump. It multiplexes
// bus deliveries against control-channel updates and ctx cancellation.
//
// Returns cleanly (nil) for:
//   - ctx cancellation (context.Canceled): client disconnected
//   - errStreamTerminated: matching session_ended observed inline
//
// All other errors (Send failures, bus errors) surface wrapped.
func (h *SubscribeHandler) runSubscribeLoop(
	ctx context.Context,
	info *session.Info,
	busStream eventbus.SessionStream,
	filterSet map[eventbus.Subject]struct{},
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
	ctrlCh chan sessionStreamUpdate,
	connID *ulid.ULID,
) error {
	type busResult struct {
		delivery eventbus.Delivery
		err      error
	}

	// Pumper goroutine — Next is blocking. Uses the handler's ctx so
	// ctx cancellation unblocks the pump cleanly.
	deliveries := make(chan busResult, 1)
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()
	go func() {
		defer close(deliveries)
		for {
			d, err := busStream.Next(pumpCtx)
			if err != nil {
				// ctx cancelled (handler returning) OR iterator closed
				// (busStream.Close was called on the handler's defer).
				select {
				case deliveries <- busResult{err: err}:
				case <-pumpCtx.Done():
				}
				return
			}
			select {
			case deliveries <- busResult{delivery: d}:
			case <-pumpCtx.Done():
				// Handler unwinding — nack so JS redelivers on rebind.
				if nackErr := d.Nack(); nackErr != nil {
					slog.DebugContext(ctx, "subscribe: nack on teardown failed",
						"session_id", info.ID, "error", nackErr)
				}
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// Must not swallow ctx.Err — the telnet drain relies on the
			// RPC returning promptly when the gateway cancels the stream
			// (holomush-umxj).
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return oops.Code("SUBSCRIPTION_CANCELLED").With("session_id", info.ID).Wrap(ctx.Err())

		case r, ok := <-deliveries:
			if !ok {
				return nil
			}
			if r.err != nil {
				if errors.Is(r.err, context.Canceled) || errors.Is(r.err, io.EOF) {
					return nil
				}
				return oops.Code("SUBSCRIPTION_ERROR").With("session_id", info.ID).Wrap(r.err)
			}
			if sendErr := h.dispatchDelivery(ctx, info, r.delivery, stream, lf, connID); sendErr != nil {
				if errors.Is(sendErr, errStreamTerminated) {
					return nil
				}
				return sendErr
			}

		case ctrl, ok := <-ctrlCh:
			if !ok {
				return nil
			}
			if ctrlErr := h.applyFilterCtrl(ctx, info, busStream, filterSet, ctrl); ctrlErr != nil {
				slog.WarnContext(ctx, "subscribe: filter ctrl update failed",
					"session_id", info.ID, "stream", ctrl.stream, "error", ctrlErr)
			}
		}
	}
}

// dispatchDelivery converts a bus delivery to a proto frame, sends it, and
// acks the message on success. Move events are routed through the
// locationFollower — synthetic location_state is sent in lieu of the raw
// event when a cross-location move is detected. session_ended events that
// match this handler's session surface errStreamTerminated so the caller
// closes the stream gracefully.
//
// connID is the per-connection ULID for the Subscribe handler. When non-nil,
// the connection's FocusKey is read from the session store to determine
// whether a scene event should be downgraded to a SCENE_ACTIVITY badge
// (INV-SCENE-62) — non-focused member connections never receive event content
// for scenes they are not currently focused on.
func (h *SubscribeHandler) dispatchDelivery(
	ctx context.Context,
	info *session.Info,
	delivery eventbus.Delivery,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
	connID *ulid.ULID,
) error {
	event := delivery.Event()

	// AUDIT_ONLY events (e.g., crypto.totp_*, crypto.policy_set) MUST NOT
	// reach client streams. They flow through the bus solely so the audit
	// projection can persist them; we ack-and-skip here so JS retention
	// can age them out at the stream level. (holomush-jxo8.6.26.)
	if event.Rendering != nil && event.Rendering.DisplayTarget == eventbus.EventChannelAuditOnly {
		if ackErr := delivery.Ack(); ackErr != nil {
			slog.WarnContext(ctx, "subscribe: ack failed on audit-only skip; will redeliver",
				"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
		}
		return nil
	}

	// Per-subject filter-at-delivery — load-bearing privacy gate per
	// holomush-iwzt §6.2 Tier 2. The stream classifiers (streamScopeFloor /
	// isCharacterStream / isLocationStream) are now dot-only (holomush-rops),
	// so we feed them the qualified NATS subject `event.Subject` directly;
	// the OpenSession-time minFloor (Tier 1) likewise classifies on NATS
	// subjects, so the floor is computed correctly at both tiers.
	//
	// Re-read SessionInfo per event so the filter picks up post-reattach /
	// post-move floor changes without subscriber restart. A follow-up bead
	// (holomush-hfb3) covers caching this in a per-Subscribe goroutine
	// local when latency profiling shows the per-event sessionStore.Get
	// matters.
	//
	// On lookup failure, fall back to the cached `info` parameter rather
	// than dropping: the only realistic Get failure in production is
	// "session not found" because the session was just deleted by quit /
	// evict (internal/grpc/server.go: EndSession → sessionStore.Delete).
	// The in-flight session_ended event in that case is the very event the
	// client needs to receive so it can close the stream gracefully —
	// fail-closed-by-drop would orphan the client on /terminal forever.
	// The cached `info` carries Subscribe-open LocationArrivedAt, which is
	// privacy-correct (never weaker than current state for legitimate
	// transitions: reattach within TTL preserves it; move only advances
	// it; delete is the only path that loses it and that path is the one
	// we want to forward through).
	//
	// Drop paths MUST ack the delivery — JetStream redelivers any unacked
	// event indefinitely on consumer reconnect.
	currentInfo, getErr := h.sessionStore.Get(ctx, info.ID)
	if getErr != nil {
		slog.DebugContext(ctx, "subscribe: filter-at-delivery using cached session info — store lookup failed",
			"session_id", info.ID, "event_id", event.ID.String(), "error", getErr)
		currentInfo = info
	}
	// Floor uses ns precision and >= semantics (INV-STORE-6 / INV-STORE-7,
	// gfo6 epic): publisher no longer truncates timestamps, so the
	// scope-floor comparison runs at full time.Now() resolution.
	floor := streamScopeFloor(currentInfo, string(event.Subject))
	if !floor.IsZero() && event.Timestamp.Before(floor) {
		slog.DebugContext(ctx, "subscribe: filter-at-delivery dropped event below scope floor",
			"session_id", info.ID, "event_id", event.ID.String(),
			"stream", string(event.Subject),
			"event_ts", event.Timestamp, "floor", floor)
		if ackErr := delivery.Ack(); ackErr != nil {
			slog.WarnContext(ctx, "subscribe: ack failed on filter-drop (below-floor) path; will redeliver",
				"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
		}
		return nil
	}

	// E9.5 badge downgrade (INV-SCENE-62): a scene event delivered to a
	// member connection that is NOT focused on that scene becomes a
	// content-free SCENE_ACTIVITY ping. The event content (which may be
	// encrypted) is never forwarded. Lossy by design — clients re-sync
	// badge state via ListMyScenes. The below-floor check above ensures
	// only temporally-eligible events reach this guard.
	//
	// connID nil → no connection-level focus tracking (e.g., legacy callers
	// without connection_id) — fall through to normal send.
	if connID != nil {
		if sid, ok := extractSceneID(string(event.Subject)); ok {
			var currentFocus *session.FocusKey
			if conn, getErr := h.sessionStore.GetConnection(ctx, *connID); getErr == nil {
				currentFocus = conn.FocusKey
			}
			focusedOn := currentFocus != nil &&
				currentFocus.Kind == session.FocusKindScene &&
				currentFocus.TargetID.String() == sid
			if !focusedOn {
				// Mute / global-notify-off suppression (D-04). Consult the
				// injected checker BEFORE building the badge: if the member has
				// global notifications off or has muted this scene, drop the
				// already-content-free frame for BOTH surfaces (web badge +
				// telnet nudge). Fail-OPEN — a nil checker or any error delivers
				// the badge, because mute/notify-pref are preferences, not
				// access control (INV-SCENE-62 privacy is unaffected: the frame
				// carries no content either way).
				if h.sceneMute != nil {
					suppress, muteErr := h.sceneMute.ShouldSuppress(ctx,
						currentInfo.CharacterID.String(), currentInfo.PlayerID.String(), sid)
					switch {
					case muteErr != nil:
						slog.DebugContext(ctx, "subscribe: scene-mute check failed; delivering badge (fail-open)",
							"session_id", info.ID, "event_id", event.ID.String(), "scene_id", sid, "error", muteErr)
					case suppress:
						// Drop paths MUST ack — JetStream redelivers unacked
						// events indefinitely (mirrors the badge-send ack below).
						if ackErr := delivery.Ack(); ackErr != nil {
							slog.WarnContext(ctx, "subscribe: ack failed on scene-mute suppression; will redeliver",
								"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
						}
						return nil
					}
				}
				badge := &corev1.SubscribeResponse{Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal:  corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY,
						SceneId: sid,
					},
				}}
				if sendErr := stream.Send(badge); sendErr != nil {
					if nackErr := delivery.Nack(); nackErr != nil {
						slog.DebugContext(ctx, "subscribe: nack after badge send failure",
							"session_id", info.ID, "error", nackErr)
					}
					return oops.With("event_id", event.ID.String()).Wrap(sendErr)
				}
				if ackErr := delivery.Ack(); ackErr != nil {
					slog.WarnContext(ctx, "subscribe: ack failed on badge downgrade; will redeliver",
						"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
				}
				return nil
			}
		}
	}

	// locationFollower consumes move events on character streams and
	// replies with a synthetic location_state — in that case the raw
	// event is dropped (ack'd) rather than forwarded.
	handled := false
	// dispatchDelivery feeds the dot-only classifiers the qualified NATS
	// `event.Subject`, so the move guard uses isCharacterStream(event.Subject)
	// rather than a colon prefix. The wire frame.Stream
	// (toProtoSubscribeResponse) now carries that same qualified subject
	// (holomush-rops).
	if string(event.Type) == string(eventvocab.EventTypeMove) &&
		isCharacterStream(string(event.Subject)) &&
		lf != nil {
		handled = lf.handleMovePayload(ctx, eventvocab.EventType(event.Type), event.Payload, stream)
	}

	if !handled {
		if sendErr := stream.Send(h.toProtoSubscribeResponse(event, delivery.MetadataOnly())); sendErr != nil {
			// Do not ack — JS redelivers when the consumer rebinds.
			if nackErr := delivery.Nack(); nackErr != nil {
				slog.DebugContext(ctx, "subscribe: nack after send failure",
					"session_id", info.ID, "error", nackErr)
			}
			return oops.With("event_id", event.ID.String()).Wrap(sendErr)
		}
	}

	if ackErr := delivery.Ack(); ackErr != nil {
		slog.WarnContext(ctx, "subscribe: ack failed; will redeliver",
			"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
	}

	// Terminal session_ended for this session → graceful close.
	if string(event.Type) == string(eventvocab.EventTypeSessionEnded) {
		var payload core.SessionEndedPayload
		if unmarshalErr := json.Unmarshal(event.Payload, &payload); unmarshalErr != nil {
			slog.WarnContext(ctx, "grpc: session_ended payload unmarshal failed — stream left open",
				"session_id", info.ID, "error", unmarshalErr)
			return nil
		}
		if payload.SessionID == info.ID {
			//nolint:errcheck // best-effort: client may already be disconnected
			_ = stream.Send(streamClosedFrame(payload.Reason))
			return errStreamTerminated
		}
	}
	return nil
}

// applyFilterCtrl applies a mid-session stream add/remove to the bus
// session's filter set via SetFilters. Location streams are rejected
// here so locationFollower remains the sole owner of those filters.
func (h *SubscribeHandler) applyFilterCtrl(
	ctx context.Context,
	info *session.Info,
	busStream eventbus.SessionStream,
	filterSet map[eventbus.Subject]struct{},
	ctrl sessionStreamUpdate,
) error {
	if strings.HasPrefix(ctrl.stream, "location.") {
		slog.WarnContext(ctx, "plugin attempted to modify location stream — rejected",
			"session_id", info.ID, "stream", ctrl.stream)
		return nil
	}
	sub, err := h.toSubject(h.currentGameID(), ctrl.stream)
	if err != nil {
		return oops.With("session_id", info.ID).With("stream", ctrl.stream).Wrap(err)
	}
	if ctrl.add {
		if _, exists := filterSet[sub]; exists {
			return nil
		}
		filterSet[sub] = struct{}{}
	} else {
		if _, exists := filterSet[sub]; !exists {
			return nil
		}
		delete(filterSet, sub)
	}
	return oops.Wrap(busStream.SetFilters(ctx, filterSetToSlice(filterSet)))
}

// makeFilterUpdater returns the locationFilterUpdater the locationFollower
// invokes when a character move is detected. It maintains the same
// filterSet bookkeeping the ctrl path uses so the two stay in sync.
func (h *SubscribeHandler) makeFilterUpdater(
	busStream eventbus.SessionStream,
	filterSet map[eventbus.Subject]struct{},
) locationFilterUpdater {
	return func(ctx context.Context, addStream, removeStream string) error {
		gameID := h.currentGameID()
		if addStream != "" {
			sub, err := h.toSubject(gameID, addStream)
			if err != nil {
				return oops.With("stream", addStream).Wrap(err)
			}
			filterSet[sub] = struct{}{}
		}
		if removeStream != "" {
			sub, err := h.toSubject(gameID, removeStream)
			if err != nil {
				return oops.With("stream", removeStream).Wrap(err)
			}
			delete(filterSet, sub)
		}
		return oops.Wrap(busStream.SetFilters(ctx, filterSetToSlice(filterSet)))
	}
}
