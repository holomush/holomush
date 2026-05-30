// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"

// EventToProto converts the canonical Event into its proto wire form for
// host→plugin delivery. It is the single host-side site mapping Event fields
// onto pluginv1.Event (used by the binary-plugin host in DeliverEvent); pair
// every edit here with EventFromProto so a field added to Event is carried in
// both directions. TestEventProtoRoundTripCarriesEveryField fails if the two
// drift — the structural guard generalizing holomush-peqfu to the event
// boundary (holomush-av954).
//
// Cursor is intentionally omitted: it is the history/tail-read pagination token
// (set on the QueryStreamHistory / focus tail response in pkg/plugin/focus_client.go
// and internal/plugin/goplugin/host_service.go), never populated on host→plugin
// event delivery, which is the boundary that forks per runtime. The Lua delivery
// path (buildEventTable) likewise omits it; Lua has no tail-read equivalent.
func EventToProto(e Event) *pluginv1.Event {
	return &pluginv1.Event{
		Id:        e.ID,
		Stream:    e.Stream,
		Type:      string(e.Type),
		Timestamp: e.Timestamp,
		ActorKind: e.ActorKind.String(),
		ActorId:   e.ActorID,
		Payload:   e.Payload,
	}
}

// EventFromProto converts a proto Event back into the canonical Event (used by
// the binary-plugin SDK adapter on HandleEvent receive). See EventToProto for
// the parity contract and the Cursor-omission rationale.
func EventFromProto(p *pluginv1.Event) Event {
	return Event{
		ID:        p.GetId(),
		Stream:    p.GetStream(),
		Type:      EventType(p.GetType()),
		Timestamp: p.GetTimestamp(),
		ActorKind: protoActorKindToActorKind(p.GetActorKind()),
		ActorID:   p.GetActorId(),
		Payload:   p.GetPayload(),
	}
}

// EmitEventToProto converts a canonical EmitEvent (a plugin's return-value emit)
// into its proto wire form. It is the single SDK-side site mapping EmitEvent
// fields onto pluginv1.EmitEvent (used by the binary-plugin SDK adapter when
// returning emits from HandleEvent / HandleCommand); pair every edit here with
// EmitEventFromProto so a field added to EmitEvent is carried in both
// directions. TestEmitEventProtoRoundTripCarriesEveryField fails if the two
// drift. Sensitive MUST cross: a binary plugin's return-value sensitive emit
// that lost this field silently downgraded to plaintext where the Lua runtime
// would encrypt (holomush-av954).
func EmitEventToProto(e EmitEvent) *pluginv1.EmitEvent {
	return &pluginv1.EmitEvent{
		Stream:    e.Stream,
		Type:      string(e.Type),
		Payload:   e.Payload,
		Sensitive: e.Sensitive,
	}
}

// EmitEventFromProto converts a proto EmitEvent back into the canonical
// EmitEvent (used by the binary-plugin host when collecting return-value emits
// from a HandleEvent / HandleCommand response). See EmitEventToProto for the
// parity contract.
func EmitEventFromProto(p *pluginv1.EmitEvent) EmitEvent {
	return EmitEvent{
		Stream:    p.GetStream(),
		Type:      EventType(p.GetType()),
		Payload:   p.GetPayload(),
		Sensitive: p.GetSensitive(),
	}
}

// EmitIntentToEmitRequest converts a host-facing EmitIntent into the binary
// active-emit RPC request. It is the single send-side site mapping EmitIntent
// onto pluginv1.PluginHostServiceEmitEventRequest (used by the binary EventSink
// when a service handler calls sink.Emit); pair every edit with
// EmitIntentFromEmitRequest so a field added to EmitIntent crosses the active-
// emit boundary in both directions. TestEmitIntentEmitRequestRoundTripCarriesEveryField
// fails if the two drift — Sensitive MUST cross or a binary service handler's
// sensitive emit silently downgrades at the fence (holomush-av954).
//
// TODO(F5): proto request field Stream renames to Subject; keep Stream on the
// wire until the proto regeneration task runs.
func EmitIntentToEmitRequest(intent EmitIntent) *pluginv1.PluginHostServiceEmitEventRequest {
	return &pluginv1.PluginHostServiceEmitEventRequest{
		Stream:    intent.Subject,
		EventType: string(intent.Type),
		Payload:   []byte(intent.Payload),
		Sensitive: intent.Sensitive,
	}
}

// EmitIntentFromEmitRequest converts the binary active-emit RPC request back
// into the host-facing EmitIntent (used by the plugin host service on receive).
// See EmitIntentToEmitRequest for the parity contract.
func EmitIntentFromEmitRequest(req *pluginv1.PluginHostServiceEmitEventRequest) EmitIntent {
	return EmitIntent{
		Subject:   req.GetStream(),
		Type:      EventType(req.GetEventType()),
		Payload:   string(req.GetPayload()),
		Sensitive: req.GetSensitive(),
	}
}
