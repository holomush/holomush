// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package presence

import (
	"context"
	"encoding/json"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventvocab"
)

// sessionTerminalCommitTimeout bounds how long EmitSessionEnded will block
// the caller waiting for the session_ended event to persist. It decouples
// the audit-critical publish from the caller's ctx (which may have been
// cancelled by a client hangup).
const sessionTerminalCommitTimeout = 5 * time.Second

// EmitSessionEnded publishes a session_ended event on the character's own
// stream.
//
// Context discipline: EmitSessionEnded uses a fresh background context with
// a bounded timeout for the publish, NOT the caller's ctx. A client that
// hangs up mid-quit (ctx cancel) still needs the session_ended event
// persisted. The caller's ctx is still honored for cancellation of
// pre-publish work.
//
// Actor selection per Design Decision #1:
//   - cause=quit: ActorCharacter (the character chose to quit)
//   - all other causes: ActorSystem{id="system"} (administrative/infra action)
//
// See docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md
// for the full design rationale and load-bearing invariants.
func (e *Emitter) EmitSessionEnded(
	_ context.Context,
	char core.CharacterRef,
	sessionID string,
	cause string,
	reason string,
) error {
	// NOTE: The caller's ctx is intentionally NOT consulted here. A pre-
	// cancelled ctx (client hangup just before EmitSessionEnded ran) MUST
	// NOT skip the audit-critical session_ended publish. The publish below
	// uses a fresh background ctx bounded by sessionTerminalCommitTimeout,
	// which is the only deadline that gates this write.

	payload, err := json.Marshal(core.SessionEndedPayload{
		SessionID:   sessionID,
		CharacterID: char.ID.String(),
		Cause:       cause,
		Reason:      reason,
	})
	if err != nil {
		return oops.With("operation", "marshal_session_ended_payload").Wrap(err)
	}

	actor := core.Actor{Kind: core.ActorSystem, ID: core.ActorSystemID}
	if cause == core.SessionEndedCauseQuit {
		actor = core.Actor{Kind: core.ActorCharacter, ID: char.ID.String()}
	}

	ev, err := e.buildEvent(
		"character."+char.ID.String(),
		eventvocab.EventTypeSessionEnded,
		actor,
		payload,
	)
	if err != nil {
		return err
	}

	appendCtx, cancel := context.WithTimeout(context.Background(), sessionTerminalCommitTimeout)
	defer cancel()

	if err := e.pub.Publish(appendCtx, ev); err != nil {
		return oops.Code("SESSION_ENDED_APPEND_FAILED").
			With("session_id", sessionID).
			With("cause", cause).
			Wrap(err)
	}

	return nil
}
