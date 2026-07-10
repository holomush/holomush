// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/holomush/holomush/pkg/plugin/comm"
	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
)

// fakeEventSink records every EmitIntent the emitter sends.
type fakeEventSink struct {
	intents []pluginsdk.EmitIntent
	err     error
}

func (f *fakeEventSink) Emit(_ context.Context, intent pluginsdk.EmitIntent) error {
	if f.err != nil {
		return f.err
	}
	f.intents = append(f.intents, intent)
	return nil
}

const (
	testGameID    = "main"
	testChannelID = "01CHANNEL000000000000000000"
)

// assertNoChannelNameField unmarshals a JSON payload and asserts it carries no
// `channel_name` field — channel identity is the subject + live name lookup by
// id, never a payload name used for authorization (D-08).
func assertNoChannelNameField(t *testing.T, payload string) {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &m))
	_, present := m["channel_name"]
	assert.False(t, present, "payload MUST NOT carry a channel_name authz field (D-08)")
}

func TestEmitSayBuildsCommunicationContentOnDotSubject(t *testing.T) {
	sink := &fakeEventSink{}
	e := newChannelEventEmitter(sink, testGameID)

	err := e.emitSay(context.Background(), testChannelID,
		comm.Author{ID: "01CHAR0000000000000000000A", Name: "Alice"}, "hello channel")
	require.NoError(t, err)

	require.Len(t, sink.intents, 1)
	got := sink.intents[0]
	assert.Equal(t, "events."+testGameID+".channel."+testChannelID, got.Subject,
		"content emits on events.<game>.channel.<id>")
	assert.Equal(t, channelSayType, got.Type, "wire type MUST be qualified core-channels:channel_say")
	assert.False(t, got.Sensitive, "channel content is plaintext (D-04)")

	var content commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(got.Payload), &content))
	assert.Equal(t, "01CHAR0000000000000000000A", content.GetActorId())
	assert.Equal(t, "Alice", content.GetActorDisplayName())
	assert.Equal(t, "hello channel", content.GetText())
	assertNoChannelNameField(t, got.Payload)
}

func TestEmitPoseUsesPoseGrammarAndQualifiedType(t *testing.T) {
	sink := &fakeEventSink{}
	e := newChannelEventEmitter(sink, testGameID)

	// ":" invocation is a pose with a leading space before the text.
	err := e.emitPose(context.Background(), testChannelID,
		comm.Author{ID: "01CHAR0000000000000000000A", Name: "Alice"}, ":", "waves")
	require.NoError(t, err)

	require.Len(t, sink.intents, 1)
	got := sink.intents[0]
	assert.Equal(t, channelPoseType, got.Type)
	assert.False(t, got.Sensitive)
	assert.Equal(t, "events."+testGameID+".channel."+testChannelID, got.Subject)

	var content commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(got.Payload), &content))
	assert.Equal(t, "waves", content.GetText())
	assertNoChannelNameField(t, got.Payload)
}

func TestEmitNoticesAreQualifiedPlaintextAndCarryNoChannelName(t *testing.T) {
	notice := channelNotice{
		ActorID:    "01CHAR0000000000000000000A",
		ActorName:  "Alice",
		TargetID:   "01CHAR0000000000000000000B",
		TargetName: "Bob",
		Reason:     "spam",
	}
	cases := []struct {
		name     string
		call     func(e *channelEventEmitter) error
		wantType pluginsdk.EventType
	}{
		{"join", func(e *channelEventEmitter) error {
			return e.emitJoin(context.Background(), testChannelID, notice)
		}, channelJoinType},
		{"leave", func(e *channelEventEmitter) error {
			return e.emitLeave(context.Background(), testChannelID, notice)
		}, channelLeaveType},
		{"mute", func(e *channelEventEmitter) error {
			return e.emitMute(context.Background(), testChannelID, notice)
		}, channelMuteType},
		{"ban", func(e *channelEventEmitter) error {
			return e.emitBan(context.Background(), testChannelID, notice)
		}, channelBanType},
		{"kick", func(e *channelEventEmitter) error {
			return e.emitKick(context.Background(), testChannelID, notice)
		}, channelKickType},
		{"rename", func(e *channelEventEmitter) error {
			return e.emitRename(context.Background(), testChannelID,
				channelNotice{ActorID: "01CHAR0000000000000000000A", OldName: "Old", NewName: "New"})
		}, channelRenameType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &fakeEventSink{}
			e := newChannelEventEmitter(sink, testGameID)
			require.NoError(t, tc.call(e))

			require.Len(t, sink.intents, 1)
			got := sink.intents[0]
			assert.Equal(t, tc.wantType, got.Type, "notice wire type MUST be qualified")
			assert.False(t, got.Sensitive, "notices are plaintext (D-04)")
			assert.Equal(t, "events."+testGameID+".channel."+testChannelID, got.Subject)
			assertNoChannelNameField(t, got.Payload)
		})
	}
}

func TestDotStyleChannelSubjectFormat(t *testing.T) {
	assert.Equal(t, "events.main.channel.01ABC", dotStyleChannelSubject("main", "01ABC"))
}
