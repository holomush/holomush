// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	access "github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/pkg/errutil"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// theChar is the dispatch subject's character used across the non-spoof
// command-registry tests; the wire character_id matches it (it is ignored for
// authorization regardless — see the spoof tests).
const theChar = "01HCHAR0000000000000000ZZZ"

// dispatchCtx stamps a host-vouched dispatch subject for theChar onto a fresh
// ctx, mirroring what DeliverCommand/DeliverEvent do before plugin code runs
// (INV-PLUGIN-51).
func dispatchCtx() context.Context {
	return pluginauthz.WithDispatch(context.Background(), pluginauthz.DispatchContext{
		Subject: access.CharacterSubject(theChar),
	})
}

func TestPluginHostServiceListCommandsFiltersByCharacter(t *testing.T) {
	reg := command.NewRegistry()
	look := command.NewTestEntry(command.CommandEntryConfig{Name: "look", PluginName: "core", Source: "core"})
	require.NoError(t, reg.Register(look))
	q := commandquery.New(reg, policytest.AllowAllEngine(), command.NewAliasCache())

	srv := &pluginHostServiceServer{
		host:       &Host{commandQuerier: q},
		pluginName: "test-plugin",
	}
	resp, err := srv.ListCommands(dispatchCtx(), &hostv1.ListCommandsRequest{
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetCommands(), 1)
	assert.Equal(t, "look", resp.GetCommands()[0].GetName())
}

func TestPluginHostServiceListCommandsFailsClosedWithoutQuerier(t *testing.T) {
	srv := &pluginHostServiceServer{host: &Host{}, pluginName: "test-plugin"}
	_, err := srv.ListCommands(dispatchCtx(), &hostv1.ListCommandsRequest{
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	require.Error(t, err)
}

func TestPluginHostServiceGetCommandHelpReturnsDetailForGranted(t *testing.T) {
	reg := command.NewRegistry()
	look := command.NewTestEntry(command.CommandEntryConfig{Name: "look", Help: "look around", Usage: "look", PluginName: "core", Source: "core"})
	require.NoError(t, reg.Register(look))
	q := commandquery.New(reg, policytest.AllowAllEngine(), command.NewAliasCache())

	srv := &pluginHostServiceServer{host: &Host{commandQuerier: q}, pluginName: "test-plugin"}
	resp, err := srv.GetCommandHelp(dispatchCtx(), &hostv1.GetCommandHelpRequest{
		Name:        "look",
		CharacterId: "01HCHAR0000000000000000ZZZ",
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

	srv := &pluginHostServiceServer{host: &Host{commandQuerier: q}, pluginName: "test-plugin"}
	_, err := srv.GetCommandHelp(dispatchCtx(), &hostv1.GetCommandHelpRequest{
		Name:        "scene",
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	require.Error(t, err)
}

func TestPluginHostServiceGetCommandHelpFailsClosedWithoutQuerier(t *testing.T) {
	srv := &pluginHostServiceServer{host: &Host{}, pluginName: "test-plugin"}
	_, err := srv.GetCommandHelp(dispatchCtx(), &hostv1.GetCommandHelpRequest{
		Name:        "look",
		CharacterId: "01HCHAR0000000000000000ZZZ",
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
	srv := &pluginHostServiceServer{host: &Host{commandQuerier: q}, pluginName: "test-plugin"}

	ctx := pluginauthz.WithDispatch(context.Background(), pluginauthz.DispatchContext{Subject: access.CharacterSubject(dispatchChar)})
	resp, err := srv.ListCommands(ctx, &hostv1.ListCommandsRequest{CharacterId: "01HCHARWIRESPOOF000000ZZZ"}) // different wire id
	require.NoError(t, err)
	require.Len(t, resp.GetCommands(), 1, "command list must reflect the DISPATCH subject's grants, not the wire character_id")
	assert.Equal(t, "look", resp.GetCommands()[0].GetName())
}

func TestListCommandsFailsClosedWithoutDispatch(t *testing.T) {
	q := commandquery.New(command.NewRegistry(), policytest.AllowAllEngine(), command.NewAliasCache())
	srv := &pluginHostServiceServer{host: &Host{commandQuerier: q}, pluginName: "test-plugin"}
	_, err := srv.ListCommands(context.Background(), &hostv1.ListCommandsRequest{CharacterId: "01HCHAR0000000000000000ZZZ"})
	errutil.AssertErrorCode(t, err, "NO_DISPATCH_SUBJECT")
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
	srv := &pluginHostServiceServer{host: &Host{commandQuerier: q}, pluginName: "test-plugin"}

	ctx := pluginauthz.WithDispatch(context.Background(), pluginauthz.DispatchContext{Subject: access.CharacterSubject(dispatchChar)})
	resp, err := srv.GetCommandHelp(ctx, &hostv1.GetCommandHelpRequest{
		Name:        "scene",
		CharacterId: "01HCHARWIRESPOOF000000ZZZ", // different wire id
	})
	require.NoError(t, err, "help detail must reflect the DISPATCH subject's grants, not the wire character_id")
	assert.Equal(t, "scene", resp.GetName())
}

func TestGetCommandHelpFailsClosedWithoutDispatch(t *testing.T) {
	q := commandquery.New(command.NewRegistry(), policytest.AllowAllEngine(), command.NewAliasCache())
	srv := &pluginHostServiceServer{host: &Host{commandQuerier: q}, pluginName: "test-plugin"}
	_, err := srv.GetCommandHelp(context.Background(), &hostv1.GetCommandHelpRequest{
		Name:        "look",
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	errutil.AssertErrorCode(t, err, "NO_DISPATCH_SUBJECT")
}
