// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// PemitHandler handles the pemit command for private GM narration.
//
// Syntax: pemit <character>=<message>
//
// Emits EventTypePemit on the target's character stream. The target sees the
// raw message text; the sender receives a confirmation. Does not require sender
// and target to be in the same location.
func PemitHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)

	idx := strings.IndexByte(args, '=')
	if idx <= 0 {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("pemit", "pemit <character>=<message>")
	}

	targetName := strings.TrimSpace(args[:idx])
	message := args[idx+1:]

	if message == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("pemit", "pemit <character>=<message>")
	}

	// Resolve target session by character name (case-insensitive).
	targetSession, err := exec.Services().Session().FindByCharacterName(ctx, targetName)
	if err != nil {
		return oops.With("operation", "find_target_session").Wrap(err)
	}
	if targetSession == nil {
		writeOutputf(ctx, exec, "pemit", "No character found named %q.\n", targetName)
		return nil
	}

	targetCharID := targetSession.CharacterID

	payload, err := json.Marshal(core.PemitPayload{
		SenderID:   exec.CharacterID().String(),
		SenderName: exec.CharacterName(),
		TargetID:   targetCharID.String(),
		Message:    message,
	})
	if err != nil {
		return oops.With("operation", "marshal_pemit_payload").Wrap(err)
	}

	event := core.Event{
		ID:        core.NewULID(),
		Stream:    world.CharacterStream(targetCharID),
		Type:      core.EventTypePemit,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
		Payload:   payload,
	}
	if err := exec.Services().Events().Append(ctx, event); err != nil {
		return oops.With("operation", "append_pemit_event").Wrap(err)
	}

	writeOutput(ctx, exec, "pemit", fmt.Sprintf("Pemit sent to %s.\n", targetSession.CharacterName))
	return nil
}
