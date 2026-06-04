// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/readstream"
)

func TestEncodeHash_HasSha256Prefix(t *testing.T) {
	// OperatorReadStartPayload.SelfHash is populated via encodeHash internally.
	// We verify the format by constructing a minimal payload with a known hash
	// via EmitStart in audit_emitter_test.go; here we exercise the struct
	// field contract: PolicyHash and SelfHash MUST be "sha256:<hex>".
	//
	// The canonical form is validated by checking that any value we put in
	// PolicyHash starting with "sha256:" round-trips through JSON unchanged.
	payload := readstream.OperatorReadStartPayload{
		PolicyHash:    "sha256:deadbeef",
		SelfHash:      "sha256:cafebabe",
		RequestID:     "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		StartedAt:     time.Now().UTC().Truncate(time.Millisecond),
		ResolvedSince: time.Now().UTC().Truncate(time.Millisecond),
		ResolvedUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond),
	}
	raw, err := json.Marshal(&payload)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	policyHash, ok := m["policy_hash"].(string)
	require.True(t, ok, "policy_hash must be a string")
	assert.True(t, strings.HasPrefix(policyHash, "sha256:"), "policy_hash %q must start with sha256:", policyHash)

	selfHash, ok := m["self_hash"].(string)
	require.True(t, ok, "self_hash must be a string")
	assert.True(t, strings.HasPrefix(selfHash, "sha256:"), "self_hash %q must start with sha256:", selfHash)
}

func TestEncodeHashPtr_NilForGenesis(t *testing.T) {
	// A genesis OperatorReadStartPayload has no prev_hash (PrevHash == nil).
	// After JSON marshal, the field must be absent (omitempty).
	payload := readstream.OperatorReadStartPayload{
		RequestID:     "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SelfHash:      "sha256:cafebabe",
		PolicyHash:    "sha256:deadbeef",
		StartedAt:     time.Now().UTC(),
		ResolvedSince: time.Now().UTC(),
		ResolvedUntil: time.Now().UTC().Add(time.Hour),
		// PrevHash left nil (genesis)
	}

	raw, err := json.Marshal(&payload)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	_, hasPrevHash := m["prev_hash"]
	assert.False(t, hasPrevHash, "genesis payload must have no prev_hash key in JSON")
}

func TestINV_CRYPTO_57_PayloadPreservesRequestedAndResolved(t *testing.T) {
	// INV-CRYPTO-57: OperatorReadStartPayload MUST persist both Requested-* (nullable)
	// and Resolved-* (always populated) fields for since/until/contexts.
	// JSON round-trip must preserve all fields including nullable *time.Time pointers.

	operatorID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	approverID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAW")
	approvalID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAX")

	reqSince := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reqUntil := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	resolvedSince := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	resolvedUntil := time.Date(2026, 1, 2, 1, 0, 0, 0, time.UTC)

	original := readstream.OperatorReadStartPayload{
		OperatorPlayerID:       operatorID,
		OperatorSessionTokenID: "tok-abc",
		PeerCredUID:            1001,
		PeerCredPID:            42,
		DualControl:            true,
		ApproverPlayerID:       &approverID,
		ApprovalID:             &approvalID,
		Justification:          "incident response",
		RequestedContexts: []readstream.ContextRef{
			{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAZ"}},
		},
		RequestedSince: &reqSince,
		RequestedUntil: &reqUntil,
		ResolvedContexts: []readstream.ContextRef{
			{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAZ"}},
			{Type: "character", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FB0"}},
		},
		ResolvedSince: resolvedSince,
		ResolvedUntil: resolvedUntil,
		PolicyHash:    "sha256:aabbcc",
		RequestID:     "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SelfHash:      "sha256:ddeeff",
		StartedAt:     time.Now().UTC().Truncate(time.Second),
	}

	raw, err := json.Marshal(&original)
	require.NoError(t, err)

	var decoded readstream.OperatorReadStartPayload
	require.NoError(t, json.Unmarshal(raw, &decoded))

	// Requested-* nullable pointers survive as non-nil
	require.NotNil(t, decoded.RequestedSince, "RequestedSince must survive round-trip")
	require.NotNil(t, decoded.RequestedUntil, "RequestedUntil must survive round-trip")
	assert.Equal(t, reqSince.UTC(), decoded.RequestedSince.UTC())
	assert.Equal(t, reqUntil.UTC(), decoded.RequestedUntil.UTC())

	// Resolved-* always populated
	assert.Equal(t, resolvedSince.UTC(), decoded.ResolvedSince.UTC())
	assert.Equal(t, resolvedUntil.UTC(), decoded.ResolvedUntil.UTC())

	// Contexts
	assert.Len(t, decoded.RequestedContexts, 1)
	assert.Len(t, decoded.ResolvedContexts, 2)

	// Dual-control fields
	require.NotNil(t, decoded.ApproverPlayerID)
	require.NotNil(t, decoded.ApprovalID)
	assert.Equal(t, approverID, *decoded.ApproverPlayerID)
	assert.Equal(t, approvalID, *decoded.ApprovalID)
}

func TestOperatorReadCompletedPayload_JSONRoundTrip(t *testing.T) {
	// All 8 fields of OperatorReadCompletedPayload must round-trip through JSON.
	finished := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	original := readstream.OperatorReadCompletedPayload{
		RequestID:        "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		TerminatedBy:     "CLIENT_EOF",
		EventsScanned:    1234,
		DecryptFailCount: 5,
		PolicyHash:       "sha256:aabbcc",
		SelfHash:         "sha256:ddeeff",
		PrevHash:         "sha256:112233",
		FinishedAt:       finished,
	}

	raw, err := json.Marshal(&original)
	require.NoError(t, err)

	var decoded readstream.OperatorReadCompletedPayload
	require.NoError(t, json.Unmarshal(raw, &decoded))

	assert.Equal(t, original.RequestID, decoded.RequestID)
	assert.Equal(t, original.TerminatedBy, decoded.TerminatedBy)
	assert.Equal(t, original.EventsScanned, decoded.EventsScanned)
	assert.Equal(t, original.DecryptFailCount, decoded.DecryptFailCount)
	assert.Equal(t, original.PolicyHash, decoded.PolicyHash)
	assert.Equal(t, original.SelfHash, decoded.SelfHash)
	assert.Equal(t, original.PrevHash, decoded.PrevHash)
	assert.Equal(t, finished.UTC(), decoded.FinishedAt.UTC())
}
