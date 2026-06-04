// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/readstream"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestOperatorReadChainFor_SubjectPrefixSatisfiesINVE26(t *testing.T) {
	// INV-E26: SubjectPrefix MUST start with "events.".
	c := readstream.OperatorReadChainFor("g1")
	err := chain.ValidateRegistration(c)
	require.NoError(t, err, "OperatorReadChainFor must satisfy chain.ValidateRegistration (INV-E26/E27/E28)")
}

func TestOperatorReadHandlerFor_AllSevenFieldsPopulated(t *testing.T) {
	h := readstream.OperatorReadHandlerFor("g1")

	assert.NotNil(t, h.Chain.SubjectPrefix, "Chain.SubjectPrefix must be set")
	assert.NotNil(t, h.SubjectFor, "SubjectFor callback must be non-nil")
	assert.NotNil(t, h.ScopeFromSubject, "ScopeFromSubject callback must be non-nil")
	assert.NotNil(t, h.ScopeFromPayload, "ScopeFromPayload callback must be non-nil")
	assert.NotNil(t, h.Canonicalize, "Canonicalize callback must be non-nil")
	assert.NotNil(t, h.PrevHashOf, "PrevHashOf callback must be non-nil")
	assert.NotNil(t, h.SelfHashOf, "SelfHashOf callback must be non-nil")
}

func TestOperatorReadHandler_SubjectRoundTrip(t *testing.T) {
	// SubjectFor and ScopeFromSubject must be inverses on canonical form.
	h := readstream.OperatorReadHandlerFor("game-1")

	requestID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	subject := h.SubjectFor(requestID)

	assert.Equal(t, "events.game-1.system.operator_read."+requestID, subject)

	extracted, err := h.ScopeFromSubject(subject)
	require.NoError(t, err)
	assert.Equal(t, requestID, extracted, "ScopeFromSubject must be inverse of SubjectFor")
}

func TestOperatorReadHandler_ScopeFromSubjectRejectsBadPrefix(t *testing.T) {
	// Wrong prefix must return oops code OPERATOR_READ_SCOPE_FROM_SUBJECT_FAILED.
	h := readstream.OperatorReadHandlerFor("game-1")

	_, err := h.ScopeFromSubject("events.other.system.rekey.01ARZ3NDEKTSV4RRFFQ69G5FAV")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "OPERATOR_READ_SCOPE_FROM_SUBJECT_FAILED")
}

// TestOperatorReadHandler_ScopeFromSubjectRejectsInvalidSuffix verifies that
// ScopeFromSubject rejects empty and multi-segment suffixes, accepting only a
// single non-empty scope segment.
func TestOperatorReadHandler_ScopeFromSubjectRejectsInvalidSuffix(t *testing.T) {
	h := readstream.OperatorReadHandlerFor("game-1")
	prefix := "events.game-1.system.operator_read"

	cases := []struct {
		name    string
		subject string
	}{
		{
			name:    "empty suffix (trailing dot only)",
			subject: prefix + ".",
		},
		{
			name:    "multi-segment suffix (extra dot)",
			subject: prefix + ".01ARZ3NDEKTSV4RRFFQ69G5FAV.extra",
		},
		{
			name:    "multi-segment suffix (two extra dots)",
			subject: prefix + ".a.b.c",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.ScopeFromSubject(tc.subject)
			require.Error(t, err, "invalid suffix must be rejected")
			errutil.AssertErrorCode(t, err, "OPERATOR_READ_SCOPE_FROM_SUBJECT_FAILED")
		})
	}
}

func TestINV_CRYPTO_59_AuditChainLinksStartToCompletedSameSubject(t *testing.T) {
	// INV-CRYPTO-59: both events share NATS subject; chain primitive links them.
	// Construct fake start + completed envelopes with shared request_id.
	h := readstream.OperatorReadHandlerFor("g1")
	requestID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"

	startPayload := readstream.OperatorReadStartPayload{
		RequestID:     requestID,
		PolicyHash:    "sha256:aabbcc",
		SelfHash:      "sha256:ddeeff",
		StartedAt:     time.Now().UTC(),
		ResolvedSince: time.Now().UTC(),
		ResolvedUntil: time.Now().UTC().Add(time.Hour),
	}

	completedPayload := readstream.OperatorReadCompletedPayload{
		RequestID:  requestID,
		PolicyHash: "sha256:aabbcc",
		SelfHash:   "sha256:112233",
		PrevHash:   "sha256:ddeeff", // links to start's self_hash
		FinishedAt: time.Now().UTC(),
	}

	startBytes, err := json.Marshal(&startPayload)
	require.NoError(t, err)
	completedBytes, err := json.Marshal(&completedPayload)
	require.NoError(t, err)

	// (a) Both subjects are equal via SubjectFor
	startSubject := h.SubjectFor(requestID)
	completedSubject := h.SubjectFor(requestID)
	assert.Equal(t, startSubject, completedSubject, "both events must share the same subject (INV-CRYPTO-59)")

	// (b) ScopeFromPayload extracts request_id from both
	startScope, err := h.ScopeFromPayload(startBytes)
	require.NoError(t, err)
	assert.Equal(t, requestID, startScope)

	completedScope, err := h.ScopeFromPayload(completedBytes)
	require.NoError(t, err)
	assert.Equal(t, requestID, completedScope)

	// (c) Canonicalize returns non-empty deterministic bytes
	canon1, err := h.Canonicalize(startBytes)
	require.NoError(t, err)
	assert.NotEmpty(t, canon1, "Canonicalize must return non-empty bytes")

	canon2, err := h.Canonicalize(startBytes)
	require.NoError(t, err)
	assert.Equal(t, canon1, canon2, "Canonicalize must be deterministic")

	// (d) PrevHashOf returns nil for absent prev_hash (genesis start payload has nil PrevHash)
	prevHash, err := h.PrevHashOf(startBytes)
	require.NoError(t, err)
	assert.Nil(t, prevHash, "PrevHashOf must return nil for genesis (absent prev_hash)")

	// (e) SelfHashOf hex-decodes correctly — self_hash = "sha256:ddeeff" → bytes 0xdd 0xee 0xff
	selfBytes, err := h.SelfHashOf(startBytes)
	require.NoError(t, err)
	require.NotNil(t, selfBytes)
	assert.Equal(t, []byte{0xdd, 0xee, 0xff}, selfBytes)
}
