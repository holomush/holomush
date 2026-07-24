// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// newCommandQuerier builds a small in-memory Querier for handler tests.
// It registers two commands (look, scene) mirroring commandquery/query_test.go.
func newCommandQuerier(t *testing.T) *commandquery.Querier {
	t.Helper()
	reg := command.NewRegistry()
	look := command.NewTestEntry(command.CommandEntryConfig{
		Name: "look", Help: "look around", Usage: "look", PluginName: "core", Source: "core",
	})
	require.NoError(t, reg.Register(look))
	scene := command.NewTestEntry(command.CommandEntryConfig{
		Name: "scene", Help: "scene control", Usage: "scene <subcommand>",
		PluginName: "core-scenes", Source: "core-scenes",
		Capabilities: []command.Capability{{Action: "write", Resource: "scene", Scope: command.ScopeLocal}},
	})
	require.NoError(t, reg.Register(scene))
	aliases := command.NewAliasCache()
	require.NoError(t, aliases.SetSystemAlias("l", "look"))
	return commandquery.New(reg, policytest.AllowAllEngine(), aliases)
}

// mkOwnedSessionWithChar returns an owned session with a non-zero CharacterID.
func mkOwnedSessionWithChar(id string, charID ulid.ULID) *session.Info {
	future := time.Now().Add(time.Hour)
	return &session.Info{
		ID:          id,
		Status:      session.StatusActive,
		ExpiresAt:   &future,
		PlayerID:    ownedPlayerID,
		CharacterID: charID,
	}
}

// TestListAvailableCommandsReturnsSessionCharacterCommandsAndAliases verifies
// that an ownership-valid request returns the character's allowed command set
// and alias map from the injected querier.
func TestListAvailableCommandsReturnsSessionCharacterCommandsAndAliases(t *testing.T) {
	char := ulid.MustParse("01HYXCHAR0000000000000000A")
	sess := mkOwnedSessionWithChar("sess-cmd-1", char)

	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-cmd-1": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
		commandQuerier:    newCommandQuerier(t),
	}
	s.buildHandlers()
	resp, err := s.ListAvailableCommands(context.Background(), &corev1.ListAvailableCommandsRequest{
		SessionId:          "sess-cmd-1",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	names := map[string]bool{}
	for _, c := range resp.Commands {
		names[c.Name] = true
	}
	assert.True(t, names["look"], "look must be in the allowed set")
	assert.True(t, names["scene"], "scene must be in the allowed set (AllowAll engine)")
	assert.Equal(t, "look", resp.Aliases["l"], "alias 'l'→'look' must appear in the alias map")
	assert.False(t, resp.Incomplete)
}

// TestListAvailableCommandsCollapsesUnknownSessionTokenToNotFound verifies
// that a bad/unknown player_session_token yields SESSION_NOT_FOUND (enumeration-safe).
func TestListAvailableCommandsCollapsesUnknownSessionTokenToNotFound(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
		commandQuerier:    newCommandQuerier(t),
	}
	s.buildHandlers()
	_, err := s.ListAvailableCommands(context.Background(), &corev1.ListAvailableCommandsRequest{
		SessionId:          "nonexistent-session",
		PlayerSessionToken: "bad-token",
	})
	require.Error(t, err)
	// Use top-level code assertion per .claude/rules/grpc-errors.md (no chain-walking).
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

// TestListAvailableCommandsFailsClosedWithoutQuerier verifies that a CoreServer
// built without WithCommandQuerier returns PERMISSION_DENIED (fail-closed).
func TestListAvailableCommandsFailsClosedWithoutQuerier(t *testing.T) {
	char := ulid.MustParse("01HYXCHAR0000000000000000B")
	sess := mkOwnedSessionWithChar("sess-cmd-2", char)

	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-cmd-2": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
		commandQuerier:    nil, // deliberately absent
	}
	s.buildHandlers()
	_, err := s.ListAvailableCommands(context.Background(), &corev1.ListAvailableCommandsRequest{
		SessionId:          "sess-cmd-2",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	// Top-level code assertion per .claude/rules/grpc-errors.md.
	o2, ok2 := oops.AsOops(err)
	require.True(t, ok2)
	assert.Equal(t, "PERMISSION_DENIED", o2.Code())
}
