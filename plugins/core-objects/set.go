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
		return pluginsdk.Errorf("Usage: set <property> of <target> to <value>"), nil
	}

	matches := setPattern.FindStringSubmatch(args)
	if matches == nil {
		return pluginsdk.Errorf("Usage: set <property> of <target> to <value>"), nil
	}

	propertyPrefix := matches[1]
	target := matches[2]
	value := matches[3]

	props, err := proxy.FindPropertyByPrefix(ctx, propertyPrefix)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("set: failed to find property %q: %v", propertyPrefix, err))
		return pluginsdk.Failuref("Unable to set property right now. Please try again."), nil
	}
	if len(props) == 0 {
		return pluginsdk.Errorf("Unknown property: %s", propertyPrefix), nil
	}
	if len(props) > 1 {
		names := make([]string, len(props))
		for i, p := range props {
			names[i] = p.Name
		}
		return pluginsdk.Errorf("Ambiguous property %q; matches: %s", propertyPrefix, strings.Join(names, ", ")), nil
	}

	propName := props[0].Name

	entityType, entityID := resolveTarget(cmd, target)
	if entityType == "" {
		return pluginsdk.Errorf("Could not find target: %s", target), nil
	}

	if err := proxy.SetProperty(ctx, cmd.CharacterID, entityType, entityID, propName, value); err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("set: failed to set %s on %s: %v", propName, target, err))
		return pluginsdk.Failuref("Unable to set %s right now. Please try again.", propName), nil
	}

	return pluginsdk.OK(fmt.Sprintf("Set %s of %s.\n", propName, target)), nil
}
