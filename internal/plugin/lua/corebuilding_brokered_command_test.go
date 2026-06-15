// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"context"
	"os"
	"path/filepath"
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
	"github.com/holomush/holomush/internal/world"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// fixedAttrResolver is a pluginauthz.AttributeResolver test double returning a
// fixed dispatch-attribute bag, used to populate DispatchContext.Attributes
// (notably "location") so scoped capability calls resolve their own-location
// fence during the test.
type fixedAttrResolver struct {
	attrs map[string]any
}

func (f fixedAttrResolver) ResolveSubject(context.Context, string) (map[string]any, error) {
	return f.attrs, nil
}

// recordingWorldMutator is a hostfunc.WorldMutator that records the mutations
// the brokered world.mutation capability drives, so the test can prove the
// migrated core-building Lua actually reached the host write surface through the
// host-brokered bufconn path (holomush-eykuh.4). Queries return ErrNotFound;
// the dig command only writes.
type recordingWorldMutator struct {
	mu             sync.Mutex
	createdLocs    []*world.Location
	createdExits   []*world.Exit
	createdObjects []*world.Object
}

var _ hostfunc.WorldMutator = (*recordingWorldMutator)(nil)

func (m *recordingWorldMutator) GetLocation(context.Context, string, ulid.ULID) (*world.Location, error) {
	return nil, world.ErrNotFound
}

func (m *recordingWorldMutator) GetCharacter(context.Context, string, ulid.ULID) (*world.Character, error) {
	return nil, world.ErrNotFound
}

func (m *recordingWorldMutator) GetCharactersByLocation(context.Context, string, ulid.ULID, world.ListOptions) ([]*world.Character, error) {
	return nil, nil
}

func (m *recordingWorldMutator) GetObject(context.Context, string, ulid.ULID) (*world.Object, error) {
	return nil, world.ErrNotFound
}

func (m *recordingWorldMutator) CreateLocation(_ context.Context, _ string, loc *world.Location) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdLocs = append(m.createdLocs, loc)
	return nil
}

func (m *recordingWorldMutator) CreateExit(_ context.Context, _ string, exit *world.Exit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdExits = append(m.createdExits, exit)
	return nil
}

func (m *recordingWorldMutator) CreateObject(_ context.Context, _ string, obj *world.Object) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdObjects = append(m.createdObjects, obj)
	return nil
}

func (m *recordingWorldMutator) UpdateLocation(context.Context, string, *world.Location) error {
	return nil
}

func (m *recordingWorldMutator) UpdateObject(context.Context, string, *world.Object) error {
	return nil
}

func (m *recordingWorldMutator) FindLocationByName(context.Context, string, string) (*world.Location, error) {
	return nil, world.ErrNotFound
}

func (m *recordingWorldMutator) snapshot() ([]*world.Location, []*world.Exit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*world.Location(nil), m.createdLocs...), append([]*world.Exit(nil), m.createdExits...)
}

// TestCoreBuildingDigDrivesBrokeredWorldMutation is the behavioral end-to-end
// proof for the atomic capability cutover (holomush-eykuh.4): the REAL migrated
// core-building plugin, dispatched the `dig` command, reaches the host write
// surface through the host-brokered world.mutation capability — NOT the retired
// legacy holomush.create_location/create_exit hostfuncs (which no longer exist).
//
// This is the weak-gate mitigation the cutover task requires: the whole-system
// census suite only proves plugins LOAD, but the migrated capability calls live
// INSIDE the command handler, which load never reaches. Here we load the real
// plugin and DELIVER a command, so a broken migration (nil-index on a retired
// global, or a wrong proto field name) fails this test even though load is green.
//
// The plugin's manifest declares requires: world.query + world.mutation, so the
// manifest-fallback grant path injects both _G["world.query"] and
// _G["world.mutation"] before the handler runs. The brokered CreateLocation /
// CreateExit RPCs reach the recordingWorldMutator wired via WithWorldService.
func TestCoreBuildingDigDrivesBrokeredWorldMutation(t *testing.T) {
	root := repoRoot(t)
	pluginDir := filepath.Join(root, "plugins", "core-building")
	mainLua := filepath.Join(pluginDir, "main.lua")
	_, err := os.Stat(mainLua)
	require.NoError(t, err, "real core-building main.lua must exist")

	// from_id passed to CreateExit is ctx.location_id; the `dig` exit write is a
	// scoped (own-location) capability, so the host enforces that the brokered
	// call's resource resolves to the acting character's dispatch location. Wire a
	// dispatch attribute resolver returning that location and stamp a character
	// actor on the context so stampDispatch populates DispatchContext.Attributes.
	locationID := ulid.Make().String()
	characterID := ulid.Make().String()

	// The `dig` exit write is a scoped (own-location) capability, so the host
	// capability interceptor runs the ABAC engine to evaluate the scope fence
	// against the dispatch location. Production wires the access engine through
	// hostfunc.WithEngine (interceptor sources it via adapter.AccessEngine() →
	// Functions.Engine()); without it the scope check fails closed with
	// EVALUATE_NO_ENGINE. AllowAllEngine lets the scope evaluation proceed so the
	// test proves the brokered write reaches the recordingWorldMutator end-to-end.
	mutator := &recordingWorldMutator{}
	host := pluginlua.NewHostWithFunctions(
		hostfunc.New(
			nil,
			hostfunc.WithWorldService(mutator),
			hostfunc.WithEngine(policytest.AllowAllEngine()),
		),
		pluginlua.WithDispatchAttributeResolver(fixedAttrResolver{attrs: map[string]any{
			"location":     locationID,
			"has_location": true,
		}}),
	)
	defer closeHost(t, host)

	// Mirror the real manifest's capability declarations so the manifest-fallback
	// grant path (no WithPluginGrants) injects both world.query and world.mutation.
	manifest := &plugins.Manifest{
		Name:      "core-building",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "world.query"},
			{Kind: plugins.DependencyCapability, Name: "world.mutation"},
		},
	}
	require.NoError(t, host.Load(context.Background(), manifest, pluginDir),
		"loading the real core-building plugin must succeed")

	// A character actor on the context is what stampDispatch vouches; without it
	// DispatchContext is unpopulated and the scoped exit write would fail closed.
	ctx := core.WithActor(context.Background(),
		core.Actor{Kind: core.ActorCharacter, ID: characterID})

	resp, err := host.DeliverCommand(ctx, "core-building", pluginsdk.CommandRequest{
		Command:       "dig",
		Args:          `north to "Plaza" return south`,
		CharacterID:   characterID,
		CharacterName: "Builder",
		LocationID:    locationID,
		SessionID:     ulid.Make().String(),
		InvokedAs:     "dig",
	})
	require.NoError(t, err, "dig delivery must not error")
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status,
		"dig must report success once the brokered create RPCs succeed; output: %q", resp.Output)
	assert.Contains(t, resp.Output, "Plaza",
		"dig success output names the created location")

	locs, exits := mutator.snapshot()
	require.Len(t, locs, 1, "dig must drive exactly one brokered CreateLocation")
	assert.Equal(t, "Plaza", locs[0].Name,
		"the brokered CreateLocation request carried the parsed location name")

	require.Len(t, exits, 1, "dig must drive exactly one brokered CreateExit")
	assert.Equal(t, "north", exits[0].Name,
		"the brokered CreateExit request carried the parsed exit name")
	assert.Equal(t, locationID, exits[0].FromLocationID.String(),
		"the exit originates from the acting character's location (ctx.location_id → from_id)")
	assert.Equal(t, locs[0].ID, exits[0].ToLocationID,
		"the exit targets the freshly created location (CreateLocation response id → to_id)")
	assert.True(t, exits[0].Bidirectional,
		"the `return south` clause MUST set bidirectional=true on the brokered CreateExit request")
	assert.Equal(t, "south", exits[0].ReturnName,
		"the `return south` clause MUST set return_name on the brokered CreateExit request")
}
