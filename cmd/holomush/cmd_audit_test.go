// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit"
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
