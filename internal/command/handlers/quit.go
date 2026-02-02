// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"fmt"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// QuitHandler ends the character's session gracefully.
func QuitHandler(_ context.Context, exec *command.CommandExecution) error {
	//nolint:errcheck // output write error is acceptable; player display is best-effort
	_, _ = fmt.Fprintln(exec.Output, "Goodbye!")

	if err := exec.Services.Session.EndSession(exec.CharacterID); err != nil {
		return oops.Code(command.CodeWorldError).
			With("message", "Unable to end session. Please try again.").
			Wrap(err)
	}

	return nil
}
