// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"log/slog"
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

// locationFilterUpdater is the narrow callback locationFollower invokes to
// add/remove location streams from the bus session's filter set. It replaces
// the pre-F3 core.Subscription.AddStream/RemoveStream wiring.
type locationFilterUpdater func(ctx context.Context, addStream, removeStream string) error

// locationFollower tracks the character's current location and handles
// location-following when move events are detected. On move it invokes
// the supplied updater so the bus session's subject filters switch
// atomically (via SessionStream.SetFilters).
type locationFollower struct {
	characterID   ulid.ULID
	currentLocID  ulid.ULID
	worldQuerier  WorldQuerier
	sessionStore  session.Store
	locStreamName string
	updateFilters locationFilterUpdater
}

// handleEvent checks if the event is a character move for the tracked character.
// If so, it builds and sends a synthetic location_state event for the new location.
// Returns true if a location_state was sent (caller may skip duplicate forwarding).
func (lf *locationFollower) handleEvent(
	ctx context.Context,
	event core.Event,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
) bool {
	return lf.handleMovePayload(ctx, event.Type, event.Payload, stream)
}

// handleMovePayload is the bus-friendly entry point: it takes the typed
// event kind and raw payload bytes instead of a full core.Event literal,
// sidestepping the I-16 ruleguard (which forbids core.Event{} outside
// core.NewEvent). Semantically identical to handleEvent.
func (lf *locationFollower) handleMovePayload(
	ctx context.Context,
	eventType core.EventType,
	payloadBytes []byte,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
) bool {
	if lf.worldQuerier == nil {
		return false
	}
	if eventType != core.EventTypeMove {
		return false
	}

	var payload world.MovePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
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
	// so the client receives events from the destination location. The
	// updater is backed by the bus session's SetFilters — JS preserves
	// the consumer cursor across the filter swap, so catch-up delivery
	// for the new stream happens automatically on the next Next().
	lf.switchLocationSubscription(ctx, newLocID)

	return true
}

// switchLocationSubscription invokes the filter updater to add the new
// location stream and remove the old one in one atomic filter swap. On
// failure we log and leave the existing filter set alone — the client
// keeps receiving the old location's events until the next move attempt.
func (lf *locationFollower) switchLocationSubscription(ctx context.Context, newLocID ulid.ULID) {
	if lf.updateFilters == nil {
		return
	}

	newStreamName := world.LocationStream(newLocID)
	oldStreamName := ""
	if lf.locStreamName != "" && lf.locStreamName != newStreamName {
		oldStreamName = lf.locStreamName
	}

	if err := lf.updateFilters(ctx, newStreamName, oldStreamName); err != nil {
		slog.WarnContext(ctx, "location-following: filter update failed",
			"new_stream", newStreamName, "old_stream", oldStreamName, "error", err)
		return
	}
	lf.locStreamName = newStreamName
}

// sendSynthetic sends the initial synthetic location_state for the current
// location. Best-effort: errors are logged and swallowed.
func (lf *locationFollower) sendSynthetic(
	ctx context.Context,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
) error {
	if lf.worldQuerier == nil || lf.currentLocID.IsZero() {
		return nil
	}
	locState, err := lf.buildLocationState(ctx, lf.currentLocID)
	if err != nil {
		return nil //nolint:nilerr // best-effort: synthetic location_state failure is non-fatal
	}
	return oops.Wrap(stream.Send(locState))
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
		Frame: &corev1.SubscribeResponse_Event{
			Event: &corev1.EventFrame{
				Id:        core.NewULID().String(),
				Stream:    world.LocationStream(locationID),
				Type:      string(core.EventTypeLocationState),
				Timestamp: timestamppb.New(time.Now()),
				ActorType: core.ActorSystem.String(),
				ActorId:   "system",
				Payload:   payloadJSON,
			},
		},
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
