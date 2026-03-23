// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// SayHandler broadcasts a say event to the character's current location stream.
// The sender sees the event via their location stream subscription — no
// command_response output is emitted.
func SayHandler(ctx context.Context, exec *command.CommandExecution) error {
	payload, err := json.Marshal(core.SayPayload{
		CharacterName: exec.CharacterName(),
		Message:       exec.Args,
	})
	if err != nil {
		return oops.With("operation", "marshal_say_payload").Wrap(err)
	}

	event := core.Event{
		ID:        core.NewULID(),
		Stream:    world.LocationStream(exec.LocationID()),
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
		Payload:   payload,
	}

	if err := exec.Services().Events().Append(ctx, event); err != nil {
		return oops.With("operation", "append_say_event").Wrap(err)
	}

	return nil
}
