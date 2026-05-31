// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

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

// TestRecomputeSessionLiveness exercises the connection-count → session-liveness
// mapping owned by recomputeSessionLiveness: zero connections detach (with the
// session's TTL window, or the 1800 s default when TTLSeconds=0) and clear
// grid_present; a live terminal connection keeps/flips the session active and
// grid-present (including reactivating a previously-detached row, the I2 case).
// Verifies: I-LIVE-3
// Verifies: I-LIVE-5
func TestRecomputeSessionLiveness(t *testing.T) {
	const sessionID = "sess"

	tests := []struct {
		name          string
		ttlSeconds    int
		startDetached bool // seed the row as StatusDetached (reactivation case)
		addConn       bool // seed a live terminal connection
		wantStatus    session.Status
		wantGrid      bool
		wantExpiryTTL time.Duration // >0 → assert ExpiresAt ∈ [before+ttl, before+ttl+5s]
	}{
		{name: "zero connections detach with explicit TTL", ttlSeconds: 300, wantStatus: session.StatusDetached, wantGrid: false, wantExpiryTTL: 300 * time.Second},
		{name: "zero connections detach with default TTL when TTLSeconds=0", ttlSeconds: 0, wantStatus: session.StatusDetached, wantGrid: false, wantExpiryTTL: 1800 * time.Second},
		{name: "live terminal connection keeps active and grid-present", ttlSeconds: 0, addConn: true, wantStatus: session.StatusActive, wantGrid: true},
		{name: "detached row with live connection reactivates (I2)", ttlSeconds: 1800, startDetached: true, addConn: true, wantStatus: session.StatusActive, wantGrid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := sessiontest.NewStore(t)

			var sess *session.Info
			if tt.startDetached {
				future := time.Now().Add(30 * time.Minute)
				sess = &session.Info{
					ID: sessionID, Status: session.StatusDetached, ExpiresAt: &future,
					CharacterID: ulid.Make(), LocationID: ulid.Make(), PlayerID: ownedPlayerID,
					GridPresent: false, TTLSeconds: tt.ttlSeconds,
				}
			} else {
				sess = mkActiveAt(sessionID, ulid.Make(), ulid.Make())
				sess.TTLSeconds = tt.ttlSeconds
			}
			require.NoError(t, store.Set(ctx, sessionID, sess))

			if tt.addConn {
				require.NoError(t, store.AddConnection(ctx, &session.Connection{
					ID: ulid.Make(), SessionID: sessionID, ClientType: "terminal",
				}))
			}

			s := &CoreServer{sessionStore: store}
			before := time.Now()
			require.NoError(t, s.recomputeSessionLiveness(ctx, sessionID))

			got, err := store.Get(ctx, sessionID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, got.Status)
			assert.Equal(t, tt.wantGrid, got.GridPresent)

			if tt.wantExpiryTTL > 0 {
				require.NotNil(t, got.ExpiresAt, "detached session must have a non-nil ExpiresAt (reattach TTL)")
				wantMin := before.Add(tt.wantExpiryTTL)
				wantMax := before.Add(tt.wantExpiryTTL + 5*time.Second) // 5 s slack for slow CI
				assert.True(t, !got.ExpiresAt.Before(wantMin) && !got.ExpiresAt.After(wantMax),
					"ExpiresAt %v should be within [%v, %v] (TTL=%v ± 5 s slack)",
					got.ExpiresAt, wantMin, wantMax, tt.wantExpiryTTL)
			}
		})
	}
}
