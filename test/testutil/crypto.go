// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package testutil

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// EmbeddedBus is the test-side handle to an embedded NATS+JetStream
// instance from internal/eventbus/eventbustest.
type EmbeddedBus = eventbustest.Embedded

// StartEmbeddedJetStream constructs a fresh embedded NATS+JetStream
// server with MemoryStorage and registers cleanup on t.Cleanup.
func StartEmbeddedJetStream(t *testing.T) *EmbeddedBus {
	t.Helper()
	return eventbustest.New(t)
}

// RandomKEKHex returns kek.KEKByteLength random bytes hex-encoded —
// the form expected by kek.NewEnvSource for test deployments.
func RandomKEKHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}

// MustParseULID parses a canonical 26-char ULID string and fails the
// test on error.
func MustParseULID(t *testing.T, s string) ulid.ULID {
	t.Helper()
	u, err := ulid.Parse(s)
	require.NoError(t, err)
	return u
}

// DefaultWait is the default timeout for the JetStream/audit
// helpers in this file. Five seconds is generous for embedded-server
// round trips; its only purpose is to fail loudly on a deadlock.
const DefaultWait = 5 * time.Second

// WaitForOneJetStreamMsg pull-fetches the first message on subject
// from the EVENTS stream and returns it. Uses bus.JS (the public field
// on eventbustest.Embedded) and the project-wide stream name
// eventbus.StreamName.
func WaitForOneJetStreamMsg(t *testing.T, bus *EmbeddedBus, subject string, timeout time.Duration) jetstream.Msg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cons, err := bus.JS.OrderedConsumer(ctx, eventbus.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{subject},
	})
	require.NoError(t, err)

	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(timeout))
	require.NoError(t, err)
	for msg := range msgs.Messages() {
		return msg
	}
	require.Fail(t, "no JetStream message received within timeout", "subject=%s", subject)
	return nil
}

// EventsAuditRow is the test-side projection of a single events_audit
// row, exposing the columns Phase 3a tests care about.
type EventsAuditRow struct {
	Codec      string
	Envelope   []byte
	DekRef     *int64
	DekVersion *int32
}

// QueryEventsAuditByID reads one events_audit row keyed by its raw
// 16-byte ULID id and returns the columns Phase 3a tests assert on.
func QueryEventsAuditByID(t *testing.T, pool *pgxpool.Pool, idBytes []byte) EventsAuditRow {
	t.Helper()
	var row EventsAuditRow
	var dekRef sql.NullInt64
	var dekVer sql.NullInt32
	err := pool.QueryRow(context.Background(),
		`SELECT codec, envelope, dek_ref, dek_version FROM events_audit WHERE id = $1`,
		idBytes,
	).Scan(&row.Codec, &row.Envelope, &dekRef, &dekVer)
	require.NoError(t, err)
	if dekRef.Valid {
		v := dekRef.Int64
		row.DekRef = &v
	}
	if dekVer.Valid {
		v := dekVer.Int32
		row.DekVersion = &v
	}
	return row
}

// MustUnmarshalEventbusEnvelope proto-unmarshals decrypted plaintext
// bytes back into the eventbus envelope. For sensitive events,
// codec.Decode returns these proto bytes; pass the result here to get
// the inner *eventbusv1.Event with original Payload, ID, etc. Defined
// for Phase 3b's subscriber-side decrypt round-trip; Phase 3a does not
// call it.
func MustUnmarshalEventbusEnvelope(t *testing.T, decryptedBytes []byte) *eventbusv1.Event {
	t.Helper()
	envelope := &eventbusv1.Event{}
	require.NoError(t, proto.Unmarshal(decryptedBytes, envelope))
	return envelope
}

// ExtractInnerJSONPayload returns the plugin's original JSON payload
// from an unmarshaled envelope. Companion to
// MustUnmarshalEventbusEnvelope; reserved for Phase 3b.
func ExtractInnerJSONPayload(_ *testing.T, envelope *eventbusv1.Event) string {
	return string(envelope.GetPayload())
}
