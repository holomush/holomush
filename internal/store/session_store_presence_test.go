// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

// TestListActiveByLocationExcludesCommsHubOnlySessions pins the grid-presence
// connection gate (holomush-5rh.8.9): a session whose only connection has
// client_type='comms_hub' MUST NOT appear in ListActiveByLocation for its
// location, and the same session MUST appear once a terminal connection is
// added. This is a distinct guarantee from INV-PRESENCE-1 (active-only); the
// connection-required gate is a registry-candidate for the Task 20 invariant
// pass (holomush-5rh.8.20) and is intentionally NOT annotated // Verifies: here.
func TestListActiveByLocationExcludesCommsHubOnlySessions(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, pool := sessiontest.NewStoreWithPool(t)
	ps := sessiontest.NewPlayerSession()
	sessiontest.SeedPlayerSession(t, pool, ps)

	si := sessiontest.NewActiveSession(ps)
	locID := si.LocationID
	require.NoError(t, st.Set(ctx, si.ID, si))

	// Add a comms_hub connection — this is the only connection on the session.
	require.NoError(t, st.AddConnection(ctx, &session.Connection{
		ID:         ulid.Make(),
		SessionID:  si.ID,
		ClientType: "comms_hub",
		Streams:    []string{},
	}))

	// Session MUST NOT appear in presence — comms_hub-only is not grid-present.
	sessions, err := st.ListActiveByLocation(ctx, locID)
	require.NoError(t, err)
	for _, s := range sessions {
		assert.NotEqual(t, si.ID, s.ID,
			"comms_hub-only session MUST NOT appear in ListActiveByLocation")
	}

	// Now add a terminal connection — session MUST appear exactly once.
	require.NoError(t, st.AddConnection(ctx, &session.Connection{
		ID:         ulid.Make(),
		SessionID:  si.ID,
		ClientType: "terminal",
		Streams:    []string{},
	}))

	sessions, err = st.ListActiveByLocation(ctx, locID)
	require.NoError(t, err)
	var found int
	for _, s := range sessions {
		if s.ID == si.ID {
			found++
		}
	}
	assert.Equal(t, 1, found,
		"session with a terminal connection MUST appear exactly once in ListActiveByLocation")
}
