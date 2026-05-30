// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestPluginHostServiceListCommandsFiltersByCharacter(t *testing.T) {
	reg := command.NewRegistry()
	look := command.NewTestEntry(command.CommandEntryConfig{Name: "look", PluginName: "core", Source: "core"})
	require.NoError(t, reg.Register(look))
	q := commandquery.New(reg, policytest.AllowAllEngine(), command.NewAliasCache())

	srv := &pluginHostServiceServer{
		host:       &Host{commandQuerier: q},
		pluginName: "test-plugin",
	}
	resp, err := srv.ListCommands(context.Background(), &pluginv1.PluginHostServiceListCommandsRequest{
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetCommands(), 1)
	assert.Equal(t, "look", resp.GetCommands()[0].GetName())
}

func TestPluginHostServiceListCommandsFailsClosedWithoutQuerier(t *testing.T) {
	srv := &pluginHostServiceServer{host: &Host{}, pluginName: "test-plugin"}
	_, err := srv.ListCommands(context.Background(), &pluginv1.PluginHostServiceListCommandsRequest{
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
	resp, err := srv.GetCommandHelp(context.Background(), &pluginv1.PluginHostServiceGetCommandHelpRequest{
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
	_, err := srv.GetCommandHelp(context.Background(), &pluginv1.PluginHostServiceGetCommandHelpRequest{
		Name:        "scene",
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	require.Error(t, err)
}

func TestPluginHostServiceGetCommandHelpFailsClosedWithoutQuerier(t *testing.T) {
	srv := &pluginHostServiceServer{host: &Host{}, pluginName: "test-plugin"}
	_, err := srv.GetCommandHelp(context.Background(), &pluginv1.PluginHostServiceGetCommandHelpRequest{
		Name:        "look",
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	require.Error(t, err)
}
