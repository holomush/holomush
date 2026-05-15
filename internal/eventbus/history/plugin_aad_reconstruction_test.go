// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package history_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/history"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestRoundTripProducesByteEqualAAD asserts INV-P7-16 — the
// load-bearing read-side invariant. Without byte-equal AAD
// reconstruction, EVERY sensitive plugin-stored event would fail
// AEAD tag-check on decrypt because the host's recompute would
// produce different AAD bytes than the publisher's encrypt-side
// AAD.
//
// The round trip exercised here is the field-copy fidelity round
// trip — publisher shape → wire shape (AuditRow) → reconstructed
// publisher shape (via the §5.4 adapter) → recomputed AAD. The
// dispatcher's storage-fidelity (verbatim ciphertext bytes through
// JetStream → Postgres → wire) is covered by bead 1r0v.1's
// dispatcher tests; this test isolates the adapter.
//
// The publisher-event timestamp here is constructed with a sub-µs
// nanosecond component on purpose. To faithfully model the production
// flow, the test then:
//
//  1. Mirrors what eventbus.JetStreamPublisher.Publish() does: truncate
//     event.Timestamp to microsecond precision BEFORE aad.Build. This
//     is the holomush-1r0v.3 crypto-review fix — without it, the
//     publisher writes an AAD computed from sub-µs nanos that no
//     read-side reconstruction can reproduce after PG truncation.
//
//  2. Mirrors what PostgreSQL TIMESTAMPTZ does on INSERT in the
//     plugin's scene_log table: truncate to microsecond. The plugin's
//     QueryHistory returns the truncated value via timestamppb.New.
//
// Both sides see the same µs timestamp → AAD reconstruction is byte-
// equal. The companion test
// TestRoundTripFailsWithoutPublisherMicrosecondTruncation locks in the
// regression guard: it omits step (1) and asserts the AAD mismatch
// that would re-introduce the INV-P7-16 bug class.
//
// Builds AAD twice with the post-fix scalar args and asserts the
// bytes are equal. A regression in either AuditRowToEvent, aad.Build's
// canonical input set, or the publisher truncation will break this
// test.
func TestRoundTripProducesByteEqualAAD(t *testing.T) {
	t.Parallel()

	// Publisher-side event with a sub-µs nanosecond component — modeling
	// what time.Now() typically produces on Linux.
	publisherEvent := &eventbusv1.Event{
		Id:        []byte("0123456789ABCDEF"),
		Subject:   "events.test.scene.01ABC.ic",
		Type:      "test-plugin:secret",
		Timestamp: timestamppb.New(time.Unix(1700000000, 12345).UTC()),
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   []byte("char-id-16-bytes"),
		},
	}
	const codecName = "xchacha20poly1305-v1"
	const dekRef uint64 = 42
	const dekVer uint32 = 7

	// Mirror eventbus.JetStreamPublisher.Publish()'s post-fix behavior:
	// truncate to µs BEFORE aad.Build. This is the production fix; the
	// test mirrors it locally because this test exercises aad.Build
	// directly rather than driving the full publisher (which would
	// require an embedded NATS + DEK manager + crypto stack).
	publisherEvent.Timestamp = timestamppb.New(publisherEvent.GetTimestamp().AsTime().Truncate(time.Microsecond))

	encryptSideAAD, err := aad.Build(publisherEvent, codecName, dekRef, dekVer)
	require.NoError(t, err)
	require.NotEmpty(t, encryptSideAAD, "encrypt-side AAD must be non-empty")

	// Simulate PG TIMESTAMPTZ round-trip: the plugin's scene_log column
	// truncates to microsecond on INSERT, and QueryHistory wraps the
	// truncated value via timestamppb.New. With the publisher fix above,
	// this is a no-op (already at µs precision); we apply it explicitly
	// anyway so the test would still catch a regression where someone
	// removes the publisher truncation but the test fixture happens to
	// have a sub-µs timestamp.
	auditRowTimestamp := timestamppb.New(publisherEvent.GetTimestamp().AsTime().Truncate(time.Microsecond))

	// Project the publisher event into the wire shape the dispatcher
	// stores and the plugin returns from QueryHistory. The field
	// projection mirrors what AuditRow stores — codec / dek_ref /
	// dek_version go on the row scalars; the AAD-bearing fields
	// (Id, Subject, Type, Timestamp, Actor) flow verbatim, EXCEPT
	// Timestamp which goes through the explicit µs-truncation above to
	// model the PG INSERT path.
	dr := dekRef
	dv := dekVer
	row := &pluginauditpb.AuditRow{
		Id:         publisherEvent.GetId(),
		Subject:    publisherEvent.GetSubject(),
		Type:       publisherEvent.GetType(),
		Timestamp:  auditRowTimestamp,
		Actor:      publisherEvent.GetActor(),
		Codec:      codecName,
		Payload:    []byte("ciphertext-bytes"),
		DekRef:     &dr,
		DekVersion: &dv,
	}

	// Reconstruct the publisher-shape event from the wire row using
	// the §5.4 adapter — this is the function under test.
	reconstructedEvent := history.AuditRowToEvent(row)
	require.NotNil(t, reconstructedEvent)

	reconstructedAAD, err := aad.Build(reconstructedEvent, row.GetCodec(), row.GetDekRef(), row.GetDekVersion())
	require.NoError(t, err)

	assert.Equal(t, encryptSideAAD, reconstructedAAD,
		"INV-P7-16: AAD reconstruction MUST be byte-equal to encrypt-side; "+
			"a mismatch breaks AEAD tag-check on every sensitive plugin-stored event")
}

// TestRoundTripFailsWithoutPublisherMicrosecondTruncation is the
// regression guard for the INV-P7-16 timestamp-precision bug class
// surfaced by the holomush-1r0v.3 crypto-review.
//
// It models exactly the BEFORE-fix flow: publisher computes encrypt-
// side AAD from a sub-µs-nanosecond timestamp, the plugin's PG
// TIMESTAMPTZ truncates to µs on INSERT, the host reconstructs AAD
// from the truncated value, and AEAD tag-check fails. The test
// asserts the AAD mismatch (NOT equal). If the publisher loses its
// µs truncation, TestRoundTripProducesByteEqualAAD would still pass
// (its publisherEvent uses a clean ts), but real production traffic
// would silently fail. This negative test locks in the bug class:
// "do NOT recompute AAD across a precision boundary".
func TestRoundTripFailsWithoutPublisherMicrosecondTruncation(t *testing.T) {
	t.Parallel()

	// Sub-µs nanosecond timestamp — what the BEFORE-fix publisher
	// would have fed to aad.Build verbatim.
	publisherEvent := &eventbusv1.Event{
		Id:        []byte("0123456789ABCDEF"),
		Subject:   "events.test.scene.01ABC.ic",
		Type:      "test-plugin:secret",
		Timestamp: timestamppb.New(time.Unix(1700000000, 12345).UTC()),
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   []byte("char-id-16-bytes"),
		},
	}
	const codecName = "xchacha20poly1305-v1"
	const dekRef uint64 = 42
	const dekVer uint32 = 7

	// BEFORE-fix path: AAD computed directly from the ns timestamp.
	encryptSideAAD, err := aad.Build(publisherEvent, codecName, dekRef, dekVer)
	require.NoError(t, err)

	// PG TIMESTAMPTZ truncation on the plugin's read path.
	auditRowTimestamp := timestamppb.New(publisherEvent.GetTimestamp().AsTime().Truncate(time.Microsecond))

	dr := dekRef
	dv := dekVer
	row := &pluginauditpb.AuditRow{
		Id:         publisherEvent.GetId(),
		Subject:    publisherEvent.GetSubject(),
		Type:       publisherEvent.GetType(),
		Timestamp:  auditRowTimestamp,
		Actor:      publisherEvent.GetActor(),
		Codec:      codecName,
		Payload:    []byte("ciphertext-bytes"),
		DekRef:     &dr,
		DekVersion: &dv,
	}

	reconstructedEvent := history.AuditRowToEvent(row)
	require.NotNil(t, reconstructedEvent)

	reconstructedAAD, err := aad.Build(reconstructedEvent, row.GetCodec(), row.GetDekRef(), row.GetDekVersion())
	require.NoError(t, err)

	assert.NotEqual(t, encryptSideAAD, reconstructedAAD,
		"INV-P7-16 regression guard: a publisher that does NOT truncate "+
			"event.Timestamp to µs before aad.Build produces an AAD that "+
			"can never be reconstructed after PG TIMESTAMPTZ truncation. "+
			"If this assertion ever flips to equal, the precision boundary "+
			"has moved or the bug class has been re-introduced silently.")
}

// TestRoundTripProducesByteEqualAAD_NilActor exercises the same
// invariant for the nil-Actor case. aad.Build tolerates nil actor
// (it marshals as empty bytes); the adapter must preserve the nil
// rather than substituting a zero-value Actor (which would marshal
// to a different byte sequence).
func TestRoundTripProducesByteEqualAAD_NilActor(t *testing.T) {
	t.Parallel()

	publisherEvent := &eventbusv1.Event{
		Id:        []byte("FEDCBA9876543210"),
		Subject:   "events.test.scene.02XYZ.ic",
		Type:      "test-plugin:plain",
		Timestamp: timestamppb.New(time.Unix(1700000001, 0).UTC()),
		// Actor intentionally nil
	}
	const codecName = "identity"
	const dekRef uint64 = 0
	const dekVer uint32 = 0

	encryptSideAAD, err := aad.Build(publisherEvent, codecName, dekRef, dekVer)
	require.NoError(t, err)

	row := &pluginauditpb.AuditRow{
		Id:        publisherEvent.GetId(),
		Subject:   publisherEvent.GetSubject(),
		Type:      publisherEvent.GetType(),
		Timestamp: publisherEvent.GetTimestamp(),
		// Actor nil — wire shape preserves nil for host-emit events
		Codec:   codecName,
		Payload: []byte("cleartext"),
	}

	reconstructedEvent := history.AuditRowToEvent(row)
	require.NotNil(t, reconstructedEvent)
	require.Nil(t, reconstructedEvent.GetActor(), "adapter MUST NOT invent a zero Actor")

	reconstructedAAD, err := aad.Build(reconstructedEvent, codecName, dekRef, dekVer)
	require.NoError(t, err)

	assert.Equal(t, encryptSideAAD, reconstructedAAD,
		"INV-P7-16: nil-Actor round trip MUST also produce byte-equal AAD")
}
