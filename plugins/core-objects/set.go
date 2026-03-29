// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package coreobjects

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// setPattern matches: set <property> of <target> to <value>
var setPattern = regexp.MustCompile(`^(\w+)\s+of\s+(\S+)\s+to\s+(.+)$`)

// handleSet handles the "set" command.
// Syntax: set <property> of <target> to <value>
// Properties support prefix matching (desc -> description).
func handleSet(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := strings.TrimSpace(cmd.Args)
	if args == "" {
		return &pluginsdk.CommandResponse{
			Output: "Usage: set <property> of <target> to <value>\n",
		}, nil
	}

	matches := setPattern.FindStringSubmatch(args)
	if matches == nil {
		return &pluginsdk.CommandResponse{
			Output: "Usage: set <property> of <target> to <value>\n",
		}, nil
	}

	propertyPrefix := matches[1]
	target := matches[2]
	value := matches[3]

	props, err := proxy.FindPropertyByPrefix(ctx, propertyPrefix)
	if err != nil || len(props) == 0 {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("Unknown property: %s\n", propertyPrefix),
		}, nil
	}

	propName := props[0].Name

	entityType, entityID := resolveTarget(cmd, target)
	if entityType == "" {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("Could not find target: %s\n", target),
		}, nil
	}

	if err := proxy.SetProperty(ctx, cmd.CharacterID, entityType, entityID, propName, value); err != nil {
		return &pluginsdk.CommandResponse{
			Output: "Failed to set property. Please try again.\n",
		}, nil
	}

	return &pluginsdk.CommandResponse{
		Output: fmt.Sprintf("Set %s of %s.\n", propName, target),
	}, nil
}
