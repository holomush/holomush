// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox

import (
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// SkipMarkerKind is the distinct envelope kind the SkipService publishes at a
// poison row's own feed_position so consumers see the position accounted for
// (INV-WORLD-3 gap-free order) without ever decoding a world-change payload for
// a poison row.
const SkipMarkerKind = "world_skip_marker"

// wireAffected mirrors wmodel.AffectedAggregate with JSON-stable field names and
// a string-encoded ULID (deterministic JSON — no maps, fixed field order).
type wireAffected struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	BeforeVersion int    `json:"before_version"`
	AfterVersion  int    `json:"after_version"`
	Tombstone     bool   `json:"tombstone"`
}

// wireEnvelope is the deterministic-JSON on-wire shape of a wmodel.Envelope. The
// WHOLE envelope is serialized into eventbus.Event.Payload (round-5 finding 5);
// consumers read game_id/epoch/feed_position by deserializing this, NOT from a
// header. The per-kind schema_version lives here inside the payload — distinct
// from the reserved global App-Schema-Version header the publisher stamps.
type wireEnvelope struct {
	EventID       string          `json:"event_id"`
	GameID        string          `json:"game_id"`
	Kind          string          `json:"kind"`
	SchemaVersion int             `json:"schema_version"`
	Actor         string          `json:"actor"`
	CausationID   string          `json:"causation_id"`
	CorrelationID string          `json:"correlation_id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	Epoch         int64           `json:"epoch"`
	FeedPosition  int64           `json:"feed_position"`
	Affected      []wireAffected  `json:"affected"`
	Payload       json.RawMessage `json:"payload"`
}

// MarshalEnvelope canonically serializes a wmodel.Envelope to deterministic JSON.
func MarshalEnvelope(env wmodel.Envelope) ([]byte, error) {
	affected := make([]wireAffected, 0, len(env.Affected))
	for _, a := range env.Affected {
		affected = append(affected, wireAffected{
			Type:          string(a.Type),
			ID:            a.ID.String(),
			BeforeVersion: a.BeforeVersion,
			AfterVersion:  a.AfterVersion,
			Tombstone:     a.Tombstone,
		})
	}
	payload := json.RawMessage(env.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}
	w := wireEnvelope{
		EventID:       env.EventID.String(),
		GameID:        env.GameID,
		Kind:          env.Kind,
		SchemaVersion: env.SchemaVersion,
		Actor:         env.Actor,
		CausationID:   env.CausationID,
		CorrelationID: env.CorrelationID,
		AggregateType: string(env.AggregateType),
		AggregateID:   env.AggregateID.String(),
		Epoch:         env.Epoch,
		FeedPosition:  env.FeedPosition,
		Affected:      affected,
		Payload:       payload,
	}
	b, err := json.Marshal(w)
	if err != nil {
		return nil, oops.Code("WORLD_OUTBOX_WIRE_MARSHAL_FAILED").
			With("event_id", w.EventID).Wrap(err)
	}
	return b, nil
}

// UnmarshalEnvelope decodes a wire payload back into a wmodel.Envelope. Used by
// the reference consumer and the round-trip test.
func UnmarshalEnvelope(payload []byte) (wmodel.Envelope, error) {
	var w wireEnvelope
	if err := json.Unmarshal(payload, &w); err != nil {
		return wmodel.Envelope{}, oops.Code("WORLD_OUTBOX_WIRE_UNMARSHAL_FAILED").Wrap(err)
	}
	eventID, err := ulid.Parse(w.EventID)
	if err != nil {
		return wmodel.Envelope{}, oops.Code("WORLD_OUTBOX_WIRE_BAD_EVENT_ID").
			With("event_id", w.EventID).Wrap(err)
	}
	aggID, err := ulid.Parse(w.AggregateID)
	if err != nil {
		return wmodel.Envelope{}, oops.Code("WORLD_OUTBOX_WIRE_BAD_AGGREGATE_ID").
			With("aggregate_id", w.AggregateID).Wrap(err)
	}
	affected := make([]wmodel.AffectedAggregate, 0, len(w.Affected))
	for _, a := range w.Affected {
		aid, aerr := ulid.Parse(a.ID)
		if aerr != nil {
			return wmodel.Envelope{}, oops.Code("WORLD_OUTBOX_WIRE_BAD_AFFECTED_ID").
				With("affected_id", a.ID).Wrap(aerr)
		}
		affected = append(affected, wmodel.AffectedAggregate{
			Type:          wmodel.AggregateType(a.Type),
			ID:            aid,
			BeforeVersion: a.BeforeVersion,
			AfterVersion:  a.AfterVersion,
			Tombstone:     a.Tombstone,
		})
	}
	var rawPayload []byte
	if len(w.Payload) > 0 && string(w.Payload) != "null" {
		rawPayload = []byte(w.Payload)
	}
	return wmodel.Envelope{
		EventID:       eventID,
		GameID:        w.GameID,
		Kind:          w.Kind,
		SchemaVersion: w.SchemaVersion,
		Actor:         w.Actor,
		CausationID:   w.CausationID,
		CorrelationID: w.CorrelationID,
		AggregateType: wmodel.AggregateType(w.AggregateType),
		AggregateID:   aggID,
		Payload:       rawPayload,
		Epoch:         w.Epoch,
		FeedPosition:  w.FeedPosition,
		Affected:      affected,
	}, nil
}

// EnvelopeToEvent is the CONCRETE wmodel.Envelope → eventbus.Event wire adapter
// (round-5 finding 5). The WHOLE envelope is serialized into Event.Payload;
// Event.Type = the envelope kind; the subject is built with the LIVE TWO-ARG
// eventbus.Qualify(gameID, <domain>.<id>) (round-9 R6-5 MEDIUM — Qualify prepends
// events.<gameID>. itself, so a DOMAIN-RELATIVE ref is passed, never a
// pre-qualified string); Nats-Msg-Id = the event ULID is stamped by the
// publisher from Event.ID. The reserved global App-Schema-Version header is left
// as the publisher stamps it. The skip marker uses this SAME adapter.
func EnvelopeToEvent(env wmodel.Envelope) (eventbus.Event, error) {
	payload, err := MarshalEnvelope(env)
	if err != nil {
		return eventbus.Event{}, err
	}
	ref := string(env.AggregateType) + "." + env.AggregateID.String()
	subject, err := eventbus.Qualify(env.GameID, ref)
	if err != nil {
		return eventbus.Event{}, oops.Code("WORLD_OUTBOX_QUALIFY_FAILED").
			With("game_id", env.GameID).With("ref", ref).Wrap(err)
	}
	typ, err := eventbus.NewType(env.Kind)
	if err != nil {
		return eventbus.Event{}, oops.Code("WORLD_OUTBOX_BAD_KIND").
			With("kind", env.Kind).Wrap(err)
	}
	return eventbus.Event{
		ID:        env.EventID,
		Subject:   subject,
		Type:      typ,
		Timestamp: time.Now(),
		// The relay publishes already-authorized, already-committed facts; the
		// human/plugin actor string is preserved inside the payload.
		Actor:   eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload: payload,
	}, nil
}
