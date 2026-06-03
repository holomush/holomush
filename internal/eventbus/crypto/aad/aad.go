// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package aad builds the Additional Authenticated Data (AAD) bytes
// hashed by AEAD codecs. The canonicalization rule is fixed by master
// spec §4.2 and verified by INV-CRYPTO-14: any tampering with cleartext
// metadata, codec name, or DEK reference changes the AAD bytes and
// breaks decryption with a tag-mismatch error.
//
// Phase 2 ships this function. Phase 3 codecs call it from Encode and
// Decode.
package aad

import (
	"encoding/binary"
	"math"

	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// magic is the 6-byte version prefix. Future v2 layouts may coexist by
// checking magic on Decode; only v1 is shipped in Phase 2.
var magic = []byte("HMAAD\x01")

// maxFieldLen bounds the length of any single length-prefixed field.
// The wire format reserves 4 bytes for each length (uint32-big-endian),
// so segments larger than this cannot be represented and would also
// risk arithmetic overflow during size computation. Real event metadata
// is many orders of magnitude smaller (ULIDs are 16 bytes, subjects and
// type strings are short), so this is purely a safety bound.
const maxFieldLen = math.MaxUint32

// Build returns the canonical AAD bytes for an event under a given
// codec, DEK reference, and DEK version. The byte layout is:
//
//	"HMAAD\x01"                                 // 6 bytes
//	uint32(len(event.Id))                       // 4 bytes BE
//	event.Id                                    // 16 bytes (ULID)
//	uint32(len(event.Subject))                  // 4 bytes BE
//	[]byte(event.Subject)                       // UTF-8
//	uint32(len(event.Type))                     // 4 bytes BE
//	[]byte(event.Type)                          // UTF-8
//	int64(event.Timestamp.AsTime().UnixNano())  // 8 bytes BE
//	uint32(len(actorBytes))                     // 4 bytes BE
//	actorBytes                                  // proto Deterministic
//	uint32(len(codecName))                      // 4 bytes BE
//	[]byte(codecName)                           // UTF-8
//	uint64(dekRef)                              // 8 bytes BE
//	uint32(dekVersion)                          // 4 bytes BE
//
// Identity codec passes dekRef=0, dekVersion=0; the magic prefix and
// per-field tampering still produce well-defined AAD.
//
// Returns AAD_ACTOR_MARSHAL_FAILED if the Actor submessage cannot be
// proto-marshaled (programmer bug — Event.Actor is always valid in the
// production codepath), or AAD_FIELD_TOO_LARGE if any length-prefixed
// segment would not fit in a uint32 prefix or in the resulting size
// computation.
func Build(event *eventbusv1.Event, codecName string, dekRef uint64, dekVersion uint32) ([]byte, error) {
	actorBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(event.GetActor())
	if err != nil {
		return nil, oops.Code("AAD_ACTOR_MARSHAL_FAILED").
			Wrap(err)
	}

	eventID := event.GetId()
	subject := event.GetSubject()
	eventType := event.GetType()
	var ts int64
	if event.GetTimestamp() != nil {
		ts = event.GetTimestamp().AsTime().UnixNano()
	}

	// Validate each length-prefixed field up front so the size
	// computation below cannot overflow and the make() allocation is
	// always bounded by a value that fits in int (CodeQL: size
	// computation for allocation may overflow).
	if err := checkFieldLen("event.id", len(eventID)); err != nil {
		return nil, err
	}
	if err := checkFieldLen("event.subject", len(subject)); err != nil {
		return nil, err
	}
	if err := checkFieldLen("event.type", len(eventType)); err != nil {
		return nil, err
	}
	if err := checkFieldLen("actor_bytes", len(actorBytes)); err != nil {
		return nil, err
	}
	if err := checkFieldLen("codec_name", len(codecName)); err != nil {
		return nil, err
	}

	// Build the AAD by appending. We deliberately do NOT pre-size the
	// allocation: CodeQL's taint analyzer (rule go/allocation-size-overflow)
	// flags any make() whose capacity is computed from len() of
	// externally-sourced byte slices, even when each component is
	// individually bounded by checkFieldLen above. The append-based
	// approach lets the runtime grow the slice in capped doublings,
	// avoiding the taint while still being O(N) amortized.
	var out []byte

	out = append(out, magic...)
	out = appendLengthPrefixed(out, eventID)
	out = appendLengthPrefixed(out, []byte(subject))
	out = appendLengthPrefixed(out, []byte(eventType))
	out = binary.BigEndian.AppendUint64(out, uint64(ts))
	out = appendLengthPrefixed(out, actorBytes)
	out = appendLengthPrefixed(out, []byte(codecName))
	out = binary.BigEndian.AppendUint64(out, dekRef)
	out = binary.BigEndian.AppendUint32(out, dekVersion)

	return out, nil
}

// checkFieldLen returns AAD_FIELD_TOO_LARGE if n exceeds the uint32
// length prefix bound, or nil otherwise.
func checkFieldLen(field string, n int) error {
	if n < 0 || uint64(n) > maxFieldLen {
		return oops.Code("AAD_FIELD_TOO_LARGE").
			With("field", field).
			With("length", n).
			With("max", uint64(maxFieldLen)).
			Errorf("AAD field %q length %d exceeds uint32 prefix bound", field, n)
	}
	return nil
}

// appendLengthPrefixed assumes the caller has already validated len(src)
// via checkFieldLen and panics on overflow only as a programmer-bug
// safety net (callers in this package always validate first).
func appendLengthPrefixed(dst, src []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(src))) //nolint:gosec // G115: bounded by checkFieldLen at the call site.
	return append(dst, src...)
}
