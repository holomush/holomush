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
		return &pluginsdk.CommandResponse{
			Output: "Usage: describe me <text> | describe here <text> | describe <target>=<text>\n",
		}, nil
	}

	target, text, err := parseDescribeArgs(args)
	if err != nil {
		return &pluginsdk.CommandResponse{Output: err.Error() + "\n"}, nil
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
		return &pluginsdk.CommandResponse{
			Output: "Failed to set description. Please try again.\n",
		}, nil
	}
	return &pluginsdk.CommandResponse{
		Output: "Description set.\n",
	}, nil
}

// describeTarget updates the description of a named target using the property system.
func describeTarget(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, target, text string) (*pluginsdk.CommandResponse, error) {
	props, err := proxy.FindPropertyByPrefix(ctx, "description")
	if err != nil || len(props) == 0 {
		return &pluginsdk.CommandResponse{
			Output: "Unknown property: description\n",
		}, nil
	}

	entityType, entityID := resolveTarget(cmd, target)
	if entityType == "" {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("Could not find target: %s\n", target),
		}, nil
	}

	if err := proxy.SetProperty(ctx, cmd.CharacterID, entityType, entityID, "description", text); err != nil {
		return &pluginsdk.CommandResponse{
			Output: "Failed to set description. Please try again.\n",
		}, nil
	}

	return &pluginsdk.CommandResponse{
		Output: "Description set.\n",
	}, nil
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
			return "object", target[1:]
		}
		return "", ""
	}
}
