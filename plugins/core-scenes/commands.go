// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// membershipLookup is the minimal store dependency resolveSceneRef needs.
// The real *SceneStore satisfies it via ListScenesForCharacter (store.go);
// tests use a small fake.
type membershipLookup interface {
	ListScenesForCharacter(ctx context.Context, characterID string) ([]string, error)
}

// resolveSceneRef returns the scene ID a "scene publish *" / "scene log *"
// command targets (spec §6.1). An explicit "#<id>" arg names the scene
// directly; the strict form has no space after the '#'. With no arg, the
// helper uses single-membership inference (mirroring handleEmit/handleOrder's
// resolveSingleSceneMembership): exactly one active membership resolves to that
// scene; zero or multiple returns SCENE_PUBLISH_NO_FOCUSED_SCENE prompting the
// player to pass "#<id>". Whitespace-only args are treated as no-arg.
//
// The plugin SDK exposes no per-connection "focused scene" query
// (pluginsdk.FocusClient has no GetConnectionFocus), so single-membership
// inference is the established pattern, reused here rather than inventing a new
// abstraction.
func resolveSceneRef(ctx context.Context, look membershipLookup, characterID, args string) (string, error) {
	args = strings.TrimSpace(args)
	if strings.HasPrefix(args, "#") {
		// Strict "#<id>": no inner trim, so a space after '#' (or an embedded
		// space/newline/'#') is a malformed reference rather than a silent fixup.
		id := args[1:]
		if id == "" || strings.ContainsAny(id, " \t\n#") {
			return "", oops.Code("SCENE_PUBLISH_REF_INVALID").
				With("arg", args).Errorf("malformed scene reference; use '#<scene_id>'")
		}
		return id, nil
	}
	if args != "" {
		return "", oops.Code("SCENE_PUBLISH_REF_INVALID").
			With("arg", args).Errorf("scene reference must use the '#<scene_id>' form")
	}

	scenes, err := look.ListScenesForCharacter(ctx, characterID)
	if err != nil {
		return "", oops.Code("SCENE_PUBLISH_REF_LOOKUP_FAILED").Wrap(err)
	}
	if len(scenes) != 1 {
		return "", oops.Code("SCENE_PUBLISH_NO_FOCUSED_SCENE").
			With("matching_scenes", len(scenes)).
			Errorf("command requires a '#<scene_id>' argument (caller is in %d active scenes)", len(scenes))
	}
	return scenes[0], nil
}

// sceneLogReplayLimit bounds a single "scene log" page (most-recent N events).
const sceneLogReplayLimit = 50

// replayEventKinds maps the IC content event types to their render kind. Only
// these three are replayed by "scene log"; joins, ops, OOC, and publish-
// lifecycle notices are skipped.
var replayEventKinds = map[string]EntryKind{
	"scene_pose": EntryKindPose,
	"scene_say":  EntryKindSay,
	"scene_emit": EntryKindEmit,
}

// decodeReplayEntries converts QueryStreamHistory events (oldest→newest) into
// renderable content entries, keeping only the IC content kinds and decoding
// each event's {actor_id, text} payload (the shape handleEmit writes). A
// payload that won't decode fails the whole replay rather than silently
// dropping a line.
func decodeReplayEntries(events []pluginsdk.Event) ([]PublishedSceneEntry, error) {
	out := make([]PublishedSceneEntry, 0, len(events))
	for i := range events {
		kind, ok := replayEventKinds[string(events[i].Type)]
		if !ok {
			continue
		}
		var pl struct {
			ActorID string `json:"actor_id"`
			Text    string `json:"text"`
		}
		if err := json.Unmarshal([]byte(events[i].Payload), &pl); err != nil {
			return nil, oops.Code("SCENE_LOG_DECODE_FAILED").
				With("event_id", events[i].ID).Wrap(err)
		}
		out = append(out, PublishedSceneEntry{Speaker: pl.ActorID, Kind: kind, Content: pl.Text})
	}
	return out, nil
}

// handleLog dispatches the "scene log" sub-commands (spec §6.1; folds in
// holomush-cb4x): the bare form replays the IC content history as plain text;
// "export <format>" (E4) renders it to markdown / plain_text / jsonl. Both are
// participant-gated (INV-S9, plugin-code) and read DECRYPTED content via the
// host's QueryStreamHistory — NOT the plugin's own audit QueryHistory, which
// serves ciphertext. Speaker is the actor character id; name resolution is a
// follow-up.
func (p *scenePlugin) handleLog(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if sub, rest := splitSubcommand(args); sub == "export" {
		return p.handleLogExport(ctx, req, rest)
	}
	entries, errResp := p.fetchSceneLogEntries(ctx, req, args)
	if errResp != nil {
		return errResp, nil
	}
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: renderPlainText(entries),
	}, nil
}

// handleLogExport renders a scene's IC content history to the requested format
// ("scene log export <markdown|plain_text|jsonl> [#<id>]"). Same participant-
// gated read as the bare replay; the format dispatches to the shared renderers
// via renderPublishedScene (C1/C2/C3).
func (p *scenePlugin) handleLogExport(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	format, ref := splitSubcommand(args)
	if format == "" {
		return pluginsdk.Errorf("Usage: scene log export <markdown|plain_text|jsonl> [#<scene id>]"), nil
	}
	if _, ok := publishRenderMime[format]; !ok {
		return pluginsdk.Errorf("Unsupported export format %q. Use markdown, plain_text, or jsonl.", format), nil
	}
	entries, errResp := p.fetchSceneLogEntries(ctx, req, ref)
	if errResp != nil {
		return errResp, nil
	}
	content, err := renderPublishedScene(format, entries)
	if err != nil {
		return pluginsdk.Errorf("Could not export scene log: %v", err), nil
	}
	return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK, Output: string(content)}, nil
}

// fetchSceneLogEntries resolves the scene, runs the INV-S9 participant gate
// (plugin-code, before any history read), and returns the decoded IC content
// entries via the host's QueryStreamHistory (decrypted host-side, membership-
// gated). On any failure it returns a non-nil error CommandResponse for the
// caller to return directly. Shared by the replay and export forms of scene log.
func (p *scenePlugin) fetchSceneLogEntries(ctx context.Context, req pluginsdk.CommandRequest, ref string) ([]PublishedSceneEntry, *pluginsdk.CommandResponse) {
	sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, ref)
	if err != nil {
		return nil, pluginsdk.Errorf("%v", err)
	}
	ok, err := p.service.store.IsParticipant(ctx, sceneID, req.CharacterID)
	if err != nil {
		return nil, pluginsdk.Errorf("Could not verify scene membership: %v", err)
	}
	if !ok {
		return nil, pluginsdk.Errorf("You are not a participant in that scene.")
	}
	if p.focusClient == nil {
		return nil, pluginsdk.Errorf("Scene log is unavailable.")
	}
	resp, err := p.focusClient.QueryStreamHistory(ctx, pluginsdk.QueryStreamHistoryRequest{
		Stream: dotStyleSceneSubjectIC(p.service.gameID, sceneID),
		Count:  sceneLogReplayLimit,
	})
	if err != nil {
		return nil, pluginsdk.Errorf("Could not read scene log: %v", err)
	}
	entries, err := decodeReplayEntries(resp.Events)
	if err != nil {
		return nil, pluginsdk.Errorf("Could not render scene log: %v", err)
	}
	return entries, nil
}

// handlePublish dispatches the "scene publish" sub-commands (spec §6.1). The
// bare form ("scene publish [#<id>]") starts an attempt; "vote yes|no" casts a
// vote (B9); "vote extend <count>" bumps the admin-gated attempt budget (E2).
// withdraw / status / download land in B10.
func (p *scenePlugin) handlePublish(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sub, rest := splitSubcommand(args)
	switch sub {
	case "vote":
		return p.handleVote(ctx, req, rest)
	case "withdraw":
		return p.handleWithdraw(ctx, req, rest)
	case "status":
		return p.handleStatus(ctx, req, rest)
	case "download":
		return p.handleDownload(ctx, req, rest)
	default:
		// Bare "scene publish [#<id>]" → start an attempt. args carries the
		// optional scene ref (empty → single-membership inference).
		return p.handlePublishStart(ctx, req, args)
	}
}

// latestAttemptID returns the most recent attempt's id (ListSceneAttempts is
// ordered by attempt_number ASC, so the last element is newest), or ("", false)
// if the scene has no attempts.
func latestAttemptID(attempts []PublishedScene) (string, bool) {
	if len(attempts) == 0 {
		return "", false
	}
	return attempts[len(attempts)-1].ID, true
}

// publishedAttemptID returns the scene's PUBLISHED attempt id (one-and-done, so
// at most one), or ("", false) if the scene was never published.
func publishedAttemptID(attempts []PublishedScene) (string, bool) {
	for i := range attempts {
		if attempts[i].Status == StatusPublished {
			return attempts[i].ID, true
		}
	}
	return "", false
}

// handleWithdraw withdraws a scene's active publish attempt (spec §6.1). Owner-
// only: WithdrawScenePublish (B4) enforces the owner check
// (SCENE_PUBLISH_NOT_OWNER → PermissionDenied), so no in-handler gate is needed.
func (p *scenePlugin) handleWithdraw(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, args)
	if err != nil {
		return pluginsdk.Errorf("%v", err), nil
	}
	attempts, err := p.service.store.ListSceneAttempts(ctx, sceneID)
	if err != nil {
		return pluginsdk.Errorf("Could not look up the publish attempt: %v", err), nil
	}
	attemptID, ok := activeAttemptID(attempts)
	if !ok {
		return pluginsdk.Errorf("There is no active publish attempt to withdraw for that scene."), nil
	}
	if _, err := p.service.WithdrawScenePublish(ctx, &scenev1.WithdrawScenePublishRequest{
		CallerCharacterId: req.CharacterID,
		PublishedSceneId:  attemptID,
	}); err != nil {
		return pluginsdk.Errorf("Could not withdraw publish attempt: %v", err), nil
	}
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: "The publish attempt has been withdrawn.",
	}, nil
}

// handleStatus shows the latest publish attempt's state for a scene (spec §6.1).
// Participant-gated: GetPublishedScene (B5) enforces the INV-S9 participant gate.
func (p *scenePlugin) handleStatus(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, args)
	if err != nil {
		return pluginsdk.Errorf("%v", err), nil
	}
	attempts, err := p.service.store.ListSceneAttempts(ctx, sceneID)
	if err != nil {
		return pluginsdk.Errorf("Could not look up publish attempts: %v", err), nil
	}
	attemptID, ok := latestAttemptID(attempts)
	if !ok {
		return pluginsdk.Errorf("There are no publish attempts for that scene."), nil
	}
	resp, err := p.service.GetPublishedScene(ctx, &scenev1.GetPublishedSceneRequest{
		CallerCharacterId: req.CharacterID,
		PublishedSceneId:  attemptID,
	})
	if err != nil {
		return pluginsdk.Errorf("Could not read publish status: %v", err), nil
	}
	out := fmt.Sprintf("Publish attempt #%d: %s", resp.GetAttemptNumber(), resp.GetStatus())
	if t := resp.GetTally(); t != nil {
		out += fmt.Sprintf(" (votes: %d yes, %d no, %d pending)", t.GetYes(), t.GetNo(), t.GetPending())
	}
	if fr := resp.GetFailureReason(); fr != "" {
		out += fmt.Sprintf(" — failed: %s", fr)
	}
	return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK, Output: out}, nil
}

// handleDownload renders a scene's PUBLISHED archive to the requested format
// (spec §6.1). Arg form: "scene publish download [<format>] [#<id>]" — a
// "#"-prefixed token is the scene ref, any other token is the format (default
// markdown). Participant-gated + format-validated by DownloadPublishedScene (B6).
func (p *scenePlugin) handleDownload(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	format := "markdown"
	var ref string
	for _, tok := range strings.Fields(args) {
		if strings.HasPrefix(tok, "#") {
			ref = tok
		} else {
			format = tok
		}
	}
	sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, ref)
	if err != nil {
		return pluginsdk.Errorf("%v", err), nil
	}
	attempts, err := p.service.store.ListSceneAttempts(ctx, sceneID)
	if err != nil {
		return pluginsdk.Errorf("Could not look up the published scene: %v", err), nil
	}
	attemptID, ok := publishedAttemptID(attempts)
	if !ok {
		return pluginsdk.Errorf("That scene has not been published."), nil
	}
	resp, err := p.service.DownloadPublishedScene(ctx, &scenev1.DownloadPublishedSceneRequest{
		CallerCharacterId: req.CharacterID,
		PublishedSceneId:  attemptID,
		Format:            format,
	})
	if err != nil {
		return pluginsdk.Errorf("Could not download scene: %v", err), nil
	}
	return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK, Output: string(resp.GetContent())}, nil
}

// handleVote casts (or changes) a publish vote for the focused or explicit
// scene's ACTIVE attempt (spec §6.1). It resolves the scene, finds its single
// non-terminal attempt, and calls CastPublishSceneVote. Roster membership is
// enforced by CastVote (SCENE_PUBLISH_NOT_A_VOTER → PermissionDenied), so no
// separate participant gate is needed here.
func (p *scenePlugin) handleVote(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	dir, rest := splitSubcommand(args)
	var vote bool
	switch dir {
	case "yes":
		vote = true
	case "no":
		vote = false
	case "extend":
		return p.handleVoteExtend(ctx, req, rest)
	default:
		return pluginsdk.Errorf("Usage: scene publish vote <yes|no|extend> [#<scene id>]"), nil
	}

	sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, rest)
	if err != nil {
		return pluginsdk.Errorf("%v", err), nil
	}

	attempts, err := p.service.store.ListSceneAttempts(ctx, sceneID)
	if err != nil {
		return pluginsdk.Errorf("Could not look up the publish vote: %v", err), nil
	}
	attemptID, ok := activeAttemptID(attempts)
	if !ok {
		return pluginsdk.Errorf("There is no active publish vote for that scene."), nil
	}

	resp, err := p.service.CastPublishSceneVote(ctx, &scenev1.CastPublishSceneVoteRequest{
		CallerCharacterId: req.CharacterID,
		PublishedSceneId:  attemptID,
		Vote:              vote,
	})
	if err != nil {
		return pluginsdk.Errorf("Could not cast vote: %v", err), nil
	}

	word := "no"
	if vote {
		word = "yes"
	}
	action := "recorded"
	if resp.GetIsChange() {
		action = "changed to"
	}
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Your publish vote was %s %q.", action, word),
	}, nil
}

// activeAttemptID returns the id of the single non-terminal (COLLECTING or
// COOLOFF) attempt in the list, or ("", false) if none is active. At most one
// attempt is active per scene (StartScenePublish's no-active-attempt
// precondition), so the first non-terminal match is authoritative.
func activeAttemptID(attempts []PublishedScene) (string, bool) {
	for i := range attempts {
		if !attempts[i].Status.IsTerminal() {
			return attempts[i].ID, true
		}
	}
	return "", false
}

// handlePublishStart starts a publish-vote attempt for a scene (spec §6.1, the
// bare "scene publish [#<id>]" form). Resolves the target scene, gates on
// participation, then calls StartScenePublish (which enforces the ended-state +
// one-and-done + budget + eligible-voters preconditions).
//
// Participant gate: the host's command-dispatch ABAC checks the scene command's
// declared 'write' capability, NOT the 'publish' action, so the
// start-publish-as-participant policy is inert at dispatch (a 'publish'
// capability declaration is a follow-up). This explicit IsParticipant check is
// therefore the effective participant gate at the command layer — it mirrors
// the policy's predicate (principal.id in resource.scene.participants) so there
// is no drift, and surfaces spec §6.1's not-a-participant rejection. Defence-in-
// depth like handleLog (E3) / WithdrawScenePublish's owner check (B4).
func (p *scenePlugin) handlePublishStart(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, args)
	if err != nil {
		return pluginsdk.Errorf("%v", err), nil
	}

	ok, err := p.service.store.IsParticipant(ctx, sceneID, req.CharacterID)
	if err != nil {
		return pluginsdk.Errorf("Could not verify scene membership: %v", err), nil
	}
	if !ok {
		return pluginsdk.Errorf("You are not a participant in that scene."), nil
	}

	resp, err := p.service.StartScenePublish(ctx, &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: req.CharacterID,
	})
	if err != nil {
		return pluginsdk.Errorf("Could not start publish vote: %v", err), nil
	}
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Publish-vote attempt #%d started for scene %s. Participants will be notified.",
			resp.GetAttemptNumber(), sceneID),
	}, nil
}

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
		return pluginsdk.Errorf("Usage: scene <subcommand> [args]\nKnown subcommands: create, emit, end, focus, grid, info, invite, join, kick, leave, list, log, ooc, order, pause, pose, publish, resume, say, set, switch, transfer"), nil
	}

	// gated dispatches through the ABAC evaluator; fails closed when evaluator is nil.
	gated := func(name, action string, resourceRef func(string) (string, error), handler func(context.Context, pluginsdk.CommandRequest, string) (*pluginsdk.CommandResponse, error)) (*pluginsdk.CommandResponse, error) {
		if p.evaluator == nil {
			slog.WarnContext(ctx, "scene.command evaluator not configured",
				"subcommand", name,
				"subject_id", req.CharacterID)
			return pluginsdk.Errorf("Permission check unavailable: evaluator not configured."), nil
		}
		return pluginsdk.GatedSubcommand{
			Name:        name,
			Action:      action,
			ResourceRef: resourceRef,
			Handler:     handler,
		}.Run(ctx, p.evaluator, req, rest)
	}

	switch sub {
	case "create":
		return p.handleCreate(ctx, req, rest)
	case "info":
		return gated("info", "read", sceneResourceRef, p.handleInfo)
	case "end":
		return gated("end", "end", sceneResourceRef, p.handleEnd)
	case "pause":
		return gated("pause", "pause", sceneResourceRef, p.handlePause)
	case "resume":
		return gated("resume", "resume", sceneResourceRef, p.handleResume)
	case "set":
		return gated("set", "update", sceneResourceRefFirstField, p.handleSet)
	case "join":
		return p.handleJoin(ctx, req, rest)
	case "leave":
		return gated("leave", "leave", sceneResourceRef, p.handleLeave)
	case "invite":
		return gated("invite", "invite", sceneResourceRefFirstField, p.handleInvite)
	case "kick":
		return gated("kick", "kick", sceneResourceRefFirstField, p.handleKick)
	case "transfer":
		return gated("transfer", "transfer-ownership", sceneResourceRefFirstField, p.handleTransfer)
	case "switch":
		return p.handleSwitch(ctx, req, rest)
	case "focus":
		// Reject anything past the expected `#<scene-id>` arg. Without
		// this, "scene focus #id extra" silently flows into the
		// substrate as a malformed scene ID. (CodeRabbit PR #4191)
		if fields := strings.Fields(rest); len(fields) > 1 {
			return pluginsdk.Errorf("Usage: scene focus #<scene id>"), nil
		}
		return p.handleSceneFocus(ctx, req, rest)
	case "grid":
		// `scene grid` takes no arguments. Reject `scene grid extra`
		// instead of silently ignoring trailing tokens. (CodeRabbit PR #4191)
		if strings.TrimSpace(rest) != "" {
			return pluginsdk.Errorf("Usage: scene grid"), nil
		}
		return p.handleSceneGrid(ctx, req)
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
	case "publish":
		return p.handlePublish(ctx, req, rest)
	case "log":
		return p.handleLog(ctx, req, rest)
	case "list":
		return p.handleSceneList(ctx, req)
	default:
		return pluginsdk.Errorf("Unknown scene subcommand %q. Known subcommands: create, emit, end, focus, grid, info, invite, join, kick, leave, list, log, ooc, order, pause, pose, publish, resume, say, set, switch, transfer.", sub), nil
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

// handleInfo shows scene metadata for the given scene ID. Authorization is
// enforced via the host ABAC evaluator (read-scene-as-participant policy)
// before this handler is called.
func (p *scenePlugin) handleInfo(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := normalizeSceneID(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene info #<scene id>"), nil
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

// handleEnd ends the specified scene. Authorization is enforced via the host
// ABAC evaluator (end-own-scene policy) before this handler is called.
//
// After the DB write commits, focusClient.LeaveFocusByTarget fans the leave
// out to every session that holds the scene's FocusMembership — owner and
// non-owner participants alike. The sweep is best-effort: DB state is
// authoritative, and focus-side errors are logged, not surfaced to the user.
func (p *scenePlugin) handleEnd(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := normalizeSceneID(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene end #<scene id>"), nil
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
func (p *scenePlugin) handlePause(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := normalizeSceneID(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene pause #<scene id>"), nil
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
func (p *scenePlugin) handleResume(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	sceneID := normalizeSceneID(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene resume #<scene id>"), nil
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
func (p *scenePlugin) handleSet(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return pluginsdk.Errorf("Usage: scene set #<scene id> field=value"), nil
	}

	sceneRef, rest := splitSubcommand(args)
	sceneID := normalizeSceneID(sceneRef)
	if sceneID == "" || rest == "" {
		return pluginsdk.Errorf("Usage: scene set #<scene id> field=value"), nil
	}

	eqIdx := strings.IndexByte(rest, '=')
	if eqIdx < 0 {
		return pluginsdk.Errorf("Usage: scene set #<scene id> field=value"), nil
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
// Authorization note: handleJoin is intentionally NOT engine-gated. Join
// authorization is enforced substrate-side by classifyJoinMiss in the store
// layer, which checks scene state (active/paused), visibility (open vs
// private), and invitation status before admitting a joiner. An engine gate
// cannot express the open-scene case because it would require a principal/owner
// check that admits non-members by definition — exactly the condition the
// engine cannot evaluate without an existing participant row.
//
//nolint:unparam // plugin SDK Handler contract requires (*CommandResponse, error); errors are conveyed via pluginsdk.Errorf returning a CommandError status response, not via Go error returns
func (p *scenePlugin) handleJoin(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	fields := strings.Fields(args)
	if len(fields) != 1 {
		return pluginsdk.Errorf("Usage: scene join #<scene id>"), nil
	}
	sceneID := normalizeSceneID(fields[0])

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

	if joinErr := p.focusClient.JoinFocus(ctx, req.SessionID, pluginsdk.FocusKey{
		Kind:     pluginsdk.FocusKindScene,
		TargetID: sceneID,
	}); joinErr != nil {
		var oe oops.OopsError
		if !errors.As(joinErr, &oe) || oe.Code() != "FOCUS_ALREADY_MEMBER" {
			// Only surface the error for non-idempotent failures. FOCUS_ALREADY_MEMBER
			// falls through to AutoFocusOnJoin: the session is already a focus member
			// but per-connection focus state may still need updating.
			slog.WarnContext(
				ctx, "scene.command.join focus join failed",
				"subject_id", req.CharacterID,
				"session_id", req.SessionID,
				"scene_id", sceneID,
				"error", joinErr,
			)
			return pluginsdk.Errorf(
				"Joined scene in database, but your session could not subscribe (%v). "+
					"Please retry `scene join %s`.", joinErr, sceneID,
			), nil
		}
	}

	// Step 3: AutoFocusOnJoin — fan-out to all terminal/telnet connections.
	// This is a best-effort call: errors are non-fatal because the character
	// is already in the scene (DB write + FocusMembership both committed).
	// The pre-condition (JoinFocus completed) is satisfied by the step above.
	afResult, afErr := p.focusClient.AutoFocusOnJoin(ctx, req.CharacterID, sceneID)
	if afErr != nil {
		slog.WarnContext(
			ctx, "scene.command.join auto-focus-on-join failed (non-fatal)",
			"subject_id", req.CharacterID,
			"session_id", req.SessionID,
			"scene_id", sceneID,
			"error", afErr,
		)
		// Detailed error is already logged; user-facing message stays
		// fixed so players don't see transport/internal-error details.
		// (CodeRabbit PR #4191 round 6)
		return &pluginsdk.CommandResponse{
			Status: pluginsdk.CommandOK,
			Output: fmt.Sprintf("Joined scene #%s. (Auto-focus is unavailable; use 'scene focus #%s' to focus manually.)", sceneID, sceneID),
		}, nil
	}

	if len(afResult.FailedConnectionIDs) > 0 {
		slog.WarnContext(
			ctx, "scene.command.join auto-focus partial failure",
			"subject_id", req.CharacterID,
			"session_id", req.SessionID,
			"scene_id", sceneID,
			"failed_count", len(afResult.FailedConnectionIDs),
		)
	}

	// 5-branch render based on substrate outcome. Failure check runs FIRST
	// so per-connection auto-focus failures aren't hidden under success or
	// skipped messaging (CodeRabbit PR #4191).
	// TODO(Phase 6 §7.4): add mixed-render branch when both focused and skipped are non-empty.
	var msg string
	switch {
	case len(afResult.FailedConnectionIDs) > 0:
		// Any failure (alone or alongside success/skip): surface warning.
		msg = fmt.Sprintf("Joined scene #%s but auto-focus failed for %d connection(s).", sceneID, len(afResult.FailedConnectionIDs))
	case len(afResult.FocusedConnectionIDs) > 0 && len(afResult.SkippedConnectionIDs) == 0:
		// Terminal-focused: one or more terminal/telnet connections were auto-focused.
		msg = fmt.Sprintf("Joined scene #%s and focused your terminal connection(s) on it.", sceneID)
	case len(afResult.SkippedConnectionIDs) > 0 && len(afResult.FocusedConnectionIDs) == 0:
		// Explicitly-focused-elsewhere: terminal stays on its current focus (INV-SCENE-24).
		msg = fmt.Sprintf("Joined scene #%s. Your terminal stays on its current focus; use 'scene focus #%s' to switch.", sceneID, sceneID)
	case afResult.TotalConnectionCount > 0 && len(afResult.FocusedConnectionIDs) == 0 && len(afResult.SkippedConnectionIDs) == 0:
		// Comms-hub-only: only non-terminal connections exist (INV-SCENE-17 filtered them out).
		msg = fmt.Sprintf("Joined scene #%s. Use 'scene focus #%s' to enter.", sceneID, sceneID)
	default:
		// TotalConnectionCount == 0: no live connections (admin / scripted join).
		msg = fmt.Sprintf("Joined scene #%s.", sceneID)
	}
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: msg,
	}, nil
}

// handleLeave parses "scene leave <scene-id>", calls LeaveScene, then calls
// focusClient.LeaveFocus. Focus errors are logged but do not fail the command
// since the DB is the source of truth for scene membership.
func (p *scenePlugin) handleLeave(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	fields := strings.Fields(args)
	if len(fields) != 1 {
		return pluginsdk.Errorf("Usage: scene leave #<scene id>"), nil
	}
	sceneID := normalizeSceneID(fields[0])

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
func (p *scenePlugin) handleInvite(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	// Strict arity: reject anything other than exactly 2 tokens — see handleJoin.
	fields := strings.Fields(args)
	if len(fields) != 2 {
		return pluginsdk.Errorf("Usage: scene invite #<scene id> <character>"), nil
	}
	sceneID, target := normalizeSceneID(fields[0]), fields[1]

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
func (p *scenePlugin) handleKick(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	// Strict arity: reject anything other than exactly 2 tokens — see handleJoin.
	fields := strings.Fields(args)
	if len(fields) != 2 {
		return pluginsdk.Errorf("Usage: scene kick #<scene id> <character>"), nil
	}
	sceneID, target := normalizeSceneID(fields[0]), fields[1]

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
func (p *scenePlugin) handleTransfer(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	// Strict arity: reject anything other than exactly 2 tokens — see handleJoin.
	fields := strings.Fields(args)
	if len(fields) != 2 {
		return pluginsdk.Errorf("Usage: scene transfer #<scene id> <character>"), nil
	}
	sceneID, target := normalizeSceneID(fields[0]), fields[1]

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
		return pluginsdk.Errorf("Usage: scene switch #<scene id>"), nil
	}
	sceneID := normalizeSceneID(fields[0])

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
				"You are not a member of scene %s. Use `scene join #%s` first.", sceneID, sceneID,
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

// handleSceneGrid implements `scene grid`. Clears the per-connection focus
// pointer back to grid (nil FocusKey) without touching Info.PresentingFocus.
//
// D10 + INV-SCENE-26: the substrate skips the PresentingFocus write when
// isSceneGrid=true, so the player's focus context (e.g., the scene they
// were last presenting) survives the grid pivot and is restored on reconnect.
// The plugin is only responsible for issuing the RPC with the correct args;
// substrate enforcement is tested in internal/grpc/focus tests (T14).
func (p *scenePlugin) handleSceneGrid(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	if p.focusClient == nil {
		slog.WarnContext(
			ctx, "scene.command.grid focus client not configured",
			"connection_id", req.ConnectionID,
		)
		return pluginsdk.Errorf("Failed to switch to grid: focus client not configured"), nil
	}
	// req.ConnectionID is allowed to be empty on server-side dispatch paths
	// (scripted / admin invocations). Calling SetConnectionFocus with "" would
	// surface as a wrapped INVALID_ULID — fail fast with a clear user-facing
	// message instead. (CodeRabbit PR #4191)
	if req.ConnectionID == "" {
		return pluginsdk.Errorf("`scene grid` requires a live connection."), nil
	}

	// D10: isSceneGrid=true so substrate skips PresentingFocus write.
	if err := p.focusClient.SetConnectionFocus(ctx, req.ConnectionID, nil /* focus_key */, true /* isSceneGrid */); err != nil {
		return nil, oops.Code("SCENE_GRID_SET_FAILED").
			With("connection_id", req.ConnectionID).
			Wrap(err)
	}
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: "Focused on the grid.",
	}, nil
}

// handleSceneFocus implements `scene focus #<id>`. Parses a scene reference,
// then calls SetConnectionFocus on the current connection. The substrate is the
// canonical authority for membership (INV-SCENE-14): FOCUS_WITHOUT_MEMBERSHIP from
// the substrate produces a user-facing denial; other substrate errors surface
// as SCENE_FOCUS_FAILED internal errors.
//
// The scene ref is normalized leniently: the '#'-prefixed display form (as
// surfaced in the web RECENT panel) and a bare ULID are both accepted, matching
// every other scene subcommand so a ref that works for `scene join` works here
// too (holomush-ehbnk).
//
// The plugin does NOT pre-check membership; substrate enforcement via T14 is
// sufficient per plan Task 19 ("let substrate be the authority").
func (p *scenePlugin) handleSceneFocus(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	// Accept the '#'-prefixed display form or a bare ULID interchangeably; strip
	// the optional '#' to get the bare scene ID. The substrate validates
	// membership and scene existence; the plugin parses + dispatches + renders.
	sceneID := normalizeSceneID(args)
	if sceneID == "" {
		return pluginsdk.Errorf("Usage: scene focus #<scene id>"), nil
	}

	if p.focusClient == nil {
		slog.WarnContext(
			ctx, "scene.command.focus focus client not configured",
			"connection_id", req.ConnectionID,
			"scene_id", sceneID,
		)
		return pluginsdk.Errorf("Failed to focus scene: focus client not configured"), nil
	}
	// req.ConnectionID is allowed to be empty on server-side dispatch paths
	// (scripted / admin invocations). Calling SetConnectionFocus with "" would
	// surface as a wrapped INVALID_ULID — fail fast with a clear user-facing
	// message instead. (CodeRabbit PR #4191)
	if req.ConnectionID == "" {
		return pluginsdk.Errorf("`scene focus` requires a live connection."), nil
	}

	fk := pluginsdk.FocusKey{Kind: pluginsdk.FocusKindScene, TargetID: sceneID}
	if err := p.focusClient.SetConnectionFocus(ctx, req.ConnectionID, &fk, false /* isSceneGrid */); err != nil {
		var oe oops.OopsError
		if errors.As(err, &oe) && oe.Code() == "FOCUS_WITHOUT_MEMBERSHIP" {
			return pluginsdk.Errorf("You're not in Scene %s.", sceneID), nil
		}
		return nil, oops.Code("SCENE_FOCUS_FAILED").
			With("connection_id", req.ConnectionID).
			With("scene_id", sceneID).
			Wrap(err)
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("You're now focused on Scene %s.", sceneID),
	}, nil
}

// handleSceneList implements `scene list`. Reads the character's scene
// memberships from the store (scene-kind only — channels and other focus
// kinds are not stored here), then calls IsAnyConnFocused per scene to
// determine the [focused] / [background] marker.
//
// Returns "You're not in any scenes." when the character has no memberships.
// RPC errors from IsAnyConnFocused surface as SCENE_LIST_FAILED.
func (p *scenePlugin) handleSceneList(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	sceneIDs, err := p.service.store.ListScenesForCharacter(ctx, req.CharacterID)
	if err != nil {
		return nil, oops.Code("SCENE_LIST_FAILED").
			With("character_id", req.CharacterID).
			Wrap(err)
	}
	if len(sceneIDs) == 0 {
		return pluginsdk.OK("You're not in any scenes."), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Your scenes (%d):\n", len(sceneIDs))
	for _, sceneID := range sceneIDs {
		// Only render a focus marker when we actually consulted the
		// substrate. Without focusClient, "unknown" must not masquerade
		// as "[background]" — that would falsely tell the user no
		// connection is focused. (CodeRabbit PR #4191 round 6)
		marker := ""
		if p.focusClient != nil {
			focused, err := p.focusClient.IsAnyConnFocused(ctx, req.CharacterID, sceneID)
			if err != nil {
				return nil, oops.Code("SCENE_LIST_FAILED").
					With("character_id", req.CharacterID).
					With("scene_id", sceneID).
					Wrap(err)
			}
			if focused {
				marker = " [focused]"
			} else {
				marker = " [background]"
			}
		}
		fmt.Fprintf(&b, "  %s%s\n", sceneID, marker)
	}
	return pluginsdk.OK(b.String()), nil
}

// handleEmit is the shared emit-subcommand handler for the four content
// verbs (pose / say / emit / ooc). The eventType determines the emitted
// type; the ooc flag determines the subject facet (.ic vs .ooc). All
// four emit with Sensitive: true to match the crypto.emits manifest's
// sensitivity:always declaration (INV-SCENE-3).
//
// Target scene resolution uses single-membership inference (Phase 4
// only); Phase 5 will replace this with focus-aware routing that
// consults the character's focus context.
//
// Authorization is enforced via the host ABAC evaluator using the
// write-scene-as-participant policy (action "write", resource "scene:<id>").
// The evaluator is called after scene-ID resolution, using the resolved
// scene ID as the resource ref.
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

	// Evaluate the write-scene-as-participant policy via the host ABAC engine.
	// Fails closed when evaluator is not configured.
	if p.evaluator == nil {
		slog.WarnContext(
			ctx, "scene.command.emit evaluator not configured",
			"subject_id", req.CharacterID,
			"event_type", eventType,
			"scene_id", sceneID,
		)
		return pluginsdk.Errorf("Permission check unavailable: evaluator not configured."), nil
	}
	dec, evalErr := p.evaluator.Evaluate(ctx, "write", "scene:"+sceneID)
	if evalErr != nil {
		evalErr = oops.Code("SCENE_EMIT_EVALUATE_FAILED").
			With("character_id", req.CharacterID).
			With("scene_id", sceneID).
			With("event_type", eventType).Wrap(evalErr)
		recordError(span, evalErr)
		slog.ErrorContext(
			ctx, "scene.command.emit permission check failed",
			"subject_id", req.CharacterID,
			"scene_id", sceneID,
			"event_type", eventType,
			"error", evalErr,
		)
		return pluginsdk.Failuref("permission check failed: %v", evalErr), nil
	}
	if !dec.Allowed {
		return pluginsdk.Errorf("You are not permitted to write to scene %s.", sceneID), nil
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
		Sensitive: true, // sensitivity:always per crypto.emits manifest §2 / INV-SCENE-3
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
//
// Authorization note: handleOrder is intentionally NOT engine-gated. Pose-order
// read authorization is enforced at the service layer via store.IsParticipant
// (INV-S9), which is fail-closed: non-members and invited-only characters both
// receive PermissionDenied before any scene data is returned. This is the one
// read path that was deliberately kept as a substrate-level check rather than
// consolidated into the engine, because the check is already precise, atomic
// with the DB query, and covered by TestGetPoseOrder_NotParticipant_PermissionDenied.
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

// normalizeSceneID strips a single optional leading '#' (and surrounding
// whitespace) from a scene-reference token, yielding the bare scene ULID used
// downstream (holomush-y5inx bare-ULID identity). The mandatory-scene-id
// subcommands (join, focus, switch, info, end, pause, resume, leave, invite,
// kick, transfer, set) accept the '#'-prefixed display form — as surfaced in
// the web RECENT panel, prompts, and join's own success hints — and the bare
// form interchangeably, so a ref that works for one subcommand works for all
// (holomush-ehbnk). Only the leading '#' is stripped; embedded junk is left for
// downstream ULID validation in the service layer.
func normalizeSceneID(token string) string {
	return strings.TrimPrefix(strings.TrimSpace(token), "#")
}

// sceneResourceRef derives the ABAC resource string for a scene subcommand
// from the subcommand args. Returns "scene:<id>" on success, error if args
// are empty after trimming. Used as the ResourceRef in GatedSubcommand for
// subcommands where the scene ID is the entire remaining args (end, pause,
// resume, leave, info — whole remainder is the scene ID). The id is normalized
// (optional '#' stripped) so the evaluated resource ref matches the bare id the
// handler passes downstream — no "scene:#<id>" skew (holomush-ehbnk).
func sceneResourceRef(args string) (string, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", fmt.Errorf("scene id is required")
	}
	return "scene:" + normalizeSceneID(fields[0]), nil // ABAC resource ref (type:id), not a pub/sub subject (INV-SCENE-1)
}

// sceneResourceRefFirstField derives the ABAC resource string for subcommands
// where the scene ID is the FIRST whitespace-separated token and additional
// tokens follow (set, invite, kick, transfer). Returns "scene:<id>" on success,
// error if args are empty.
func sceneResourceRefFirstField(args string) (string, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return "", oops.Errorf("scene id is required")
	}
	return "scene:" + normalizeSceneID(fields[0]), nil // ABAC resource ref (type:id), not a pub/sub subject (INV-SCENE-1)
}

// handleScenesBoard implements the top-level `scenes` command: the public
// open-scene board browser. It is distinct from `scene list` (handleSceneList,
// which lists the calling character's own memberships).
//
// Arg parsing: tokens of the form `tag:<t>` are collected into Tags; tokens of
// the form `hide:<cw>` are collected into ExcludeContentWarnings. Unrecognised
// tokens are silently ignored to keep the interface lenient for clients that
// pass extra whitespace or future unknown flags.
//
// Identity safety (iokti.14): PlayerID and CharacterID are taken exclusively
// from the authenticated CommandRequest — never from parsed args — so the CW
// block resolution inside ListScenes reads settings owned by the dispatch-token
// principal. This matches the ownership gate in iokti.19 and means persistent
// player/character blocks actually apply without any privilege escalation.
//
// Note on participant count: SceneInfo carries no participant count field.
// Rendering it would require an N+1 per-scene query, which is an anti-pattern
// for a list endpoint. The count is omitted from the board render; per-scene
// detail is available via `scene info <id>` (deviation from the plan draft).
func (p *scenePlugin) handleScenesBoard(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	// Parse tag:<t> / hide:<cw> tokens from args.
	var tags, hides []string
	for _, tok := range strings.Fields(req.Args) {
		switch {
		case strings.HasPrefix(tok, "tag:"):
			if t := strings.TrimPrefix(tok, "tag:"); t != "" {
				tags = append(tags, t)
			}
		case strings.HasPrefix(tok, "hide:"):
			if cw := strings.TrimPrefix(tok, "hide:"); cw != "" {
				hides = append(hides, cw)
			}
		default:
			// Unrecognized tokens are silently ignored.
		}
	}

	// Build the request with identity from the authenticated CommandRequest
	// (iokti.14 safety invariant: these match the dispatch-token's owning principal).
	boardReq := &scenev1.ListScenesRequest{
		Limit:                  50,
		Tags:                   tags,
		ExcludeContentWarnings: hides,
		CharacterId:            req.CharacterID,
		PlayerId:               req.PlayerID,
	}

	resp, err := p.service.ListScenes(ctx, boardReq)
	if err != nil {
		slog.WarnContext(ctx, "scenes board list failed", "error", err)
		return pluginsdk.Errorf("Could not load the scene board."), nil
	}

	scenes := resp.GetScenes()
	if len(scenes) == 0 {
		return pluginsdk.OK("No open scenes."), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Open scenes (%d):\n", len(scenes))
	for _, s := range scenes {
		// Build the CW label suffix — omit entirely when empty.
		cwLabel := ""
		if len(s.GetContentWarnings()) > 0 {
			cwLabel = fmt.Sprintf(" [CW: %s]", strings.Join(s.GetContentWarnings(), ", "))
		}
		// Build the tag suffix — omit when empty.
		tagLabel := ""
		if len(s.GetTags()) > 0 {
			tagLabel = fmt.Sprintf(" [tags: %s]", strings.Join(s.GetTags(), ", "))
		}
		// Paused marker.
		pausedMarker := ""
		if s.GetState() == string(SceneStatePaused) {
			pausedMarker = " [paused]"
		}
		fmt.Fprintf(
			&b, "  %s — %s (owner: %s)%s%s%s\n",
			s.GetId(),
			s.GetTitle(),
			s.GetOwnerId(),
			pausedMarker,
			cwLabel,
			tagLabel,
		)
	}
	return pluginsdk.OK(b.String()), nil
}

// handleVoteExtend implements `scene publish vote extend <count> [#<scene id>]`
// (spec §6.1 E2). It bumps the scene's max_publish_attempts budget by <count>
// (a positive integer). The caller must be a server admin: the admin-extend-
// publish-attempts ABAC policy (action "extend_publish_attempts", resource
// "scene:<id>") is evaluated in-handler BEFORE calling the service RPC, making
// this the live security gate for that policy.
//
// Arg form: <count> [#<scene id>] — count is first, optional scene ref is last.
// On success reports the new maximum.
func (p *scenePlugin) handleVoteExtend(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	// Parse <count> [#<scene id>].
	countStr, sceneRef := splitSubcommand(args)
	if countStr == "" {
		return pluginsdk.Errorf("Usage: scene publish vote extend <count> [#<scene id>]"), nil
	}
	count, atoiErr := strconv.Atoi(countStr)
	if atoiErr != nil || count <= 0 || count > math.MaxInt32 {
		// Non-integer, non-positive, or out-of-int32-range value — treat as usage error.
		return pluginsdk.Errorf("Usage: scene publish vote extend <count> [#<scene id>]"), nil //nolint:nilerr // atoiErr is intentionally converted to a user-facing usage message, not propagated as a Go error
	}

	// Resolve scene via the shared resolveSceneRef helper (handles #<id> +
	// single-membership inference, consistent with the other publish commands).
	sceneID, err := resolveSceneRef(ctx, p.service.store, req.CharacterID, strings.TrimSpace(sceneRef))
	if err != nil {
		return pluginsdk.Errorf("%v", err), nil
	}

	// ABAC gate — enforce the admin-extend-publish-attempts policy. Fails closed
	// when no evaluator is configured so a non-admin cannot bypass on a
	// misconfigured server.
	if p.evaluator == nil {
		slog.WarnContext(
			ctx, "scene.command.vote_extend evaluator not configured",
			"subject_id", req.CharacterID,
			"scene_id", sceneID,
		)
		return pluginsdk.Errorf("Permission check unavailable: evaluator not configured."), nil
	}
	dec, evalErr := p.evaluator.Evaluate(ctx, "extend_publish_attempts", "scene:"+sceneID)
	if evalErr != nil {
		evalErr = oops.Code("SCENE_VOTE_EXTEND_EVALUATE_FAILED").
			With("character_id", req.CharacterID).
			With("scene_id", sceneID).Wrap(evalErr)
		slog.ErrorContext(
			ctx, "scene.command.vote_extend permission check failed",
			"subject_id", req.CharacterID,
			"scene_id", sceneID,
			"error", evalErr,
		)
		return pluginsdk.Failuref("permission check failed: %v", evalErr), nil
	}
	if !dec.Allowed {
		reason := dec.Reason
		if reason == "" {
			reason = "permission denied"
		}
		return pluginsdk.Errorf("%s", reason), nil
	}

	// Gate passed — call the service RPC.
	resp, err := p.service.ExtendScenePublishVoteAttempts(ctx, &scenev1.ExtendScenePublishVoteAttemptsRequest{
		CallerCharacterId: req.CharacterID,
		SceneId:           sceneID,
		Additional:        int32(count), //nolint:gosec // count bounded to [1, math.MaxInt32] above
	})
	if err != nil {
		return pluginsdk.Errorf("Could not extend publish-vote budget: %v", err), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("Publish-vote attempt budget extended; new max is %d.", resp.GetNewMax()),
	}, nil
}
