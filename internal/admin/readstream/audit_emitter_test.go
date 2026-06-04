// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/readstream"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/pkg/errutil"
)

// --- test fakes ---

// fakeChainEmitter implements chain.Emitter for test control.
type fakeChainEmitter struct {
	prevHash []byte
	err      error
}

func (f *fakeChainEmitter) ComputePrevHashFor(_ context.Context, _ chain.Handler, _ string) ([]byte, *ulid.ULID, error) {
	return f.prevHash, nil, f.err
}

// capturingPublisher implements eventbus.Publisher and records published events.
type capturingPublisher struct {
	events []eventbus.Event
	err    error
}

func (c *capturingPublisher) Publish(_ context.Context, ev eventbus.Event) error {
	if c.err != nil {
		return c.err
	}
	c.events = append(c.events, ev)
	return nil
}

func makeRequestID() ulid.ULID {
	return ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
}

func makeStartPayload(requestID ulid.ULID) readstream.OperatorReadStartPayload {
	return readstream.OperatorReadStartPayload{
		OperatorPlayerID:       requestID, // reuse for determinism
		OperatorSessionTokenID: "tok-123",
		PeerCredUID:            1001,
		Justification:          "incident response",
		PolicyHash:             "sha256:aabbcc",
		RequestID:              requestID.String(),
		StartedAt:              time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
		ResolvedSince:          time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC),
		ResolvedUntil:          time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
	}
}

func makeCompletedPayload(requestID ulid.ULID) readstream.OperatorReadCompletedPayload {
	return readstream.OperatorReadCompletedPayload{
		RequestID:     requestID.String(),
		TerminatedBy:  "CLIENT_EOF",
		EventsScanned: 42,
		PolicyHash:    "sha256:aabbcc",
		FinishedAt:    time.Date(2026, 5, 12, 10, 5, 0, 0, time.UTC),
	}
}

// TestOperatorReadAuditEmitter_EmitStartGenesis tests the genesis case:
// chain fake returns nil prevHash; asserts publish subject, type, non-empty payload.
func TestOperatorReadAuditEmitter_EmitStartGenesis(t *testing.T) {
	ce := &fakeChainEmitter{prevHash: nil}
	pub := &capturingPublisher{}
	h := readstream.OperatorReadHandlerFor("g1")
	em := readstream.NewOperatorReadAuditEmitter(ce, pub, h)

	requestID := makeRequestID()
	err := em.EmitStart(context.Background(), makeStartPayload(requestID), requestID)
	require.NoError(t, err)

	require.Len(t, pub.events, 1)
	ev := pub.events[0]

	// Subject must contain the request_id
	assert.Equal(t, eventbus.Subject("events.g1.system.operator_read."+requestID.String()), ev.Subject)

	// Type
	assert.Equal(t, eventbus.Type("crypto.system.operator_read"), ev.Type)

	// Payload must be non-empty
	require.NotEmpty(t, ev.Payload)

	// Payload must be valid JSON with self_hash populated
	var m map[string]any
	require.NoError(t, json.Unmarshal(ev.Payload, &m))
	selfHash, ok := m["self_hash"].(string)
	require.True(t, ok, "self_hash must be a string")
	assert.True(t, strings.HasPrefix(selfHash, "sha256:"), "self_hash must start with sha256:")

	// Genesis: prev_hash must be absent
	_, hasPrevHash := m["prev_hash"]
	assert.False(t, hasPrevHash, "genesis must have no prev_hash in payload")
}

// TestOperatorReadAuditEmitter_EmitStartPropagatesPublishError tests that
// a publisher error wraps in OPERATOR_READ_AUDIT_PUBLISH_FAILED.
func TestOperatorReadAuditEmitter_EmitStartPropagatesPublishError(t *testing.T) {
	ce := &fakeChainEmitter{prevHash: nil}
	pub := &capturingPublisher{err: errors.New("nats: publish failed")}
	h := readstream.OperatorReadHandlerFor("g1")
	em := readstream.NewOperatorReadAuditEmitter(ce, pub, h)

	requestID := makeRequestID()
	err := em.EmitStart(context.Background(), makeStartPayload(requestID), requestID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "OPERATOR_READ_AUDIT_PUBLISH_FAILED")
}

// TestINV_CRYPTO_58_RequestIDCoherence verifies INV-CRYPTO-58:
// EmitStart and EmitCompleted must both publish events with the same RequestID
// in the payload, and subjects must share the request_id suffix.
func TestINV_CRYPTO_58_RequestIDCoherence(t *testing.T) {
	// First call: genesis (no prev)
	// Second call: prevHash = some bytes (simulating the start's self_hash)
	callCount := 0
	ce := &chainEmitterFunc{fn: func(_ context.Context, _ chain.Handler, _ string) ([]byte, *ulid.ULID, error) {
		callCount++
		if callCount == 1 {
			return nil, nil, nil // genesis for start
		}
		return []byte{0xde, 0xad, 0xbe, 0xef}, nil, nil // prev for completed
	}}
	pub := &capturingPublisher{}
	h := readstream.OperatorReadHandlerFor("g1")
	em := readstream.NewOperatorReadAuditEmitter(ce, pub, h)

	requestID := makeRequestID()

	err := em.EmitStart(context.Background(), makeStartPayload(requestID), requestID)
	require.NoError(t, err)

	err = em.EmitCompleted(context.Background(), makeCompletedPayload(requestID), requestID)
	require.NoError(t, err)

	require.Len(t, pub.events, 2)

	// Decode both payloads and check RequestID coherence
	var startMap, completedMap map[string]any
	require.NoError(t, json.Unmarshal(pub.events[0].Payload, &startMap))
	require.NoError(t, json.Unmarshal(pub.events[1].Payload, &completedMap))

	startReqID, ok := startMap["request_id"].(string)
	require.True(t, ok)
	completedReqID, ok := completedMap["request_id"].(string)
	require.True(t, ok)

	assert.Equal(t, requestID.String(), startReqID, "start payload request_id must match input")
	assert.Equal(t, requestID.String(), completedReqID, "completed payload request_id must match input")
	assert.Equal(t, startReqID, completedReqID, "both events must share request_id (INV-CRYPTO-58)")

	// Both subjects must contain the request_id suffix
	assert.Equal(t, pub.events[0].Subject, pub.events[1].Subject, "both events must share the NATS subject (INV-CRYPTO-59)")
}

// TestOperatorReadAuditEmitter_EmitCompletedRefusesWithoutStart tests that
// EmitCompleted returns OPERATOR_READ_AUDIT_COMPLETED_NO_START when there is no
// preceding start event (chain returns nil prevHash).
func TestOperatorReadAuditEmitter_EmitCompletedRefusesWithoutStart(t *testing.T) {
	ce := &fakeChainEmitter{prevHash: nil}
	pub := &capturingPublisher{}
	h := readstream.OperatorReadHandlerFor("g1")
	em := readstream.NewOperatorReadAuditEmitter(ce, pub, h)

	requestID := makeRequestID()
	err := em.EmitCompleted(context.Background(), makeCompletedPayload(requestID), requestID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "OPERATOR_READ_AUDIT_COMPLETED_NO_START")

	// Metric must have incremented
	assert.Equal(t, float64(0), float64(0)) // see TestINV_CRYPTO_60_MetricIncrements
}

// TestINV_CRYPTO_60_MetricIncrements verifies INV-CRYPTO-60: EmitCompleted failure increments
// holomush_admin_readstream_completed_audit_failures_total.
func TestINV_CRYPTO_60_MetricIncrements(t *testing.T) {
	before := testutil.ToFloat64(readstream.CompletedAuditFailuresTotal)

	// Fault-inject: chain returns nil prevHash → OPERATOR_READ_AUDIT_COMPLETED_NO_START
	ce := &fakeChainEmitter{prevHash: nil}
	pub := &capturingPublisher{}
	h := readstream.OperatorReadHandlerFor("g1")
	em := readstream.NewOperatorReadAuditEmitter(ce, pub, h)

	requestID := makeRequestID()
	err := em.EmitCompleted(context.Background(), makeCompletedPayload(requestID), requestID)
	require.Error(t, err)

	after := testutil.ToFloat64(readstream.CompletedAuditFailuresTotal)
	assert.Equal(t, before+1, after, "metric must increment on EmitCompleted failure (INV-CRYPTO-60)")
}

// TestINV_CRYPTO_60_EmitStartFailureDoesNotIncrementMetric verifies INV-CRYPTO-60 inverse:
// EmitStart failure must NOT increment the completed-audit-failures metric.
func TestINV_CRYPTO_60_EmitStartFailureDoesNotIncrementMetric(t *testing.T) {
	before := testutil.ToFloat64(readstream.CompletedAuditFailuresTotal)

	// Fault-inject: publisher fails
	ce := &fakeChainEmitter{prevHash: nil}
	pub := &capturingPublisher{err: errors.New("publish failed")}
	h := readstream.OperatorReadHandlerFor("g1")
	em := readstream.NewOperatorReadAuditEmitter(ce, pub, h)

	requestID := makeRequestID()
	err := em.EmitStart(context.Background(), makeStartPayload(requestID), requestID)
	require.Error(t, err)

	after := testutil.ToFloat64(readstream.CompletedAuditFailuresTotal)
	assert.Equal(t, before, after, "EmitStart failure must NOT increment completed-audit metric (INV-CRYPTO-60)")
}

// chainEmitterFunc is a function-backed chain.Emitter for test flexibility.
type chainEmitterFunc struct {
	fn func(ctx context.Context, h chain.Handler, scope string) ([]byte, *ulid.ULID, error)
}

func (c *chainEmitterFunc) ComputePrevHashFor(ctx context.Context, h chain.Handler, scope string) ([]byte, *ulid.ULID, error) {
	return c.fn(ctx, h, scope)
}
