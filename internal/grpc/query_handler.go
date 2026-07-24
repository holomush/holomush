// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// QueryDeps carries the collaborators the current-state query cluster actually
// uses. It is a struct rather than a positional parameter list for the same
// reason SubscribeDeps and CommandDeps are (arch-review LOW-8).
//
// This cluster carries the densest set of fail-closed defaults in the server.
// The per-field comments below are the ONLY written record of which direction
// each nil collaborator fails; they travelled here verbatim with their fields.
// A "cleanup" that inverts one would be invisible to any test that exercises
// only the fully-wired path.
type QueryDeps struct {
	// SessionStore is the session-state source of truth for every method in
	// this cluster: ownership validation, expiry checks, command-history reads,
	// presence lookups, and the connection-lease refresh all go through it.
	SessionStore session.Store

	// PlayerSessionRepo backs the auth.ValidateSessionOwnership preamble that
	// gates GetCommandHistory, ListFocusPresence, ListSessionStreams,
	// ListAvailableCommands, and RefreshConnection (SECURITY bd-jv7z).
	PlayerSessionRepo auth.PlayerSessionRepository

	// AccessEngine evaluates ABAC policies for stream read authorization
	// (Layer 2) and for the ListFocusPresence list_presence gate.
	// Nil if ABAC is not configured (public stream reads will be denied).
	AccessEngine accessTypes.AccessPolicyEngine

	// HistoryReader serves QueryStreamHistory from the JetStream/PostgreSQL
	// tier crossover (F4). Required post-F7; returns INTERNAL when nil.
	HistoryReader eventbus.HistoryReader

	// IdentityRegistry resolves plugin/system ULIDs to display names for the
	// gRPC wire (actorIDString). Nil means non-character actors fall back to
	// ULID-string form.
	IdentityRegistry plugins.IdentityRegistry

	// CharacterNameResolver resolves character display names by ID for
	// ListFocusPresence (5b2j). Nil is a misconfiguration, not a security
	// boundary, so it surfaces as INTERNAL rather than a denial.
	CharacterNameResolver characterNameResolver

	// CommandQuerier is the ABAC-filtered command enumeration for
	// ListAvailableCommands (2zjio). Nil fails closed with PERMISSION_DENIED.
	CommandQuerier *commandquery.Querier

	// FocusCoordinator supplies the RestorePlan that ListSessionStreams
	// projects into stream names. Nil falls back to the ambient-stream
	// assembly that mirrors Subscribe.
	FocusCoordinator focus.Coordinator

	// StreamContributor collects plugin-contributed stream names for a session
	// on the ListSessionStreams fallback path.
	StreamContributor SessionStreamContributor

	// Bindings backs the player-character binding lookup in
	// buildCharacterIdentity. Nil yields the zero (passthrough) identity.
	Bindings BindingRepo

	// CryptoActive is true when a KEK (RekeyManager) is wired. It gates the
	// binding lookup so characters without a binding row do not break
	// QueryStreamHistory in KEK-less deployments.
	CryptoActive bool

	// GameID qualifies domain-relative dot stream references (e.g.
	// "location.01ABC") into fully-qualified JetStream subjects via
	// eventbus.Qualify. Colon-style references are rejected, not translated.
	// Defaults to "main".
	GameID GameIDProvider
}

// QueryHandler owns the current-state query RPCs: GetCommandHistory,
// QueryStreamHistory, ListFocusPresence, ListSessionStreams,
// ListAvailableCommands, and RefreshConnection. CoreServer delegates to it and
// holds no query logic of its own.
//
// It also owns buildCharacterIdentity, which SubscribeHandler consumes through
// the SessionIdentityBuilder seam rather than duplicating.
type QueryHandler struct {
	sessionStore      session.Store
	playerSessionRepo auth.PlayerSessionRepository

	// accessEngine evaluates ABAC policies for stream read authorization (Layer 2).
	// Nil if ABAC is not configured (public stream reads will be denied).
	accessEngine accessTypes.AccessPolicyEngine

	// historyReader serves QueryStreamHistory from the JetStream/PostgreSQL
	// tier crossover (F4). Required post-F7; returns INTERNAL when nil.
	historyReader eventbus.HistoryReader

	// identityRegistry resolves plugin/system ULIDs to display names for the
	// gRPC wire (actorIDString). Nil means non-character actors fall back to
	// ULID-string form.
	identityRegistry plugins.IdentityRegistry

	// characterNameResolver resolves character display names by ID for
	// ListFocusPresence and other current-state RPCs (5b2j).
	characterNameResolver characterNameResolver

	// commandQuerier is the ABAC-filtered command enumeration for ListAvailableCommands
	// (2zjio). Nil until WithCommandQuerier is called; nil fails closed with PERMISSION_DENIED.
	commandQuerier *commandquery.Querier

	focusCoordinator  focus.Coordinator
	streamContributor SessionStreamContributor

	// bindings backs the current-binding lookup in buildCharacterIdentity;
	// cryptoActive gates it on KEK presence.
	bindings     BindingRepo
	cryptoActive bool

	gameID GameIDProvider
}

// NewQueryHandler constructs the handler from its own collaborators only.
// No parent pointer is accepted or retained (D-02) — this is what makes the
// unit constructible from an external test package.
func NewQueryHandler(deps QueryDeps) *QueryHandler {
	return &QueryHandler{
		sessionStore:          deps.SessionStore,
		playerSessionRepo:     deps.PlayerSessionRepo,
		accessEngine:          deps.AccessEngine,
		historyReader:         deps.HistoryReader,
		identityRegistry:      deps.IdentityRegistry,
		characterNameResolver: deps.CharacterNameResolver,
		commandQuerier:        deps.CommandQuerier,
		focusCoordinator:      deps.FocusCoordinator,
		streamContributor:     deps.StreamContributor,
		bindings:              deps.Bindings,
		cryptoActive:          deps.CryptoActive,
		gameID:                deps.GameID,
	}
}

// currentGameID returns the configured game id, falling back to "main".
func (h *QueryHandler) currentGameID() string {
	if h.gameID != nil {
		if g := h.gameID(); g != "" {
			return g
		}
	}
	return "main"
}

// GetCommandHistory retrieves command history for a session.
//
// SECURITY (bd-jv7z): Before returning history, the caller's
// player_session_token is validated against the target session via
// auth.ValidateSessionOwnership. Any failure — missing/invalid token,
// expired token, unknown session, or ownership mismatch — returns the
// enumeration-safe "session not found" response (success=false) with
// an empty command list. This closes the IDOR surface where one player
// could read another player's typed command history with just the
// session_id.
func (h *QueryHandler) GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	// Validate session ownership before any store read.
	// Enumeration-safe: every failure mode collapses to the same
	// "session not found" response with no commands.
	if _, err := auth.ValidateSessionOwnership(
		ctx,
		h.playerSessionRepo,
		h.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		slog.DebugContext(
			ctx, "get_command_history session ownership validation failed",
			"request_id", requestID,
			"session_id", req.GetSessionId(),
			"error", err,
		)
		return &corev1.GetCommandHistoryResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   "session not found",
		}, nil
	}

	sessionID := req.GetSessionId()
	history, err := h.sessionStore.GetCommandHistory(ctx, sessionID)
	if err != nil {
		return nil, oops.Code("COMMAND_HISTORY_FAILED").With("session_id", sessionID).Wrap(err)
	}

	return &corev1.GetCommandHistoryResponse{
		Meta:     responseMeta(requestID),
		Success:  true,
		Commands: history,
	}, nil
}
