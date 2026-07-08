// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"

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

// emitSay builds a CommunicationContent JSON payload for a channel "say" and
// emits it plaintext (Sensitive:false). RED stub — GREEN implements the build.
func (e *channelEventEmitter) emitSay(_ context.Context, _ string, _ comm.Author, _ string) error {
	return nil
}

// emitPose builds a CommunicationContent JSON payload for a channel "pose" and
// emits it plaintext. RED stub.
func (e *channelEventEmitter) emitPose(_ context.Context, _ string, _ comm.Author, _, _ string) error {
	return nil
}

// emitJoin/emitLeave/emitMute/emitBan/emitKick/emitRename emit the notice
// events. RED stubs.
func (e *channelEventEmitter) emitJoin(_ context.Context, _ string, _ channelNotice) error {
	return nil
}

func (e *channelEventEmitter) emitLeave(_ context.Context, _ string, _ channelNotice) error {
	return nil
}

func (e *channelEventEmitter) emitMute(_ context.Context, _ string, _ channelNotice) error {
	return nil
}

func (e *channelEventEmitter) emitBan(_ context.Context, _ string, _ channelNotice) error {
	return nil
}

func (e *channelEventEmitter) emitKick(_ context.Context, _ string, _ channelNotice) error {
	return nil
}

func (e *channelEventEmitter) emitRename(_ context.Context, _ string, _ channelNotice) error {
	return nil
}
