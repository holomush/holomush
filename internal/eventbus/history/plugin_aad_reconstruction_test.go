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
// Builds AAD twice with identical scalar args and asserts the bytes
// are equal. A regression in either AuditRowToEvent or aad.Build's
// canonical input set will break this test.
func TestRoundTripProducesByteEqualAAD(t *testing.T) {
	t.Parallel()

	// Publisher-side event (this is what aad.Build sees at encrypt time).
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

	encryptSideAAD, err := aad.Build(publisherEvent, codecName, dekRef, dekVer)
	require.NoError(t, err)
	require.NotEmpty(t, encryptSideAAD, "encrypt-side AAD must be non-empty")

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
		Timestamp:  publisherEvent.GetTimestamp(),
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
