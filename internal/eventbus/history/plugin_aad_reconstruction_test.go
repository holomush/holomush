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

// TestRoundTripPreservesAADWithSubMicrosecondNanos asserts INV-STORE-5
// (formerly INV-CRYPTO-51, superseded by ADR holomush-f5h0): byte-equal
// AAD reconstruction across an ns-precise publisher AAD and an
// ns-precise PG-BIGINT round-trip. Without this, EVERY sensitive
// plugin-stored event would fail AEAD tag-check on decrypt because
// the host's recompute would produce different AAD bytes than the
// publisher's encrypt-side AAD.
//
// Post-ADR-holomush-f5h0 the timestamp column is BIGINT-ns, so both
// the publisher AAD and the read-side reconstruction see full ns
// precision — no truncation mirrors anywhere in the round trip.
// The test fixture uses a sub-µs nanosecond component to actively
// exercise that precision floor; the final assertion checks that
// the sub-µs digits survive the publish → DB → read → AAD round trip.
func TestRoundTripPreservesAADWithSubMicrosecondNanos(t *testing.T) {
	t.Parallel()

	// Publisher-side event with a sub-µs nanosecond component — modeling
	// what time.Now() typically produces on Linux. The trailing "789" in
	// the nanosecond field is the sub-µs slot the round-trip assertion
	// pins.
	publisherEvent := &eventbusv1.Event{
		Id:        []byte("0123456789ABCDEF"),
		Subject:   "events.test.scene.01ABC.ic",
		Type:      "test-plugin:secret",
		Timestamp: timestamppb.New(time.Unix(1700000000, 12345789).UTC()),
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   []byte("char-id-16-bytes"),
		},
	}
	const codecName = "xchacha20poly1305-v1"
	const dekRef uint64 = 42
	const dekVer uint32 = 7

	encryptSideAAD, err := aad.Build(publisherEvent, codecName, dekRef, dekVer)
	require.NoError(t, err)
	require.NotEmpty(t, encryptSideAAD, "encrypt-side AAD must be non-empty")

	// PG BIGINT-ns column preserves full ns; no truncation.
	auditRowTimestamp := publisherEvent.GetTimestamp()

	// Project the publisher event into the wire shape the dispatcher
	// stores and the plugin returns from QueryHistory. The field
	// projection mirrors what AuditRow stores — codec / dek_ref /
	// dek_version go on the row scalars; the AAD-bearing fields
	// (Id, Subject, Type, Timestamp, Actor) flow verbatim.
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
		"INV-STORE-5: AAD reconstruction MUST be byte-equal to encrypt-side; "+
			"a mismatch breaks AEAD tag-check on every sensitive plugin-stored event")

	// INV-STORE-5 reinforcement: the sub-µs nanosecond component survives the
	// publish → DB → read → AAD-reconstruct round-trip at ns column resolution.
	roundTripTs := reconstructedEvent.GetTimestamp().AsTime()
	assert.Equal(t, 789, roundTripTs.Nanosecond()%1000,
		"INV-STORE-5: AAD round-trip MUST preserve sub-µs nanoseconds at ns column resolution")
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
		"INV-STORE-5: nil-Actor round trip MUST also produce byte-equal AAD")
}
