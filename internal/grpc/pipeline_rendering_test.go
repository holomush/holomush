// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/core/coretest"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	corecomm "github.com/holomush/holomush/plugins/core-communication"
)

// TestPipelineRendering verifies the full command -> event -> replay pipeline
// that drives category-based rendering on the web client. Each subtest sends
// a command via HandleCommand, then replays the events from the character
// stream and verifies the stored event carries correct type and payload fields.
//
// This is the server-side E2E test for category-based event rendering:
// the web client's category renderers consume these event types and payloads.
func TestPipelineRendering(t *testing.T) {
	t.Run("say_produces_communication_event", func(t *testing.T) {
		charID := core.NewULID()
		sessionID := core.NewULID()
		locationID := core.NewULID()

		store := coretest.NewMemoryEventStore()
		server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:   charID,
				CharacterName: "Alice",
				LocationID:    locationID,
				Status:        session.StatusActive,
			},
		}))

		ctx := context.Background()
		resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
			Meta:               &corev1.RequestMeta{RequestId: "say-pipeline", Timestamp: timestamppb.Now()},
			SessionId:          sessionID.String(),
			Command:            "say Hello world",
			PlayerSessionToken: testPlayerSessionToken,
		})
		require.NoError(t, err)
		assert.True(t, resp.Success, "say command should succeed: %s", resp.Error)

		// Replay from the location stream where say events are emitted.
		events, err := store.Replay(ctx, "location."+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)

		// Find the say event.
		var sayEvent *core.Event
		for i := range events {
			if events[i].Type == eventvocab.EventType(corecomm.EventTypeSay) {
				sayEvent = &events[i]
				break
			}
		}
		require.NotNil(t, sayEvent, "expected a say event in the location stream")

		// Verify payload fields that drive CommunicationRenderer.
		var payload struct {
			CharacterName string `json:"character_name"`
			Message       string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(sayEvent.Payload, &payload))
		assert.Equal(t, "Alice", payload.CharacterName)
		assert.Equal(t, "Hello world", payload.Message)
	})

	t.Run("pose_produces_communication_event", func(t *testing.T) {
		charID := core.NewULID()
		sessionID := core.NewULID()
		locationID := core.NewULID()

		store := coretest.NewMemoryEventStore()
		server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:   charID,
				CharacterName: "Bob",
				LocationID:    locationID,
				Status:        session.StatusActive,
			},
		}))

		ctx := context.Background()
		resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
			Meta:               &corev1.RequestMeta{RequestId: "pose-pipeline", Timestamp: timestamppb.Now()},
			SessionId:          sessionID.String(),
			Command:            "pose waves cheerfully.",
			PlayerSessionToken: testPlayerSessionToken,
		})
		require.NoError(t, err)
		assert.True(t, resp.Success, "pose command should succeed: %s", resp.Error)

		events, err := store.Replay(ctx, "location."+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)

		var poseEvent *core.Event
		for i := range events {
			if events[i].Type == eventvocab.EventType(corecomm.EventTypePose) {
				poseEvent = &events[i]
				break
			}
		}
		require.NotNil(t, poseEvent, "expected a pose event in the location stream")

		var payload struct {
			CharacterName string `json:"character_name"`
			Action        string `json:"action"`
		}
		require.NoError(t, json.Unmarshal(poseEvent.Payload, &payload))
		assert.Equal(t, "Bob", payload.CharacterName)
		assert.Equal(t, "waves cheerfully.", payload.Action)
	})

	t.Run("unknown_command_produces_error_event", func(t *testing.T) {
		charID := core.NewULID()
		sessionID := core.NewULID()
		locationID := core.NewULID()

		store := coretest.NewMemoryEventStore()
		server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:   charID,
				CharacterName: "Carol",
				LocationID:    locationID,
				Status:        session.StatusActive,
			},
		}))

		ctx := context.Background()
		resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
			Meta:               &corev1.RequestMeta{RequestId: "unknown-pipeline", Timestamp: timestamppb.Now()},
			SessionId:          sessionID.String(),
			Command:            "xyzzy abracadabra",
			PlayerSessionToken: testPlayerSessionToken,
		})
		require.NoError(t, err)
		// Unknown commands succeed at the RPC level; error delivered via event.
		assert.True(t, resp.Success, "unknown command succeeds at RPC level")

		// Error events go to the character's personal stream.
		charEvents, err := store.Replay(ctx, "character."+charID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)
		require.NotEmpty(t, charEvents, "expected command_error event on character stream")

		var found bool
		for _, ev := range charEvents {
			if ev.Type == eventvocab.EventTypeCommandError {
				var payload eventvocab.CommandResponsePayload
				require.NoError(t, json.Unmarshal(ev.Payload, &payload))
				assert.NotEmpty(t, payload.Text,
					"error text should not be empty")
				found = true
				break
			}
		}
		assert.True(t, found, "expected a command_error event")
	})

	t.Run("say_event_replay_produces_correct_frame", func(t *testing.T) {
		// Verifies that the stored event can be replayed and converted to an
		// EventFrame with the correct fields for the web translation layer.
		// The translation itself is tested in internal/web/translate_pipeline_test.go.
		charID := core.NewULID()
		sessionID := core.NewULID()
		locationID := core.NewULID()

		store := coretest.NewMemoryEventStore()
		server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:   charID,
				CharacterName: "Zara",
				LocationID:    locationID,
				Status:        session.StatusActive,
			},
		}))

		ctx := context.Background()
		resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
			Meta:               &corev1.RequestMeta{RequestId: "e2e-pipeline", Timestamp: timestamppb.Now()},
			SessionId:          sessionID.String(),
			Command:            "say Testing the full pipeline",
			PlayerSessionToken: testPlayerSessionToken,
		})
		require.NoError(t, err)
		assert.True(t, resp.Success, "say command should succeed: %s", resp.Error)

		// Replay the stored event (mirrors what Subscribe does).
		events, err := store.Replay(ctx, "location."+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)
		require.NotEmpty(t, events, "expected events in location stream")

		// Verify the event carries the fields needed for EventFrame construction.
		ev := events[0]
		assert.Equal(t, eventvocab.EventType(corecomm.EventTypeSay), ev.Type)
		assert.Equal(t, "location."+locationID.String(), ev.Stream)
		assert.False(t, ev.ID.IsZero(), "event ID must be set")
		assert.False(t, ev.Timestamp.IsZero(), "timestamp must be set")

		// Verify the payload matches what translateEvent expects.
		var payload struct {
			CharacterName string `json:"character_name"`
			Message       string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(ev.Payload, &payload))
		assert.Equal(t, "Zara", payload.CharacterName)
		assert.Equal(t, "Testing the full pipeline", payload.Message)
	})

	t.Run("ooc_produces_communication_event", func(t *testing.T) {
		charID := core.NewULID()
		sessionID := core.NewULID()
		locationID := core.NewULID()

		store := coretest.NewMemoryEventStore()
		server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:   charID,
				CharacterName: "Eve",
				LocationID:    locationID,
				Status:        session.StatusActive,
			},
		}))

		ctx := context.Background()
		resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
			Meta:               &corev1.RequestMeta{RequestId: "ooc-pipeline", Timestamp: timestamppb.Now()},
			SessionId:          sessionID.String(),
			Command:            "ooc heading out for lunch",
			PlayerSessionToken: testPlayerSessionToken,
		})
		require.NoError(t, err)
		assert.True(t, resp.Success, "ooc command should succeed: %s", resp.Error)

		events, err := store.Replay(ctx, "location."+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)

		var oocEvent *core.Event
		for i := range events {
			if events[i].Type == eventvocab.EventType(corecomm.EventTypeOOC) {
				oocEvent = &events[i]
				break
			}
		}
		require.NotNil(t, oocEvent, "expected an ooc event in the location stream")

		var payload eventvocab.OOCPayload
		require.NoError(t, json.Unmarshal(oocEvent.Payload, &payload))
		assert.Equal(t, "Eve", payload.CharacterName)
		assert.Equal(t, "heading out for lunch", payload.Message)
		assert.Equal(t, "say", payload.Style)
	})
}
