// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
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

func TestAutoFocusOnJoinDrivesPerConnectionDeltas(t *testing.T) {
	charID := ulid.Make()
	sceneID := ulid.Make()
	locID := ulid.Make()
	termConnID := ulid.Make()

	sessions := map[string]*session.Info{
		"sess-1": {
			ID:          "sess-1",
			CharacterID: charID,
			LocationID:  locID,
			Status:      session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		},
	}
	coord, _ := newTestCoordinator(t, sessions)
	cs := &captureConnSender{}
	coord.connectionSender = cs
	coord.gameID = "main"

	require.NoError(t, coord.sessionStore.AddConnection(context.Background(), &session.Connection{
		ID: termConnID, SessionID: "sess-1", ClientType: "terminal",
	}))

	resp, err := coord.AutoFocusOnJoin(context.Background(), charID, sceneID)
	require.NoError(t, err)
	require.Len(t, resp.FocusedConnectionIDs, 1)

	scene := sceneID.String()
	loc := locID.String()
	for _, c := range cs.calls {
		assert.Equal(t, "sess-1", c.sessionID)
		assert.Equal(t, termConnID, c.connectionID)
	}
	assert.ElementsMatch(t, []string{
		"events.main.scene." + scene + ".ic",
		"events.main.scene." + scene + ".ooc",
	}, cs.adds(), "grid→scene MUST add scene IC + OOC")
	assert.ElementsMatch(t, []string{"location:" + loc}, cs.removes(), "grid→scene MUST remove the location stream")
}
