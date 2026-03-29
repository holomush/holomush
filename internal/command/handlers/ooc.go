// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// OOCHandler broadcasts an out-of-character communication event to the
// character's current location stream. The sender sees the event via their
// location stream subscription — no command_response output is emitted.
func OOCHandler(ctx context.Context, exec *command.CommandExecution) error {
	msg := strings.TrimSpace(exec.Args)
	if msg == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("ooc", "ooc <message>")
	}

	var style, text string
	switch {
	case strings.HasPrefix(msg, ":"):
		style = "pose"
		text = msg[1:]
	case strings.HasPrefix(msg, ";"):
		style = "semipose"
		text = msg[1:]
	default:
		style = "say"
		text = msg
	}

	payload, err := json.Marshal(core.OOCPayload{
		CharacterName: exec.CharacterName(),
		Message:       text,
		Style:         style,
	})
	if err != nil {
		return oops.With("operation", "marshal_ooc_payload").Wrap(err)
	}

	event := core.Event{
		ID:        core.NewULID(),
		Stream:    world.LocationStream(exec.LocationID()),
		Type:      core.EventTypeOOC,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
		Payload:   payload,
	}

	if err := exec.Services().Events().Append(ctx, event); err != nil {
		return oops.With("operation", "append_ooc_event").Wrap(err)
	}

	return nil
}
