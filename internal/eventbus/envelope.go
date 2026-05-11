// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

// EventID is a typed ULID for event identity. Used as the lookup key in
// LookupByID (source.ColdTierLookup) and in ResolvedSource.
type EventID = ulid.ULID

// ColdRow holds the row fields from events_audit used to construct an Envelope
// in the INV-39 fallback path. Populated by cold_postgres.LookupByID.
type ColdRow struct {
	EventID    EventID
	Subject    string
	Type       string
	Payload    []byte      // The marshaled Event proto envelope bytes (events_audit.envelope column).
	Codec      string
	KeyID      codec.KeyID
	KeyVersion uint32
	Timestamp  time.Time
}

// Envelope is a thin carrier for the data needed by source.FallbackResolver:
// the raw event proto bytes plus the codec/DEK columns that supply AAD inputs.
// It intentionally does NOT duplicate the full eventbus.Event decoding — the
// FallbackResolver hands the Envelope to the dispatcher for full decode after
// DEK resolution succeeds.
type Envelope struct {
	eventID    EventID
	subject    string
	evType     string
	payload    []byte
	codecName  codec.Name
	keyID      codec.KeyID
	keyVersion uint32
	timestamp  time.Time
}

// NewEnvelopeFromColdRow constructs an Envelope from a cold-tier row.
// Called by cold_postgres.LookupByID to package the raw row data for the
// source.FallbackResolver.
func NewEnvelopeFromColdRow(row ColdRow) Envelope {
	return Envelope{
		eventID:    row.EventID,
		subject:    row.Subject,
		evType:     row.Type,
		payload:    row.Payload,
		codecName:  codec.Name(row.Codec),
		keyID:      row.KeyID,
		keyVersion: row.KeyVersion,
		timestamp:  row.Timestamp,
	}
}

// EventID returns the event's ULID identity.
func (e Envelope) EventID() EventID { return e.eventID }

// Subject returns the event's NATS subject.
func (e Envelope) Subject() string { return e.subject }

// Type returns the event's declared type.
func (e Envelope) Type() string { return e.evType }

// Payload returns the raw marshaled Event proto bytes (events_audit.envelope).
func (e Envelope) Payload() []byte { return e.payload }

// Codec returns the codec name for this event.
func (e Envelope) Codec() codec.Name { return e.codecName }

// KeyID returns the DEK key ID (events_audit.dek_ref). Zero for identity-codec events.
func (e Envelope) KeyID() codec.KeyID { return e.keyID }

// KeyVersion returns the DEK key version (events_audit.dek_version). Zero for identity-codec events.
func (e Envelope) KeyVersion() uint32 { return e.keyVersion }

// Timestamp returns the event's timestamp.
func (e Envelope) Timestamp() time.Time { return e.timestamp }
