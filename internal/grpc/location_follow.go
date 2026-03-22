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

// locationFollower tracks the character's current location and handles
// location-following when move events are detected. It also manages
// dynamic eventStore subscriptions so the client receives events
// from the new location after a move.
type locationFollower struct {
	characterID   ulid.ULID
	currentLocID  ulid.ULID
	worldQuerier  WorldQuerier
	sessionStore  session.Store
	eventStore    core.EventStore
	locStreamName string
	locCancel     context.CancelFunc
	notifyCh      chan<- streamNotification
	errCh         chan<- error
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
	// so the client receives events from the destination location.
	//
	// Catch-up replay for the new stream is handled automatically: when the
	// first notification arrives on the new stream, the live loop in
	// Subscribe calls replayAndSend with lastSentID[newStream] == zero ULID,
	// which replays all events from the beginning of the stream.
	lf.switchLocationSubscription(ctx, newLocID)

	return true
}

// switchLocationSubscription subscribes to the new location stream, then
// cancels the old one. Subscribing first ensures the stream always has an
// active location feed — if the new subscribe fails, the old stays alive
// and the error terminates the stream via errCh.
//
// Catch-up replay for the new location is handled by the caller: the first
// LISTEN notification triggers replayAndSend with lastSentID[newStream]=zero,
// replaying from the beginning of the destination stream.
func (lf *locationFollower) switchLocationSubscription(ctx context.Context, newLocID ulid.ULID) {
	if lf.eventStore == nil || lf.notifyCh == nil {
		return
	}

	newStreamName := world.LocationStream(newLocID)

	// Subscribe to the new stream BEFORE cancelling the old one.
	locCtx, locCancel := context.WithCancel(ctx)
	eventCh, subErrCh, err := lf.eventStore.Subscribe(locCtx, newStreamName)
	if err != nil {
		locCancel()
		slog.WarnContext(ctx, "location-following: subscribe failed",
			"stream", newStreamName, "error", err)
		select {
		case lf.errCh <- oops.With("stream", newStreamName).Wrap(err):
		default:
		}
		return
	}

	// New subscription succeeded — tear down the old one.
	if lf.locCancel != nil {
		lf.locCancel()
	}
	lf.locCancel = locCancel
	lf.locStreamName = newStreamName

	startNotificationRelay(locCtx, newStreamName, eventCh, lf.notifyCh)

	go func() {
		select {
		case e, ok := <-subErrCh:
			if ok && e != nil {
				select {
				case lf.errCh <- e:
				default:
				}
			}
		case <-locCtx.Done():
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
