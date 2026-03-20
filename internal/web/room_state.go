// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// systemSubjectID is used as the ABAC subject for synthetic room_state
// queries that are not on behalf of a specific character.
const systemSubjectID = "system"

// buildRoomState queries the WorldQuerier for location details, exits, and
// present characters, then assembles a synthetic room_state GameEvent.
func (h *Handler) buildRoomState(ctx context.Context, locationID ulid.ULID) (*webv1.GameEvent, error) {
	loc, err := h.worldService.GetLocation(ctx, systemSubjectID, locationID)
	if err != nil {
		return nil, oops.With("location_id", locationID.String()).Wrap(err)
	}

	exits, err := h.worldService.GetExitsByLocation(ctx, systemSubjectID, locationID)
	if err != nil {
		return nil, oops.With("location_id", locationID.String()).Wrap(err)
	}

	chars, err := h.worldService.GetCharactersByLocation(ctx, systemSubjectID, locationID, world.ListOptions{})
	if err != nil {
		return nil, oops.With("location_id", locationID.String()).Wrap(err)
	}

	payload := core.RoomStatePayload{
		Location: core.RoomStateLocation{
			ID:          loc.ID.String(),
			Name:        loc.Name,
			Description: loc.Description,
		},
		Exits:   convertExits(exits),
		Present: convertCharacters(chars),
	}

	return roomStateToGameEvent(payload)
}

// convertExits maps world.Exit slices to core.RoomStateExit slices.
func convertExits(exits []*world.Exit) []core.RoomStateExit {
	result := make([]core.RoomStateExit, 0, len(exits))
	for _, e := range exits {
		result = append(result, core.RoomStateExit{
			Direction: e.Name,
			Name:      e.Name,
			Locked:    e.Locked,
		})
	}
	return result
}

// convertCharacters maps world.Character slices to core.RoomStateChar slices.
func convertCharacters(chars []*world.Character) []core.RoomStateChar {
	result := make([]core.RoomStateChar, 0, len(chars))
	for _, c := range chars {
		result = append(result, core.RoomStateChar{
			Name: c.Name,
			Idle: false,
		})
	}
	return result
}

// roomStateToGameEvent marshals a RoomStatePayload into a GameEvent with
// structpb metadata, matching the pattern used by translateEvent.
func roomStateToGameEvent(payload core.RoomStatePayload) (*webv1.GameEvent, error) {
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, oops.Errorf("marshal room_state payload: %w", err)
	}

	var m map[string]interface{}
	if err = json.Unmarshal(jsonBytes, &m); err != nil {
		return nil, oops.Errorf("unmarshal room_state to map: %w", err)
	}

	var meta *structpb.Struct
	meta, err = structpb.NewStruct(m)
	if err != nil {
		return nil, oops.Errorf("convert room_state to structpb: %w", err)
	}

	return &webv1.GameEvent{
		Type:      string(core.EventTypeRoomState),
		Timestamp: time.Now().Unix(),
		Channel:   webv1.EventChannel_EVENT_CHANNEL_STATE,
		Metadata:  meta,
	}, nil
}
