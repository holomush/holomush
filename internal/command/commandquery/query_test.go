// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package commandquery_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
)

// countingErrorEngine always returns an engine error and records how many times
// Evaluate is called. It lets a test observe the circuit breaker's trip: once
// maxEngineErrors errors accrue, Available must stop calling Evaluate for the
// remaining gated commands, so the recorded call count caps at MaxEngineErrors.
type countingErrorEngine struct {
	evaluateCalls int
}

func (e *countingErrorEngine) Evaluate(context.Context, types.AccessRequest) (types.Decision, error) {
	e.evaluateCalls++
	return types.Decision{}, assert.AnError
}

// CanPerformAction is unreachable in this engine's tests — Evaluate always errors
// first and short-circuits before the capability pre-flight — so it returns a
// plain deny rather than an error.
func (e *countingErrorEngine) CanPerformAction(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

// capErrorCountingEngine permits at Evaluate but errors at the capability
// pre-flight, exercising the breaker's OTHER error source (CanPerformAction
// failures, query.go canExecute) and recording how many pre-flight calls occur.
type capErrorCountingEngine struct {
	canPerformActionCalls int
}

func (e *capErrorCountingEngine) Evaluate(context.Context, types.AccessRequest) (types.Decision, error) {
	return types.NewDecision(types.EffectAllow, "allow", "test"), nil
}

func (e *capErrorCountingEngine) CanPerformAction(context.Context, string, string, string, string) (bool, error) {
	e.canPerformActionCalls++
	return false, assert.AnError
}

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

func TestQuerierAvailableTripsCircuitBreaker(t *testing.T) {
	// The breaker counts engine failures from BOTH the Evaluate stage and the
	// capability pre-flight (CanPerformAction). Each row drives one error source
	// with a counting engine; the count must cap at MaxEngineErrors because the
	// trip guard stops evaluating the remaining gated commands. Registering more
	// gated commands than the threshold (MaxEngineErrors+2) guarantees there are
	// post-trip commands the engine must NOT be asked about — without the guard
	// every gated command would be evaluated and the count would exceed it.
	tests := []struct {
		name    string
		subject string
		// engine returns the engine under test plus an accessor for its
		// per-source call counter (evaluateCalls vs canPerformActionCalls).
		engine func() (types.AccessPolicyEngine, func() int)
	}{
		{
			name:    "trips on Evaluate errors",
			subject: "01HCHAR0000000000000000FFF",
			engine: func() (types.AccessPolicyEngine, func() int) {
				e := &countingErrorEngine{}
				return e, func() int { return e.evaluateCalls }
			},
		},
		{
			name:    "trips on capability pre-flight errors",
			subject: "01HCHAR0000000000000000GGG",
			engine: func() (types.AccessPolicyEngine, func() int) {
				e := &capErrorCountingEngine{}
				return e, func() int { return e.canPerformActionCalls }
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng, count := tt.engine()
			reg := command.NewRegistry()
			for i := range commandquery.MaxEngineErrors + 2 {
				entry := command.NewTestEntry(command.CommandEntryConfig{
					Name: fmt.Sprintf("gated%d", i), Help: "gated", Usage: "gated",
					PluginName: "core", Source: "core",
					Capabilities: []command.Capability{{Action: "write", Resource: "scene", Scope: command.ScopeLocal}},
				})
				require.NoError(t, reg.Register(entry))
			}

			q := commandquery.New(reg, eng, command.NewAliasCache())
			res, err := q.Available(context.Background(), access.CharacterSubject(tt.subject))
			require.NoError(t, err)

			assert.True(t, res.Incomplete, "engine errors must set Incomplete")
			assert.Equal(t, commandquery.MaxEngineErrors, count(),
				"circuit breaker must stop calling the engine once maxEngineErrors trips it")
		})
	}
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
