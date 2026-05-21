// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	ctx, span := startSpan(
		ctx, "scene.command.dispatch",
		attribute.String("subject_id", req.CharacterID),
	)
	defer span.End()

	sub, rest := splitSubcommand(req.Args)
	span.SetAttributes(attribute.String("subcommand", sub))

	if sub == "" {
		return pluginsdk.Errorf("Usage: scene <subcommand> [args]\nKnown subcommands: create, emit, end, info, invite, join, kick, leave, ooc, order, pause, pose, resume, say, set, switch, transfer"), nil
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
	case "switch":
		return p.handleSwitch(ctx, req, rest)
	case "pose":
		return p.handleEmit(ctx, req, rest, "scene_pose", false)
	case "say":
		return p.handleEmit(ctx, req, rest, "scene_say", false)
	case "emit":
		return p.handleEmit(ctx, req, rest, "scene_emit", false)
	case "ooc":
		return p.handleEmit(ctx, req, rest, "scene_ooc", true)
	case "order":
		return p.handleOrder(ctx, req, rest)
	default:
		return pluginsdk.Errorf("Unknown scene subcommand %q. Known subcommands: create, emit, end, info, invite, join, kick, leave, ooc, order, pause, pose, resume, say, set, switch, transfer.", sub), nil
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
// After the DB write commits, focusClient.LeaveFocusByTarget fans the leave
// out to every session that holds the scene's FocusMembership — owner and
// non-owner participants alike. The sweep is best-effort: DB state is
// authoritative, and focus-side errors are logged, not surfaced to the user.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleEnd(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := strings.TrimSpace(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene end <scene id>"), nil
	}

	if _, err := p.service.EndScene(ctx, &scenev1.EndSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	}); err != nil {
		return pluginsdk.Errorf("Failed to end scene: %v", err), nil
	}

	if p.focusClient != nil {
		result, err := p.focusClient.LeaveFocusByTarget(ctx, pluginsdk.FocusKey{
			Kind:     pluginsdk.FocusKindScene,
			TargetID: sceneID,
		})
		switch {
		case err != nil:
			// Enumeration failed entirely: host could not list members.
			// DB transition has committed; focus state across sessions is
			// inconsistent until a subsequent RestoreFocus reconciles.
			slog.WarnContext(
				ctx, "scene.command.end focus sweep enumeration failed",
				"subject_id", req.CharacterID,
				"session_id", req.SessionID,
				"scene_id", sceneID,
				"error", err,
			)
		case len(result.Failed) > 0:
			// Partial sweep: some sessions still hold stale FocusMemberships.
			// Non-fatal (the scene has ended; stale memberships stop
			// receiving events), but worth auditing so an operator can
			// see which participants need reconciliation.
			failedIDs := make([]string, 0, len(result.Failed))
			for _, f := range result.Failed {
				failedIDs = append(failedIDs, f.SessionID)
			}
			slog.WarnContext(
				ctx, "scene.command.end focus sweep partial",
				"subject_id", req.CharacterID,
				"session_id", req.SessionID,
				"scene_id", sceneID,
				"succeeded", result.Succeeded,
				"total_scanned", result.TotalScanned,
				"failed_session_ids", failedIDs,
			)
		}
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

// handleJoin parses "scene join <scene-id>", calls JoinScene, then calls
// focusClient.JoinFocus to register the session as a subscriber.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleJoin(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	fields := strings.Fields(args)
	if len(fields) != 1 {
		return pluginsdk.Errorf("Usage: scene join <scene id>"), nil
	}
	sceneID := fields[0]

	if _, err := p.service.JoinScene(ctx, &scenev1.JoinSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	}); err != nil {
		return pluginsdk.Errorf("Failed to join scene: %v", err), nil
	}

	if p.focusClient == nil {
		// Misconfiguration, not a transient error: retries will hit the
		// same nil guard. Surface the operator-action hint rather than the
		// user-retry hint used for transient JoinFocus failures below.
		slog.WarnContext(
			ctx, "scene.command.join focus client not configured; subscription not updated",
			"subject_id", req.CharacterID,
			"session_id", req.SessionID,
			"scene_id", sceneID,
		)
		return pluginsdk.Errorf(
			"Joined scene in database, but your session could not subscribe " +
				"(focus client not configured — this is a server configuration error, " +
				"please contact an administrator).",
		), nil
	}

	err := p.focusClient.JoinFocus(ctx, req.SessionID, pluginsdk.FocusKey{
		Kind:     pluginsdk.FocusKindScene,
		TargetID: sceneID,
	})
	if err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "FOCUS_ALREADY_MEMBER" {
			return &pluginsdk.CommandResponse{
				Status: pluginsdk.CommandOK,
				Output: fmt.Sprintf("Joined scene %s.", sceneID),
			}, nil
		}
		slog.WarnContext(
			ctx, "scene.command.join focus join failed",
			"subject_id", req.CharacterID,
			"session_id", req.SessionID,
			"scene_id", sceneID,
			"error", err,
		)
		return pluginsdk.Errorf(
			"Joined scene in database, but your session could not subscribe (%v). "+
				"Please retry `scene join %s`.", err, sceneID,
		), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Joined scene %s.", sceneID),
	}, nil
}

// handleLeave parses "scene leave <scene-id>", calls LeaveScene, then calls
// focusClient.LeaveFocus. Focus errors are logged but do not fail the command
// since the DB is the source of truth for scene membership.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleLeave(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	fields := strings.Fields(args)
	if len(fields) != 1 {
		return pluginsdk.Errorf("Usage: scene leave <scene id>"), nil
	}
	sceneID := fields[0]

	if _, err := p.service.LeaveScene(ctx, &scenev1.LeaveSceneRequest{
		CharacterId: req.CharacterID,
		SceneId:     sceneID,
	}); err != nil {
		return pluginsdk.Errorf("Failed to leave scene: %v", err), nil
	}

	if p.focusClient != nil {
		if err := p.focusClient.LeaveFocus(ctx, req.SessionID, pluginsdk.FocusKey{
			Kind:     pluginsdk.FocusKindScene,
			TargetID: sceneID,
		}); err != nil {
			slog.WarnContext(
				ctx, "scene.command.leave focus leave failed",
				"subject_id", req.CharacterID,
				"session_id", req.SessionID,
				"scene_id", sceneID,
				"error", err,
			)
		}
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
	// Strict arity: reject anything other than exactly 2 tokens — see handleJoin.
	fields := strings.Fields(args)
	if len(fields) != 2 {
		return pluginsdk.Errorf("Usage: scene invite <scene id> <character>"), nil
	}
	sceneID, target := fields[0], fields[1]

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
	// Strict arity: reject anything other than exactly 2 tokens — see handleJoin.
	fields := strings.Fields(args)
	if len(fields) != 2 {
		return pluginsdk.Errorf("Usage: scene kick <scene id> <character>"), nil
	}
	sceneID, target := fields[0], fields[1]

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
	// Strict arity: reject anything other than exactly 2 tokens — see handleJoin.
	fields := strings.Fields(args)
	if len(fields) != 2 {
		return pluginsdk.Errorf("Usage: scene transfer <scene id> <character>"), nil
	}
	sceneID, target := fields[0], fields[1]

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

// handleSwitch implements `scene switch <scene id>`. The coordinator's
// PresentFocus is a pure session-state column update; the session MUST
// already be a member of the target scene (enforced server-side via
// FOCUS_NOT_MEMBER). No DB write happens here — scene membership and
// subscriptions are unchanged by switch.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleSwitch(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	fields := strings.Fields(args)
	if len(fields) != 1 {
		return pluginsdk.Errorf("Usage: scene switch <scene id>"), nil
	}
	sceneID := fields[0]

	if p.focusClient == nil {
		slog.WarnContext(
			ctx, "scene.command.switch focus client not configured",
			"subject_id", req.CharacterID,
			"session_id", req.SessionID,
			"scene_id", sceneID,
		)
		return pluginsdk.Errorf("Failed to switch scene: focus client not configured"), nil
	}

	if err := p.focusClient.PresentFocus(ctx, req.SessionID, pluginsdk.FocusKey{
		Kind:     pluginsdk.FocusKindScene,
		TargetID: sceneID,
	}); err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "FOCUS_NOT_MEMBER" {
			return pluginsdk.Errorf(
				"You are not a member of scene %s. Use `scene join %s` first.", sceneID, sceneID,
			), nil
		}
		slog.WarnContext(
			ctx, "scene.command.switch focus present failed",
			"subject_id", req.CharacterID,
			"session_id", req.SessionID,
			"scene_id", sceneID,
			"error", err,
		)
		return pluginsdk.Errorf("Failed to switch scene: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Switched to scene %s.", sceneID),
	}, nil
}

// handleEmit is the shared emit-subcommand handler for the four content
// verbs (pose / say / emit / ooc). The eventType determines the emitted
// type; the ooc flag determines the subject facet (.ic vs .ooc). All
// four emit with Sensitive: true to match the crypto.emits manifest's
// sensitivity:always declaration (INV-P4-3).
//
// Target scene resolution uses single-membership inference (Phase 4
// only); Phase 5 will replace this with focus-aware routing that
// consults the character's focus context.
//
// Defense-in-depth: the Layer-1 ABAC execute-scene-commands policy
// fires at the command-execute layer before this handler runs in
// production; the IsParticipant check here is belt + suspenders so a
// hypothetical mis-route of a non-participant cannot leak into the IC
// stream.
func (p *scenePlugin) handleEmit(
	ctx context.Context,
	req pluginsdk.CommandRequest,
	args string,
	eventType string,
	ooc bool,
) (*pluginsdk.CommandResponse, error) {
	verb := strings.TrimPrefix(eventType, "scene_")
	ctx, span := startSpan(
		ctx, "scene.command.emit",
		attribute.String("subject_id", req.CharacterID),
		attribute.String("event_type", eventType),
	)
	defer span.End()

	text := strings.TrimSpace(args)
	if text == "" {
		return pluginsdk.Errorf("Usage: scene %s <text>", verb), nil
	}

	// Resolve target scene via single-membership inference. Phase 5 will
	// replace this with focus-aware routing that consults the character's
	// focus context.
	sceneID, userErr, internalErr := p.resolveSingleSceneMembership(ctx, req.CharacterID)
	if internalErr != nil {
		recordError(span, internalErr)
		slog.WarnContext(
			ctx, "scene.command.emit membership lookup failed",
			"subject_id", req.CharacterID,
			"event_type", eventType,
			"error", internalErr,
		)
		return nil, internalErr
	}
	if userErr != "" {
		return pluginsdk.Errorf("%s", userErr), nil
	}
	span.SetAttributes(attribute.String("scene_id", sceneID))

	// Defense-in-depth participant check. Layer-1 ABAC already gated
	// command-execute; this re-verifies on the plugin side per
	// INV-P4-11 (TestSceneSubcommand_NonParticipant_PermissionDenied).
	isPart, err := p.service.store.IsParticipant(ctx, sceneID, req.CharacterID)
	if err != nil {
		err = oops.Code("SCENE_EMIT_MEMBERSHIP_LOOKUP_FAILED").
			With("scene_id", sceneID).
			With("character_id", req.CharacterID).
			With("event_type", eventType).Wrap(err)
		recordError(span, err)
		return nil, err
	}
	if !isPart {
		return pluginsdk.Errorf("You are not a participant of scene %s.", sceneID), nil
	}

	// Build subject + payload.
	subject := dotStyleSceneSubjectIC(p.service.gameID, sceneID)
	if ooc {
		subject = dotStyleSceneSubjectOOC(p.service.gameID, sceneID)
	}

	payload, err := json.Marshal(map[string]string{
		"actor_id": req.CharacterID,
		"scene_id": sceneID,
		"text":     text,
	})
	if err != nil {
		err = oops.Code("SCENE_EMIT_PAYLOAD_MARSHAL_FAILED").
			With("event_type", eventType).
			With("scene_id", sceneID).Wrap(err)
		recordError(span, err)
		return nil, err
	}

	if p.service.eventSink == nil {
		err := oops.Code("SCENE_EVENT_SINK_NOT_CONFIGURED").
			With("event_type", eventType).
			With("scene_id", sceneID).
			New("scene event sink is not configured")
		recordError(span, err)
		return nil, err
	}

	intent := pluginsdk.EmitIntent{
		Subject:   subject,
		Type:      pluginsdk.EventType(eventType),
		Payload:   string(payload),
		Sensitive: true, // sensitivity:always per crypto.emits manifest §2 / INV-P4-3
	}
	if err := p.service.eventSink.Emit(ctx, intent); err != nil {
		err = oops.Code("SCENE_EMIT_FAILED").
			With("event_type", eventType).
			With("scene_id", sceneID).Wrap(err)
		recordError(span, err)
		return nil, err
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("You %s: %s", verb, text),
	}, nil
}

// handleOrder is the scene/order subcommand handler — renders the current
// pose order for the caller's scene per spec §8.
func (p *scenePlugin) handleOrder(ctx context.Context, req pluginsdk.CommandRequest, _ string) (*pluginsdk.CommandResponse, error) {
	sceneID, userErr, internalErr := p.resolveSingleSceneMembership(ctx, req.CharacterID)
	if internalErr != nil {
		return nil, internalErr
	}
	if userErr != "" {
		return pluginsdk.Errorf("%s", userErr), nil
	}

	resp, err := p.service.GetPoseOrder(ctx, &scenev1.GetPoseOrderRequest{
		SceneId:     sceneID,
		CharacterId: req.CharacterID,
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st != nil && st.Code() == codes.PermissionDenied {
			return pluginsdk.Errorf("You are not a participant of scene %s.", sceneID), nil
		}
		return nil, oops.Code("SCENE_ORDER_GETPOSEORDER_FAILED").
			With("scene_id", sceneID).Wrap(err)
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: renderPoseOrder(sceneID, resp),
	}, nil
}

// renderPoseOrder formats a GetPoseOrderResponse as plain text per spec §8.
// Pure function — testable without a service mock.
func renderPoseOrder(sceneID string, resp *scenev1.GetPoseOrderResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Scene %s — pose order: %s (%d total poses)\n",
		sceneID, resp.GetMode(), resp.GetTotalPoseCount())

	entries := resp.GetEntries()
	if len(entries) == 0 {
		b.WriteString("  (no participants)\n")
		return b.String()
	}

	switch resp.GetMode() {
	case "strict":
		var head *scenev1.PoseOrderEntry
		var rest []*scenev1.PoseOrderEntry
		for _, e := range entries {
			if e.GetEligible() && head == nil {
				head = e
			} else {
				rest = append(rest, e)
			}
		}
		if head != nil {
			fmt.Fprintf(&b, "  → Next to pose: %s\n", poseOrderDisplayName(head))
		}
		if len(rest) > 0 {
			b.WriteString("  Then:\n")
			for _, e := range rest {
				fmt.Fprintf(&b, "    %s\n", poseOrderDisplayName(e))
			}
		}

	case "3pr", "5pr":
		var threshold uint32
		if resp.GetMode() == "3pr" {
			threshold = 3
		} else {
			threshold = 5
		}
		var eligible, cooldown []*scenev1.PoseOrderEntry
		for _, e := range entries {
			if e.GetEligible() {
				eligible = append(eligible, e)
			} else {
				cooldown = append(cooldown, e)
			}
		}
		if len(eligible) > 0 {
			fmt.Fprintf(&b, "  Eligible to pose (poses_since_last ≥ %d):\n", threshold)
			for _, e := range eligible {
				fmt.Fprintf(&b, "    %s\n", poseOrderDisplayName(e))
			}
		}
		if len(cooldown) > 0 {
			b.WriteString("  Cooldown (needs more poses to elapse):\n")
			for _, e := range cooldown {
				psl := uint32(0)
				if e.PosesSinceLast != nil {
					psl = *e.PosesSinceLast
				}
				// Guard against future Compute changes that could push
				// psl >= threshold while still surfacing the entry in
				// the cooldown bucket; without this, uint32 wraps.
				var needs uint32
				if psl < threshold {
					needs = threshold - psl
				}
				fmt.Fprintf(&b, "    %s (needs %d more)\n", poseOrderDisplayName(e), needs)
			}
		}

	default: // "free" and any unrecognised mode
		b.WriteString("  Participants:\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "    %s\n", poseOrderDisplayName(e))
		}
	}

	return b.String()
}

// poseOrderDisplayName returns the character's display name, falling back to
// character_id when name is empty (Phase 4 default; nameResolver wiring is a
// future bead).
func poseOrderDisplayName(e *scenev1.PoseOrderEntry) string {
	if e.GetCharacterName() != "" {
		return e.GetCharacterName()
	}
	return e.GetCharacterId()
}

// resolveSingleSceneMembership returns the scene_id this character is
// currently a participant of, if exactly one. Returns a user-facing
// message in userErr when membership count is ambiguous (zero or >1);
// returns internalErr for genuine lookup failures.
//
// Phase 5 will replace this with focus-aware routing that consults the
// character's focus context.
func (p *scenePlugin) resolveSingleSceneMembership(ctx context.Context, characterID string) (sceneID, userErr string, internalErr error) {
	scenes, err := p.service.store.ListScenesForCharacter(ctx, characterID)
	if err != nil {
		return "", "", oops.Code("SCENE_EMIT_MEMBERSHIP_LIST_FAILED").
			With("character_id", characterID).Wrap(err)
	}
	switch len(scenes) {
	case 0:
		return "", "You are not currently in any scene. Join one with `scene join <scene-id>` first.", nil
	case 1:
		return scenes[0], "", nil
	default:
		return "", fmt.Sprintf(
			"You are in %d scenes. Phase 5 will add focus-aware routing; for now, explicit scene targeting is not yet supported.",
			len(scenes),
		), nil
	}
}
