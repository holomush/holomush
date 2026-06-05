// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/history"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// newViolationEmitter constructs a ViolationEmitter that publishes
// `events.<game>.system.plugin_integrity_violation` events on every
// PluginDowngradeFence INV-CRYPTO-42 refusal. The events.> prefix is required
// by INV-CRYPTO-113 (Phase 5 sub-epic E §3.6) — only the EVENTS JetStream
// SubjectFilter feeds events_audit. The emitter MUST NOT block —
// the fence already enforces a 100ms ceiling around EmitViolation, but
// this implementation also serializes the payload into a tiny Event so
// it never allocates beyond the violation message itself.
//
// Takes the RAW EventBus publisher and the verb registry separately,
// then wraps internally with a fresh RenderingPublisher. Encapsulating
// the wrap here makes it structurally impossible for callers to pass a
// pre-wrapped publisher chain — which would otherwise fail with
// EMIT_RESERVED_HEADER inside RenderingPublisher.Publish (the inner RP
// sees App-Rendering already stamped by the outer one). Pass nil
// rawPub for the degraded "no audit publisher configured" deployment —
// EmitViolation becomes a no-op, the fence still refuses the row.
//
// registry is required when rawPub is non-nil; passing nil registry
// with non-nil rawPub panics, mirroring eventbus.NewRenderingPublisher's
// own nil-registry contract.
func newViolationEmitter(rawPub eventbus.Publisher, registry *core.VerbRegistry, gameID string) history.ViolationEmitter {
	if rawPub == nil {
		return &violationEmitter{publisher: nil, gameID: gameID}
	}
	return &violationEmitter{
		publisher: eventbus.NewRenderingPublisher(rawPub, registry),
		gameID:    gameID,
	}
}

type violationEmitter struct {
	publisher eventbus.Publisher
	gameID    string
}

func (e *violationEmitter) EmitViolation(
	ctx context.Context,
	pluginName string,
	row *pluginauditpb.AuditRow,
	expectedSensitivity string,
	refusalCode string,
) error {
	if e.publisher == nil {
		// Degraded deployment — the fence still refuses the row; we just
		// can't emit the audit signal. Return nil so the fence does not
		// log an emit-error on every refusal.
		return nil
	}
	// Subject prefix MUST be `events.<game>.` per INV-CRYPTO-113 (Phase 5 sub-epic E
	// §3.6 supersession of master spec §4.6 line 830): the EVENTS JetStream
	// SubjectFilter at internal/eventbus/subsystem.go:24,27 is the only path
	// by which audit projection writes to events_audit, so audit-bearing
	// events MUST live under that filter. The `audit.<game>.` prefix is
	// forbidden — it bypasses the filter and silently drops the event.
	subjectStr := fmt.Sprintf("events.%s.system.plugin_integrity_violation", e.gameID)
	subj, err := eventbus.NewSubject(subjectStr)
	if err != nil {
		return oops.Code("PLUGIN_INTEGRITY_VIOLATION_INVALID_SUBJECT").
			With("subject", subjectStr).
			Wrap(err)
	}
	evType, err := eventbus.NewType("system:plugin_integrity_violation")
	if err != nil {
		return oops.Code("PLUGIN_INTEGRITY_VIOLATION_INVALID_TYPE").Wrap(err)
	}
	rowID := ulid.ULID{}
	if len(row.GetId()) == 16 {
		copy(rowID[:], row.GetId())
	}
	payload, err := json.Marshal(map[string]string{
		"plugin_name":          pluginName,
		"event_id":             rowID.String(),
		"event_type":           row.GetType(),
		"claimed_codec":        row.GetCodec(),
		"expected_sensitivity": expectedSensitivity,
		"refusal_code":         refusalCode,
	})
	if err != nil {
		return oops.Code("PLUGIN_INTEGRITY_VIOLATION_PAYLOAD_MARSHAL").Wrap(err)
	}
	ev := eventbus.NewEvent(subj, evType, eventbus.Actor{Kind: eventbus.ActorKindSystem}, payload)
	if perr := e.publisher.Publish(ctx, ev); perr != nil {
		return oops.Code("PLUGIN_INTEGRITY_VIOLATION_EMIT_FAILED").
			With("plugin_name", pluginName).
			With("subject", subjectStr).
			Wrap(perr)
	}
	return nil
}
