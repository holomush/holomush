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
	if target == "#" {
		return nil, fmt.Errorf("missing location ID after #")
	}
	if strings.HasPrefix(target, "#") {
		id := target[1:]
		loc, err := proxy.QueryLocation(ctx, subjectID, id)
		if err != nil {
			proxy.Log(ctx, "error", fmt.Sprintf("link: failed to query location %s: %v", id, err))
			return nil, fmt.Errorf("unable to find location %q", id)
		}
		return loc, nil
	}

	loc, err := proxy.FindLocation(ctx, subjectID, target)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("link: failed to find location %q: %v", target, err))
		return nil, fmt.Errorf("unable to find location %q", target)
	}
	return loc, nil
}

func handleLink(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	if cmd.Args == "" {
		return pluginsdk.Errorf("%s", linkUsage), nil
	}

	parsed, err := parseLink(cmd.Args)
	if err != nil {
		return pluginsdk.Errorf("%s", err.Error()), nil
	}

	targetLoc, err := resolveLocation(ctx, proxy, cmd.CharacterID, parsed.target)
	if err != nil {
		return pluginsdk.Errorf("%s", err.Error()), nil
	}

	if err := proxy.CreateExit(ctx, cmd.CharacterID, cmd.LocationID, targetLoc.ID, parsed.exitName, plugins.CreateExitOpts{}); err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("link: failed to create exit %q: %v", parsed.exitName, err))
		return pluginsdk.Failuref("Unable to create exit right now. Please try again."), nil
	}

	return pluginsdk.OK(fmt.Sprintf("Linked %q to %q.", parsed.exitName, targetLoc.Name)), nil
}
