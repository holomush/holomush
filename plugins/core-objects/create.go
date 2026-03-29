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

// createPattern matches: create <type> "<name>"
var createPattern = regexp.MustCompile(`^(\w+)\s+"([^"]+)"$`)

// handleCreate handles the "create" command.
// Syntax: create <type> "<name>"
// Types: object, location
func handleCreate(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := strings.TrimSpace(cmd.Args)
	if args == "" {
		return &pluginsdk.CommandResponse{
			Output: "Usage: create <type> \"<name>\"\n",
		}, nil
	}

	matches := createPattern.FindStringSubmatch(args)
	if matches == nil {
		return &pluginsdk.CommandResponse{
			Output: "Usage: create <type> \"<name>\"\n",
		}, nil
	}

	entityType := strings.ToLower(matches[1])
	name := matches[2]

	switch entityType {
	case "object":
		return createObject(ctx, cmd, proxy, name)
	case "location":
		return createLocation(ctx, cmd, proxy, name)
	default:
		return &pluginsdk.CommandResponse{
			Output: "Usage: create <type> \"<name>\" (valid types: object, location)\n",
		}, nil
	}
}

func createObject(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, name string) (*pluginsdk.CommandResponse, error) {
	result, err := proxy.CreateObject(ctx, cmd.CharacterID, name, "")
	if err != nil {
		return &pluginsdk.CommandResponse{
			Output: "Failed to create object.\n",
		}, nil
	}

	return &pluginsdk.CommandResponse{
		Output: fmt.Sprintf("Created object \"%s\" (#%s)\n", name, result.ID),
	}, nil
}

func createLocation(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, name string) (*pluginsdk.CommandResponse, error) {
	result, err := proxy.CreateLocation(ctx, cmd.CharacterID, name, "", "persistent")
	if err != nil {
		return &pluginsdk.CommandResponse{
			Output: "Failed to create location.\n",
		}, nil
	}

	return &pluginsdk.CommandResponse{
		Output: fmt.Sprintf("Created location \"%s\" (#%s)\n", name, result.ID),
	}, nil
}
