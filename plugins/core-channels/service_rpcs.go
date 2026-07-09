// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/pkg/plugin/comm"
	channelv1 "github.com/holomush/holomush/pkg/proto/holomush/channel/v1"
)

// This file completes the holomush.channel.v1.ChannelService surface (01-05b,
// review HIGH-4): the eight RPCs 01-05 left as UnimplementedChannelServiceServer
// stubs. Each moderation/structural verb self-enforces ABAC per verb via the
// host evaluator BEFORE mutating state (INV-SCENE-65 analog, mirroring
// plugins/core-scenes/service.go); the read verbs (who/history) are
// membership-gated on the plugin-owned store. Every RPC binds to the
// host-vouched dispatch subject and never trusts client-supplied identity.

// gateAction self-enforces the ABAC gate for a moderation/structural verb
// (invite/mute/ban/kick/transfer) BEFORE any store mutation, independent of the
// command wrapper. A nil evaluator or engine error fails closed (Internal). A
// denied decision collapses to a uniform codes.NotFound: a caller who is not the
// owner/admin (or cannot see the channel at all) MUST NOT be able to distinguish
// "the channel exists but you lack authority" from "no such channel" (T-01-12).
func (s *channelService) gateAction(ctx context.Context, span trace.Span, action, channelID, op string) error {
	if s.evaluator == nil {
		slog.WarnContext(ctx, "channel service action gate: evaluator not configured", "op", op, "channel_id", channelID)
		return status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	dec, err := s.evaluator.Evaluate(ctx, action, "channel:"+channelID)
	if err != nil {
		recordError(span, err)
		errutil.LogErrorContext(ctx, "channel service action gate: evaluation failed", err, "op", op)
		return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if !dec.Allowed {
		return status.Error(codes.NotFound, "channel not found") //nolint:wrapcheck // uniform hidden/absent/unauthorized per T-01-12
	}
	return nil
}

// emitNoticeBestEffort emits a moderation/membership notice after a successful
// store mutation. It is non-fatal: the durable effect (the membership change +
// the channel_ops_events row) has already committed, so a notice emit failure is
// logged, not returned. Skipped when the emitter is not wired (test harnesses).
func (s *channelService) emitNoticeBestEffort(ctx context.Context, channelID, actorID, targetID, op string, emit func(context.Context, string, channelNotice) error) {
	if s.emitter == nil {
		return
	}
	notice := channelNotice{ActorID: actorID, ActorName: actorID, TargetID: targetID, TargetName: targetID}
	if err := emit(ctx, channelID, notice); err != nil {
		errutil.LogErrorContext(ctx, "channel service: notice emit failed", err, "op", op)
	}
}

// InviteToChannel admits a target character to a channel on the authority of an
// owner or admin (D-05). It self-enforces the `invite` ABAC gate, then records
// the target's membership via the store (the join-flow the read gate consumes),
// and emits a join notice. A non-owner non-admin — or a caller who cannot see
// the channel — receives a uniform not-found.
func (s *channelService) InviteToChannel(ctx context.Context, req *channelv1.InviteToChannelRequest) (*channelv1.InviteToChannelResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.invite_to_channel",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()),
		attribute.String("target_id", req.GetTargetCharacterId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.invite_to_channel actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to invite for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if err := s.gateAction(ctx, span, "invite", req.GetChannelId(), "channel.service.invite_to_channel"); err != nil {
		return nil, err
	}
	if err := s.store.JoinChannel(ctx, req.GetChannelId(), req.GetTargetCharacterId()); err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.invite_to_channel")
	}
	s.emitNoticeBestEffort(ctx, req.GetChannelId(), req.GetCharacterId(), req.GetTargetCharacterId(),
		"channel.service.invite_to_channel", s.emitter.emitJoin)
	slog.InfoContext(ctx, "channel.service.invite_to_channel ok",
		"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId(), "target_id", req.GetTargetCharacterId())
	return &channelv1.InviteToChannelResponse{}, nil
}

// MuteMember suppresses a member's posts (owner+admin only, D-05). It
// self-enforces the `mute` ABAC gate, sets the muted flag via the store (which
// also appends the moderation.mute ops event), and emits a mute notice.
func (s *channelService) MuteMember(ctx context.Context, req *channelv1.MuteMemberRequest) (*channelv1.MuteMemberResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.mute_member",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()),
		attribute.String("target_id", req.GetTargetCharacterId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.mute_member actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to moderate for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if err := s.gateAction(ctx, span, "mute", req.GetChannelId(), "channel.service.mute_member"); err != nil {
		return nil, err
	}
	if err := s.store.SetMuted(ctx, req.GetChannelId(), req.GetTargetCharacterId(), true); err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.mute_member")
	}
	s.emitNoticeBestEffort(ctx, req.GetChannelId(), req.GetCharacterId(), req.GetTargetCharacterId(),
		"channel.service.mute_member", s.emitter.emitMute)
	slog.InfoContext(ctx, "channel.service.mute_member ok",
		"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId(), "target_id", req.GetTargetCharacterId())
	return &channelv1.MuteMemberResponse{}, nil
}

// BanMember bans a member from a channel (owner+admin only, D-05). It
// self-enforces the `ban` ABAC gate, sets the banned flag via the store (which
// retains the row so JoinChannel refuses a rejoin, and appends the
// moderation.ban ops event), and emits a ban notice. Live-delivery removal is
// the leave/RemoveStream path (01-08).
func (s *channelService) BanMember(ctx context.Context, req *channelv1.BanMemberRequest) (*channelv1.BanMemberResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.ban_member",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()),
		attribute.String("target_id", req.GetTargetCharacterId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.ban_member actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to moderate for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if err := s.gateAction(ctx, span, "ban", req.GetChannelId(), "channel.service.ban_member"); err != nil {
		return nil, err
	}
	if err := s.store.SetBanned(ctx, req.GetChannelId(), req.GetTargetCharacterId(), true); err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.ban_member")
	}
	s.emitNoticeBestEffort(ctx, req.GetChannelId(), req.GetCharacterId(), req.GetTargetCharacterId(),
		"channel.service.ban_member", s.emitter.emitBan)
	slog.InfoContext(ctx, "channel.service.ban_member ok",
		"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId(), "target_id", req.GetTargetCharacterId())
	return &channelv1.BanMemberResponse{}, nil
}

// KickMember removes a member from a channel (owner+admin only, D-05). It
// self-enforces the `kick` ABAC gate, removes the target's membership via the
// store (which appends the membership.kick ops event and refuses to kick the
// owner), and emits a kick notice.
func (s *channelService) KickMember(ctx context.Context, req *channelv1.KickMemberRequest) (*channelv1.KickMemberResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.kick_member",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()),
		attribute.String("target_id", req.GetTargetCharacterId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.kick_member actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to moderate for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if err := s.gateAction(ctx, span, "kick", req.GetChannelId(), "channel.service.kick_member"); err != nil {
		return nil, err
	}
	if err := s.store.KickMember(ctx, req.GetChannelId(), req.GetCharacterId(), req.GetTargetCharacterId()); err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.kick_member")
	}
	s.emitNoticeBestEffort(ctx, req.GetChannelId(), req.GetCharacterId(), req.GetTargetCharacterId(),
		"channel.service.kick_member", s.emitter.emitKick)
	slog.InfoContext(ctx, "channel.service.kick_member ok",
		"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId(), "target_id", req.GetTargetCharacterId())
	return &channelv1.KickMemberResponse{}, nil
}

// TransferOwnership reassigns ownership to another member (owner+admin only,
// D-05). It self-enforces the `transfer` ABAC gate, then the store promotes the
// new owner (who MUST already be a member) and demotes the previous owner, and
// emits a rename-style notice. Transferring to a non-member is rejected.
func (s *channelService) TransferOwnership(ctx context.Context, req *channelv1.TransferOwnershipRequest) (*channelv1.TransferOwnershipResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.transfer_ownership",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()),
		attribute.String("new_owner", req.GetNewOwnerCharacterId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.transfer_ownership actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to transfer for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if err := s.gateAction(ctx, span, "transfer", req.GetChannelId(), "channel.service.transfer_ownership"); err != nil {
		return nil, err
	}
	if err := s.store.TransferOwnership(ctx, req.GetChannelId(), req.GetCharacterId(), req.GetNewOwnerCharacterId()); err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.transfer_ownership")
	}
	s.emitNoticeBestEffort(ctx, req.GetChannelId(), req.GetCharacterId(), req.GetNewOwnerCharacterId(),
		"channel.service.transfer_ownership", s.emitter.emitRename)
	slog.InfoContext(ctx, "channel.service.transfer_ownership ok",
		"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId(), "new_owner", req.GetNewOwnerCharacterId())
	return &channelv1.TransferOwnershipResponse{}, nil
}

// PostToChannel publishes one line of member-authored content to a channel. It
// self-enforces the `emit` (membership) ABAC gate — a non-member (or a caller
// who cannot see the channel) receives a uniform not-found — then rejects a
// muted member, and emits the content through the 01-06 emit path. Identity is
// the subject plus a live name lookup, never a payload channel_name (D-08).
func (s *channelService) PostToChannel(ctx context.Context, req *channelv1.PostToChannelRequest) (*channelv1.PostToChannelResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.post_to_channel",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.post_to_channel actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to post for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	text := strings.TrimSpace(req.GetText())
	if text == "" {
		return nil, status.Error(codes.InvalidArgument, "message text is required") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	// Self-enforced membership gate via the Layer-2 emit policy (membership for
	// EVERY type; a banned character is excluded from resource.channel.members).
	if s.evaluator == nil {
		slog.WarnContext(ctx, "channel.service.post_to_channel evaluator not configured",
			"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "emit", "channel:"+req.GetChannelId())
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "channel.service.post_to_channel evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.NotFound, "channel not found") //nolint:wrapcheck // uniform hidden/absent/non-member per T-01-12
	}

	// A muted member is a member (they can see the channel), so a muted denial is
	// not an existence oracle — return PermissionDenied, not a uniform not-found.
	muted, mErr := s.store.IsMuted(ctx, req.GetChannelId(), req.GetCharacterId())
	if mErr != nil {
		return nil, mapStoreError(ctx, span, mErr, "channel.service.post_to_channel")
	}
	if muted {
		return nil, status.Error(codes.PermissionDenied, "you are muted in this channel") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	if s.emitter == nil {
		slog.WarnContext(ctx, "channel.service.post_to_channel emitter not configured",
			"channel_id", req.GetChannelId())
		return nil, status.Error(codes.Internal, "channel content unavailable") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	author := comm.Author{ID: req.GetCharacterId(), Name: req.GetCharacterId()}
	var emitErr error
	switch req.GetKind() {
	case "", "say":
		emitErr = s.emitter.emitSay(ctx, req.GetChannelId(), author, text)
	case "pose":
		emitErr = s.emitter.emitPose(ctx, req.GetChannelId(), author, ":", text)
	case "semipose":
		// The "=name ;text" shorthand: a no-space pose (comm ";"/":" grammar).
		emitErr = s.emitter.emitPose(ctx, req.GetChannelId(), author, ";", text)
	case "ooc":
		// No channel_ooc verb is declared (01-06 D-04): the two content verbs are
		// say + pose. Reject ooc explicitly rather than emit an unknown wire type.
		return nil, status.Error(codes.InvalidArgument, "ooc is not supported on channels") //nolint:wrapcheck // opaque per grpc-errors.md
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown content kind") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if emitErr != nil {
		recordError(span, emitErr)
		errutil.LogErrorContext(ctx, "channel.service.post_to_channel emit failed", emitErr, "channel_id", req.GetChannelId())
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	slog.InfoContext(ctx, "channel.service.post_to_channel ok",
		"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId(), "kind", req.GetKind())
	return &channelv1.PostToChannelResponse{}, nil
}

// WhoInChannel returns the channel's active member roster to a member. It is
// membership-gated on the plugin-owned store (existence-oracle-safe): a
// non-member — or a channel the caller cannot see — receives a uniform
// not-found. It never references location (channels are location-independent).
func (s *channelService) WhoInChannel(ctx context.Context, req *channelv1.WhoInChannelRequest) (*channelv1.WhoInChannelResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.who_in_channel",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.who_in_channel actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to read for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	isMember, _, err := s.store.MembershipForHistory(ctx, req.GetChannelId(), req.GetCharacterId())
	if err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.who_in_channel")
	}
	if !isMember {
		return nil, status.Error(codes.NotFound, "channel not found") //nolint:wrapcheck // uniform hidden/absent/non-member per T-01-12
	}

	rows, err := s.store.ListMembers(ctx, req.GetChannelId())
	if err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.who_in_channel")
	}
	members := make([]*channelv1.MemberInfo, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		members = append(members, &channelv1.MemberInfo{
			CharacterId:   r.CharacterID,
			CharacterName: r.CharacterID, // best-effort: no name resolver wired
			Role:          r.Role,
			Muted:         r.Muted,
			Banned:        r.Banned,
			JoinedAt:      timestamppb.New(r.JoinedAt),
		})
	}
	return &channelv1.WhoInChannelResponse{Members: members}, nil
}

// QueryChannelHistory reads recent channel content as a member. It delegates to
// the 01-06 membership-gated audit read (HistoryForMember), which shares the
// SAME membership fence as the streaming PluginAuditService.QueryHistory — the
// authorization is NOT re-implemented here. A non-member deny (or an absent
// channel) is presented as a uniform not-found at the service boundary (T-01-12).
func (s *channelService) QueryChannelHistory(ctx context.Context, req *channelv1.QueryChannelHistoryRequest) (*channelv1.QueryChannelHistoryResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.query_channel_history",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.query_channel_history actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to read for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if s.history == nil {
		slog.WarnContext(ctx, "channel.service.query_channel_history history fetcher not configured",
			"channel_id", req.GetChannelId())
		return nil, status.Error(codes.Internal, "channel history unavailable") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	subject := dotStyleChannelSubject(s.gameID, req.GetChannelId())
	rows, err := s.history.HistoryForMember(ctx, subject, req.GetChannelId(), req.GetCharacterId(), int(req.GetLimit()))
	if err != nil {
		// The fence returns PermissionDenied for a non-member (and, uniformly, for
		// an absent channel). Present the uniform not-found the rest of the new
		// per-channel RPCs use so history cannot be a hidden-channel oracle either.
		if status.Code(err) == codes.PermissionDenied {
			return nil, status.Error(codes.NotFound, "channel not found") //nolint:wrapcheck // uniform hidden/absent/non-member per T-01-12
		}
		return nil, err //nolint:wrapcheck // already a gRPC status from the fence
	}

	entries := make([]*channelv1.ChannelHistoryEntry, 0, len(rows))
	for i := range rows {
		entries = append(entries, channelLogRowToHistoryEntry(&rows[i]))
	}
	return &channelv1.QueryChannelHistoryResponse{Entries: entries}, nil
}

// channelContentPayload is the subset of a CommunicationContent payload needed
// to project a content event into a ChannelHistoryEntry. The 01-06 emit path
// marshals CommunicationContent with proto snake_case names (comm.build), so the
// JSON keys are actor_id / actor_display_name / text. Notice payloads carry none
// of these, yielding an empty content line (the display grammar is chosen from
// the entry type by the renderer).
type channelContentPayload struct {
	ActorID          string `json:"actor_id"`
	ActorDisplayName string `json:"actor_display_name"`
	Text             string `json:"text"`
}

// channelLogRowToHistoryEntry projects a stored channel_log row into the wire
// ChannelHistoryEntry. Best-effort: the actor name falls back to the id, and the
// rendered content falls back to empty for a non-content (notice) row.
func channelLogRowToHistoryEntry(r *channelLogRow) *channelv1.ChannelHistoryEntry {
	id := ""
	if len(r.id) == 16 {
		var u ulid.ULID
		copy(u[:], r.id)
		id = u.String()
	}
	var cp channelContentPayload
	if err := json.Unmarshal(r.payload, &cp); err != nil {
		cp = channelContentPayload{} // best-effort: a notice payload isn't CommunicationContent; leave fields empty
	}
	actorID := cp.ActorID
	if actorID == "" && len(r.actorID) == 16 {
		var u ulid.ULID
		copy(u[:], r.actorID)
		actorID = u.String()
	}
	actorName := cp.ActorDisplayName
	if actorName == "" {
		actorName = actorID
	}
	return &channelv1.ChannelHistoryEntry{
		Id:        id,
		Type:      r.eventType,
		ActorId:   actorID,
		ActorName: actorName,
		Content:   cp.Text,
		CreatedAt: timestamppb.New(r.timestamp),
	}
}
