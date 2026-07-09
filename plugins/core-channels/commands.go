// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	channelv1 "github.com/holomush/holomush/pkg/proto/holomush/channel/v1"
)

// channelNameResolver resolves a channel name to its persisted row for the
// command layer (name → id). *channelStore satisfies it via GetByName. The
// lookup is a plugin-internal read used only to translate a human-typed channel
// name into the id the ABAC-self-enforcing ChannelService RPCs consume; it is
// NOT an authorization decision (every operation still funnels through the
// service's own per-RPC gate, and a hidden channel and an absent channel both
// present the same uniform not-found — T-01-12).
type channelNameResolver interface {
	GetByName(ctx context.Context, name string) (*channelRow, error)
}

// channelCommandUsage is the top-level usage string for the `channel` command.
const channelCommandUsage = "Usage: channel <create|join|leave|list|say|who|history|invite|mute|ban|kick|transfer> [args]\n" +
	"Shorthand: =<channel> <message>  (=<channel> :pose  /  =<channel> ;semipose)"

// HandleCommand routes the `channel` command — and its `=` prefix-alias
// reassembly (`=Public hello` → `channel Public hello`, seeded as a system
// prefix alias by the host manifest alias-seeder, MED-6) — to the per-subcommand
// dispatcher. The command path is the human/CLI conversational surface
// (gateway-boundary rule); every structural/moderation subcommand delegates to
// the ABAC-self-enforcing ChannelService, and content posting flows through the
// service's emit path.
func (p *channelPlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	switch req.Command {
	case "channel":
		return p.dispatchChannelCommand(ctx, req)
	default:
		return pluginsdk.Errorf("core-channels does not handle command %q", req.Command), nil
	}
}

// dispatchChannelCommand parses the first token as a subcommand and routes it. A
// first token that is NOT a reserved subcommand is treated as a channel-name
// post (so the `=`-alias-reassembled `channel Public hello` posts `hello` to
// channel `Public`).
func (p *channelPlugin) dispatchChannelCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	sub, rest := splitToken(req.Args)
	if sub == "" {
		return pluginsdk.Errorf("%s", channelCommandUsage), nil
	}

	switch strings.ToLower(sub) {
	case "create":
		return p.handleChannelCreate(ctx, req, rest)
	case "join":
		return p.handleChannelJoin(ctx, req, rest)
	case "leave":
		return p.handleChannelLeave(ctx, req, rest)
	case "list":
		return p.handleChannelList(ctx, req)
	case "say":
		return p.handleChannelSay(ctx, req, rest)
	case "who":
		return p.handleChannelWho(ctx, req, rest)
	case "history":
		return p.handleChannelHistory(ctx, req, rest)
	case "invite":
		return p.handleChannelInvite(ctx, req, rest)
	case "mute":
		return p.handleChannelMute(ctx, req, rest)
	case "ban":
		return p.handleChannelBan(ctx, req, rest)
	case "kick":
		return p.handleChannelKick(ctx, req, rest)
	case "transfer":
		return p.handleChannelTransfer(ctx, req, rest)
	default:
		// Non-reserved first token → channel-name post (the `=name` shorthand).
		return p.handleChannelPost(ctx, req, sub, rest)
	}
}

// handleChannelCreate creates a channel owned by the caller. It stamps the
// host-vouched owning-player id into the trusted dispatch binding (R2-C) BEFORE
// delegating so the 5/hr per-player create limiter keys on the dispatcher-stamped
// req.PlayerID (never a client field); without it a non-admin create fails
// closed. `channel create <name> [type]`.
func (p *channelPlugin) handleChannelCreate(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	name, rest := splitToken(args)
	if name == "" {
		return pluginsdk.Errorf("Usage: channel create <name> [public|private|admin]"), nil
	}
	channelType, _ := splitToken(rest)

	ctx = withTrustedOwningPlayer(ctx, req.PlayerID)
	resp, err := p.service.CreateChannel(ctx, &channelv1.CreateChannelRequest{
		CharacterId: req.CharacterID,
		Name:        name,
		Type:        channelType,
	})
	if err != nil {
		return channelCommandError("create", err), nil
	}
	info := resp.GetChannel()
	return pluginsdk.OK(fmt.Sprintf("Channel created: %s (%s)", info.GetName(), info.GetId())), nil
}

// handleChannelJoin joins a channel by name. `channel join <name>`.
func (p *channelPlugin) handleChannelJoin(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	name, _ := splitToken(args)
	if name == "" {
		return pluginsdk.Errorf("Usage: channel join <name>"), nil
	}
	channelID, miss := p.resolveChannelID(ctx, name)
	if miss != nil {
		return miss, nil
	}
	if _, err := p.service.JoinChannel(ctx, &channelv1.JoinChannelRequest{
		CharacterId: req.CharacterID,
		ChannelId:   channelID,
		SessionId:   req.SessionID,
	}); err != nil {
		return channelCommandError("join", err), nil
	}
	return pluginsdk.OK(fmt.Sprintf("Joined %s.", name)), nil
}

// handleChannelLeave leaves a channel by name. `channel leave <name>`.
func (p *channelPlugin) handleChannelLeave(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	name, _ := splitToken(args)
	if name == "" {
		return pluginsdk.Errorf("Usage: channel leave <name>"), nil
	}
	channelID, miss := p.resolveChannelID(ctx, name)
	if miss != nil {
		return miss, nil
	}
	if _, err := p.service.LeaveChannel(ctx, &channelv1.LeaveChannelRequest{
		CharacterId: req.CharacterID,
		ChannelId:   channelID,
		SessionId:   req.SessionID,
	}); err != nil {
		return channelCommandError("leave", err), nil
	}
	return pluginsdk.OK(fmt.Sprintf("Left %s.", name)), nil
}

// handleChannelList lists the caller's channel memberships (CHAN-01).
func (p *channelPlugin) handleChannelList(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	resp, err := p.service.ListChannels(ctx, &channelv1.ListChannelsRequest{CharacterId: req.CharacterID})
	if err != nil {
		return channelCommandError("list", err), nil
	}
	channels := resp.GetChannels()
	if len(channels) == 0 {
		return pluginsdk.OK("You are not a member of any channels."), nil
	}
	var b strings.Builder
	b.WriteString("Channels:\n")
	for _, c := range channels {
		fmt.Fprintf(&b, "  %s (%s)\n", c.GetName(), c.GetType())
	}
	return pluginsdk.OK(b.String()), nil
}

// handleChannelSay posts a plain spoken line. `channel say <name> <text>`.
func (p *channelPlugin) handleChannelSay(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	name, text := splitToken(args)
	if name == "" || strings.TrimSpace(text) == "" {
		return pluginsdk.Errorf("Usage: channel say <name> <message>"), nil
	}
	return p.postContent(ctx, req, name, "say", text)
}

// handleChannelPost is the `=name <message>` shorthand path: a leading ':'
// makes it a spaced pose, a leading ';' a no-space semipose, otherwise a say.
func (p *channelPlugin) handleChannelPost(ctx context.Context, req pluginsdk.CommandRequest, name, raw string) (*pluginsdk.CommandResponse, error) {
	kind, text := classifyChannelContent(raw)
	if strings.TrimSpace(text) == "" {
		return pluginsdk.Errorf("Usage: =<channel> <message>"), nil
	}
	return p.postContent(ctx, req, name, kind, text)
}

// classifyChannelContent maps a raw shorthand body to a PostToChannel kind:
// ':' → pose (spaced), ';' → semipose (no-space), otherwise say. Mirrors the
// comm ";"/":" grammar shared by both plugin runtimes.
func classifyChannelContent(raw string) (kind, text string) {
	r := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(r, ";"):
		return "semipose", strings.TrimSpace(r[1:])
	case strings.HasPrefix(r, ":"):
		return "pose", strings.TrimSpace(r[1:])
	default:
		return "say", r
	}
}

// postContent resolves the channel name then delegates the content post to the
// ABAC-self-enforcing PostToChannel RPC (membership + not-muted gate + emit).
// The command layer never emits directly — the service is the single fence.
func (p *channelPlugin) postContent(ctx context.Context, req pluginsdk.CommandRequest, name, kind, text string) (*pluginsdk.CommandResponse, error) {
	channelID, miss := p.resolveChannelID(ctx, name)
	if miss != nil {
		return miss, nil
	}
	if _, err := p.service.PostToChannel(ctx, &channelv1.PostToChannelRequest{
		CharacterId: req.CharacterID,
		ChannelId:   channelID,
		Kind:        kind,
		Text:        text,
	}); err != nil {
		return channelCommandError("post", err), nil
	}
	return pluginsdk.OK(""), nil
}

// handleChannelWho renders the channel roster to a member. `channel who <name>`.
func (p *channelPlugin) handleChannelWho(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	name, _ := splitToken(args)
	if name == "" {
		return pluginsdk.Errorf("Usage: channel who <name>"), nil
	}
	channelID, miss := p.resolveChannelID(ctx, name)
	if miss != nil {
		return miss, nil
	}
	resp, err := p.service.WhoInChannel(ctx, &channelv1.WhoInChannelRequest{
		CharacterId: req.CharacterID,
		ChannelId:   channelID,
	})
	if err != nil {
		return channelCommandError("who", err), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Members of %s:\n", name)
	for _, m := range resp.GetMembers() {
		marker := ""
		if m.GetMuted() {
			marker = " (muted)"
		}
		fmt.Fprintf(&b, "  %s [%s]%s\n", m.GetCharacterName(), m.GetRole(), marker)
	}
	return pluginsdk.OK(b.String()), nil
}

// handleChannelHistory renders recent channel content to a member.
// `channel history <name> [count]`. The optional count is forwarded to the
// service as the requested page limit (the service/audit layer clamps it to the
// scrollback cap).
func (p *channelPlugin) handleChannelHistory(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	name, rest := splitToken(args)
	if name == "" {
		return pluginsdk.Errorf("Usage: channel history <name> [count]"), nil
	}
	limit, ok := parseHistoryCount(rest)
	if !ok {
		return pluginsdk.Errorf("Usage: channel history <name> [count]"), nil
	}
	channelID, miss := p.resolveChannelID(ctx, name)
	if miss != nil {
		return miss, nil
	}
	resp, err := p.service.QueryChannelHistory(ctx, &channelv1.QueryChannelHistoryRequest{
		CharacterId: req.CharacterID,
		ChannelId:   channelID,
		Limit:       limit,
	})
	if err != nil {
		return channelCommandError("history", err), nil
	}
	entries := resp.GetEntries()
	if len(entries) == 0 {
		return pluginsdk.OK(fmt.Sprintf("No history for %s.", name)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "History for %s:\n", name)
	for _, e := range entries {
		fmt.Fprintf(&b, "  %s: %s\n", e.GetActorName(), e.GetContent())
	}
	return pluginsdk.OK(b.String()), nil
}

// parseHistoryCount parses the optional history count token. An empty token
// yields (0, true) — the service applies its default page size. A malformed or
// out-of-range token yields (0, false) so the caller can surface usage. The
// value is clamped to a non-negative int32 (the wire limit type); the
// service/audit layer clamps it to the scrollback cap.
func parseHistoryCount(rest string) (int32, bool) {
	token, _ := splitToken(rest)
	if token == "" {
		return 0, true
	}
	n, err := strconv.Atoi(token)
	if err != nil || n < 0 {
		return 0, false
	}
	const maxHistoryCount = 1 << 30 // well above any scrollback cap; guards the int32 conversion
	if n > maxHistoryCount {
		n = maxHistoryCount
	}
	return int32(n), true //nolint:gosec // n is clamped to 1<<30, well within int32 range
}

// handleChannelInvite admits a target to a channel (owner+admin only, D-05).
// `channel invite <name> <target>`.
func (p *channelPlugin) handleChannelInvite(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	channelID, target, miss := p.resolveModerationTarget(ctx, "invite", args)
	if miss != nil {
		return miss, nil
	}
	if _, err := p.service.InviteToChannel(ctx, &channelv1.InviteToChannelRequest{
		CharacterId:       req.CharacterID,
		ChannelId:         channelID,
		TargetCharacterId: target,
	}); err != nil {
		return channelCommandError("invite", err), nil
	}
	return pluginsdk.OK("Invited."), nil
}

// handleChannelMute mutes a member (owner+admin only, D-05).
// `channel mute <name> <target>`.
func (p *channelPlugin) handleChannelMute(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	channelID, target, miss := p.resolveModerationTarget(ctx, "mute", args)
	if miss != nil {
		return miss, nil
	}
	if _, err := p.service.MuteMember(ctx, &channelv1.MuteMemberRequest{
		CharacterId:       req.CharacterID,
		ChannelId:         channelID,
		TargetCharacterId: target,
	}); err != nil {
		return channelCommandError("mute", err), nil
	}
	return pluginsdk.OK("Muted."), nil
}

// handleChannelBan bans a member (owner+admin only, D-05).
// `channel ban <name> <target>`.
func (p *channelPlugin) handleChannelBan(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	channelID, target, miss := p.resolveModerationTarget(ctx, "ban", args)
	if miss != nil {
		return miss, nil
	}
	if _, err := p.service.BanMember(ctx, &channelv1.BanMemberRequest{
		CharacterId:       req.CharacterID,
		ChannelId:         channelID,
		TargetCharacterId: target,
	}); err != nil {
		return channelCommandError("ban", err), nil
	}
	return pluginsdk.OK("Banned."), nil
}

// handleChannelKick removes a member (owner+admin only, D-05).
// `channel kick <name> <target>`.
func (p *channelPlugin) handleChannelKick(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	channelID, target, miss := p.resolveModerationTarget(ctx, "kick", args)
	if miss != nil {
		return miss, nil
	}
	if _, err := p.service.KickMember(ctx, &channelv1.KickMemberRequest{
		CharacterId:       req.CharacterID,
		ChannelId:         channelID,
		TargetCharacterId: target,
	}); err != nil {
		return channelCommandError("kick", err), nil
	}
	return pluginsdk.OK("Kicked."), nil
}

// handleChannelTransfer reassigns ownership to another member (owner+admin only,
// D-05). `channel transfer <name> <newowner>`.
func (p *channelPlugin) handleChannelTransfer(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	channelID, newOwner, miss := p.resolveModerationTarget(ctx, "transfer", args)
	if miss != nil {
		return miss, nil
	}
	if _, err := p.service.TransferOwnership(ctx, &channelv1.TransferOwnershipRequest{
		CharacterId:         req.CharacterID,
		ChannelId:           channelID,
		NewOwnerCharacterId: newOwner,
	}); err != nil {
		return channelCommandError("transfer", err), nil
	}
	return pluginsdk.OK("Ownership transferred."), nil
}

// resolveModerationTarget parses `<name> <target>` and resolves the channel name
// to its id. It returns a usage error when either token is missing, or the
// uniform not-found when the channel cannot be resolved. The target is treated
// as a character id (no name resolver is wired in-plugin; name→character
// resolution is a host follow-up).
func (p *channelPlugin) resolveModerationTarget(ctx context.Context, verb, args string) (channelID, target string, miss *pluginsdk.CommandResponse) {
	name, rest := splitToken(args)
	target, _ = splitToken(rest)
	if name == "" || target == "" {
		return "", "", pluginsdk.Errorf("Usage: channel %s <name> <character>", verb)
	}
	id, m := p.resolveChannelID(ctx, name)
	if m != nil {
		return "", "", m
	}
	return id, target, nil
}

// resolveChannelID translates a channel name to its id via the resolver. A
// not-found (or lookup failure) returns the uniform not-found response so a
// hidden or absent channel are indistinguishable at the command surface
// (T-01-12). A non-not-found error surfaces as a service failure.
func (p *channelPlugin) resolveChannelID(ctx context.Context, name string) (string, *pluginsdk.CommandResponse) {
	row, err := p.channels.GetByName(ctx, name)
	if err != nil {
		if isChannelNotFound(err) {
			return "", uniformChannelNotFound()
		}
		return "", pluginsdk.Failuref("Channel lookup failed.")
	}
	return row.ID, nil
}

// isChannelNotFound reports whether err is the store's CHANNEL_NOT_FOUND.
func isChannelNotFound(err error) bool {
	var oe oops.OopsError
	return errors.As(err, &oe) && oe.Code() == "CHANNEL_NOT_FOUND"
}

// uniformChannelNotFound is the single user-facing not-found the command layer
// returns for a hidden channel, an absent channel, and an authority-denied
// per-channel operation — so none of them is an existence oracle (T-01-12).
func uniformChannelNotFound() *pluginsdk.CommandResponse {
	return pluginsdk.Errorf("No such channel.")
}

// channelCommandError maps a ChannelService gRPC status error to a user-facing
// command response. codes.NotFound collapses to the uniform not-found (hidden /
// absent / authority-denied are indistinguishable, T-01-12); the remaining codes
// map to specific but non-leaking messages.
func channelCommandError(op string, err error) *pluginsdk.CommandResponse {
	switch status.Code(err) {
	case codes.NotFound:
		return uniformChannelNotFound()
	case codes.PermissionDenied:
		return pluginsdk.Errorf("You are not permitted to %s here.", op)
	case codes.InvalidArgument:
		return pluginsdk.Errorf("Invalid %s request.", op)
	case codes.AlreadyExists:
		return pluginsdk.Errorf("A channel with that name already exists.")
	case codes.FailedPrecondition:
		return pluginsdk.Errorf("That %s is not allowed in the channel's current state.", op)
	case codes.ResourceExhausted:
		return pluginsdk.Errorf("Channel creation rate limit exceeded; try again later.")
	default:
		return pluginsdk.Failuref("Channel %s failed.", op)
	}
}

// splitToken splits args into the first whitespace-delimited token and the
// remainder (trimmed). Mirrors core-scenes splitSubcommand.
func splitToken(args string) (token, rest string) {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return "", ""
	}
	if i := strings.IndexFunc(trimmed, func(r rune) bool { return r == ' ' || r == '\t' }); i >= 0 {
		return trimmed[:i], strings.TrimSpace(trimmed[i+1:])
	}
	return trimmed, ""
}
