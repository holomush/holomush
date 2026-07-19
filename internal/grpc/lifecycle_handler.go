// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/presence"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// LifecycleDeps carries the collaborators the session-lifecycle cluster
// actually uses. It is a struct rather than a positional parameter list for the
// same reason SubscribeDeps is (arch-review LOW-8).
type LifecycleDeps struct {
	// Presence emits the leave / session_ended events that accompany a
	// teardown. Nil is not a supported configuration — the pre-split
	// CoreServer took it as a required positional constructor argument and
	// dereferenced it unguarded, and that behavior is preserved verbatim.
	Presence *presence.Emitter

	// SessionStore is the session-state source of truth: ownership validation,
	// connection removal and counting, and the liveness transition all go
	// through it.
	SessionStore session.Store

	// PlayerSessionRepo backs the auth.ValidateSessionOwnership preamble that
	// gates Disconnect (SECURITY bd-jv7z).
	PlayerSessionRepo auth.PlayerSessionRepository

	// DisconnectHooks are invoked after a session tears down, IN REGISTRATION
	// ORDER and sequentially. Hooks observe teardown, so reordering or
	// parallelizing them changes what each one sees — that is a behavior
	// change, not a refactor.
	DisconnectHooks []func(session.Info)
}

// LifecycleHandler owns CoreService.Disconnect and the session-liveness
// transition. CoreServer delegates to it and holds no lifecycle logic of its
// own.
//
// recomputeSessionLiveness and runDisconnectHooks are shared with clusters that
// live elsewhere: SubscribeHandler takes the former as a SessionLivenessRecomputer
// function value, CommandHandler takes the latter as a DisconnectHookRunner, and
// CoreServer's own Logout path calls runDisconnectHooks directly. This unit is
// their single owner; nobody duplicates them and nobody reaches them through a
// parent pointer (D-02).
type LifecycleHandler struct {
	presence          *presence.Emitter
	sessionStore      session.Store
	playerSessionRepo auth.PlayerSessionRepository
	disconnectHooks   []func(session.Info)
}

// NewLifecycleHandler constructs the handler from its own collaborators only.
// No parent pointer is accepted or retained (D-02) — this is what makes the
// unit constructible from an external test package.
func NewLifecycleHandler(deps LifecycleDeps) *LifecycleHandler {
	return &LifecycleHandler{
		presence:          deps.Presence,
		sessionStore:      deps.SessionStore,
		playerSessionRepo: deps.PlayerSessionRepo,
		disconnectHooks:   deps.DisconnectHooks,
	}
}

// Disconnect removes a connection and handles session lifecycle.
// When all terminal/telnet connections close but comms_hub remains, the
// character phases out (leave event) but the session stays active.
// When ALL connections close, non-guest sessions detach with TTL;
// guest sessions are deleted immediately.
//
// SECURITY (bd-jv7z): Before acting, the caller's player_session_token is
// validated against the target session via auth.ValidateSessionOwnership.
// Any failure — missing/invalid token, expired token, unknown session, or
// ownership mismatch — returns the enumeration-safe "session not found"
// response (success=false) with no state change. This closes the IDOR
// surface where one player could forcibly disconnect another player's
// session with just the session_id.
func (h *LifecycleHandler) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(
		ctx, "disconnect request",
		"request_id", requestID,
		"session_id", req.SessionId,
		"connection_id", req.ConnectionId,
	)

	// Validate session ownership before any state-changing work.
	// Enumeration-safe: every failure mode collapses to the same
	// "session not found" response.
	if _, err := auth.ValidateSessionOwnership(
		ctx,
		h.playerSessionRepo,
		h.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		slog.DebugContext(
			ctx, "disconnect session ownership validation failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return &corev1.DisconnectResponse{
			Meta:    responseMeta(requestID),
			Success: false,
		}, nil
	}

	// Remove the named connection and read the session's remaining connection
	// counts. With a connection_id these run in ONE transaction under a
	// sessions-row lock (RemoveConnectionAndCount): concurrent disconnects on
	// the same session serialize, so exactly one observes totalCount==0 and
	// runs cleanup — closing the double-cleanup race (holomush-cizj). Done
	// BEFORE the Get below so a transient Get error still removes the
	// connection, matching the prior remove-then-lookup ordering.
	var totalCount, gridConns int
	var counted bool
	// mayCleanup gates the lifecycle transition below. The session-level
	// (no connection_id) path removes nothing and has no removal signal, so it
	// keeps the prior unconditional behavior.
	mayCleanup := true
	if connID, parseErr := ulid.Parse(req.ConnectionId); req.ConnectionId != "" && parseErr == nil {
		counts, removed, cErr := h.sessionStore.RemoveConnectionAndCount(ctx, req.SessionId, connID)
		if cErr != nil {
			slog.WarnContext(
				ctx, "failed to remove connection and count — skipping lifecycle transition",
				"request_id", requestID,
				"session_id", req.SessionId,
				"connection_id", req.ConnectionId,
				"error", cErr,
			)
			return &corev1.DisconnectResponse{Meta: responseMeta(requestID), Success: true}, nil
		}
		totalCount, gridConns = counts.Total, counts.Grid
		counted = true
		// Only the disconnect that actually removed the connection owns the
		// lifecycle cleanup; a duplicate disconnect for an already-removed
		// connection_id (removed=false) must not re-emit leave/session_ended
		// (holomush-cizj duplicate-disconnect guard).
		mayCleanup = removed
	}

	// Look up session — needed for the lifecycle branch below. If it is
	// already gone, Disconnect is idempotent success.
	info, err := h.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		//nolint:nilerr // intentional: session already gone, return success (idempotent)
		return &corev1.DisconnectResponse{
			Meta:    responseMeta(requestID),
			Success: true,
		}, nil
	}

	// No-connection_id path (e.g. a session-level disconnect) removes nothing,
	// so these separate counts carry no remove/count race.
	if !counted {
		totalCount, err = h.sessionStore.CountConnections(ctx, req.SessionId)
		if err != nil {
			slog.WarnContext(
				ctx, "failed to count connections — skipping lifecycle transition",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
			return &corev1.DisconnectResponse{Meta: responseMeta(requestID), Success: true}, nil
		}
		termCount, tErr := h.sessionStore.CountConnectionsByType(ctx, req.SessionId, "terminal")
		if tErr != nil {
			slog.WarnContext(ctx, "failed to count terminal connections", "request_id", requestID, "error", tErr)
		}
		telCount, tlErr := h.sessionStore.CountConnectionsByType(ctx, req.SessionId, "telnet")
		if tlErr != nil {
			slog.WarnContext(ctx, "failed to count telnet connections", "request_id", requestID, "error", tlErr)
		}
		gridConns = termCount + telCount
	}

	if mayCleanup && totalCount == 0 {
		// No connections at all
		if info.IsGuest {
			// Guests can't reconnect — delete immediately
			char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
			if err := h.presence.EmitLeave(ctx, char, "quit"); err != nil {
				slog.WarnContext(
					ctx, "leave event failed",
					"request_id", requestID,
					"error", err,
				)
			}

			if endErr := h.presence.EmitSessionEnded(ctx, char, info.ID,
				core.SessionEndedCauseGuestEnd, "Session ended."); endErr != nil {
				// If we can't append session_ended, subscribers will not receive
				// STREAM_CLOSED. Retain the session row so the reaper can retry
				// (or at least so the row is not orphaned from its audit event).
				slog.WarnContext(
					ctx, "guest session_ended event failed — retaining session row for reap",
					"request_id", requestID,
					"session_id", info.ID,
					"error", endErr,
				)
				h.runDisconnectHooks(ctx, *info)
				return &corev1.DisconnectResponse{
					Meta:    responseMeta(requestID),
					Success: true,
				}, nil
			}

			if err := h.sessionStore.Delete(ctx, req.SessionId); err != nil {
				slog.WarnContext(
					ctx, "failed to delete guest session",
					"request_id", requestID,
					"error", err,
				)
			}

			// Run disconnect hooks for guests
			h.runDisconnectHooks(ctx, *info)
		} else {
			// Non-guest: detach session with TTL instead of deleting.
			// Do NOT emit leave event — player may reconnect.
			// Reaper handles leave events when TTL expires.
			if err := h.recomputeSessionLiveness(ctx, req.SessionId); err != nil {
				slog.WarnContext(
					ctx, "failed to recompute session liveness on disconnect",
					"request_id", requestID,
					"session_id", req.SessionId,
					"error", err,
				)
			}
		}
	} else if mayCleanup && gridConns == 0 && info.GridPresent {
		// Only comms_hub connections remain — phase out from grid.
		// Emit the leave event (engine concern), then update grid presence via helper.
		char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
		if err := h.presence.EmitLeave(ctx, char, "phased out"); err != nil {
			slog.WarnContext(
				ctx, "phase-out leave event failed",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
		}
		if err := h.recomputeSessionLiveness(ctx, req.SessionId); err != nil {
			slog.WarnContext(
				ctx, "failed to recompute session liveness on phase-out",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
		}
	}

	slog.InfoContext(
		ctx, "session disconnected",
		"request_id", requestID,
		"session_id", req.SessionId,
		"character_id", info.CharacterID.String(),
	)

	return &corev1.DisconnectResponse{
		Meta:    responseMeta(requestID),
		Success: true,
	}, nil
}

// recomputeSessionLiveness inspects the current connection counts for
// sessionID and applies the canonical liveness transition:
//
//   - 0 total connections → detach (StatusDetached, grid_present=false,
//     expires_at = now + reattach TTL). TTL is taken from info.TTLSeconds;
//     defaults to 1800 s (30 min) when ≤0. Does NOT emit a leave event —
//     the caller is responsible for any engine-level side effects.
//   - >0 total connections → ensure status=active; set grid_present=true
//     when at least one terminal or telnet connection exists, false otherwise.
//
// This helper is the single place that owns the connection-count →
// session-liveness mapping. Disconnect, the lease sweep (Task 5), and
// AddConnection all call it so the rule stays DRY.
func (h *LifecycleHandler) recomputeSessionLiveness(ctx context.Context, sessionID string) error {
	info, err := h.sessionStore.Get(ctx, sessionID)
	if err != nil {
		return oops.With("session_id", sessionID).Wrap(err)
	}

	totalCount, err := h.sessionStore.CountConnections(ctx, sessionID)
	if err != nil {
		return oops.With("session_id", sessionID).Wrap(err)
	}

	if totalCount == 0 {
		// Detach: set status + expires_at; clear grid presence.
		now := time.Now()
		ttlSeconds := info.TTLSeconds
		if ttlSeconds <= 0 {
			ttlSeconds = 1800 // default 30 minutes
		}
		expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)
		err = h.sessionStore.UpdateStatus(ctx, sessionID,
			session.StatusDetached, &now, &expiresAt)
		if err != nil {
			return oops.With("session_id", sessionID).Wrap(err)
		}
		if info.GridPresent {
			if err = h.sessionStore.UpdateGridPresent(ctx, sessionID, false); err != nil {
				return oops.With("session_id", sessionID).Wrap(err)
			}
		}
		return nil
	}

	// >0 connections: ensure status=active + correct grid presence.
	// This matters when the lease sweep (Task 5) or a future AddConnection
	// caller invokes recomputeSessionLiveness on a session that is still
	// StatusDetached because a prior recompute was interrupted or missed.
	if info.Status != session.StatusActive {
		if err = h.sessionStore.UpdateStatus(ctx, sessionID,
			session.StatusActive, nil, nil); err != nil {
			return oops.With("session_id", sessionID).Wrap(err)
		}
	}

	termCount, err := h.sessionStore.CountConnectionsByType(ctx, sessionID, "terminal")
	if err != nil {
		return oops.With("session_id", sessionID).Wrap(err)
	}
	telCount, err := h.sessionStore.CountConnectionsByType(ctx, sessionID, "telnet")
	if err != nil {
		return oops.With("session_id", sessionID).Wrap(err)
	}
	gridConns := termCount + telCount
	wantGrid := gridConns > 0

	if info.GridPresent != wantGrid {
		err = h.sessionStore.UpdateGridPresent(ctx, sessionID, wantGrid)
		if err != nil {
			return oops.With("session_id", sessionID).Wrap(err)
		}
	}
	return nil
}

// runDisconnectHooks runs all registered disconnect hooks with panic recovery.
//
// A nil receiver runs no hooks. That is behavior preservation, not a new
// defensive branch: before the extraction this was a *CoreServer method, so a
// fixture built as a bare &CoreServer{} literal ranged over a nil
// disconnectHooks slice and did nothing. Those fixtures bypass NewCoreServer
// and therefore leave lifecycleHandler nil; without this guard they would fault
// where they previously no-opped. Production always builds the handler in
// NewCoreServer and never re-nils it.
func (h *LifecycleHandler) runDisconnectHooks(ctx context.Context, info session.Info) {
	if h == nil {
		return
	}
	for _, hook := range h.disconnectHooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.ErrorContext(
						ctx, "disconnect hook panicked",
						"panic", r,
						"stack", string(debug.Stack()),
					)
				}
			}()
			hook(info)
		}()
	}
}
