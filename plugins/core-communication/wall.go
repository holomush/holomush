// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package communication

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/oops"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// wallUrgency represents the urgency level of a wall message.
type wallUrgency string

const (
	wallUrgencyInfo     wallUrgency = "info"
	wallUrgencyWarning  wallUrgency = "warning"
	wallUrgencyCritical wallUrgency = "critical"
)

var urgencyPrefixes = map[wallUrgency]string{
	wallUrgencyInfo:     "[ADMIN ANNOUNCEMENT]",
	wallUrgencyWarning:  "[ADMIN WARNING]",
	wallUrgencyCritical: "[ADMIN CRITICAL]",
}

// WallHandler handles the "wall" command by broadcasting an announcement
// to all connected sessions.
type WallHandler struct{}

func (h *WallHandler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := strings.TrimSpace(cmd.Args)
	if args == "" {
		return &pluginsdk.CommandResponse{
			Output: "Usage: wall [info|warning|critical] <message>",
		}, nil
	}

	urgency, message := parseWallArgs(args)
	if message == "" {
		return &pluginsdk.CommandResponse{
			Output: "Usage: wall [info|warning|critical] <message>",
		}, nil
	}

	sessions, err := proxy.ListActiveSessions(ctx)
	if err != nil {
		return nil, oops.With("operation", "list_active_sessions").Wrap(err)
	}

	prefix := urgencyPrefixes[urgency]
	announcement := fmt.Sprintf("%s %s: %s", prefix, cmd.CharacterName, message)

	proxy.Log(ctx, "info", fmt.Sprintf("admin wall: admin=%s urgency=%s sessions=%d message=%s",
		cmd.CharacterName, urgency, len(sessions), message))

	if err := proxy.BroadcastSystemMessage(ctx, announcement); err != nil {
		return nil, oops.With("operation", "broadcast_wall").Wrap(err)
	}

	sessionWord := "sessions"
	if len(sessions) == 1 {
		sessionWord = "session"
	}

	return &pluginsdk.CommandResponse{
		Output: fmt.Sprintf("Announcement sent to %d %s.", len(sessions), sessionWord),
	}, nil
}

func parseWallArgs(args string) (wallUrgency, string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) == 1 {
		return wallUrgencyInfo, parts[0]
	}

	switch strings.ToLower(parts[0]) {
	case "info":
		return wallUrgencyInfo, parts[1]
	case "warning", "warn":
		return wallUrgencyWarning, parts[1]
	case "critical", "crit":
		return wallUrgencyCritical, parts[1]
	default:
		return wallUrgencyInfo, args
	}
}
