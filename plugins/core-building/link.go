// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corebuilding

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

const linkUsage = "Usage: link <exit> to <target>"

// linkArgs holds parsed link command arguments.
type linkArgs struct {
	exitName string
	target   string
}

// Matches: <exit> to <target>
var linkPattern = regexp.MustCompile(`^(\S+)\s+to\s+(.+)$`)

func parseLink(args string) (*linkArgs, error) {
	m := linkPattern.FindStringSubmatch(args)
	if m == nil {
		return nil, fmt.Errorf("%s", linkUsage)
	}

	target := strings.TrimSpace(m[2])
	// Remove surrounding quotes if present.
	if len(target) >= 2 && target[0] == '"' && target[len(target)-1] == '"' {
		target = target[1 : len(target)-1]
	}

	return &linkArgs{
		exitName: m[1],
		target:   target,
	}, nil
}

func resolveLocation(ctx context.Context, proxy plugins.ServiceProxy, subjectID, target string) (*plugins.LocationResult, error) {
	if strings.HasPrefix(target, "#") {
		id := target[1:]
		loc, err := proxy.QueryLocation(ctx, subjectID, id)
		if err != nil {
			return nil, fmt.Errorf("location not found: %s", id)
		}
		return loc, nil
	}

	loc, err := proxy.FindLocation(ctx, subjectID, target)
	if err != nil {
		return nil, fmt.Errorf("location not found: %s", target)
	}
	return loc, nil
}

func handleLink(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	if cmd.Args == "" {
		return &pluginsdk.CommandResponse{Output: linkUsage}, nil
	}

	parsed, err := parseLink(cmd.Args)
	if err != nil {
		return &pluginsdk.CommandResponse{Output: err.Error()}, nil
	}

	targetLoc, err := resolveLocation(ctx, proxy, cmd.CharacterID, parsed.target)
	if err != nil {
		return &pluginsdk.CommandResponse{Output: err.Error()}, nil
	}

	if err := proxy.CreateExit(ctx, cmd.CharacterID, cmd.LocationID, targetLoc.ID, parsed.exitName, plugins.CreateExitOpts{}); err != nil {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("Failed to create exit: %v", err),
		}, nil
	}

	return &pluginsdk.CommandResponse{
		Output: fmt.Sprintf("Linked %q to %q.", parsed.exitName, targetLoc.Name),
	}, nil
}
