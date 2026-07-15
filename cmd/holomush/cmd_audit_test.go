// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/store"
)

func TestRootHasAuditSubcommand(t *testing.T) {
	root := NewRootCmd()
	auditCmd, _, err := root.Find([]string{"audit"})
	require.NoError(t, err)
	require.NotNil(t, auditCmd)
	assert.Equal(t, "audit", auditCmd.Name())
}

func TestAuditDLQSubcommandsResolve(t *testing.T) {
	root := NewRootCmd()
	for _, sub := range []string{"list", "show", "replay"} {
		resolved, _, err := root.Find([]string{"audit", "dlq", sub})
		require.NoError(t, err, "audit dlq %s must resolve", sub)
		assert.Equal(t, sub, resolved.Name())
	}
}

func TestAuditDLQPrintsHelp(t *testing.T) {
	out, code := runCmd(t, []string{"audit", "dlq", "--help"})
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "dead-letter")
}

func TestReplayOptsFromFlagsRejectsAllWithMsgID(t *testing.T) {
	_, err := replayOptsFromFlags(true, "01ABC", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestReplayOptsFromFlagsRequiresASelection(t *testing.T) {
	_, err := replayOptsFromFlags(false, "", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "specify")
}

func TestReplayOptsFromFlagsMapsAll(t *testing.T) {
	opts, err := replayOptsFromFlags(true, "", 0)
	require.NoError(t, err)
	assert.Equal(t, audit.ReplayOptions{}, opts)
}

func TestReplayOptsFromFlagsMapsMsgIDAndLimit(t *testing.T) {
	opts, err := replayOptsFromFlags(false, "01ABC", 5)
	require.NoError(t, err)
	assert.Equal(t, audit.ReplayOptions{MsgID: "01ABC", Limit: 5}, opts)
}

func TestDLQConfigForGameScopesSubject(t *testing.T) {
	cfg := dlqConfigForGame("holo42")
	assert.Equal(t, "internal.holo42.audit.dlq", cfg.Subject)
}

func TestDLQConfigForGameLeavesSubjectDefaultWhenNoGameID(t *testing.T) {
	cfg := dlqConfigForGame("")
	assert.Empty(t, cfg.Subject, "empty game id must leave Subject for DLQConfig.Defaults()")
}

// stubSysInfo returns a sysInfoReader that yields the given value/error for
// any key — the injected DB seam resolveGameID reads for its persisted leg.
func stubSysInfo(value string, err error) sysInfoReader {
	return func(context.Context, string) (string, error) { return value, err }
}

// TestResolveGameIDPrefersOverride proves the --game-id override wins over
// both the configured core.game_id and the persisted DB value (leg 1).
func TestResolveGameIDPrefersOverride(t *testing.T) {
	got, err := resolveGameID(t.Context(), stubSysInfo("dbgame", nil), "override", "coregame")
	require.NoError(t, err)
	assert.Equal(t, "override", got)
}

// TestResolveGameIDPrefersCoreGameIDOverDB proves core.game_id beats the DB
// value when no override is set (leg 2) — MIRRORING the server (core.go:300-303),
// which prefers the configured core.game_id over the persisted DB value.
func TestResolveGameIDPrefersCoreGameIDOverDB(t *testing.T) {
	got, err := resolveGameID(t.Context(), stubSysInfo("dbgame", nil), "", "coregame")
	require.NoError(t, err)
	assert.Equal(t, "coregame", got, "configured core.game_id must beat the persisted DB value")
}

// TestResolveGameIDFallsBackToDB proves the persisted DB value is used only
// when both the override and core.game_id are empty (leg 3 — the
// zero-operator-burden auto-resolve of D-05).
func TestResolveGameIDFallsBackToDB(t *testing.T) {
	got, err := resolveGameID(t.Context(), stubSysInfo("dbgame", nil), "", "")
	require.NoError(t, err)
	assert.Equal(t, "dbgame", got)
}

// TestResolveGameIDReturnsEmptyWhenAllUnset proves the resolver invents
// nothing: override empty, core.game_id empty, and no persisted DB value
// (ErrSystemInfoNotFound) yields "" — the caller then defaults to the legacy
// internal.main.audit.dlq single-game shape.
func TestResolveGameIDReturnsEmptyWhenAllUnset(t *testing.T) {
	got, err := resolveGameID(t.Context(), stubSysInfo("", store.ErrSystemInfoNotFound), "", "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestResolveGameIDPropagatesDBError proves a genuine DB read failure (not the
// benign ErrSystemInfoNotFound) surfaces as an error rather than being
// swallowed into an empty game_id.
func TestResolveGameIDPropagatesDBError(t *testing.T) {
	_, err := resolveGameID(t.Context(), stubSysInfo("", errors.New("connection refused")), "", "")
	require.Error(t, err)
}

// TestConfigLoadCoreSectionSelectsCoreGameID pins the command-side loader's
// SECTION selection (round-4 LOW): a temp YAML with DIVERGENT core.game_id and
// event_bus.game_id, loaded via config.Load(..., "core") — the exact call
// runAuditDLQReplay uses — yields the core.game_id value, NOT the event_bus
// value and NOT the YAML root. This is the seam previously covered only by
// inspection.
func TestConfigLoadCoreSectionSelectsCoreGameID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `game_id: rootgame
core:
  game_id: coregame
event_bus:
  game_id: eventbusgame
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	var coreCfg coreConfig
	require.NoError(t, config.Load(path, &cobra.Command{}, &coreCfg, "core"))
	assert.Equal(t, "coregame", coreCfg.GameID,
		"config.Load(..., \"core\") must select core.game_id, not event_bus.game_id or the YAML root")
}
