// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// dispatchCommand handles the "scene" top-level command. Phase 2 supports
// the create, info, end, pause, resume, and set subcommands; later phases
// will plug additional handlers in here without changing the dispatcher
// shape.
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
		return pluginsdk.Errorf("Usage: scene <subcommand> [args]\nKnown subcommands: create, info, end, pause, resume, set, join, leave, invite, kick, transfer"), nil
	}

	switch sub {
	case "create":
		return p.handleCreate(ctx, req, rest)
	case "info":
		return p.handleInfo(ctx, req, rest)
	case "end":
		return p.handleEnd(ctx, req, rest)
	case "pause":
		return p.handlePause(ctx, req, rest)
	case "resume":
		return p.handleResume(ctx, req, rest)
	case "set":
		return p.handleSet(ctx, req, rest)
	case "join":
		return p.handleJoin(ctx, req, rest)
	case "leave":
		return p.handleLeave(ctx, req, rest)
	case "invite":
		return p.handleInvite(ctx, req, rest)
	case "kick":
		return p.handleKick(ctx, req, rest)
	case "transfer":
		return p.handleTransfer(ctx, req, rest)
	default:
		return pluginsdk.Errorf("Unknown scene subcommand %q. Known subcommands: create, info, end, pause, resume, set, join, leave, invite, kick, transfer.", sub), nil
	}
}

// handleCreate creates a new scene with the given title. The title is the
// rest of the command line (allowing whitespace) so "scene create The Manor"
// works without quoting.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
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
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
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

// handleEnd ends the specified scene. Owner ABAC enforcement is gateway-side
// (the host's ABAC engine evaluates end-own-scene before this code runs in
// production); in unit tests, ABAC is bypassed so the test must use the
// scene owner's character ID.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleEnd(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene end <scene id>"), nil
	}

	_, err := p.service.EndScene(ctx, &scenev1.EndSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to end scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Scene %s ended.", sceneID),
	}, nil
}

// handlePause transitions an active scene to the paused state. Owner-only.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handlePause(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene pause <scene id>"), nil
	}

	_, err := p.service.PauseScene(ctx, &scenev1.PauseSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to pause scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Scene %s paused.", sceneID),
	}, nil
}

// handleResume transitions a paused scene back to active. Owner-only in
// Phase 2; Phase 3 widens to any member per spec D6.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleResume(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene resume <scene id>"), nil
	}

	_, err := p.service.ResumeScene(ctx, &scenev1.ResumeSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to resume scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Scene %s resumed.", sceneID),
	}, nil
}

// handleSet parses "scene set <id> field=value" and applies the change.
// Phase 2 supports the five scalar mutable fields via this command. Repeated
// fields (tags, content_warnings) are not exposed via the terminal command
// surface because the simple field=value syntax can't express list semantics
// cleanly. They remain editable via UpdateScene gRPC for richer clients.
//
// The command constructs an UpdateSceneRequest with a FieldMask containing
// the single field path being set. The service handler then applies it via
// the standard mask-iteration path, getting the same per-field validation
// any other UpdateScene call would.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleSet(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return pluginsdk.Errorf("Usage: scene set <scene id> field=value"), nil
	}

	sceneID, rest := splitSubcommand(args)
	if sceneID == "" || rest == "" {
		return pluginsdk.Errorf("Usage: scene set <scene id> field=value"), nil
	}

	eqIdx := strings.IndexByte(rest, '=')
	if eqIdx < 0 {
		return pluginsdk.Errorf("Usage: scene set <scene id> field=value"), nil
	}
	field := strings.TrimSpace(rest[:eqIdx])
	value := strings.TrimSpace(rest[eqIdx+1:])

	update := &scenev1.UpdateSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{field}},
	}

	// Set the request field matching `field`. The handler would reject
	// unknown mask paths via buildSceneUpdate, but we pre-validate here
	// so we can return a more helpful command-style error message before
	// bouncing off the gRPC handler.
	switch field {
	case "title":
		update.Title = value
	case "description":
		update.Description = value
	case "visibility":
		update.Visibility = value
	case "pose_order_mode":
		update.PoseOrderMode = value
	case "location_id":
		update.LocationId = value
	default:
		return pluginsdk.Errorf("unknown field %q. Known fields: title, description, visibility, pose_order_mode, location_id", field), nil
	}

	_, err := p.service.UpdateScene(ctx, update)
	if err != nil {
		return pluginsdk.Errorf("Failed to update scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Scene %s updated: %s = %s", sceneID, field, value),
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

// handleJoin parses "scene join <scene-id>" and calls JoinScene.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleJoin(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene join <scene id>"), nil
	}

	_, err := p.service.JoinScene(ctx, &scenev1.JoinSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to join scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Joined scene %s.", sceneID),
	}, nil
}

// handleLeave parses "scene leave <scene-id>" and calls LeaveScene.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleLeave(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene leave <scene id>"), nil
	}

	_, err := p.service.LeaveScene(ctx, &scenev1.LeaveSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to leave scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Left scene %s.", sceneID),
	}, nil
}

// handleInvite parses "scene invite <scene-id> <character>".
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleInvite(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, rest := splitSubcommand(args)
	target := strings.TrimSpace(rest)
	if sceneID == "" || target == "" {
		return pluginsdk.Errorf("Usage: scene invite <scene id> <character>"), nil
	}

	_, err := p.service.InviteToScene(ctx, &scenev1.InviteToSceneRequest{
		CharacterId:       req.CharacterID,
		SceneId:           sceneID,
		TargetCharacterId: target,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to invite: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Invited %s to scene %s.", target, sceneID),
	}, nil
}

// handleKick parses "scene kick <scene-id> <character>".
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleKick(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, rest := splitSubcommand(args)
	target := strings.TrimSpace(rest)
	if sceneID == "" || target == "" {
		return pluginsdk.Errorf("Usage: scene kick <scene id> <character>"), nil
	}

	_, err := p.service.KickFromScene(ctx, &scenev1.KickFromSceneRequest{
		CharacterId:       req.CharacterID,
		SceneId:           sceneID,
		TargetCharacterId: target,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to kick: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Removed %s from scene %s.", target, sceneID),
	}, nil
}

// handleTransfer parses "scene transfer <scene-id> <character>".
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleTransfer(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, rest := splitSubcommand(args)
	target := strings.TrimSpace(rest)
	if sceneID == "" || target == "" {
		return pluginsdk.Errorf("Usage: scene transfer <scene id> <character>"), nil
	}

	_, err := p.service.TransferOwnership(ctx, &scenev1.TransferOwnershipRequest{
		CharacterId:         req.CharacterID,
		SceneId:             sceneID,
		NewOwnerCharacterId: target,
	})
	if err != nil {
		return pluginsdk.Errorf("Failed to transfer ownership: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Transferred ownership of scene %s to %s.", sceneID, target),
	}, nil
}
