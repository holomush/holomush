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
		return pluginsdk.Errorf("Usage: create <type> \"<name>\""), nil
	}

	matches := createPattern.FindStringSubmatch(args)
	if matches == nil {
		return pluginsdk.Errorf("Usage: create <type> \"<name>\""), nil
	}

	entityType := strings.ToLower(matches[1])
	name := matches[2]

	switch entityType {
	case "object":
		return createObject(ctx, cmd, proxy, name)
	case "location":
		return createLocation(ctx, cmd, proxy, name)
	default:
		return pluginsdk.Errorf("Usage: create <type> \"<name>\" (valid types: object, location)"), nil
	}
}

func createObject(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, name string) (*pluginsdk.CommandResponse, error) {
	result, err := proxy.CreateObject(ctx, cmd.CharacterID, name, "")
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("create: failed to create object %q: %v", name, err))
		return pluginsdk.Failuref("Unable to create object right now. Please try again."), nil
	}

	return pluginsdk.OK(fmt.Sprintf("Created object \"%s\" (#%s)\n", name, result.ID)), nil
}

func createLocation(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, name string) (*pluginsdk.CommandResponse, error) {
	result, err := proxy.CreateLocation(ctx, cmd.CharacterID, name, "", "persistent")
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("create: failed to create location %q: %v", name, err))
		return pluginsdk.Failuref("Unable to create location right now. Please try again."), nil
	}

	return pluginsdk.OK(fmt.Sprintf("Created location \"%s\" (#%s)\n", name, result.ID)), nil
}
