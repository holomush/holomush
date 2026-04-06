// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func handleCommand(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	subcommand, subargs := splitSubcommand(req.Args)

	switch subcommand {
	case "create":
		return handleCreate(ctx, store, req, subargs)
	case "delete":
		return handleDelete(ctx, store, req, subargs)
	case "join":
		return handleJoin(ctx, store, req, subargs)
	case "leave":
		return handleLeave(ctx, store, req, subargs)
	case "list":
		return handleList(ctx, store, req)
	case "say":
		return handleSay(ctx, store, req, subargs)
	case "who":
		return handleWho(ctx, store, req, subargs)
	case "history":
		return handleHistory(ctx, store, req, subargs)
	case "gag":
		return handleGag(ctx, store, req, subargs)
	case "ungag":
		return handleUngag(ctx, store, req, subargs)
	case "":
		return pluginsdk.Errorf("Usage: channel <create|join|leave|list|say|who|history|gag|ungag>"), nil
	default:
		return pluginsdk.Errorf("Unknown channel subcommand: %s", subcommand), nil
	}
}

func handleCreate(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return pluginsdk.Errorf("Usage: channel create <name> [type]"), nil
	}

	name := parts[0]
	ct := channelTypePublic
	if len(parts) > 1 {
		ct = channelType(strings.ToLower(parts[1]))
	}

	ch, err := newChannel(name, ct, "", req.CharacterID)
	if err != nil {
		return pluginsdk.Errorf("Could not create channel: %s", err), nil
	}

	if err := store.createChannel(ctx, ch); err != nil {
		return pluginsdk.Errorf("Could not create channel: %s", err), nil
	}

	membership := &membershipRow{
		ChannelID: ch.ID,
		PlayerID:  req.CharacterID,
		Role:      roleOwner,
		JoinedAt:  time.Now().UTC(),
	}
	if err := store.addMembership(ctx, membership); err != nil {
		return pluginsdk.Errorf("Channel created but failed to set ownership: %s", err), nil
	}

	return pluginsdk.OK(fmt.Sprintf("Channel '%s' created (%s).", ch.Name, ch.Type)), nil
}

func handleDelete(ctx context.Context, store *channelStore, _ pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel delete <name>"), nil
	}
	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if err := store.archiveChannel(ctx, ch.ID); err != nil {
		return pluginsdk.Errorf("Could not delete channel: %s", err), nil
	}
	return pluginsdk.OK(fmt.Sprintf("Channel '%s' archived.", ch.Name)), nil
}

func handleJoin(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel join <name>"), nil
	}

	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if ch.isArchived() {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	// Check if already a member
	if _, memErr := store.getMembership(ctx, ch.ID, req.CharacterID); memErr == nil {
		return pluginsdk.Errorf("You are already a member of channel '%s'.", ch.Name), nil
	}

	count, err := store.countMembershipsByPlayer(ctx, req.CharacterID)
	if err != nil {
		return pluginsdk.Errorf("Could not join channel: %s", err), nil
	}
	if count >= maxMemberships {
		return pluginsdk.Errorf("You have reached the maximum of %d channel memberships.", maxMemberships), nil
	}

	membership := &membershipRow{
		ChannelID: ch.ID,
		PlayerID:  req.CharacterID,
		Role:      roleMember,
		JoinedAt:  time.Now().UTC(),
	}
	if err := store.addMembership(ctx, membership); err != nil {
		return pluginsdk.Errorf("Could not join channel: %s", err), nil
	}

	payload, _ := json.Marshal(core.ChannelNotificationPayload{
		ChannelID:     ch.ID,
		ChannelName:   ch.Name,
		CharacterID:   req.CharacterID,
		CharacterName: req.CharacterName,
		PlayerID:      req.CharacterID,
	})

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("You have joined channel '%s'.", ch.Name),
		Events: []pluginsdk.EmitEvent{{
			Stream:  ch.streamName(),
			Type:    pluginsdk.EventType(core.EventTypeChannelJoin),
			Payload: string(payload),
		}},
	}, nil
}

func handleLeave(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel leave <name>"), nil
	}

	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if err := store.removeMembership(ctx, ch.ID, req.CharacterID); err != nil {
		return pluginsdk.Errorf("Could not leave channel: %s", err), nil
	}

	payload, _ := json.Marshal(core.ChannelNotificationPayload{
		ChannelID:     ch.ID,
		ChannelName:   ch.Name,
		CharacterID:   req.CharacterID,
		CharacterName: req.CharacterName,
		PlayerID:      req.CharacterID,
	})

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("You have left channel '%s'.", ch.Name),
		Events: []pluginsdk.EmitEvent{{
			Stream:  ch.streamName(),
			Type:    pluginsdk.EventType(core.EventTypeChannelLeave),
			Payload: string(payload),
		}},
	}, nil
}

func handleList(ctx context.Context, store *channelStore, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	channels, err := store.listChannels(ctx, false)
	if err != nil {
		return pluginsdk.Errorf("Could not list channels: %s", err), nil
	}
	if len(channels) == 0 {
		return pluginsdk.OK("No channels available."), nil
	}

	var sb strings.Builder
	sb.WriteString("Available channels:\n")
	for _, ch := range channels {
		sb.WriteString(fmt.Sprintf("  %-20s %-8s %s\n", ch.Name, ch.Type, ch.Description))
	}
	return pluginsdk.OK(sb.String()), nil
}

func handleSay(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	channelName, message, err := parseChannelMessage(args)
	if err != nil {
		return pluginsdk.Errorf("Usage: channel say <name>=<message>"), nil
	}

	ch, err := store.getChannelByName(ctx, channelName)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	if _, memErr := store.getMembership(ctx, ch.ID, req.CharacterID); memErr != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	if len(message) > defaultMaxMessageLength {
		return pluginsdk.Errorf("Message too long (max %d characters).", defaultMaxMessageLength), nil
	}

	eventType := core.EventTypeChannelSay
	displayMessage := message
	if strings.HasPrefix(message, ":") {
		eventType = core.EventTypeChannelPose
		displayMessage = strings.TrimLeft(strings.TrimPrefix(message, ":"), " ")
	} else if strings.HasPrefix(message, ";") {
		eventType = core.EventTypeChannelPose
		displayMessage = strings.TrimPrefix(message, ";")
	}

	// Store message for history (dual-write)
	msg := &messageRow{
		ChannelID:  ch.ID,
		AuthorID:   req.CharacterID,
		AuthorName: req.CharacterName,
		Message:    displayMessage,
		EventType:  string(eventType),
		Source:     "game",
	}
	if insertErr := store.insertMessage(ctx, msg); insertErr != nil {
		return pluginsdk.Errorf("Could not send message: %s", insertErr), nil
	}

	payload, _ := json.Marshal(core.ChannelMessagePayload{
		ChannelID:     ch.ID,
		ChannelName:   ch.Name,
		CharacterID:   req.CharacterID,
		CharacterName: req.CharacterName,
		AuthorName:    req.CharacterName,
		Message:       displayMessage,
		Source:        "game",
	})

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Events: []pluginsdk.EmitEvent{{
			Stream:  ch.streamName(),
			Type:    pluginsdk.EventType(eventType),
			Payload: string(payload),
		}},
	}, nil
}

func handleWho(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel who <name>"), nil
	}

	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if _, memErr := store.getMembership(ctx, ch.ID, req.CharacterID); memErr != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	members, err := store.listMembersByChannel(ctx, ch.ID)
	if err != nil {
		return pluginsdk.Errorf("Could not list members: %s", err), nil
	}
	if len(members) == 0 {
		return pluginsdk.OK(fmt.Sprintf("Channel '%s' has no members.", ch.Name)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Members of '%s' (%d):\n", ch.Name, len(members)))
	for _, m := range members {
		role := ""
		if m.Role == roleOwner {
			role = " (owner)"
		} else if m.Role == roleOp {
			role = " (op)"
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n", m.PlayerID, role))
	}
	return pluginsdk.OK(sb.String()), nil
}

func handleHistory(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return pluginsdk.Errorf("Usage: channel history <name> [count]"), nil
	}

	channelName := parts[0]
	count := defaultHistoryCount
	if len(parts) > 1 {
		parsed, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil || parsed < 1 {
			return pluginsdk.Errorf("Count must be a positive number."), nil
		}
		if parsed > maxHistoryCount {
			count = maxHistoryCount
		} else {
			count = parsed
		}
	}

	ch, err := store.getChannelByName(ctx, channelName)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	membership, memErr := store.getMembership(ctx, ch.ID, req.CharacterID)
	if memErr != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	messages, err := store.getHistory(ctx, ch.ID, count, membership.JoinedAt)
	if err != nil {
		return pluginsdk.Errorf("Could not retrieve history: %s", err), nil
	}
	if len(messages) == 0 {
		return pluginsdk.OK(fmt.Sprintf("No history for channel '%s'.", ch.Name)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("History for '%s' (last %d):\n", ch.Name, len(messages)))
	for _, m := range messages {
		ts := m.CreatedAt.Format("15:04")
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", ts, m.AuthorName, m.Message))
	}
	return pluginsdk.OK(sb.String()), nil
}

func handleGag(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel gag <name>"), nil
	}
	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if err := store.setGag(ctx, ch.ID, req.CharacterID, true); err != nil {
		return pluginsdk.Errorf("Could not gag channel: %s", err), nil
	}
	return pluginsdk.OK(fmt.Sprintf("Channel '%s' gagged for this character.", ch.Name)), nil
}

func handleUngag(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel ungag <name>"), nil
	}
	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if err := store.setGag(ctx, ch.ID, req.CharacterID, false); err != nil {
		return pluginsdk.Errorf("Could not ungag channel: %s", err), nil
	}
	return pluginsdk.OK(fmt.Sprintf("Channel '%s' ungagged for this character.", ch.Name)), nil
}

// --- Helpers ---

func splitSubcommand(args string) (string, string) {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return "", ""
	}
	idx := strings.IndexByte(trimmed, ' ')
	if idx == -1 {
		return trimmed, ""
	}
	return trimmed[:idx], strings.TrimSpace(trimmed[idx+1:])
}

func parseChannelMessage(args string) (channelName, message string, err error) {
	if args == "" {
		return "", "", fmt.Errorf("empty input")
	}
	if idx := strings.IndexByte(args, '='); idx > 0 {
		return args[:idx], strings.TrimSpace(args[idx+1:]), nil
	}
	idx := strings.IndexByte(args, ' ')
	if idx == -1 {
		return "", "", fmt.Errorf("no message provided")
	}
	return args[:idx], strings.TrimSpace(args[idx+1:]), nil
}
