// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus/history"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestAuditRowToEvent_CopiesAllAADFields verifies the C.1 adapter
// performs a verbatim per-field copy of the six AAD-bearing fields:
// Id, Subject, Type, Timestamp, Actor.Kind, Actor.Id. (Codec /
// dek_ref / dek_version are scalar args to aad.Build, NOT Event
// fields; payload is the AEAD input, not AAD; schema_ver and
// rendering are not in AAD canonical inputs per master §4.2,
// verified at internal/eventbus/crypto/aad/aad.go:62-117.)
func TestAuditRowToEvent_CopiesAllAADFields(t *testing.T) {
	t.Parallel()
	ts := timestamppb.New(time.Unix(1700000000, 12345))
	row := &pluginauditpb.AuditRow{
		Id:        []byte("0123456789ABCDEF"),
		Subject:   "events.test.scene.01ABC.ic",
		Type:      "test-plugin:secret",
		Timestamp: ts,
		Actor: &eventbusv1.Actor{
			Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
			Id:   []byte("char-id-16-bytes"),
		},
		Codec:   "xchacha20poly1305-v1",
		Payload: []byte("ciphertext"),
	}

	ev := history.AuditRowToEvent(row)
	require.NotNil(t, ev)
	assert.Equal(t, row.GetId(), ev.GetId())
	assert.Equal(t, row.GetSubject(), ev.GetSubject())
	assert.Equal(t, row.GetType(), ev.GetType())
	assert.Equal(t, row.GetTimestamp().AsTime().UnixNano(), ev.GetTimestamp().AsTime().UnixNano())
	assert.Equal(t, row.GetActor().GetKind(), ev.GetActor().GetKind())
	assert.Equal(t, row.GetActor().GetId(), ev.GetActor().GetId())
}

// TestAuditRowToEvent_NilSafety verifies the adapter tolerates nil
// Timestamp and nil Actor — both legitimate in the wire shape and
// both passed through without panicking. aad.Build itself tolerates
// nil Actor (returns empty actor bytes) and nil Timestamp (treats
// the nano value as 0); the adapter must not invent values.
func TestAuditRowToEvent_NilSafety(t *testing.T) {
	t.Parallel()
	row := &pluginauditpb.AuditRow{
		Id:      []byte("0123456789ABCDEF"),
		Subject: "events.test.foo",
		Type:    "test-plugin:plain",
		Codec:   "identity",
		Payload: []byte("hello"),
		// Timestamp + Actor intentionally nil
	}
	ev := history.AuditRowToEvent(row)
	require.NotNil(t, ev)
	assert.Nil(t, ev.GetTimestamp())
	assert.Nil(t, ev.GetActor())
}

// TestAuditRowToEvent_NilInput verifies the explicit nil-in / nil-out
// contract from the §5.4 adapter table.
func TestAuditRowToEvent_NilInput(t *testing.T) {
	t.Parallel()
	assert.Nil(t, history.AuditRowToEvent(nil))
}
