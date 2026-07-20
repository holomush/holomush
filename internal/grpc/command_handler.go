// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/presence"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// DisconnectHookRunner runs the registered disconnect hooks for a session that
// has just torn down. LifecycleHandler supplies its own runDisconnectHooks
// here; the seam exists because command execution can end a session (quit) or
// forcibly end others (admin boot), and the hook list belongs to the lifecycle
// cluster. Injected as a narrow function value rather than reached through a
// parent pointer (D-02). A nil runner is a no-op.
type DisconnectHookRunner func(ctx context.Context, info session.Info)

// CommandDeps carries the collaborators the command-execution cluster actually
// uses. It is a struct rather than a positional parameter list for the same
// reason SubscribeDeps is (arch-review LOW-8).
type CommandDeps struct {
	// Dispatcher is the unified command dispatcher. NewCoreServer panics when
	// it is nil, so production never sees an unwired dispatcher; that
	// fail-fast wiring guard stays in NewCoreServer rather than moving here.
	Dispatcher *command.Dispatcher

	// CmdServices is the service bundle handed to each CommandExecution.
	// Nil is rejected by NewCoreServer for the same reason as Dispatcher.
	CmdServices *command.Services

	// Presence emits the leave / session_ended events for quit and admin-boot
	// teardowns. Nil is not a supported configuration — the pre-split
	// CoreServer dereferenced it unguarded and that behavior is preserved.
	Presence *presence.Emitter

	// Publisher delivers command_response / command_error events to the
	// character's personal stream. Nil is a supported, silent no-op: many
	// fixtures construct without wiring a publisher at all.
	Publisher eventbus.Publisher

	// SessionStore backs the command-history append, the ownership preamble,
	// and the booted-session lookups.
	SessionStore session.Store

	// PlayerSessionRepo backs the auth.ValidateSessionOwnership preamble that
	// gates HandleCommand (SECURITY bd-jv7z).
	PlayerSessionRepo auth.PlayerSessionRepository

	// GameID qualifies the domain-relative character stream reference into a
	// fully-qualified JetStream subject. Defaults to "main" when nil or empty.
	GameID GameIDProvider

	// RunDisconnectHooks is the lifecycle cluster's hook runner, shared here
	// because quit and admin-boot teardowns originate on the command path.
	RunDisconnectHooks DisconnectHookRunner
}

// CommandHandler owns CoreService.HandleCommand and the command-execution
// pipeline. CoreServer delegates to it and holds no command logic of its own.
type CommandHandler struct {
	dispatcher        *command.Dispatcher
	cmdServices       *command.Services
	presence          *presence.Emitter
	sessionStore      session.Store
	playerSessionRepo auth.PlayerSessionRepository

	// publisher delivers command_response / command_error events. Nil is a
	// supported, silent no-op (see emitCommandResponse).
	publisher eventbus.Publisher

	gameID             GameIDProvider
	runDisconnectHooks DisconnectHookRunner
}

// NewCommandHandler constructs the handler from its own collaborators only.
// No parent pointer is accepted or retained (D-02) — this is what makes the
// unit constructible from an external test package.
func NewCommandHandler(deps CommandDeps) *CommandHandler {
	return &CommandHandler{
		dispatcher:         deps.Dispatcher,
		cmdServices:        deps.CmdServices,
		presence:           deps.Presence,
		sessionStore:       deps.SessionStore,
		playerSessionRepo:  deps.PlayerSessionRepo,
		publisher:          deps.Publisher,
		gameID:             deps.GameID,
		runDisconnectHooks: deps.RunDisconnectHooks,
	}
}

// currentGameID returns the configured game id, falling back to "main".
func (h *CommandHandler) currentGameID() string {
	if h.gameID != nil {
		if g := h.gameID(); g != "" {
			return g
		}
	}
	return "main"
}

// fireDisconnectHooks applies the injected hook runner, treating a nil runner
// as a no-op. The pre-split call was s.runDisconnectHooks, which iterated an
// empty slice when no hook was registered — same observable result.
func (h *CommandHandler) fireDisconnectHooks(ctx context.Context, info session.Info) {
	if h.runDisconnectHooks == nil {
		return
	}
	h.runDisconnectHooks(ctx, info)
}

// HandleCommand processes a game command.
//
// SECURITY (bd-jv7z): Before executing, the caller's player_session_token is
// validated against the target session via auth.ValidateSessionOwnership.
// Any failure — missing/invalid token, expired token, unknown session, or
// ownership mismatch — returns the enumeration-safe "session not found"
// response. This closes the IDOR surface where one player could submit a
// command against another player's session id.
func (h *CommandHandler) HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(
		ctx, "handle command request",
		"request_id", requestID,
		"session_id", req.SessionId,
		"command", req.Command,
	)

	info, err := auth.ValidateSessionOwnership(
		ctx,
		h.playerSessionRepo,
		h.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	)
	if err != nil {
		slog.DebugContext(
			ctx, "session ownership validation failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return &corev1.HandleCommandResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   "session not found",
		}, nil
	}

	// Record command in session history (best-effort)
	if appendErr := h.sessionStore.AppendCommand(ctx, req.SessionId, req.Command, info.MaxHistory); appendErr != nil {
		slog.WarnContext(
			ctx, "command history append failed",
			"session_id", req.SessionId,
			"error", appendErr,
		)
	}

	// Parse and execute command
	if err := h.executeCommand(ctx, info, req.Command, req.GetConnectionId()); err != nil {
		slog.WarnContext(
			ctx, "command execution failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"command", req.Command,
			"error", err,
		)
		return &corev1.HandleCommandResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &corev1.HandleCommandResponse{
		Meta:    responseMeta(requestID),
		Success: true,
	}, nil
}

// executeCommand parses and executes a command via the unified dispatcher.
// Output is delivered via command_response events emitted to the character's
// personal stream.
func (h *CommandHandler) executeCommand(ctx context.Context, info *session.Info, input, connectionIDStr string) error {
	return h.executeViaDispatcher(ctx, info, input, connectionIDStr)
}

// executeViaDispatcher uses the unified command.Dispatcher for command
// execution. Handler output written to the CommandExecution's io.Writer is
// captured in a buffer and emitted as a command_response event afterward.
// connectionIDStr is the originating gateway connection ULID string (Phase 5);
// empty string is accepted for non-gateway callers (parsed as zero ULID).
func (h *CommandHandler) executeViaDispatcher(ctx context.Context, info *session.Info, input, connectionIDStr string) error {
	char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}

	sessionID, parseErr := ulid.Parse(info.ID)
	if parseErr != nil {
		return oops.Code("INVALID_SESSION_ID").
			With("session_id", info.ID).
			Wrap(parseErr)
	}

	// Parse connectionIDStr to ULID. Empty is allowed (legacy non-gateway
	// callers omit connection_id), but a NON-EMPTY value that fails to
	// parse is an explicit error from the caller — failing silently with
	// a zero ULID would silently bypass per-connection command semantics.
	// (CodeRabbit PR #4191)
	var connectionID ulid.ULID
	if connectionIDStr != "" {
		parsed, connParseErr := ulid.Parse(connectionIDStr)
		if connParseErr != nil {
			return oops.Code("INVALID_CONNECTION_ID").
				With("session_id", info.ID).
				With("connection_id", connectionIDStr).
				Wrap(connParseErr)
		}
		connectionID = parsed
	}

	var buf bytes.Buffer
	exec, err := command.NewCommandExecution(command.CommandExecutionConfig{
		CharacterID:   info.CharacterID,
		PlayerID:      info.PlayerID,
		LocationID:    info.LocationID,
		CharacterName: info.CharacterName,
		SessionID:     sessionID,
		ConnectionID:  connectionID, // Phase 5 (holomush-5rh.14 T19 follow-up)
		Output:        &buf,
		Services:      h.cmdServices,
	})
	if err != nil {
		return oops.Code("EXECUTION_SETUP_FAILED").Wrap(err)
	}

	dispatchErr := h.dispatcher.Dispatch(ctx, input, exec)

	// Emit any buffered output as a command_response event.
	if buf.Len() > 0 {
		isError := exec.ResponseIsError() || (dispatchErr != nil && !errors.Is(dispatchErr, command.ErrSessionEnded))
		if emitErr := h.emitCommandResponse(ctx, char, strings.TrimRight(buf.String(), "\n"), isError); emitErr != nil {
			return oops.Wrap(emitErr)
		}
	}

	// Quit/self-boot detection: handler signals intent, server does teardown.
	if errors.Is(dispatchErr, command.ErrSessionEnded) {
		if dcErr := h.presence.EmitLeave(ctx, char, "quit"); dcErr != nil {
			slog.WarnContext(ctx, "leave event failed", "error", dcErr)
		}
		if endErr := h.presence.EmitSessionEnded(ctx, char, info.ID,
			core.SessionEndedCauseQuit, "Goodbye!"); endErr != nil {
			// If we can't append session_ended, subscribers will not receive
			// STREAM_CLOSED. Retain the session row so the reaper can retry
			// (or at least so the row is not orphaned from its audit event).
			slog.WarnContext(
				ctx, "session_ended event failed — retaining session row for reap",
				"session_id", info.ID,
				"error", endErr,
			)
			h.fireDisconnectHooks(ctx, *info)
			return nil
		}
		if delErr := h.sessionStore.Delete(ctx, info.ID); delErr != nil {
			slog.WarnContext(ctx, "session delete failed", "error", delErr)
		}
		h.fireDisconnectHooks(ctx, *info)
		return nil
	}

	if dispatchErr != nil {
		// User-facing errors are delivered as command_response events, not
		// RPC-level failures. Emit the player message and return nil so
		// HandleCommand returns Success=true.
		if isUserFacingError(dispatchErr) {
			if buf.Len() == 0 {
				if emitErr := h.emitCommandResponse(ctx, char, command.PlayerMessage(dispatchErr), true); emitErr != nil {
					return oops.Wrap(emitErr)
				}
			}
			return nil
		}
		// Infrastructure errors propagate as RPC failures (Success=false).
		return oops.Wrap(dispatchErr)
	}

	// Process booted sessions: emit leave events and run disconnect hooks
	// for targets that were forcibly removed by admin boot.
	bootedSessions := exec.BootedSessions()
	for i := range bootedSessions {
		booted := &bootedSessions[i]

		// If CharacterRef is empty (e.g. plugin-originated boot that only
		// provided a session ID), look up the session to populate it.
		if booted.CharacterRef.ID.IsZero() && booted.SessionInfo.ID != "" {
			info, lookupErr := h.sessionStore.Get(ctx, booted.SessionInfo.ID)
			if lookupErr != nil {
				slog.WarnContext(ctx, "failed to look up booted session",
					"session_id", booted.SessionInfo.ID, "error", lookupErr)
				continue
			}
			booted.CharacterRef = core.CharacterRef{
				ID:         info.CharacterID,
				Name:       info.CharacterName,
				LocationID: info.LocationID,
			}
			booted.SessionInfo = *info
		}

		if dcErr := h.presence.EmitLeave(ctx, booted.CharacterRef, "booted"); dcErr != nil {
			slog.WarnContext(ctx, "boot leave event failed",
				"target_id", booted.CharacterRef.ID.String(),
				"error", dcErr)
		}
		if endErr := h.presence.EmitSessionEnded(ctx, booted.CharacterRef, booted.SessionInfo.ID,
			core.SessionEndedCauseKicked,
			"You have been disconnected by an administrator."); endErr != nil {
			// If we can't append session_ended, subscribers will not receive
			// STREAM_CLOSED. Retain the session row so the reaper can retry
			// (or at least so the row is not orphaned from its audit event).
			slog.WarnContext(ctx, "boot session_ended event failed — retaining session row for reap",
				"session_id", booted.SessionInfo.ID,
				"target_id", booted.CharacterRef.ID.String(),
				"error", endErr)
			h.fireDisconnectHooks(ctx, booted.SessionInfo)
			continue
		}
		if delErr := h.sessionStore.Delete(ctx, booted.SessionInfo.ID); delErr != nil {
			slog.WarnContext(ctx, "boot session delete failed",
				"session_id", booted.SessionInfo.ID,
				"target_id", booted.CharacterRef.ID.String(),
				"error", delErr)
		}
		h.fireDisconnectHooks(ctx, booted.SessionInfo)
	}

	return nil
}

// isUserFacingError returns true for errors that should be delivered to the
// player via a command_response event rather than as an RPC-level failure.
// Delegates to command.PlayerMessage to stay in sync with the command package's
// error classification — if PlayerMessage returns a specific message (not the
// generic fallback), the error is user-facing.
func isUserFacingError(err error) bool {
	msg := command.PlayerMessage(err)
	return msg != "Something went wrong. Try again."
}

// emitCommandResponse emits a command_response or command_error event to the
// character's personal stream. Returns an error if the event could not be emitted.
func (h *CommandHandler) emitCommandResponse(ctx context.Context, char core.CharacterRef, text string, isError bool) error {
	payload, err := json.Marshal(eventvocab.CommandResponsePayload{
		Text: text,
	})
	if err != nil {
		slog.ErrorContext(
			ctx, "failed to marshal command_response payload",
			"character_id", char.ID.String(),
			"error", err,
		)
		return oops.Code("COMMAND_RESPONSE_MARSHAL_FAILED").Wrap(err)
	}

	eventType := eventvocab.EventTypeCommandResponse
	if isError {
		eventType = eventvocab.EventTypeCommandError
	}

	if h.publisher == nil {
		slog.DebugContext(ctx, "emitCommandResponse: publisher not configured, event not emitted")
		return nil
	}

	sub, err := qualifyStreamSubject(h.currentGameID(), world.CharacterStream(char.ID))
	if err != nil {
		return oops.Code("COMMAND_RESPONSE_EMIT_FAILED").With("character_id", char.ID.String()).Wrap(err)
	}
	typ, err := eventbus.NewType(string(eventType))
	if err != nil {
		return oops.Code("COMMAND_RESPONSE_EMIT_FAILED").With("character_id", char.ID.String()).Wrap(err)
	}
	event := eventbus.NewEvent(sub, typ, eventbus.Actor{
		Kind: eventbus.ActorKindSystem,
		ID:   core.SystemActorULID,
	}, payload)

	if err := h.publisher.Publish(ctx, event); err != nil {
		slog.WarnContext(
			ctx, "failed to publish command_response event",
			"character_id", char.ID.String(),
			"error", err,
		)
		return oops.Code("COMMAND_RESPONSE_EMIT_FAILED").Wrap(err)
	}
	return nil
}
