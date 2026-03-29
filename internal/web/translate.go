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

// genericPayload captures the common fields from any event payload.
type genericPayload struct {
	CharacterName string `json:"character_name"`
	SenderName    string `json:"sender_name"`
	TargetName    string `json:"target_name"`
	Message       string `json:"message"`
	Text          string `json:"text"`
	Action        string `json:"action"`
	Notice        string `json:"notice"`
	NoSpace       bool   `json:"no_space,omitempty"`
	Style         string `json:"style,omitempty"`
	Channel       string `json:"channel,omitempty"`
	IsPose        bool   `json:"is_pose,omitempty"`
}

// translateEvent converts an EventFrame proto into a GameEvent proto suitable
// for the web client. Uses the VerbRegistry to populate category, format,
// display_target, and label. Unknown types fall back to system/narrative/TERMINAL.
// Corrupt payloads are logged and return nil.
func (h *Handler) translateEvent(ev *corev1.EventFrame) *webv1.GameEvent {
	var ts int64
	if ev.GetTimestamp() != nil {
		ts = ev.GetTimestamp().GetSeconds()
	}

	eventType := ev.GetType()

	// Look up type in registry.
	var category, format, label string
	var displayTarget webv1.EventChannel

	if h.verbRegistry != nil {
		if reg, found := h.verbRegistry.Lookup(eventType); found {
			category = reg.Category
			format = reg.Format
			label = reg.Label
			displayTarget = reg.DisplayTarget
		}
	}

	// Fallback for unknown types.
	if category == "" {
		category = "system"
		format = "narrative"
		displayTarget = webv1.EventChannel_EVENT_CHANNEL_TERMINAL
	}

	// State events (location_state, exit_update): payload is the metadata.
	if category == "state" {
		return h.translateStateEvent(ev, eventType, category, format, displayTarget, ts)
	}

	// All other events: unmarshal into generic payload.
	var p genericPayload
	if err := json.Unmarshal(ev.GetPayload(), &p); err != nil {
		slog.Error("web: failed to unmarshal event payload",
			"type", eventType, "error", err)
		return nil
	}

	// Extract actor: prefer character_name, then sender_name.
	actor := p.CharacterName
	if actor == "" {
		actor = p.SenderName
	}

	// Extract text: prefer message, then text, then action, then notice.
	text := p.Message
	if text == "" {
		text = p.Text
	}
	if text == "" {
		text = p.Action
	}
	if text == "" {
		text = p.Notice
	}

	// Build metadata with type-specific fields.
	meta := make(map[string]any)
	if label != "" {
		meta["label"] = label
	}
	if p.NoSpace {
		meta["no_space"] = true
	}
	if p.Style != "" {
		meta["style"] = p.Style
	}
	if p.Channel != "" {
		meta["channel"] = p.Channel
	}
	if p.TargetName != "" {
		meta["target_name"] = p.TargetName
	}

	var metadata *structpb.Struct
	if len(meta) > 0 {
		s, err := structpb.NewStruct(meta)
		if err != nil {
			slog.Error("web: failed to create metadata struct",
				"type", eventType, "error", err)
		} else {
			metadata = s
		}
	}

	return &webv1.GameEvent{
		Type:          eventType,
		Category:      category,
		Format:        format,
		DisplayTarget: displayTarget,
		Timestamp:     ts,
		Actor:         actor,
		Text:          text,
		Metadata:      metadata,
	}
}

// translateStateEvent handles state-category events where the entire payload
// becomes the metadata struct.
func (h *Handler) translateStateEvent(
	ev *corev1.EventFrame,
	eventType, category, format string,
	displayTarget webv1.EventChannel,
	ts int64,
) *webv1.GameEvent {
	var m map[string]interface{}
	if err := json.Unmarshal(ev.GetPayload(), &m); err != nil {
		slog.Error("web: failed to unmarshal state event payload",
			"type", eventType, "error", err)
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		slog.Error("web: failed to create structpb from state payload",
			"type", eventType, "error", err)
		return nil
	}
	return &webv1.GameEvent{
		Type:          eventType,
		Category:      category,
		Format:        format,
		DisplayTarget: displayTarget,
		Timestamp:     ts,
		Metadata:      s,
	}
}
