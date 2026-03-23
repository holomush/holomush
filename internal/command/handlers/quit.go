// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// QuitHandler ends the character's session gracefully.
// It returns ErrSessionEnded so the gRPC layer can perform teardown
// (leave event, PG delete, hooks).
func QuitHandler(ctx context.Context, exec *command.CommandExecution) error {
	writeOutput(ctx, exec, "quit", "Goodbye!")
	return oops.Code("SESSION_ENDED").Wrap(command.ErrSessionEnded)
}
