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
// in the INV-CRYPTO-22 fallback path. Populated by cold_postgres.LookupByID.
type ColdRow struct {
	EventID    EventID
	Subject    string
	Type       string
	Payload    []byte // The marshaled Event proto envelope bytes (events_audit.envelope column).
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
//
// Callers MUST NOT construct Envelope literals directly; use one of the
// constructors below (NewEnvelopeFromColdRow for cold-tier rows,
// NewEnvelopeFromFields for direct construction, NewEnvelopeForTest in tests).
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
		payload:    copyBytes(row.Payload),
		codecName:  codec.Name(row.Codec),
		keyID:      row.KeyID,
		keyVersion: row.KeyVersion,
		timestamp:  row.Timestamp,
	}
}

// EnvelopeFields is the constructor argument for Envelope when the caller has
// the fields in hand directly (not from a cold-tier row). All zero values are
// valid (identity codec, no DEK, empty payload, no subject/type/timestamp);
// hot-tier callers that need full envelope metadata SHOULD populate every
// field rather than relying on zero defaults — INV-CRYPTO-14 AAD construction and
// the history dispatch path both read Subject and Type.
type EnvelopeFields struct {
	EventID    EventID
	Subject    string
	Type       string
	Timestamp  time.Time
	Codec      codec.Name
	KeyID      codec.KeyID
	KeyVersion uint32
	Payload    []byte
}

// NewEnvelopeFromFields constructs an Envelope from caller-supplied fields.
// Used by adapters that have parsed payload + DEK columns directly rather
// than via a cold-tier row.
func NewEnvelopeFromFields(f EnvelopeFields) Envelope {
	return Envelope{
		eventID:    f.EventID,
		subject:    f.Subject,
		evType:     f.Type,
		timestamp:  f.Timestamp,
		codecName:  f.Codec,
		keyID:      f.KeyID,
		keyVersion: f.KeyVersion,
		payload:    copyBytes(f.Payload),
	}
}

// copyBytes returns a defensive copy of b so Envelope cannot be mutated by
// the caller via the original slice. nil and zero-length inputs return nil
// to avoid unnecessary allocations on identity-codec envelopes.
func copyBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// NewEnvelopeForTest constructs an Envelope for use in unit tests.
// Identical to NewEnvelopeFromFields; named separately so test-only
// callsites are easy to grep.
func NewEnvelopeForTest(f EnvelopeFields) Envelope {
	return NewEnvelopeFromFields(f)
}

// EventID returns the event's ULID identity.
func (e Envelope) EventID() EventID { return e.eventID }

// Subject returns the event's NATS subject.
func (e Envelope) Subject() string { return e.subject }

// Type returns the event's declared type.
func (e Envelope) Type() string { return e.evType }

// Payload returns the raw marshaled Event proto bytes (events_audit.envelope).
// Returns a defensive copy so callers cannot mutate the envelope's backing
// storage. For identity-codec envelopes with empty payloads, returns nil
// without allocating.
func (e Envelope) Payload() []byte { return copyBytes(e.payload) }

// Codec returns the codec name for this event.
func (e Envelope) Codec() codec.Name { return e.codecName }

// KeyID returns the DEK key ID (events_audit.dek_ref). Zero for identity-codec events.
func (e Envelope) KeyID() codec.KeyID { return e.keyID }

// KeyVersion returns the DEK key version (events_audit.dek_version). Zero for identity-codec events.
func (e Envelope) KeyVersion() uint32 { return e.keyVersion }

// Timestamp returns the event's timestamp.
func (e Envelope) Timestamp() time.Time { return e.timestamp }
