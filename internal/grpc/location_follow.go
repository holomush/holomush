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

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// systemSubjectID is used as the ABAC subject for synthetic location_state
// queries during location-following.
const systemSubjectID = "system"

// locationFollower tracks the character's current location and handles
// location-following when move events are detected. It also manages
// dynamic broadcaster subscriptions so the client receives events
// from the new location after a move.
type locationFollower struct {
	characterID  ulid.ULID
	currentLocID ulid.ULID
	worldQuerier WorldQuerier
	sessionStore session.Store

	// broadcaster, locCh, and locRelay enable dynamic subscription switching.
	// When the character moves, the old location stream is unsubscribed
	// and a new one is subscribed. Events flow: locCh -> locRelay -> merged.
	broadcaster   *core.Broadcaster
	locStreamName string
	locCh         chan core.Event
	locRelay      chan core.Event
}

// handleEvent checks if the event is a character move for the tracked character.
// If so, it builds and sends a synthetic location_state event for the new location.
// Returns true if a location_state was sent (caller may skip duplicate forwarding).
func (lf *locationFollower) handleEvent(
	ctx context.Context,
	event core.Event,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
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

	if payload.ToType != world.ContainmentTypeLocation {
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
		return false
	}

	// Commit the new location only after successful send.
	lf.currentLocID = newLocID

	// Switch the live broadcaster subscription to the new location stream
	// so the client receives events from the destination room.
	lf.switchLocationSubscription(ctx, newLocID)

	return true
}

// switchLocationSubscription unsubscribes from the old location stream and
// subscribes to the new one. A new relay goroutine is started to forward
// events from the new broadcaster channel into the shared locRelay channel,
// which feeds into the merged fan-in.
func (lf *locationFollower) switchLocationSubscription(ctx context.Context, newLocID ulid.ULID) {
	if lf.broadcaster == nil || lf.locRelay == nil {
		return
	}

	newStreamName := world.LocationStream(newLocID)

	// Unsubscribe from old location stream (closes old channel, which
	// causes the old relay goroutine to exit).
	if lf.locStreamName != "" && lf.locCh != nil {
		slog.DebugContext(ctx, "location-following: unsubscribing from old location stream",
			"old_stream", lf.locStreamName,
			"new_stream", newStreamName,
		)
		lf.broadcaster.Unsubscribe(lf.locStreamName, lf.locCh)
	}

	// Subscribe to new location stream and start relay goroutine.
	lf.locStreamName = newStreamName
	lf.locCh = lf.broadcaster.Subscribe(newStreamName)
	startLocRelay(ctx, lf.locCh, lf.locRelay)

	slog.DebugContext(ctx, "location-following: subscribed to new location stream",
		"stream", newStreamName,
	)
}

// startLocRelay starts a goroutine that copies events from a broadcaster
// channel (locCh) into the relay channel that feeds the merged fan-in.
// The goroutine exits when locCh is closed (by Unsubscribe) or ctx is done.
func startLocRelay(ctx context.Context, locCh <-chan core.Event, relay chan<- core.Event) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-locCh:
				if !ok {
					return
				}
				select {
				case relay <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}

// buildLocationState queries the world service for location data and builds
// a location_state proto event.
func (lf *locationFollower) buildLocationState(ctx context.Context, locationID ulid.ULID) (*corev1.SubscribeResponse, error) {
	// Use system context for ABAC bypass — these are server-internal queries
	// not on behalf of a specific character. locationID comes from session.Info
	// (trusted server-side state), not from client input.
	sysCtx := access.WithSystemSubject(ctx)

	// Location and exits are best-effort — the location may not exist in the
	// world model yet (e.g., guest start locations that are only referenced by
	// ID). We still build the event with whatever data we have.
	var locInfo core.LocationStateInfo
	if loc, err := lf.worldQuerier.GetLocation(sysCtx, systemSubjectID, locationID); err != nil {
		slog.DebugContext(ctx, "location_state: location not found, using ID only",
			"location_id", locationID.String())
		locInfo = core.LocationStateInfo{ID: locationID.String()}
	} else {
		locInfo = core.LocationStateInfo{
			ID:          loc.ID.String(),
			Name:        loc.Name,
			Description: loc.Description,
		}
	}

	var exitList []core.LocationStateExit
	if exits, err := lf.worldQuerier.GetExitsByLocation(sysCtx, systemSubjectID, locationID); err == nil {
		exitList = convertExits(exits)
	}

	// Presence = active sessions at this location.
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
		Location: locInfo,
		Exits:    exitList,
		Present:  present,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, oops.Wrapf(err, "marshal location_state")
	}

	return &corev1.SubscribeResponse{
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

// forwardLiveEventsWithLocationFollow reads from the merged channel and sends
// events to the client stream. Location events arrive via the locRelay channel
// which is part of merged. When a move event is detected for the session's
// character, it sends a synthetic location_state and switches the room
// subscription so subsequent events come from the new location.
func (s *CoreServer) forwardLiveEventsWithLocationFollow(
	ctx context.Context,
	info *session.Info,
	merged <-chan core.Event,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
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
				// Only skip forwarding if the location_state was sent successfully.
				if lf.handleEvent(ctx, event, stream) {
					continue
				}
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
