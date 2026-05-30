// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/samber/oops"
)

// sessionTerminalCommitTimeout bounds how long EndSession will block the
// caller waiting for the session_ended event to persist. It decouples the
// audit-critical append from the caller's ctx (which may have been cancelled
// by a client hangup).
const sessionTerminalCommitTimeout = 5 * time.Second

// EndSession emits a session_ended event on the character's own stream.
//
// Context discipline: EndSession uses a fresh background context with a
// bounded timeout for the append, NOT the caller's ctx. A client that hangs
// up mid-quit (ctx cancel) still needs the session_ended event persisted.
// The caller's ctx is still honored for cancellation of pre-append work.
//
// Actor selection per Design Decision #1:
//   - cause=quit: ActorCharacter (the character chose to quit)
//   - all other causes: ActorSystem{id="system"} (administrative/infra action)
//
// See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md
// for the full design rationale and load-bearing invariants.
func (e *Engine) EndSession(
	_ context.Context,
	char CharacterRef,
	sessionID string,
	cause string,
	reason string,
) error {
	// NOTE: The caller's ctx is intentionally NOT consulted here. A pre-
	// cancelled ctx (client hangup just before EndSession ran) MUST NOT skip
	// the audit-critical session_ended append. The append below uses a fresh
	// background ctx bounded by sessionTerminalCommitTimeout, which is the
	// only deadline that gates this write.

	payload, err := json.Marshal(SessionEndedPayload{
		SessionID:   sessionID,
		CharacterID: char.ID.String(),
		Cause:       cause,
		Reason:      reason,
	})
	if err != nil {
		return oops.With("operation", "marshal_session_ended_payload").Wrap(err)
	}

	actor := Actor{Kind: ActorSystem, ID: ActorSystemID}
	if cause == SessionEndedCauseQuit {
		actor = Actor{Kind: ActorCharacter, ID: char.ID.String()}
	}

	event := NewEvent(
		"character."+char.ID.String(),
		EventTypeSessionEnded,
		actor,
		payload,
	)

	appendCtx, cancel := context.WithTimeout(context.Background(), sessionTerminalCommitTimeout)
	defer cancel()

	if err := e.store.Append(appendCtx, event); err != nil {
		return oops.Code("SESSION_ENDED_APPEND_FAILED").
			With("session_id", sessionID).
			With("cause", cause).
			Wrap(err)
	}

	return nil
}
