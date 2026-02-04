// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// QuitHandler ends the character's session gracefully.
func QuitHandler(ctx context.Context, exec *command.CommandExecution) error {
	// Output write errors are logged but don't fail the command - the session end will proceed
	writeOutput(ctx, exec, "quit", "Goodbye!")

	if err := exec.Services.Session().EndSession(exec.CharacterID); err != nil {
		return oops.Code(command.CodeWorldError).
			With("message", "Unable to end session. Please try again.").
			Wrap(err)
	}

	return nil
}
