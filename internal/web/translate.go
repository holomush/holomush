// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"encoding/json"
	"log/slog"

	"google.golang.org/protobuf/types/known/structpb"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// sayPayload is the JSON payload for say events.
type sayPayload struct {
	CharacterName string `json:"character_name"`
	Message       string `json:"message"`
}

// posePayload is the JSON payload for pose events.
type posePayload struct {
	CharacterName string `json:"character_name"`
	Action        string `json:"action"`
}

// arriveLeavePayload is the JSON payload for arrive and leave events.
type arriveLeavePayload struct {
	CharacterName string `json:"character_name"`
}

// systemPayload is the JSON payload for system events.
type systemPayload struct {
	Message string `json:"message"`
}

// movePayload is the JSON payload for move events.
type movePayload struct {
	CharacterName string `json:"character_name"`
	Message       string `json:"message"`
}

// channelForType returns the EventChannel for the given event type string.
func channelForType(eventType string) webv1.EventChannel {
	switch eventType {
	case "say", "pose", "system":
		return webv1.EventChannel_EVENT_CHANNEL_TERMINAL
	case "location_state", "exit_update":
		return webv1.EventChannel_EVENT_CHANNEL_STATE
	case "arrive", "leave", "move":
		return webv1.EventChannel_EVENT_CHANNEL_BOTH
	default:
		return webv1.EventChannel_EVENT_CHANNEL_TERMINAL
	}
}

// payloadToMetadata unmarshals a JSON payload into a structpb.Struct for use
// as GameEvent metadata. Returns nil and logs on error.
func payloadToMetadata(payload []byte) *structpb.Struct {
	var m map[string]interface{}
	if err := json.Unmarshal(payload, &m); err != nil {
		slog.Error("web: failed to unmarshal payload to metadata", "error", err)
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		slog.Error("web: failed to create structpb from payload", "error", err)
		return nil
	}
	return s
}

// translateEvent converts a core Event proto into a GameEvent proto suitable
// for the web client. Unknown event types are silently dropped (returns nil).
// Corrupt payloads are logged and also return nil.
func translateEvent(ev *corev1.SubscribeResponse) *webv1.GameEvent {
	var ts int64
	if ev.GetTimestamp() != nil {
		ts = ev.GetTimestamp().GetSeconds()
	}

	ch := channelForType(ev.GetType())

	switch ev.GetType() {
	case "say":
		var p sayPayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("web: failed to unmarshal say payload", "error", err)
			return nil
		}
		return &webv1.GameEvent{
			Type:          "say",
			CharacterName: p.CharacterName,
			Text:          p.Message,
			Timestamp:     ts,
			Channel:       ch,
		}

	case "pose":
		var p posePayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("web: failed to unmarshal pose payload", "error", err)
			return nil
		}
		return &webv1.GameEvent{
			Type:          "pose",
			CharacterName: p.CharacterName,
			Text:          p.Action,
			Timestamp:     ts,
			Channel:       ch,
		}

	case "arrive":
		var p arriveLeavePayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("web: failed to unmarshal arrive payload", "error", err)
			return nil
		}
		return &webv1.GameEvent{
			Type:          "arrive",
			CharacterName: p.CharacterName,
			Text:          "has arrived.",
			Timestamp:     ts,
			Channel:       ch,
		}

	case "leave":
		var p arriveLeavePayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("web: failed to unmarshal leave payload", "error", err)
			return nil
		}
		return &webv1.GameEvent{
			Type:          "leave",
			CharacterName: p.CharacterName,
			Text:          "has left.",
			Timestamp:     ts,
			Channel:       ch,
		}

	case "system":
		var p systemPayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("web: failed to unmarshal system payload", "error", err)
			return nil
		}
		return &webv1.GameEvent{
			Type:      "system",
			Text:      p.Message,
			Timestamp: ts,
			Channel:   ch,
		}

	case "move":
		var p movePayload
		if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
			slog.Error("web: failed to unmarshal move payload", "error", err)
			return nil
		}
		return &webv1.GameEvent{
			Type:          "move",
			CharacterName: p.CharacterName,
			Text:          p.Message,
			Timestamp:     ts,
			Channel:       ch,
		}

	case "location_state", "exit_update":
		meta := payloadToMetadata(ev.GetPayload())
		if meta == nil {
			return nil
		}
		return &webv1.GameEvent{
			Type:      ev.GetType(),
			Timestamp: ts,
			Channel:   ch,
			Metadata:  meta,
		}

	default:
		return nil
	}
}
