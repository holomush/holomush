// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// recordingSessionAccess is a session.Access test double backing the brokered
// `session` capability (SessionService: FindByName / ListActive /
// SetLastWhispered). It returns canned sessions keyed by lowercase character
// name and records every SetLastWhispered call, so the migrated
// core-communication handlers can be proven to reach the host-brokered session
// surface end-to-end through the bufconn path (holomush-eykuh.4).
type recordingSessionAccess struct {
	mu sync.Mutex
	// byName maps lowercase character name → the session to return.
	byName map[string]*session.Info
	// active is the list ListActive returns.
	active []*session.Info
	// lastWhispered records (session_id → name) for every SetLastWhispered call.
	lastWhispered map[string]string
}

var _ session.Access = (*recordingSessionAccess)(nil)

func newRecordingSessionAccess() *recordingSessionAccess {
	return &recordingSessionAccess{
		byName:        map[string]*session.Info{},
		lastWhispered: map[string]string{},
	}
}

func (r *recordingSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*session.Info(nil), r.active...), nil
}

func (r *recordingSessionAccess) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (r *recordingSessionAccess) FindByCharacterName(_ context.Context, name string) (*session.Info, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byName[strings.ToLower(name)], nil
}

func (r *recordingSessionAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (r *recordingSessionAccess) UpdateActivity(_ context.Context, _ string) error { return nil }

func (r *recordingSessionAccess) UpdateLastPaged(_ context.Context, _, _ string) error { return nil }

func (r *recordingSessionAccess) UpdateLastWhispered(_ context.Context, sessionID, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastWhispered[sessionID] = name
	return nil
}

func (r *recordingSessionAccess) lastWhisperedFor(sessionID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.lastWhispered[sessionID]
	return v, ok
}

// recordingSessionAdmin is a hostcap.SessionAdmin test double backing the
// brokered `session.admin` capability (SessionAdminService: Broadcast /
// Disconnect). It records every broadcast message so the migrated `wall`
// handler can be proven to reach session_admin.Broadcast end-to-end.
type recordingSessionAdmin struct {
	mu         sync.Mutex
	broadcasts []string
}

func (r *recordingSessionAdmin) BroadcastSystemMessage(_ context.Context, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.broadcasts = append(r.broadcasts, message)
	return nil
}

func (r *recordingSessionAdmin) DisconnectSession(_ context.Context, _, _ string) error { return nil }

func (r *recordingSessionAdmin) snapshotBroadcasts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.broadcasts...)
}

// corecommManifest mirrors plugins/core-communication/plugin.yaml's capability
// declarations so the manifest-fallback grant path injects both _G["session"]
// and _G["session.admin"] before the command handlers run. The crypto.emits
// block mirrors the real manifest so (a) the INV-PLUGIN-32 Load capture pass
// installs register_emit_type for main.lua's top-level calls, and (b) the host
// emit fence permits the sensitivity:always page/whisper/pemit events that the
// handlers emit with sensitive=true.
func corecommManifest() *plugins.Manifest {
	return &plugins.Manifest{
		Name:      "core-communication",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "session"},
			{Kind: plugins.DependencyCapability, Name: "session.admin"},
		},
		Crypto: &plugins.CryptoSection{
			Emits: []plugins.CryptoEmit{
				{EventType: "say", Sensitivity: plugins.SensitivityNever},
				{EventType: "pose", Sensitivity: plugins.SensitivityNever},
				{EventType: "ooc", Sensitivity: plugins.SensitivityNever},
				{EventType: "emit", Sensitivity: plugins.SensitivityNever},
				{EventType: "page", Sensitivity: plugins.SensitivityAlways},
				{EventType: "whisper", Sensitivity: plugins.SensitivityAlways},
				{EventType: "pemit", Sensitivity: plugins.SensitivityAlways},
				{EventType: "whisper_notice", Sensitivity: plugins.SensitivityNever},
			},
		},
	}
}

// loadCorecomm builds a Lua host wired with the recording session backings,
// loads the REAL core-communication plugin, and returns the host plus the two
// recorders. The session capability (read/update) is backed by the
// session.Access recorder via WithSessionAccess; the session.admin capability
// (broadcast) is backed by the hostcap.SessionAdmin recorder via
// WithSessionAdmin. AllowAllEngine is wired so any (future) scope/entitlement
// evaluation proceeds rather than failing closed.
func loadCorecomm(t *testing.T) (*pluginlua.Host, *recordingSessionAccess, *recordingSessionAdmin) {
	t.Helper()

	root := repoRoot(t)
	pluginDir := filepath.Join(root, "plugins", "core-communication")
	mainLua := filepath.Join(pluginDir, "main.lua")
	_, err := os.Stat(mainLua)
	require.NoError(t, err, "real core-communication main.lua must exist")

	sessions := newRecordingSessionAccess()
	admin := &recordingSessionAdmin{}

	host := pluginlua.NewHostWithFunctions(
		hostfunc.New(
			nil,
			hostfunc.WithSessionAccess(sessions),
			hostfunc.WithEngine(policytest.AllowAllEngine()),
		),
		pluginlua.WithSessionAdmin(admin),
	)
	t.Cleanup(func() { closeHost(t, host) })

	require.NoError(t, host.Load(context.Background(), corecommManifest(), pluginDir),
		"loading the real core-communication plugin must succeed")

	return host, sessions, admin
}

// corecommCtx returns a context carrying a character actor, which stampDispatch
// vouches so host-capability calls have an identity on the server side.
func corecommCtx(characterID string) context.Context {
	return core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: characterID})
}

// TestCoreCommunicationWhisperDrivesBrokeredSession is the behavioral end-to-end
// proof (linmh.2) that the migrated `whisper` handler reaches the host-brokered
// `session` capability and emits the right events. It loads the REAL plugin and
// delivers a whisper command: the handler resolves the target via
// session_caps.FindByName (reading the nested resp.session entity), emits the
// public whisper_notice to the location plus the sensitive whisper to the
// target's character stream, and records the last-whispered target through the
// brokered SetLastWhispered RPC.
//
// A broken migration — a wrong proto field (e.g. reading the entity directly
// instead of resp.session) or a nil-index on a retired global — fails here even
// though the load-only census stays green, because the capability call lives
// inside the command handler that load never reaches.
func TestCoreCommunicationWhisperDrivesBrokeredSession(t *testing.T) {
	host, sessions, _ := loadCorecomm(t)

	senderID := ulid.Make().String()
	senderSession := ulid.Make().String()
	targetCharID := ulid.Make()
	locationID := ulid.Make()

	// Target is in the SAME location as the sender (whisper requires co-location).
	sessions.byName["bob"] = &session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   targetCharID,
		CharacterName: "Bob",
		LocationID:    locationID,
	}

	resp, err := host.DeliverCommand(corecommCtx(senderID), "core-communication", pluginsdk.CommandRequest{
		Command:       "whisper",
		Args:          "Bob=meet me outside",
		CharacterID:   senderID,
		CharacterName: "Alice",
		LocationID:    locationID.String(),
		SessionID:     senderSession,
		InvokedAs:     "whisper",
	})
	require.NoError(t, err, "whisper delivery must not error")
	require.NotNil(t, resp)
	require.Equal(t, pluginsdk.CommandOK, resp.Status,
		"whisper must succeed once the brokered session lookup resolves; output: %q", resp.Output)
	assert.Contains(t, resp.Output, "You whisper to Bob: meet me outside")

	// Two events: a public location notice and the sensitive whisper to the target.
	require.Len(t, resp.Events, 2, "whisper emits a location notice plus the sensitive target whisper")

	notice := resp.Events[0]
	assert.Equal(t, "location."+locationID.String(), notice.Stream)
	assert.Equal(t, "core-communication:whisper_notice", string(notice.Type))
	assert.False(t, notice.Sensitive, "the public notice carries no whisper content and is not sensitive")

	whisper := resp.Events[1]
	assert.Equal(t, "character."+targetCharID.String(), whisper.Stream,
		"the sensitive whisper targets the resolved character (proves resp.session.character_id was read)")
	assert.Equal(t, "core-communication:whisper", string(whisper.Type))
	assert.True(t, whisper.Sensitive, "the whisper to the target MUST be sensitive (crypto.emits: always)")
	assert.Contains(t, whisper.Payload, "meet me outside")

	// The brokered SetLastWhispered RPC recorded the target on the sender's session.
	got, ok := sessions.lastWhisperedFor(senderSession)
	require.True(t, ok, "whisper must record the last-whispered target via the brokered SetLastWhispered RPC")
	assert.Equal(t, "Bob", got)
}

// TestCoreCommunicationPageDrivesBrokeredSession proves the migrated `page`
// handler reaches the brokered `session` capability: it resolves the target via
// session_caps.FindByName (reading resp.session), emits a single sensitive page
// to the target's character stream, and records the last-paged target through
// the brokered SetLastWhispered RPC. Page does not require co-location.
func TestCoreCommunicationPageDrivesBrokeredSession(t *testing.T) {
	host, sessions, _ := loadCorecomm(t)

	senderID := ulid.Make().String()
	senderSession := ulid.Make().String()
	targetCharID := ulid.Make()

	sessions.byName["bob"] = &session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   targetCharID,
		CharacterName: "Bob",
		LocationID:    ulid.Make(), // a different location — page is cross-location
	}

	resp, err := host.DeliverCommand(corecommCtx(senderID), "core-communication", pluginsdk.CommandRequest{
		Command:       "page",
		Args:          "Bob=are you around?",
		CharacterID:   senderID,
		CharacterName: "Alice",
		LocationID:    ulid.Make().String(),
		SessionID:     senderSession,
		InvokedAs:     "page",
	})
	require.NoError(t, err, "page delivery must not error")
	require.NotNil(t, resp)
	require.Equal(t, pluginsdk.CommandOK, resp.Status,
		"page must succeed once the brokered session lookup resolves; output: %q", resp.Output)
	assert.Contains(t, resp.Output, "You paged Bob: are you around?")

	require.Len(t, resp.Events, 1, "page emits exactly one sensitive event to the target")
	page := resp.Events[0]
	assert.Equal(t, "character."+targetCharID.String(), page.Stream,
		"the page targets the resolved character (proves resp.session.character_id was read)")
	assert.Equal(t, "core-communication:page", string(page.Type))
	assert.True(t, page.Sensitive, "the page MUST be sensitive (crypto.emits: always)")
	assert.Contains(t, page.Payload, "Alice pages: are you around?")

	got, ok := sessions.lastWhisperedFor(senderSession)
	require.True(t, ok, "page must record the last-paged target via the brokered SetLastWhispered RPC")
	assert.Equal(t, "Bob", got)
}

// TestCoreCommunicationPemitDrivesBrokeredSession proves the migrated `pemit`
// handler reaches the brokered `session` capability: it resolves the target via
// session_caps.FindByName (reading resp.session) and emits a single sensitive
// pemit to the target's character stream.
func TestCoreCommunicationPemitDrivesBrokeredSession(t *testing.T) {
	host, sessions, _ := loadCorecomm(t)

	senderID := ulid.Make().String()
	targetCharID := ulid.Make()

	sessions.byName["bob"] = &session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   targetCharID,
		CharacterName: "Bob",
		LocationID:    ulid.Make(),
	}

	resp, err := host.DeliverCommand(corecommCtx(senderID), "core-communication", pluginsdk.CommandRequest{
		Command:       "pemit",
		Args:          "Bob=A chill runs down your spine.",
		CharacterID:   senderID,
		CharacterName: "Storyteller",
		LocationID:    ulid.Make().String(),
		SessionID:     ulid.Make().String(),
		InvokedAs:     "pemit",
	})
	require.NoError(t, err, "pemit delivery must not error")
	require.NotNil(t, resp)
	require.Equal(t, pluginsdk.CommandOK, resp.Status,
		"pemit must succeed once the brokered session lookup resolves; output: %q", resp.Output)
	assert.Contains(t, resp.Output, "Pemit sent to Bob.")

	require.Len(t, resp.Events, 1, "pemit emits exactly one sensitive event to the target")
	pemit := resp.Events[0]
	assert.Equal(t, "character."+targetCharID.String(), pemit.Stream,
		"the pemit targets the resolved character (proves resp.session.character_id was read)")
	assert.Equal(t, "core-communication:pemit", string(pemit.Type))
	assert.True(t, pemit.Sensitive, "the pemit MUST be sensitive (crypto.emits: always)")
	assert.Contains(t, pemit.Payload, "A chill runs down your spine.")
}

// TestCoreCommunicationWallDrivesBrokeredSessionAdmin proves the migrated `wall`
// handler reaches the brokered `session.admin` capability: it lists active
// sessions via session_caps.ListActive (reading the nested resp.sessions array)
// and broadcasts the announcement through session_admin.Broadcast. A broken
// migration — wrong global name, or reading the broadcast response shape wrong —
// fails here.
func TestCoreCommunicationWallDrivesBrokeredSessionAdmin(t *testing.T) {
	host, sessions, admin := loadCorecomm(t)

	// Two active sessions so the success output reports the count from the
	// brokered ListActive response.
	sessions.active = []*session.Info{
		{ID: ulid.Make().String(), CharacterID: ulid.Make(), CharacterName: "Bob"},
		{ID: ulid.Make().String(), CharacterID: ulid.Make(), CharacterName: "Carol"},
	}

	adminID := ulid.Make().String()
	resp, err := host.DeliverCommand(corecommCtx(adminID), "core-communication", pluginsdk.CommandRequest{
		Command:       "wall",
		Args:          "warning Server restart in 10 minutes",
		CharacterID:   adminID,
		CharacterName: "Admin",
		LocationID:    ulid.Make().String(),
		SessionID:     ulid.Make().String(),
		InvokedAs:     "wall",
	})
	require.NoError(t, err, "wall delivery must not error")
	require.NotNil(t, resp)
	require.Equal(t, pluginsdk.CommandOK, resp.Status,
		"wall must succeed once the brokered broadcast lands; output: %q", resp.Output)
	assert.Contains(t, resp.Output, "Announcement sent to 2 sessions.",
		"wall reports the count from the brokered ListActive response (proves resp.sessions was read)")

	// The brokered session_admin.Broadcast RPC received the prefixed announcement.
	broadcasts := admin.snapshotBroadcasts()
	require.Len(t, broadcasts, 1, "wall must drive exactly one brokered Broadcast")
	assert.Equal(t, "[ADMIN WARNING] Admin: Server restart in 10 minutes", broadcasts[0],
		"the brokered Broadcast carried the urgency-prefixed announcement")
}
