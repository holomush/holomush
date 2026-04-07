// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// dispatchCommand handles the "scene" top-level command. Phase 1 supports
// only the "create" and "info" subcommands; the rest return "not yet
// implemented" so future phases can plug in their handlers without
// changing this dispatcher.
//
// Subcommand parsing is intentionally simple: the first whitespace-separated
// token is the subcommand, the rest is its argument string. The "scenes"
// command (no subcommand, browses the board) lands in Phase 8 and is
// handled separately.
func (p *scenePlugin) dispatchCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	ctx, span := startSpan(ctx, "scene.command.dispatch",
		attribute.String("subject_id", req.CharacterID),
	)
	defer span.End()

	sub, rest := splitSubcommand(req.Args)
	span.SetAttributes(attribute.String("subcommand", sub))

	if sub == "" {
		return pluginsdk.Errorf("Usage: scene <subcommand> [args]\nKnown subcommands: create, info"), nil
	}

	switch sub {
	case "create":
		return p.handleCreate(ctx, req, rest)
	case "info":
		return p.handleInfo(ctx, req, rest)
	default:
		return pluginsdk.Errorf("Unknown scene subcommand %q. Known subcommands: create, info.", sub), nil
	}
}

// handleCreate creates a new scene with the given title. The title is the
// rest of the command line (allowing whitespace) so "scene create The Manor"
// works without quoting.
func (p *scenePlugin) handleCreate(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	title := strings.TrimSpace(args)
	if title == "" {
		return pluginsdk.Errorf("Usage: scene create <title>"), nil
	}

	resp, err := p.service.CreateScene(ctx, &scenev1.CreateSceneRequest{
		CharacterId: req.CharacterID,
		Title:       title,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to create scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Scene created: %s", resp.GetScene().GetId()),
	}, nil
}

// handleInfo shows scene metadata for the given scene ID. Per the read-own-scene
// policy, the host's ABAC engine has already verified the caller is the owner
// before this code runs (when invoked via the dispatcher's full ABAC pipeline);
// in unit tests, ABAC is bypassed and the service is called directly.
func (p *scenePlugin) handleInfo(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene info <scene id>"), nil
	}

	resp, err := p.service.GetScene(ctx, &scenev1.GetSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to get scene: %v", err), nil
	}

	info := resp.GetScene()
	var b strings.Builder
	fmt.Fprintf(&b, "Scene: %s (%s)\n", info.GetTitle(), info.GetId())
	fmt.Fprintf(&b, "Owner: %s\n", info.GetOwnerId())
	fmt.Fprintf(&b, "State: %s\n", info.GetState())
	fmt.Fprintf(&b, "Visibility: %s\n", info.GetVisibility())
	if info.GetDescription() != "" {
		fmt.Fprintf(&b, "Description: %s\n", info.GetDescription())
	}
	if info.GetLocationId() != "" {
		fmt.Fprintf(&b, "Location: %s\n", info.GetLocationId())
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: b.String(),
	}, nil
}

// splitSubcommand splits args into the first whitespace-delimited token and
// the remainder. Used by dispatchCommand to extract the subcommand name.
func splitSubcommand(args string) (sub, rest string) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", ""
	}
	idx := strings.IndexAny(args, " \t")
	if idx < 0 {
		return args, ""
	}
	return args[:idx], strings.TrimSpace(args[idx+1:])
}
