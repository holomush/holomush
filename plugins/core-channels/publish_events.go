// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"

	"github.com/samber/oops"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/holomush/holomush/pkg/plugin/comm"
)

// Channel wire event types. Every emitted event carries a plugin-qualified wire
// type (`core-channels:<verb>`, one colon) per INV-PLUGIN-40; the host's
// RenderingPublisher.Lookup resolves it against the manifest verbs[].type set
// and hard-fails EMIT_UNKNOWN_VERB on a miss. Channels declare NO crypto.emits
// (plaintext, D-04), so there is no bare registered-emit set to keep in sync.
const (
	channelSayType    = pluginsdk.EventType("core-channels:channel_say")
	channelPoseType   = pluginsdk.EventType("core-channels:channel_pose")
	channelJoinType   = pluginsdk.EventType("core-channels:channel_join")
	channelLeaveType  = pluginsdk.EventType("core-channels:channel_leave")
	channelMuteType   = pluginsdk.EventType("core-channels:channel_mute")
	channelBanType    = pluginsdk.EventType("core-channels:channel_ban")
	channelKickType   = pluginsdk.EventType("core-channels:channel_kick")
	channelRenameType = pluginsdk.EventType("core-channels:channel_rename")
)

// channelNotice is the small bespoke payload for a channel notice (membership /
// moderation / lifecycle) event. It carries operational metadata only — NEVER a
// channel_name field used for authorization: channel identity is the subject
// plus a live name lookup by id (D-08). old_name / new_name on a rename are
// display metadata, not an authz key.
type channelNotice struct {
	ActorID    string `json:"actor_id,omitempty"`
	ActorName  string `json:"actor_name,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	TargetName string `json:"target_name,omitempty"`
	OldName    string `json:"old_name,omitempty"`
	NewName    string `json:"new_name,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// channelEventEmitter emits channel content + notice events through the shared
// EventBus (CHAN-03). It mirrors core-scenes' publishEventEmitter: it wraps the
// SDK EventSink plus the game id and builds JetStream-native dot subjects. The
// content-emit + notice-emit helpers built here are reused by 01-05b's
// PostToChannel / moderation-notice RPCs (HIGH-4).
type channelEventEmitter struct {
	sink   pluginsdk.EventSink
	gameID string
}

// newChannelEventEmitter returns an emitter backed by the given sink and gameID.
func newChannelEventEmitter(sink pluginsdk.EventSink, gameID string) *channelEventEmitter {
	return &channelEventEmitter{sink: sink, gameID: gameID}
}

// dotStyleChannelSubject builds the JetStream-native subject a channel event is
// emitted on: events.<game_id>.channel.<channel_id>. Mirrors core-scenes'
// dotStyleSceneSubjectIC. The host owns any further Qualify; this is the fully
// qualified subject the audit block (events.*.channel.>) matches.
func dotStyleChannelSubject(gameID, channelID string) string {
	return "events." + gameID + ".channel." + channelID
}

// emitContent builds the CommunicationContent JSON payload (via the comm
// builder) and emits it plaintext (Sensitive:false, D-04) on the channel
// subject with the given qualified wire type. The payload carries the actor +
// text only — never a channel_name authz field (D-08).
func (e *channelEventEmitter) emitContent(ctx context.Context, channelID string, evType pluginsdk.EventType, payload string) error {
	return e.sink.Emit(ctx, pluginsdk.EmitIntent{ //nolint:wrapcheck // EventSink error passes through as-is; caller decides logging
		Subject:   dotStyleChannelSubject(e.gameID, channelID),
		Type:      evType,
		Payload:   payload,
		Sensitive: false,
	})
}

// emitSay builds a CommunicationContent JSON payload for a channel "say" and
// emits it plaintext.
func (e *channelEventEmitter) emitSay(ctx context.Context, channelID string, author comm.Author, text string) error {
	payload, err := comm.Say(author, text)
	if err != nil {
		return oops.Code("CHANNEL_EMIT_PAYLOAD_FAILED").With("type", string(channelSayType)).Wrap(err)
	}
	return e.emitContent(ctx, channelID, channelSayType, payload)
}

// emitPose builds a CommunicationContent JSON payload for a channel "pose"
// (":"/";" grammar via comm.Pose) and emits it plaintext.
func (e *channelEventEmitter) emitPose(ctx context.Context, channelID string, author comm.Author, invokedAs, raw string) error {
	payload, err := comm.Pose(author, invokedAs, raw)
	if err != nil {
		return oops.Code("CHANNEL_EMIT_PAYLOAD_FAILED").With("type", string(channelPoseType)).Wrap(err)
	}
	return e.emitContent(ctx, channelID, channelPoseType, payload)
}

// emitNotice marshals the bespoke notice payload and emits it plaintext on the
// channel subject with the given qualified wire type.
func (e *channelEventEmitter) emitNotice(ctx context.Context, channelID string, evType pluginsdk.EventType, notice channelNotice) error {
	payload, err := json.Marshal(notice)
	if err != nil {
		return oops.Code("CHANNEL_EMIT_PAYLOAD_FAILED").With("type", string(evType)).Wrap(err)
	}
	return e.emitContent(ctx, channelID, evType, string(payload))
}

// emitJoin/emitLeave/emitMute/emitBan/emitKick/emitRename emit the notice
// events. Each stamps its qualified wire type; the payload is operational
// metadata only.
func (e *channelEventEmitter) emitJoin(ctx context.Context, channelID string, notice channelNotice) error {
	return e.emitNotice(ctx, channelID, channelJoinType, notice)
}

func (e *channelEventEmitter) emitLeave(ctx context.Context, channelID string, notice channelNotice) error {
	return e.emitNotice(ctx, channelID, channelLeaveType, notice)
}

func (e *channelEventEmitter) emitMute(ctx context.Context, channelID string, notice channelNotice) error {
	return e.emitNotice(ctx, channelID, channelMuteType, notice)
}

func (e *channelEventEmitter) emitBan(ctx context.Context, channelID string, notice channelNotice) error {
	return e.emitNotice(ctx, channelID, channelBanType, notice)
}

func (e *channelEventEmitter) emitKick(ctx context.Context, channelID string, notice channelNotice) error {
	return e.emitNotice(ctx, channelID, channelKickType, notice)
}

func (e *channelEventEmitter) emitRename(ctx context.Context, channelID string, notice channelNotice) error {
	return e.emitNotice(ctx, channelID, channelRenameType, notice)
}
