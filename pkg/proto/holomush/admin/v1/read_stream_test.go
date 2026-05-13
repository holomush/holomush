// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminv1_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestAdminReadStreamRequest_RoundTrip verifies that all fields of
// AdminReadStreamRequest survive a proto Marshal → Unmarshal cycle.
func TestAdminReadStreamRequest_RoundTrip(t *testing.T) {
	t.Parallel()

	ts := timestamppb.New(time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC))
	original := &adminv1.AdminReadStreamRequest{
		SessionToken:   "tok-abc123",
		SubjectPattern: "scene.>",
		TypeFilter:     "crypto.",
		Context: []*adminv1.ContextRef{
			{Type: "scene", Ids: []string{"01H", "01J"}},
			{Type: "dm", Ids: []string{"01A", "01B", "01C"}},
		},
		Since:                     ts,
		Until:                     ts,
		Limit:                     500,
		DualControl:               true,
		DualControlTimeoutSeconds: 120,
		Justification:             "incident-2026-05-12 P0 investigation",
	}

	b, err := proto.Marshal(original)
	require.NoError(t, err, "Marshal must succeed")

	decoded := &adminv1.AdminReadStreamRequest{}
	require.NoError(t, proto.Unmarshal(b, decoded), "Unmarshal must succeed")

	assert.Equal(t, original.GetSessionToken(), decoded.GetSessionToken())
	assert.Equal(t, original.GetSubjectPattern(), decoded.GetSubjectPattern())
	assert.Equal(t, original.GetTypeFilter(), decoded.GetTypeFilter())
	assert.Equal(t, original.GetLimit(), decoded.GetLimit())
	assert.True(t, decoded.GetDualControl())
	assert.Equal(t, original.GetDualControlTimeoutSeconds(), decoded.GetDualControlTimeoutSeconds())
	assert.Equal(t, original.GetJustification(), decoded.GetJustification())

	require.Len(t, decoded.GetContext(), 2)
	assert.Equal(t, "scene", decoded.GetContext()[0].GetType())
	assert.Equal(t, []string{"01H", "01J"}, decoded.GetContext()[0].GetIds())
	assert.Equal(t, "dm", decoded.GetContext()[1].GetType())
	assert.Equal(t, []string{"01A", "01B", "01C"}, decoded.GetContext()[1].GetIds())

	assert.True(t, original.GetSince().AsTime().Equal(decoded.GetSince().AsTime()))
	assert.True(t, original.GetUntil().AsTime().Equal(decoded.GetUntil().AsTime()))
}

// TestReadFinished_TerminatedByEnum asserts all 7 TerminatedBy values have
// the expected stable wire indices 0..6.
func TestReadFinished_TerminatedByEnum(t *testing.T) {
	t.Parallel()

	cases := []struct {
		val  adminv1.ReadFinished_TerminatedBy
		wire int32
	}{
		{adminv1.ReadFinished_TERMINATED_BY_UNSPECIFIED, 0},
		{adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF, 1},
		{adminv1.ReadFinished_TERMINATED_BY_CLIENT_DISCONNECT, 2},
		{adminv1.ReadFinished_TERMINATED_BY_DEADLINE_EXCEEDED, 3},
		{adminv1.ReadFinished_TERMINATED_BY_SERVER_ERROR, 4},
		{adminv1.ReadFinished_TERMINATED_BY_DUAL_CONTROL_TIMEOUT, 5},
		{adminv1.ReadFinished_TERMINATED_BY_AUDIT_EMIT_FAILURE, 6},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.wire, int32(tc.val),
			"TerminatedBy wire index mismatch for %s", tc.val)
	}
}

// TestEventFrameRoundTripPreservesMetadataOnly verifies that an
// AdminReadStreamResponse carrying an EventFrame with metadata_only=true and a
// typed NoPlaintextReason round-trips faithfully through proto Marshal/Unmarshal.
//
// This is the CRITICAL ADR-0017 invariant: the response uses corev1.EventFrame
// (with typed redaction fields), NOT a raw eventbusv1.Event.
func TestEventFrameRoundTripPreservesMetadataOnly(t *testing.T) {
	t.Parallel()

	frame := &corev1.EventFrame{
		Id:                "01JWZZ0000000000000000000A",
		Stream:            "scene.01H",
		Type:              "crypto.system.operator_read.started",
		MetadataOnly:      true,
		NoPlaintextReason: corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_MISSING,
	}

	original := &adminv1.AdminReadStreamResponse{
		Payload: &adminv1.AdminReadStreamResponse_Event{
			Event: frame,
		},
	}

	b, err := proto.Marshal(original)
	require.NoError(t, err, "Marshal must succeed")

	decoded := &adminv1.AdminReadStreamResponse{}
	require.NoError(t, proto.Unmarshal(b, decoded), "Unmarshal must succeed")

	ev, ok := decoded.GetPayload().(*adminv1.AdminReadStreamResponse_Event)
	require.True(t, ok, "payload oneof must be Event variant")
	require.NotNil(t, ev.Event)

	assert.True(t, ev.Event.GetMetadataOnly(), "metadata_only must round-trip as true")
	assert.Equal(
		t,
		corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_DEK_MISSING,
		ev.Event.GetNoPlaintextReason(),
		"no_plaintext_reason must round-trip with the typed value",
	)
	assert.Equal(t, frame.GetId(), ev.Event.GetId())
	assert.Equal(t, frame.GetStream(), ev.Event.GetStream())
	assert.Equal(t, frame.GetType(), ev.Event.GetType())
}
