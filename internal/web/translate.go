// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"encoding/json"
	"log/slog"

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

// translateEvent converts a core Event proto into a GameEvent proto suitable
// for the web client. Unknown event types are silently dropped (returns nil).
// Corrupt payloads are logged and also return nil.
func translateEvent(ev *corev1.Event) *webv1.GameEvent {
	ts := ev.GetTimestamp().GetSeconds()

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
		}

	default:
		return nil
	}
}
