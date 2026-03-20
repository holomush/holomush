// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// systemSubjectID is used as the ABAC subject for synthetic location_state
// queries during location-following.
const systemSubjectID = "system"

// locationFollower tracks the character's current location and handles
// location-following when move events are detected.
type locationFollower struct {
	characterID  ulid.ULID
	currentLocID ulid.ULID
	worldQuerier WorldQuerier
	sessionStore session.Store
	broadcaster  *core.Broadcaster
}

// handleEvent checks if the event is a character move for the tracked character.
// If so, it builds and sends a synthetic location_state event for the new location.
// Returns true if a location_state was sent (caller may skip duplicate forwarding).
func (lf *locationFollower) handleEvent(
	ctx context.Context,
	event core.Event,
	stream grpc.ServerStreamingServer[corev1.Event],
) bool {
	if lf.worldQuerier == nil {
		return false
	}
	if event.Type != core.EventTypeMove {
		return false
	}

	var payload world.MovePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false
	}

	if payload.EntityType != world.EntityTypeCharacter || payload.EntityID != lf.characterID {
		return false
	}

	newLocID := payload.ToID
	if newLocID == lf.currentLocID {
		return false
	}

	slog.DebugContext(ctx, "location-following: character moved",
		"character_id", lf.characterID.String(),
		"from_location", lf.currentLocID.String(),
		"to_location", newLocID.String(),
	)

	lf.currentLocID = newLocID

	// Build and send synthetic location_state for the new location.
	locState, err := lf.buildLocationState(ctx, newLocID)
	if err != nil {
		slog.WarnContext(ctx, "location-following: failed to build location_state",
			"location_id", newLocID.String(),
			"error", err,
		)
		return false
	}

	if err := stream.Send(locState); err != nil {
		slog.WarnContext(ctx, "location-following: failed to send location_state",
			"location_id", newLocID.String(),
			"error", err,
		)
	}

	return true
}

// buildLocationState queries the world service for location data and builds
// a location_state proto event.
func (lf *locationFollower) buildLocationState(ctx context.Context, locationID ulid.ULID) (*corev1.Event, error) {
	loc, err := lf.worldQuerier.GetLocation(ctx, systemSubjectID, locationID)
	if err != nil {
		return nil, oops.With("location_id", locationID.String()).Wrap(err)
	}

	exits, err := lf.worldQuerier.GetExitsByLocation(ctx, systemSubjectID, locationID)
	if err != nil {
		return nil, oops.With("location_id", locationID.String()).Wrap(err)
	}

	// Presence = active sessions at this location, not character repo entries.
	// Guest characters exist only in sessions, not the character repository.
	var present []core.LocationStateChar
	if lf.sessionStore != nil {
		sessions, sessErr := lf.sessionStore.ListActiveByLocation(ctx, locationID)
		if sessErr != nil {
			slog.WarnContext(ctx, "location_state: failed to list sessions at location",
				"location_id", locationID.String(), "error", sessErr)
		} else {
			present = make([]core.LocationStateChar, 0, len(sessions))
			for _, sess := range sessions {
				present = append(present, core.LocationStateChar{
					Name: sess.CharacterName,
					Idle: false,
				})
			}
		}
	}

	payload := core.LocationStatePayload{
		Location: core.LocationStateInfo{
			ID:          loc.ID.String(),
			Name:        loc.Name,
			Description: loc.Description,
		},
		Exits:   convertExits(exits),
		Present: present,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, oops.Errorf("marshal location_state: %w", err)
	}

	return &corev1.Event{
		Id:        core.NewULID().String(),
		Stream:    world.LocationStream(locationID),
		Type:      string(core.EventTypeLocationState),
		Timestamp: timestamppb.New(time.Now()),
		ActorType: core.ActorSystem.String(),
		ActorId:   "system",
		Payload:   payloadJSON,
	}, nil
}

// convertExits maps world.Exit slices to core.LocationStateExit slices.
func convertExits(exits []*world.Exit) []core.LocationStateExit {
	result := make([]core.LocationStateExit, 0, len(exits))
	for _, e := range exits {
		result = append(result, core.LocationStateExit{
			Direction: e.Name,
			Name:      e.Name,
			Locked:    e.Locked,
		})
	}
	return result
}

// convertCharacters maps world.Character slices to core.LocationStateChar slices.
func convertCharacters(chars []*world.Character) []core.LocationStateChar {
	result := make([]core.LocationStateChar, 0, len(chars))
	for _, c := range chars {
		result = append(result, core.LocationStateChar{
			Name: c.Name,
			Idle: false,
		})
	}
	return result
}

// forwardLiveEventsWithLocationFollow reads from merged and sends events to the
// client stream. When a move event is detected for the session's character,
// it sends a synthetic location_state for the new location.
func (s *CoreServer) forwardLiveEventsWithLocationFollow(
	ctx context.Context,
	info *session.Info,
	merged <-chan core.Event,
	stream grpc.ServerStreamingServer[corev1.Event],
	requestID string,
	sessionID string,
	lf *locationFollower,
) error {
	for {
		select {
		case <-ctx.Done():
			slog.DebugContext(ctx, "subscription ended",
				"request_id", requestID,
				"session_id", sessionID,
				"reason", ctx.Err(),
			)
			if ctx.Err() == context.Canceled {
				return nil // normal client disconnect
			}
			return oops.Code("SUBSCRIPTION_CANCELLED").With("session_id", sessionID).Wrap(ctx.Err())

		case event, ok := <-merged:
			if !ok {
				return nil
			}

			// Skip move events from the character stream that duplicate the
			// location stream event. The move event is emitted to both the
			// destination location stream and the character stream. Only
			// forward the one from the location stream to avoid duplicates.
			if event.Type == core.EventTypeMove && strings.HasPrefix(event.Stream, world.StreamPrefixCharacter) {
				// Handle location-following on the character stream copy.
				lf.handleEvent(ctx, event, stream)
				// Don't forward this copy to the client.
				continue
			}

			if err := stream.Send(eventToProto(event)); err != nil {
				slog.WarnContext(ctx, "failed to send event",
					"request_id", requestID,
					"session_id", sessionID,
					"event_id", event.ID.String(),
					"error", err,
				)
				return oops.Code("SEND_FAILED").With("session_id", sessionID).With("event_id", event.ID.String()).Wrap(err)
			}

			s.sessions.UpdateCursor(info.CharacterID, event.Stream, event.ID)
			s.persistCursorAsync(sessionID, event.Stream, event.ID)
		}
	}
}
