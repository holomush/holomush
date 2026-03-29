// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package coreobjects

import (
	"context"
	"fmt"
	"strings"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// handleDescribe handles "describe" and "desc" commands.
//
// Syntax:
//   - describe me <text>        -- set own character description
//   - describe here <text>      -- set current location description (via property)
//   - describe <target>=<text>  -- set named target description (via property)
func handleDescribe(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := strings.TrimSpace(cmd.Args)
	if args == "" {
		return pluginsdk.Errorf("Usage: describe me <text> | describe here <text> | describe <target>=<text>"), nil
	}

	target, text, err := parseDescribeArgs(args)
	if err != nil {
		return pluginsdk.Errorf("%s", err.Error()), nil
	}

	if target == "me" {
		return describeSelf(ctx, cmd, proxy, text)
	}
	return describeTarget(ctx, cmd, proxy, target, text)
}

// parseDescribeArgs parses describe command arguments into target and text.
func parseDescribeArgs(args string) (target, text string, err error) {
	if strings.HasPrefix(args, "me ") {
		text = strings.TrimSpace(args[3:])
		if text == "" {
			return "", "", fmt.Errorf("usage: describe me <text>")
		}
		return "me", text, nil
	}

	if strings.HasPrefix(args, "here ") {
		text = strings.TrimSpace(args[5:])
		if text == "" {
			return "", "", fmt.Errorf("usage: describe here <text>")
		}
		return "here", text, nil
	}

	idx := strings.IndexByte(args, '=')
	if idx > 0 {
		tgt := strings.TrimSpace(args[:idx])
		txt := strings.TrimSpace(args[idx+1:])
		if tgt == "" || txt == "" {
			return "", "", fmt.Errorf("usage: describe <target>=<text>")
		}
		return tgt, txt, nil
	}

	return "", "", fmt.Errorf("usage: describe me <text> | describe here <text> | describe <target>=<text>")
}

// describeSelf updates the calling character's own description.
func describeSelf(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, text string) (*pluginsdk.CommandResponse, error) {
	if err := proxy.UpdateCharacterDescription(ctx, cmd.CharacterID, cmd.CharacterID, text); err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("describe: failed to update character description: %v", err))
		return pluginsdk.Failuref("Unable to set description right now. Please try again."), nil
	}
	return pluginsdk.OK("Description set.\n"), nil
}

// describeTarget updates the description of a named target using the property system.
func describeTarget(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, target, text string) (*pluginsdk.CommandResponse, error) {
	props, err := proxy.FindPropertyByPrefix(ctx, "description")
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("describe: failed to find property: %v", err))
		return pluginsdk.Failuref("Unable to set description right now. Please try again."), nil
	}
	if len(props) == 0 {
		return pluginsdk.Errorf("Unknown property: description"), nil
	}

	propName := props[0].Name

	entityType, entityID := resolveTarget(cmd, target)
	if entityType == "" {
		return pluginsdk.Errorf("Could not find target: %s", target), nil
	}

	if err := proxy.SetProperty(ctx, cmd.CharacterID, entityType, entityID, propName, text); err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("describe: failed to set property: %v", err))
		return pluginsdk.Failuref("Unable to set description right now. Please try again."), nil
	}

	return pluginsdk.OK("Description set.\n"), nil
}

// resolveTarget resolves a target string to an entity type and ID.
func resolveTarget(cmd pluginsdk.CommandRequest, target string) (entityType, entityID string) {
	switch target {
	case "here":
		return "location", cmd.LocationID
	case "me":
		return "character", cmd.CharacterID
	default:
		if strings.HasPrefix(target, "#") {
			id := strings.TrimSpace(target[1:])
			if id == "" {
				return "", ""
			}
			return "object", id
		}
		return "", ""
	}
}
