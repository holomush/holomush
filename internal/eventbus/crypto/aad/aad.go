// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package aad builds the Additional Authenticated Data (AAD) bytes
// hashed by AEAD codecs. The canonicalization rule is fixed by master
// spec §4.2 and verified by INV-25: any tampering with cleartext
// metadata, codec name, or DEK reference changes the AAD bytes and
// breaks decryption with a tag-mismatch error.
//
// Phase 2 ships this function. Phase 3 codecs call it from Encode and
// Decode.
package aad

import (
	"encoding/binary"
	"math"

	"google.golang.org/protobuf/proto"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// magic is the 6-byte version prefix. Future v2 layouts may coexist by
// checking magic on Decode; only v1 is shipped in Phase 2.
var magic = []byte("HMAAD\x01")

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
func Build(event *eventbusv1.Event, codecName string, dekRef uint64, dekVersion uint32) []byte {
	actorBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(event.GetActor())
	if err != nil {
		// Build is called from inside the codec; an Actor marshal error
		// here would be a programmer bug (the proto is always valid).
		// Returning empty AAD would silently accept tampering, so we
		// panic to fail loudly. Phase 3 wraps this in a recover at the
		// codec boundary if needed.
		panic("aad: Actor proto marshal failed: " + err.Error())
	}

	eventID := event.GetId()
	subject := event.GetSubject()
	eventType := event.GetType()
	var ts int64
	if event.GetTimestamp() != nil {
		ts = event.GetTimestamp().AsTime().UnixNano()
	}

	size := len(magic) +
		4 + len(eventID) +
		4 + len(subject) +
		4 + len(eventType) +
		8 +
		4 + len(actorBytes) +
		4 + len(codecName) +
		8 +
		4
	out := make([]byte, 0, size)

	out = append(out, magic...)
	out = appendLengthPrefixed(out, eventID)
	out = appendLengthPrefixed(out, []byte(subject))
	out = appendLengthPrefixed(out, []byte(eventType))
	out = binary.BigEndian.AppendUint64(out, uint64(ts))
	out = appendLengthPrefixed(out, actorBytes)
	out = appendLengthPrefixed(out, []byte(codecName))
	out = binary.BigEndian.AppendUint64(out, dekRef)
	out = binary.BigEndian.AppendUint32(out, dekVersion)

	return out
}

func appendLengthPrefixed(dst, src []byte) []byte {
	n := len(src)
	if n < 0 || n > math.MaxUint32 {
		// AAD inputs are bounded by event metadata sizes (event IDs are
		// 16-byte ULIDs, subjects/types are short strings). Hitting this
		// branch indicates a programmer bug feeding multi-GiB inputs.
		panic("aad: length-prefixed segment exceeds uint32")
	}
	dst = binary.BigEndian.AppendUint32(dst, uint32(n))
	return append(dst, src...)
}
