// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// Verify AuditCapability satisfies the Capability interface at compile time.
var _ hostfunc.Capability = (*hostfunc.AuditCapability)(nil)

func TestCapAuditNamespaceIsAudit(t *testing.T) {
	cap := hostfunc.NewAuditCapability()
	assert.Equal(t, "audit", cap.Namespace())
}

func TestCapAuditRegisterInjectsAuditGlobalTable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	auditGlobal := L.GetGlobal("audit")
	require.Equal(t, lua.LTTable, auditGlobal.Type(),
		"audit global should be a table")
}

func TestCapAuditDenyPushesHintToContextBoundSlice(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Attach a dispatch context to the LState.
	ctx := audit.NewContextForDispatch(context.Background())
	L.SetContext(ctx)

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	err := L.DoString(`audit.deny("not_member", "player not in channel members", {channel_type = "public"})`)
	require.NoError(t, err)

	events := audit.EventsFromContext(ctx)
	require.Len(t, events, 1)
	assert.Equal(t, "not_member", events[0].ID)
	assert.Equal(t, "player not in channel members", events[0].Message)
	assert.Equal(t, audit.SourcePlugin, events[0].Source)
	assert.Equal(t, "test-plugin", events[0].Component)
	assert.Equal(t, types.EffectDeny, events[0].Effect)
	assert.Equal(t, "public", events[0].Attributes["channel_type"])
}

func TestCapAuditAllowPushesHintWithAllowEffect(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ctx := audit.NewContextForDispatch(context.Background())
	L.SetContext(ctx)

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	err := L.DoString(`audit.allow("speak_ok", "message delivered")`)
	require.NoError(t, err)

	events := audit.EventsFromContext(ctx)
	require.Len(t, events, 1)
	assert.Equal(t, types.EffectAllow, events[0].Effect)
}

func TestCapAuditIsNoOpWhenNoContextAttachedToLState(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// No SetContext — luaContext() returns context.Background which has
	// no attached event slice.
	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	err := L.DoString(`audit.deny("orphan", "no context")`)
	require.NoError(t, err)
	// No assertion needed — just verify the call did not panic.
}

func TestCapAuditHandlesOptionalAttributesTable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ctx := audit.NewContextForDispatch(context.Background())
	L.SetContext(ctx)

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	// Call without the third argument.
	err := L.DoString(`audit.deny("simple", "minimal form")`)
	require.NoError(t, err)

	events := audit.EventsFromContext(ctx)
	require.Len(t, events, 1)
	assert.Empty(t, events[0].Attributes)
}

// T34a — SHOULD negative: Lua table with int/bool keys is silently
// skipped. The attribute map contract requires string keys; non-string
// keys are a plugin-author error but should not crash the capability.
func TestCapAuditSkipsNonStringKeysInAttributesTable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ctx := audit.NewContextForDispatch(context.Background())
	L.SetContext(ctx)

	cap := hostfunc.NewAuditCapability()
	cap.Register(L, "test-plugin")

	// Mix of valid (string) and invalid (int, bool) keys.
	script := `
		audit.deny("mixed_keys", "test", {
			valid_key = "included",
			[1] = "dropped",
			[true] = "dropped",
		})
	`
	err := L.DoString(script)
	require.NoError(t, err, "capability must not crash on non-string keys")

	events := audit.EventsFromContext(ctx)
	require.Len(t, events, 1)

	// Only the string-keyed entry survives.
	assert.Equal(t, "included", events[0].Attributes["valid_key"])
	assert.Len(t, events[0].Attributes, 1,
		"non-string keys should be silently dropped")
}
