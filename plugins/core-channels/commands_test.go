// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/pgnanos"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// fakeNameResolver is a controllable channelNameResolver for command tests. It
// maps a lower-cased channel name to a row; an unknown name returns
// CHANNEL_NOT_FOUND (the store's real not-found code), which the command layer
// presents as a uniform not-found.
type fakeNameResolver struct {
	byName map[string]*channelRow
	err    error
}

func (f *fakeNameResolver) GetByName(_ context.Context, name string) (*channelRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	row, ok := f.byName[strings.ToLower(name)]
	if !ok {
		return nil, oops.Code("CHANNEL_NOT_FOUND").With("name", name).Errorf("channel not found")
	}
	return row, nil
}

// newCommandPlugin builds a channelPlugin whose service is backed by a
// capturing sink + fake store and whose name resolver is a fake. The evaluator
// drives the service's per-RPC ABAC self-enforcement.
func newCommandPlugin(ev pluginsdk.HostEvaluator) (*channelPlugin, *fakeServiceStore, *fakeEventSink, *fakeNameResolver) {
	store := &fakeServiceStore{
		listFor:   map[string][]channelRow{},
		members:   map[string][]channelMemberRow{},
		mutedMap:  map[string]bool{},
		memberMap: map[string]time.Time{},
	}
	svc, sink := newFullServiceForTest(store, ev)
	res := &fakeNameResolver{byName: map[string]*channelRow{}}
	p := &channelPlugin{service: svc, channels: res}
	return p, store, sink, res
}

func cmd(command, args string) pluginsdk.CommandRequest {
	return pluginsdk.CommandRequest{
		Command:     command,
		Args:        args,
		CharacterID: testCharID,
		PlayerID:    testPlayerID,
	}
}

// ── router basics ────────────────────────────────────────────────────────

func TestHandleCommandRejectsForeignCommand(t *testing.T) {
	p, _, _, _ := newCommandPlugin(allowEvaluator)
	resp, err := p.HandleCommand(context.Background(), cmd("frobnicate", ""))
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
}

func TestHandleCommandReturnsUsageWhenSubcommandMissing(t *testing.T) {
	p, _, _, _ := newCommandPlugin(allowEvaluator)
	resp, err := p.HandleCommand(context.Background(), cmd("channel", ""))
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage")
}

// ── structural subcommands delegate to the service ───────────────────────

func TestHandleCommandCreateStampsTrustedPlayerAndCreates(t *testing.T) {
	p, store, _, _ := newCommandPlugin(adminEvaluator) // admin bypasses the rate limit
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "create Newroom private"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	require.Len(t, store.created, 1)
	assert.Equal(t, "Newroom", store.created[0].Name)
	assert.Equal(t, "private", store.created[0].Type)
	assert.Equal(t, testCharID, store.created[0].OwnerID)
}

func TestHandleCommandCreateNonAdminWithoutTrustedPlayerFailsClosed(t *testing.T) {
	// grantEvaluator = non-admin permit ⇒ rate-limited ⇒ needs a trusted
	// owning-player id. A request with an empty PlayerID must fail closed.
	p, store, _, _ := newCommandPlugin(grantEvaluator)
	req := cmd("channel", "create Newroom")
	req.PlayerID = ""
	resp, err := p.HandleCommand(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Empty(t, store.created)
}

func TestHandleCommandJoinDelegatesToService(t *testing.T) {
	p, store, _, res := newCommandPlugin(allowEvaluator)
	res.byName["public"] = &channelRow{ID: "ch-pub", Name: "Public", Type: "public"}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "join Public"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	require.Len(t, store.joined, 1)
	assert.Equal(t, [2]string{"ch-pub", testCharID}, store.joined[0])
}

func TestHandleCommandLeaveDelegatesToService(t *testing.T) {
	p, store, _, res := newCommandPlugin(allowEvaluator)
	res.byName["public"] = &channelRow{ID: "ch-pub", Name: "Public", Type: "public"}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "leave Public"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	require.Len(t, store.left, 1)
	assert.Equal(t, [2]string{"ch-pub", testCharID}, store.left[0])
}

func TestHandleCommandListRendersMemberships(t *testing.T) {
	p, store, _, _ := newCommandPlugin(allowEvaluator)
	store.listFor[testCharID] = []channelRow{
		{ID: "ch-pub", Name: "Public", Type: "public"},
		{ID: "ch-priv", Name: "Staff", Type: "private"},
	}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "list"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	assert.Contains(t, resp.Output, "Public")
	assert.Contains(t, resp.Output, "Staff")
}

// ── content posting: say / =name shorthand / :/; sigils ──────────────────

func TestHandleCommandSayPostsSayContent(t *testing.T) {
	p, _, sink, res := newCommandPlugin(allowEvaluator)
	res.byName["public"] = &channelRow{ID: "ch-pub", Name: "Public", Type: "public"}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "say Public hello there"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelSayType, sink.intents[0].Type)
	assert.Contains(t, sink.intents[0].Payload, "hello there")
}

func TestHandleCommandShorthandPostsSayContent(t *testing.T) {
	// The `=` prefix alias reassembles `=Public hello` → `channel Public hello`;
	// the router treats a non-reserved first token as a channel-name post.
	p, _, sink, res := newCommandPlugin(allowEvaluator)
	res.byName["public"] = &channelRow{ID: "ch-pub", Name: "Public", Type: "public"}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "Public hello"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelSayType, sink.intents[0].Type)
	assert.Contains(t, sink.intents[0].Payload, "hello")
}

func TestHandleCommandShorthandPoseMapsToChannelPoseWithSpace(t *testing.T) {
	p, _, sink, res := newCommandPlugin(allowEvaluator)
	res.byName["public"] = &channelRow{ID: "ch-pub", Name: "Public", Type: "public"}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "Public :waves"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelPoseType, sink.intents[0].Type)
	assert.False(t, poseNoSpace(t, sink.intents[0].Payload), "':' pose is a spaced pose (no_space=false)")
}

func TestHandleCommandShorthandSemiposeMapsToNoSpace(t *testing.T) {
	p, _, sink, res := newCommandPlugin(allowEvaluator)
	res.byName["public"] = &channelRow{ID: "ch-pub", Name: "Public", Type: "public"}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "Public ;grins"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelPoseType, sink.intents[0].Type)
	assert.True(t, poseNoSpace(t, sink.intents[0].Payload), "';' semipose sets no_space=true")
}

// poseNoSpace unmarshals a CommunicationContent JSON payload and reports whether
// its no_space field is set (protojson omits it when false).
func poseNoSpace(t *testing.T, payload string) bool {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &m))
	v, ok := m["no_space"].(bool)
	return ok && v
}

// ── read subcommands ─────────────────────────────────────────────────────

func TestHandleCommandWhoRendersRoster(t *testing.T) {
	p, store, _, res := newCommandPlugin(allowEvaluator)
	res.byName["public"] = &channelRow{ID: "ch-pub", Name: "Public", Type: "public"}
	store.memberMap["ch-pub|"+testCharID] = time.Now()
	store.members["ch-pub"] = []channelMemberRow{
		{CharacterID: testCharID, Role: "owner", JoinedAt: pgnanos.From(time.Now())},
	}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "who Public"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	assert.Contains(t, resp.Output, testCharID)
}

func TestHandleCommandHistoryForwardsCountToService(t *testing.T) {
	p, store, _, res := newCommandPlugin(allowEvaluator)
	res.byName["public"] = &channelRow{ID: "ch-pub", Name: "Public", Type: "public"}
	store.memberMap["ch-pub|"+testCharID] = time.Now()
	hist := &fakeHistory{}
	p.service.history = hist
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "history Public 25"))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	assert.Equal(t, 25, hist.gotLimit)
	assert.Equal(t, "ch-pub", hist.gotChannel)
}

// ── moderation: owner+admin only, non-owner denied uniformly ─────────────

func TestHandleCommandMuteOwnerPermitted(t *testing.T) {
	p, store, _, res := newCommandPlugin(allowEvaluator) // permit ⇒ owner/admin
	res.byName["staff"] = &channelRow{ID: "ch-staff", Name: "Staff", Type: "private"}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "mute Staff "+testTargetID))
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status, resp.Output)
	require.Len(t, store.setMuted, 1)
	assert.Equal(t, [2]string{"ch-staff", testTargetID}, store.setMuted[0])
}

func TestHandleCommandMuteNonOwnerDeniedUniformNotFound(t *testing.T) {
	p, store, _, res := newCommandPlugin(denyEvaluator) // deny ⇒ non-owner non-admin
	res.byName["staff"] = &channelRow{ID: "ch-staff", Name: "Staff", Type: "private"}
	resp, err := p.HandleCommand(context.Background(), cmd("channel", "mute Staff "+testTargetID))
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Empty(t, store.setMuted)
}

func TestHandleCommandBanKickInviteTransferDelegate(t *testing.T) {
	p, store, _, res := newCommandPlugin(allowEvaluator)
	res.byName["staff"] = &channelRow{ID: "ch-staff", Name: "Staff", Type: "private"}

	_, err := p.HandleCommand(context.Background(), cmd("channel", "ban Staff "+testTargetID))
	require.NoError(t, err)
	_, err = p.HandleCommand(context.Background(), cmd("channel", "kick Staff "+testTargetID))
	require.NoError(t, err)
	_, err = p.HandleCommand(context.Background(), cmd("channel", "invite Staff "+testTargetID))
	require.NoError(t, err)
	_, err = p.HandleCommand(context.Background(), cmd("channel", "transfer Staff "+testTargetID))
	require.NoError(t, err)

	require.Len(t, store.setBanned, 1)
	assert.Equal(t, [2]string{"ch-staff", testTargetID}, store.setBanned[0])
	require.Len(t, store.kicked, 1)
	assert.Equal(t, [2]string{"ch-staff", testTargetID}, store.kicked[0])
	require.Len(t, store.joined, 1) // invite = JoinChannel(target)
	assert.Equal(t, [2]string{"ch-staff", testTargetID}, store.joined[0])
	require.Len(t, store.transferred, 1)
	assert.Equal(t, [2]string{"ch-staff", testTargetID}, store.transferred[0])
}

// ── hidden vs absent channel: uniform not-found ──────────────────────────

func TestHandleCommandHiddenAndAbsentChannelBothUniformNotFound(t *testing.T) {
	// Hidden: resolver finds the row, but the RPC's read/emit gate denies.
	pHidden, _, _, resHidden := newCommandPlugin(denyEvaluator)
	resHidden.byName["secret"] = &channelRow{ID: "ch-secret", Name: "Secret", Type: "private"}
	hiddenResp, err := pHidden.HandleCommand(context.Background(), cmd("channel", "who Secret"))
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, hiddenResp.Status)

	// Absent: resolver returns CHANNEL_NOT_FOUND.
	pAbsent, _, _, _ := newCommandPlugin(denyEvaluator)
	absentResp, err := pAbsent.HandleCommand(context.Background(), cmd("channel", "who Nope"))
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, absentResp.Status)

	assert.Equal(t, hiddenResp.Output, absentResp.Output,
		"hidden and absent channels MUST present an identical not-found (T-01-12)")
}
