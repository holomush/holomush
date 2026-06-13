// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	access "github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// theChar is the dispatch actor's character ULID used across the non-spoof
// command-registry tests. After the holomush-eykuh.3 fix the binary
// command-registry handlers derive the ABAC subject from the host-vouched actor
// recovered via LookupActor (the dispatch token in incoming metadata), NOT from
// the wire character_id, which is ignored for authorization regardless.
const theChar = "01HCHAR0000000000000000ZZZ"

// wireSpoof is a DIFFERENT character ULID supplied on the wire character_id to
// prove the handlers never trust it (the proto field is structurally unused).
const wireSpoof = "01HCHARWIRESPOOF000000ZZZ"

// commandTestServer builds a pluginHostServiceServer backed by a real *Host (so
// the binary tokenStore is wired) with the supplied command querier. Returning
// the host lets callers issue dispatch tokens against its tokenStore.
func commandTestServer(t *testing.T, q *commandquery.Querier) *pluginHostServiceServer {
	t.Helper()
	h := NewHost(WithCommandQuerier(q))
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return &pluginHostServiceServer{host: h, pluginName: "test-plugin"}
}

// dispatchTokenCtx issues a host-vouched dispatch token for the given character
// actor and returns an incoming-metadata context carrying it — the binary
// equivalent of what DeliverCommand/DeliverEvent stamp before plugin code runs
// (INV-PLUGIN-51). LookupActor recovers the actor from this token; the wire
// character_id is irrelevant.
func dispatchTokenCtx(t *testing.T, srv *pluginHostServiceServer, charID string) context.Context {
	t.Helper()
	actor := core.Actor{Kind: core.ActorCharacter, ID: charID}
	token, err := srv.host.tokenStore.Issue(srv.pluginName, actor, "")
	require.NoError(t, err, "failed to issue dispatch token")
	t.Cleanup(func() { srv.host.tokenStore.Revoke(token) })
	return metadata.NewIncomingContext(
		context.Background(),
		metadata.New(map[string]string{"x-holomush-emit-token": token}),
	)
}

func TestPluginHostServiceListCommandsFiltersByCharacter(t *testing.T) {
	reg := command.NewRegistry()
	look := command.NewTestEntry(command.CommandEntryConfig{Name: "look", PluginName: "core", Source: "core"})
	require.NoError(t, reg.Register(look))
	q := commandquery.New(reg, policytest.AllowAllEngine(), command.NewAliasCache())

	srv := commandTestServer(t, q)
	resp, err := srv.ListCommands(dispatchTokenCtx(t, srv, theChar), &hostv1.ListCommandsRequest{
		CharacterId: wireSpoof,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetCommands(), 1)
	assert.Equal(t, "look", resp.GetCommands()[0].GetName())
}

func TestPluginHostServiceListCommandsFailsClosedWithoutQuerier(t *testing.T) {
	srv := &pluginHostServiceServer{host: &Host{}, pluginName: "test-plugin"}
	_, err := srv.ListCommands(context.Background(), &hostv1.ListCommandsRequest{
		CharacterId: theChar,
	})
	require.Error(t, err)
}

func TestPluginHostServiceGetCommandHelpReturnsDetailForGranted(t *testing.T) {
	reg := command.NewRegistry()
	look := command.NewTestEntry(command.CommandEntryConfig{Name: "look", Help: "look around", Usage: "look", PluginName: "core", Source: "core"})
	require.NoError(t, reg.Register(look))
	q := commandquery.New(reg, policytest.AllowAllEngine(), command.NewAliasCache())

	srv := commandTestServer(t, q)
	resp, err := srv.GetCommandHelp(dispatchTokenCtx(t, srv, theChar), &hostv1.GetCommandHelpRequest{
		Name:        "look",
		CharacterId: wireSpoof,
	})
	require.NoError(t, err)
	assert.Equal(t, "look", resp.GetName())
	assert.Equal(t, "look around", resp.GetHelp())
}

func TestPluginHostServiceGetCommandHelpDeniesUngranted(t *testing.T) {
	reg := command.NewRegistry()
	scene := command.NewTestEntry(command.CommandEntryConfig{
		Name: "scene", PluginName: "core-scenes", Source: "core-scenes",
		Capabilities: []command.Capability{{Action: "write", Resource: "scene", Scope: command.ScopeLocal}},
	})
	require.NoError(t, reg.Register(scene))
	q := commandquery.New(reg, policytest.DenyAllEngine(), command.NewAliasCache())

	srv := commandTestServer(t, q)
	_, err := srv.GetCommandHelp(dispatchTokenCtx(t, srv, theChar), &hostv1.GetCommandHelpRequest{
		Name:        "scene",
		CharacterId: wireSpoof,
	})
	require.Error(t, err)
}

func TestPluginHostServiceGetCommandHelpFailsClosedWithoutQuerier(t *testing.T) {
	srv := &pluginHostServiceServer{host: &Host{}, pluginName: "test-plugin"}
	_, err := srv.GetCommandHelp(context.Background(), &hostv1.GetCommandHelpRequest{
		Name:        "look",
		CharacterId: theChar,
	})
	require.Error(t, err)
}

// Verifies: INV-PLUGIN-51
func TestListCommandsIgnoresWireCharacterIDUsesDispatch(t *testing.T) {
	reg := command.NewRegistry()
	require.NoError(t, reg.Register(command.NewTestEntry(command.CommandEntryConfig{Name: "look", PluginName: "core", Source: "core"})))
	dispatchChar := "01HCHARDISPATCH00000000ZZZ"
	// Grant the command ONLY for the dispatch subject; the wire id is a DIFFERENT char.
	eng := policytest.NewGrantEngine()
	eng.GrantCommandExecution(access.CharacterSubject(dispatchChar), "look")
	q := commandquery.New(reg, eng, command.NewAliasCache())
	srv := commandTestServer(t, q)

	ctx := dispatchTokenCtx(t, srv, dispatchChar)
	resp, err := srv.ListCommands(ctx, &hostv1.ListCommandsRequest{CharacterId: wireSpoof}) // different wire id
	require.NoError(t, err)
	require.Len(t, resp.GetCommands(), 1, "command list must reflect the host-vouched DISPATCH actor's grants, not the wire character_id")
	assert.Equal(t, "look", resp.GetCommands()[0].GetName())
}

// TestListCommandsFailsClosedWithoutDispatch asserts the fail-closed path when no
// host-vouched actor is recoverable: with no dispatch token in the incoming
// metadata, LookupActor fails closed (EMIT_TOKEN_MISSING) and the handler never
// reaches the querier — no command visibility leaks to an unauthenticated caller.
func TestListCommandsFailsClosedWithoutDispatch(t *testing.T) {
	q := commandquery.New(command.NewRegistry(), policytest.AllowAllEngine(), command.NewAliasCache())
	srv := commandTestServer(t, q)
	_, err := srv.ListCommands(context.Background(), &hostv1.ListCommandsRequest{CharacterId: theChar})
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")
}

// Verifies: INV-PLUGIN-51
func TestGetCommandHelpIgnoresWireCharacterIDUsesDispatch(t *testing.T) {
	reg := command.NewRegistry()
	require.NoError(t, reg.Register(command.NewTestEntry(command.CommandEntryConfig{
		Name: "scene", Help: "scene help", PluginName: "core-scenes", Source: "core-scenes",
		Capabilities: []command.Capability{{Action: "write", Resource: "scene", Scope: command.ScopeLocal}},
	})))
	dispatchChar := "01HCHARDISPATCH00000000ZZZ"
	// Grant Layer 1 + Layer 2 ONLY for the dispatch subject; the wire id is a DIFFERENT char.
	eng := policytest.NewGrantEngine()
	eng.GrantCommandExecution(access.CharacterSubject(dispatchChar), "scene")
	eng.Grant(access.CharacterSubject(dispatchChar), "write", "scene")
	q := commandquery.New(reg, eng, command.NewAliasCache())
	srv := commandTestServer(t, q)

	ctx := dispatchTokenCtx(t, srv, dispatchChar)
	resp, err := srv.GetCommandHelp(ctx, &hostv1.GetCommandHelpRequest{
		Name:        "scene",
		CharacterId: wireSpoof, // different wire id
	})
	require.NoError(t, err, "help detail must reflect the host-vouched DISPATCH actor's grants, not the wire character_id")
	assert.Equal(t, "scene", resp.GetName())
}

// TestGetCommandHelpFailsClosedWithoutDispatch asserts the fail-closed path: no
// dispatch token ⇒ LookupActor returns EMIT_TOKEN_MISSING and the handler refuses
// before any querier access.
func TestGetCommandHelpFailsClosedWithoutDispatch(t *testing.T) {
	q := commandquery.New(command.NewRegistry(), policytest.AllowAllEngine(), command.NewAliasCache())
	srv := commandTestServer(t, q)
	_, err := srv.GetCommandHelp(context.Background(), &hostv1.GetCommandHelpRequest{
		Name:        "look",
		CharacterId: theChar,
	})
	errutil.AssertErrorCode(t, err, "EMIT_TOKEN_MISSING")
}
