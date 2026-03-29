// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corebuilding

import (
	"context"
	"fmt"
	"regexp"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

const digUsage = `Usage: dig <exit> to "<location>" [return <exit>]`

// digArgs holds parsed dig command arguments.
type digArgs struct {
	exitName     string
	locationName string
	returnExit   string
}

// Matches: <exit> to "<location>" [return <exit>]
var digPattern = regexp.MustCompile(`^(\S+)\s+to\s+"([^"]+)"(?:\s+return\s+(\S+))?$`)

func parseDig(args string) (*digArgs, error) {
	m := digPattern.FindStringSubmatch(args)
	if m == nil {
		return nil, fmt.Errorf("%s", digUsage)
	}
	return &digArgs{
		exitName:     m[1],
		locationName: m[2],
		returnExit:   m[3], // empty string if not present
	}, nil
}

func handleDig(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	if cmd.Args == "" {
		return &pluginsdk.CommandResponse{Output: digUsage}, nil
	}

	parsed, err := parseDig(cmd.Args)
	if err != nil {
		return &pluginsdk.CommandResponse{Output: err.Error()}, nil
	}

	loc, err := proxy.CreateLocation(ctx, cmd.CharacterID, parsed.locationName, "", "persistent")
	if err != nil {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("Failed to create location: %v", err),
		}, nil
	}

	opts := plugins.CreateExitOpts{}
	if parsed.returnExit != "" {
		opts.Bidirectional = true
		opts.ReturnName = parsed.returnExit
	}

	if err := proxy.CreateExit(ctx, cmd.CharacterID, cmd.LocationID, loc.ID, parsed.exitName, opts); err != nil {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("Location created but exit failed: %v", err),
		}, nil
	}

	msg := fmt.Sprintf("Created %q with exit %q", parsed.locationName, parsed.exitName)
	if parsed.returnExit != "" {
		msg += fmt.Sprintf(" and return exit %q", parsed.returnExit)
	}
	msg += "."

	return &pluginsdk.CommandResponse{Output: msg}, nil
}
