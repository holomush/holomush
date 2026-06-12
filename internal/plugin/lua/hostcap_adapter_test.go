// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostcap"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// Compile-time assertion: luaHostCapAdapter satisfies hostcap.HostCapabilities.
var _ hostcap.HostCapabilities = (*luaHostCapAdapter)(nil)

// TestLuaAdapterSatisfiesHostCapabilities pins the compile-time contract (INV-PLUGIN-49).
func TestLuaAdapterSatisfiesHostCapabilities(_ *testing.T) {
	var _ hostcap.HostCapabilities = (*luaHostCapAdapter)(nil)
}

// TestLuaAdapterLookupActorUsesContextActor verifies Lua identity comes from
// core.ActorFromContext, not a dispatch token (spec §0 — no forgery surface on Lua side).
func TestLuaAdapterLookupActorUsesContextActor(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorPlugin, ID: "echo-bot"})
	actor, subj, err := a.LookupActor(ctx, "echo-bot")
	require.NoError(t, err)
	assert.Equal(t, "echo-bot", actor.ID)
	assert.Equal(t, "plugin:echo-bot", subj)
}

// TestLuaAdapterLookupActorWithNoContextActorFails verifies that absent context actor
// produces an error (fail-closed per spec §0 security model).
func TestLuaAdapterLookupActorWithNoContextActorFails(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	_, _, err := a.LookupActor(context.Background(), "echo-bot")
	assert.Error(t, err)
}

// TestLuaAdapterIssueEmitTokenReturnsUnsupportedError verifies no-token-store behavior:
// the Lua runtime has no emit-token forgery surface, so this always errors.
func TestLuaAdapterIssueEmitTokenReturnsUnsupportedError(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	_, err := a.IssueEmitToken(context.Background(), "echo-bot", core.Actor{})
	assert.Error(t, err)
}

// TestLuaAdapterAccessEngineDelegatesToFunctions spot-checks that AccessEngine delegates
// to the Functions backing. A nil engine is the unconfigured/zero-value case.
func TestLuaAdapterAccessEngineDelegatesToFunctions(t *testing.T) {
	f := hostfunc.New(nil) // no engine wired
	a := newLuaHostCapAdapter(f)
	// unconfigured Functions → nil engine
	assert.Nil(t, a.AccessEngine())
}

// TestLuaAdapterWorldQuerierReturnsNilWhenNoWorldService verifies WorldQuerier returns
// nil when the Functions has no world mutator configured.
func TestLuaAdapterWorldQuerierReturnsNilWhenNoWorldService(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	assert.Nil(t, a.WorldQuerier("echo-bot"))
}

// TestLuaAdapterIdentityRegistrySnapshotReturnsNil verifies nil is returned for the
// identity registry (Lua has no emit-token forgery surface; nil is acceptable per port).
func TestLuaAdapterIdentityRegistrySnapshotReturnsNil(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	assert.Nil(t, a.IdentityRegistrySnapshot())
}

// TestLuaAdapterOwnedResourceTypesReturnsNilMap verifies nil map returned when no
// manifest is configured (Lua plugins declare types in their manifest; adapter has none).
func TestLuaAdapterOwnedResourceTypesReturnsNilMap(t *testing.T) {
	a := newLuaHostCapAdapter(hostfunc.New(nil))
	assert.Nil(t, a.OwnedResourceTypes("echo-bot"))
}
