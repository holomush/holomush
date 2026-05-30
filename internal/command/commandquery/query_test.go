// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package commandquery_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
)

func newRegistry(t *testing.T) *command.Registry {
	t.Helper()
	reg := command.NewRegistry()
	// NewTestEntry skips the Handler/PluginName validation NewCommandEntry enforces
	// (types.go:670) — the querier only reads Name/Help/Usage/Source/capabilities.
	// no-capability command — always visible
	look := command.NewTestEntry(command.CommandEntryConfig{
		Name: "look", Help: "look around", Usage: "look", PluginName: "core", Source: "core",
	})
	require.NoError(t, reg.Register(look))
	// capability-gated command
	scene := command.NewTestEntry(command.CommandEntryConfig{
		Name: "scene", Help: "scene control", Usage: "scene <subcommand>", PluginName: "core-scenes", Source: "core-scenes",
		Capabilities: []command.Capability{{Action: "write", Resource: "scene", Scope: command.ScopeLocal}},
	})
	require.NoError(t, reg.Register(scene))
	return reg
}

func TestQuerierAvailableReturnsAllowedCommandsForGrantedSubject(t *testing.T) {
	reg := newRegistry(t)
	aliases := command.NewAliasCache()
	require.NoError(t, aliases.SetSystemAlias("l", "look"))
	require.NoError(t, aliases.SetSystemAlias(`"`, "say")) // say not registered here → must be filtered out of alias map

	q := commandquery.New(reg, policytest.AllowAllEngine(), aliases)
	res, err := q.Available(context.Background(), access.CharacterSubject("01HCHAR0000000000000000AAA"))
	require.NoError(t, err)
	assert.False(t, res.Incomplete)

	names := map[string]bool{}
	for _, c := range res.Commands {
		names[c.Name] = true
	}
	assert.True(t, names["look"])
	assert.True(t, names["scene"])
	// alias map carries only aliases whose target is in the visible set
	assert.Equal(t, "look", res.Aliases["l"])
	_, hasSay := res.Aliases[`"`]
	assert.False(t, hasSay, `alias for unregistered "say" must be omitted`)
}

func TestQuerierAvailableOmitsDeniedCapabilityCommands(t *testing.T) {
	reg := newRegistry(t)
	q := commandquery.New(reg, policytest.DenyAllEngine(), command.NewAliasCache())
	res, err := q.Available(context.Background(), access.CharacterSubject("01HCHAR0000000000000000BBB"))
	require.NoError(t, err)
	names := map[string]bool{}
	for _, c := range res.Commands {
		names[c.Name] = true
	}
	assert.True(t, names["look"], "no-capability command always visible")
	assert.False(t, names["scene"], "capability-gated command denied")
}

func TestQuerierAvailableMarksIncompleteOnEngineErrors(t *testing.T) {
	reg := newRegistry(t)
	q := commandquery.New(reg, policytest.NewErrorEngine(assert.AnError), command.NewAliasCache())
	res, err := q.Available(context.Background(), access.CharacterSubject("01HCHAR0000000000000000CCC"))
	require.NoError(t, err)
	assert.True(t, res.Incomplete, "engine errors must set Incomplete")
	// no-capability command still present despite circuit breaker
	found := false
	for _, c := range res.Commands {
		if c.Name == "look" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestQuerierHelpReturnsDetailForGranted(t *testing.T) {
	reg := newRegistry(t)
	q := commandquery.New(reg, policytest.AllowAllEngine(), command.NewAliasCache())
	d, err := q.Help(context.Background(), access.CharacterSubject("01HCHAR0000000000000000DDD"), "scene")
	require.NoError(t, err)
	assert.Equal(t, "scene", d.Name)
	assert.Len(t, d.Capabilities, 1)
}

func TestQuerierHelpDeniesUngranted(t *testing.T) {
	reg := newRegistry(t)
	q := commandquery.New(reg, policytest.DenyAllEngine(), command.NewAliasCache())
	_, err := q.Help(context.Background(), access.CharacterSubject("01HCHAR0000000000000000EEE"), "scene")
	require.Error(t, err)
}
