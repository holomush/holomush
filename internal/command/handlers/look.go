// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package handlers provides command handler implementations.
package handlers

import (
	"context"
	"fmt"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// LookHandler displays the current location's name and description.
func LookHandler(ctx context.Context, exec *command.CommandExecution) error {
	subjectID := "char:" + exec.CharacterID.String()

	loc, err := exec.Services.World.GetLocation(ctx, subjectID, exec.LocationID)
	if err != nil {
		return oops.Code(command.CodeWorldError).
			With("message", "You can't see anything here.").
			Wrap(err)
	}

	//nolint:errcheck // output write error is acceptable; player display is best-effort
	_, _ = fmt.Fprintf(exec.Output, "%s\n%s\n", loc.Name, loc.Description)
	return nil
}
