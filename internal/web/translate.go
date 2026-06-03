// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"encoding/json"
	"log/slog"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/holomush/holomush/internal/gatewaymetrics"
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
	Reason        string `json:"reason"`
	NoSpace       bool   `json:"no_space,omitempty"`
	Style         string `json:"style,omitempty"`
	Channel       string `json:"channel,omitempty"`
	IsPose        bool   `json:"is_pose,omitempty"`
}

// translateEvent converts an EventFrame proto into a GameEvent proto suitable
// for the web client. Reads rendering metadata from EventFrame.Rendering
// (populated by core's RenderingPublisher at emit time).
//
// INV-EVENTBUS-6: events arriving without rendering metadata are dropped at the
// gateway and counted via gatewaymetrics.DroppedNilRenderingTotal. A
// non-zero counter indicates the core process's RenderingPublisher failed
// to stamp rendering before publish, or a publisher path bypassed it.
// Corrupt payloads are logged and return nil.
func (h *Handler) translateEvent(ev *corev1.EventFrame) *webv1.GameEvent {
	var ts int64
	if ev.GetTimestamp() != nil {
		ts = ev.GetTimestamp().GetSeconds()
	}

	eventType := ev.GetType()

	rendering := ev.GetRendering()
	if rendering == nil {
		slog.Error(
			"web: dropping event with nil Rendering (INV-EVENTBUS-6)",
			"event_id", ev.GetId(),
			"event_type", eventType,
			"stream", ev.GetStream(),
		)
		gatewaymetrics.DroppedNilRenderingTotal.WithLabelValues(gatewaymetrics.SurfaceWeb, eventType).Inc()
		return nil
	}

	category := rendering.GetCategory()
	format := rendering.GetFormat()
	label := rendering.GetLabel()
	displayTarget := webv1.EventChannel(rendering.GetDisplayTarget())

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

	// Arrive/leave events need formatted text (e.g., "X has arrived.") since
	// their payloads only carry character_name. Mirrors the telnet gateway's
	// formatMovement in gateway_handler.go. Move events already have text
	// (e.g., "Eve goes north.") from the generic extraction above.
	if category == "movement" && (eventType == "arrive" || eventType == "leave") {
		text = formatMovementText(eventType, actor, &p)
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
		ActorId:       ev.GetActorId(),
		Text:          text,
		Metadata:      metadata,
		EventId:       ev.GetId(),
	}
}

// formatMovementText synthesizes human-readable text for movement events.
// Mirrors the telnet gateway's formatMovement (gateway_handler.go).
func formatMovementText(eventType, actor string, p *genericPayload) string {
	if actor == "" {
		return ""
	}
	switch eventType {
	case "arrive":
		return actor + " has arrived."
	case "leave":
		if reason := p.Reason; reason != "" {
			return actor + " has left (" + reason + ")."
		}
		return actor + " has left."
	default:
		return ""
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
		ActorId:       ev.GetActorId(),
		Metadata:      s,
		EventId:       ev.GetId(),
	}
}
