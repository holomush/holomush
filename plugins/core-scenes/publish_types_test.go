// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublishedSceneStatusIsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		s     PublishedSceneStatus
		valid bool
	}{
		{"returns true for COLLECTING", StatusCollecting, true},
		{"returns true for COOLOFF", StatusCoolOff, true},
		{"returns true for PUBLISHED", StatusPublished, true},
		{"returns true for ATTEMPT_FAILED", StatusAttemptFailed, true},
		{"returns false for empty string", PublishedSceneStatus(""), false},
		{"returns false for unknown status", PublishedSceneStatus("WAT"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.valid, tc.s.IsValid())
		})
	}
}

func TestPublishedSceneStatusIsTerminal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		s        PublishedSceneStatus
		terminal bool
	}{
		{"returns true for PUBLISHED", StatusPublished, true},
		{"returns true for ATTEMPT_FAILED", StatusAttemptFailed, true},
		{"returns false for COLLECTING", StatusCollecting, false},
		{"returns false for COOLOFF", StatusCoolOff, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.terminal, tc.s.IsTerminal())
		})
	}
}

func TestPublishFailureReasonIsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		r     PublishFailureReason
		valid bool
	}{
		{"returns true for ANY_NO", FailureAnyNo, true},
		{"returns true for TIMEOUT", FailureTimeout, true},
		{"returns true for WITHDRAWN", FailureWithdrawn, true},
		{"returns true for SNAPSHOT_DECRYPT_FAILED", FailureSnapshotDecryptFailed, true},
		{"returns true for SNAPSHOT_RENDER_FAILED", FailureSnapshotRenderFailed, true},
		{"returns true for COOLOFF_INVARIANT_BROKEN", FailureCoolOffInvariantBroken, true},
		{"returns false for empty string", PublishFailureReason(""), false},
		{"returns false for unknown reason", PublishFailureReason("WAT"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.valid, tc.r.IsValid())
		})
	}
}

func TestEntryKindIsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		k     EntryKind
		valid bool
	}{
		{"returns true for pose", EntryKindPose, true},
		{"returns true for say", EntryKindSay, true},
		{"returns true for emit", EntryKindEmit, true},
		{"returns false for ooc (excluded from publication content per spec §12)", EntryKind("ooc"), false},
		{"returns false for empty string", EntryKind(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.valid, tc.k.IsValid())
		})
	}
}
