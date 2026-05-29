// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

// connDelta is one captured SendToConnection call.
type connDelta struct {
	sessionID    string
	connectionID ulid.ULID
	stream       string
	add          bool
}

// captureConnSender is a focus.ConnectionSender test double. errOn maps a
// stream name to an error the sender returns for that stream (boundary tests).
type captureConnSender struct {
	calls []connDelta
	errOn map[string]error
}

func (s *captureConnSender) SendToConnection(sessionID string, connectionID ulid.ULID, stream string, add bool) error {
	s.calls = append(s.calls, connDelta{sessionID, connectionID, stream, add})
	if s.errOn != nil {
		if err, ok := s.errOn[stream]; ok {
			return err
		}
	}
	return nil
}

func (s *captureConnSender) adds() []string {
	var out []string
	for _, c := range s.calls {
		if c.add {
			out = append(out, c.stream)
		}
	}
	return out
}

func (s *captureConnSender) removes() []string {
	var out []string
	for _, c := range s.calls {
		if !c.add {
			out = append(out, c.stream)
		}
	}
	return out
}

// TestCoordinatorDrivesPerConnectionDeltas is the table-driven matrix of
// single-connection focus transitions and the streams each must add/remove.
// Behavioural edge cases (nil sender, send-error continuation, session-not-
// found, multi-connection fan-out) keep their own focused tests below, since
// they assert delivery semantics rather than the add/remove delta itself.
func TestCoordinatorDrivesPerConnectionDeltas(t *testing.T) {
	charID := ulid.Make()
	sceneA := ulid.Make()
	sceneB := ulid.Make()
	locID := ulid.Make()
	connID := ulid.Make()
	a, b, loc := sceneA.String(), sceneB.String(), locID.String()
	fkA := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneA}

	memberships := []session.FocusMembership{
		{Kind: session.FocusKindScene, TargetID: sceneA, JoinedAt: time.Now()},
		{Kind: session.FocusKindScene, TargetID: sceneB, JoinedAt: time.Now()},
	}

	tests := []struct {
		name        string
		connFK      *session.FocusKey // connection's focus before the call (nil = grid)
		invoke      func(coord *defaultCoordinator) error
		wantAdds    []string
		wantRemoves []string
	}{
		{
			name:   "SetConnectionFocus scene→scene adds the new scene's streams and removes the old",
			connFK: &fkA,
			invoke: func(coord *defaultCoordinator) error {
				fkB := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneB}
				_, err := coord.SetConnectionFocus(context.Background(), connID, &fkB, false)
				return err
			},
			wantAdds:    []string{"events.main.scene." + b + ".ic", "events.main.scene." + b + ".ooc"},
			wantRemoves: []string{"events.main.scene." + a + ".ic", "events.main.scene." + a + ".ooc"},
		},
		{
			name:   "SetConnectionFocus scene→grid adds the location stream and removes the scene streams",
			connFK: &fkA,
			invoke: func(coord *defaultCoordinator) error {
				_, err := coord.SetConnectionFocus(context.Background(), connID, nil, true)
				return err
			},
			wantAdds:    []string{"location:" + loc},
			wantRemoves: []string{"events.main.scene." + a + ".ic", "events.main.scene." + a + ".ooc"},
		},
		{
			name:   "AutoFocusOnJoin grid→scene adds the scene streams and removes the location stream",
			connFK: nil,
			invoke: func(coord *defaultCoordinator) error {
				_, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneA)
				return err
			},
			wantAdds:    []string{"events.main.scene." + a + ".ic", "events.main.scene." + a + ".ooc"},
			wantRemoves: []string{"location:" + loc},
		},
		{
			// holomush-fqv8z review finding: a connection already focused on
			// the target scene re-enters "focused" but its stream set is
			// unchanged, so it MUST NOT receive a redundant grid→scene delta.
			name:   "AutoFocusOnJoin re-focus on the same scene drives no delta",
			connFK: &fkA,
			invoke: func(coord *defaultCoordinator) error {
				_, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneA)
				return err
			},
			wantAdds:    nil,
			wantRemoves: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions := map[string]*session.Info{
				"sess-1": {
					ID:               "sess-1",
					CharacterID:      charID,
					LocationID:       locID,
					Status:           session.StatusActive,
					FocusMemberships: memberships,
				},
			}
			coord, _ := newTestCoordinator(t, sessions)
			cs := &captureConnSender{}
			coord.connectionSender = cs
			coord.gameID = "main"

			require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
				ID: connID, SessionID: "sess-1", ClientType: "terminal", FocusKey: tt.connFK,
			}))

			require.NoError(t, tt.invoke(coord))

			for _, c := range cs.calls {
				assert.Equal(t, "sess-1", c.sessionID, "every delta targets the connection's session")
				assert.Equal(t, connID, c.connectionID, "every delta targets the one connection")
			}
			assert.ElementsMatch(t, tt.wantAdds, cs.adds(), "delta adds")
			assert.ElementsMatch(t, tt.wantRemoves, cs.removes(), "delta removes")
		})
	}
}

func TestAutoFocusOnJoinNilSenderIsNoOp(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	locID := ulid.Make()
	connID := ulid.Make()
	sessions := map[string]*session.Info{
		"sess-1": {
			ID: "sess-1", CharacterID: charID, LocationID: locID,
			Status: session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		},
	}
	coord, _ := newTestCoordinator(t, sessions) // connectionSender stays nil
	require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
		ID: connID, SessionID: "sess-1", ClientType: "terminal",
	}))
	// Must not panic; focus mutation still succeeds.
	resp, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err)
	assert.Len(t, resp.FocusedConnectionIDs, 1)
}

func TestDriveFocusDeltasContinuesPastSendError(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	locID := ulid.Make()
	connID := ulid.Make()
	sessions := map[string]*session.Info{
		"sess-1": {
			ID: "sess-1", CharacterID: charID, LocationID: locID,
			Status: session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		},
	}
	coord, _ := newTestCoordinator(t, sessions)
	scene := sceneID.String()
	// Fail the IC add; the OOC add and the location remove must still be attempted.
	cs := &captureConnSender{errOn: map[string]error{
		"events.main.scene." + scene + ".ic": errors.New("CONNECTION_NOT_REGISTERED"),
	}}
	coord.connectionSender = cs
	coord.gameID = "main"
	require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
		ID: connID, SessionID: "sess-1", ClientType: "terminal",
	}))

	resp, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err, "delivery failure MUST NOT fail the focus mutation")
	require.Len(t, resp.FocusedConnectionIDs, 1)
	// All three deltas attempted despite the IC failure.
	assert.Len(t, cs.calls, 3, "one failing send MUST NOT abort the remaining sends")
}

func TestAutoFocusOnJoinSessionNotFoundDrivesNoDeltas(t *testing.T) {
	coord, _ := newTestCoordinator(t, map[string]*session.Info{})
	cs := &captureConnSender{}
	coord.connectionSender = cs
	coord.gameID = "main"
	resp, err := coord.AutoFocusOnJoin(context.Background(), ulid.Make(), ulid.Make())
	require.NoError(t, err)
	assert.Equal(t, uint32(0), resp.TotalConnectionCount)
	assert.Empty(t, cs.calls, "no session → no deltas")
}

func TestAutoFocusOnJoinFansOutDeltasToAllConnections(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	locID := ulid.Make()
	conn1 := ulid.Make()
	conn2 := ulid.Make()

	sessions := map[string]*session.Info{
		"sess-1": {
			ID: "sess-1", CharacterID: charID, LocationID: locID,
			Status: session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		},
	}
	coord, _ := newTestCoordinator(t, sessions)
	cs := &captureConnSender{}
	coord.connectionSender = cs
	coord.gameID = "main"

	for _, id := range []ulid.ULID{conn1, conn2} {
		require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
			ID: id, SessionID: "sess-1", ClientType: "terminal",
		}))
	}

	resp, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err)
	require.Len(t, resp.FocusedConnectionIDs, 2, "both terminal connections auto-focus")

	scene := sceneID.String()
	loc := locID.String()
	// Each connection independently receives the full grid→scene delta — proves
	// the per-connection fan-out loop in driveFocusDeltas reaches every conn.
	for _, id := range []ulid.ULID{conn1, conn2} {
		var adds, removes []string
		for _, c := range cs.calls {
			if c.connectionID != id {
				continue
			}
			assert.Equal(t, "sess-1", c.sessionID)
			if c.add {
				adds = append(adds, c.stream)
			} else {
				removes = append(removes, c.stream)
			}
		}
		assert.ElementsMatch(t, []string{
			"events.main.scene." + scene + ".ic", "events.main.scene." + scene + ".ooc",
		}, adds, "each connection MUST get scene IC + OOC")
		assert.ElementsMatch(t, []string{"location:" + loc}, removes,
			"each connection MUST drop the location stream")
	}
}
