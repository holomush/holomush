// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"time"

	"github.com/holomush/holomush/internal/pgnanos"
)

// PublishedSceneStatus is the publish-attempt state machine status per
// spec §4. See the transition table at §4.1 for legal transitions.
type PublishedSceneStatus string

const (
	StatusCollecting    PublishedSceneStatus = "COLLECTING"
	StatusCoolOff       PublishedSceneStatus = "COOLOFF"
	StatusPublished     PublishedSceneStatus = "PUBLISHED"
	StatusAttemptFailed PublishedSceneStatus = "ATTEMPT_FAILED"
)

func (s PublishedSceneStatus) IsValid() bool {
	switch s {
	case StatusCollecting, StatusCoolOff, StatusPublished, StatusAttemptFailed:
		return true
	}
	return false
}

func (s PublishedSceneStatus) IsTerminal() bool {
	return s == StatusPublished || s == StatusAttemptFailed
}

// PublishFailureReason explains why an attempt reached ATTEMPT_FAILED.
// Set ONLY on terminal ATTEMPT_FAILED rows. See spec §4.1 and §11.4.
type PublishFailureReason string

const (
	FailureAnyNo                  PublishFailureReason = "ANY_NO"
	FailureTimeout                PublishFailureReason = "TIMEOUT"
	FailureWithdrawn              PublishFailureReason = "WITHDRAWN"
	FailureSnapshotDecryptFailed  PublishFailureReason = "SNAPSHOT_DECRYPT_FAILED"
	FailureSnapshotRenderFailed   PublishFailureReason = "SNAPSHOT_RENDER_FAILED"
	FailureCoolOffInvariantBroken PublishFailureReason = "COOLOFF_INVARIANT_BROKEN"
)

func (r PublishFailureReason) IsValid() bool {
	switch r {
	case FailureAnyNo, FailureTimeout, FailureWithdrawn,
		FailureSnapshotDecryptFailed, FailureSnapshotRenderFailed,
		FailureCoolOffInvariantBroken:
		return true
	}
	return false
}

// EntryKind discriminates the three IC content kinds that survive into
// a published scene. OOC and ops events are EXCLUDED — see spec §12 and
// ADR holomush-sb3n.
type EntryKind string

const (
	EntryKindPose EntryKind = "pose"
	EntryKindSay  EntryKind = "say"
	EntryKindEmit EntryKind = "emit"
)

func (k EntryKind) IsValid() bool {
	switch k {
	case EntryKindPose, EntryKindSay, EntryKindEmit:
		return true
	}
	return false
}

// PublishedSceneEntry is one row in the published_scenes.content_entries
// JSONB array. Frozen at PUBLISHED transition; immutable thereafter. Named
// to match the proto message `PublishedSceneEntry` (and to avoid colliding
// with Ginkgo's dot-imported table-DSL `Entry` in the package's integration
// tests).
type PublishedSceneEntry struct {
	Speaker string    `json:"speaker"`
	Kind    EntryKind `json:"kind"`
	Content string    `json:"content"`
}

// PublishedScene is the in-memory representation of a published_scenes row.
type PublishedScene struct {
	ID                   string
	SceneID              string
	AttemptNumber        int
	Status               PublishedSceneStatus
	InitiatedBy          string
	InitiatedAt          pgnanos.Time
	CoolOffStartedAt     *pgnanos.Time
	ResolvedAt           *pgnanos.Time
	VoteWindow           time.Duration
	CoolOffWindow        time.Duration
	MaxAttemptsSnapshot  int
	ContentEntries       []PublishedSceneEntry // nil unless PUBLISHED
	TitleSnapshot        *string
	ParticipantsSnapshot []string
	PublishedAt          *pgnanos.Time
	FailureReason        *PublishFailureReason
}

// PublishedSceneVote is one row in published_scene_votes — one voter's
// state on one attempt. Roster is frozen at attempt creation.
type PublishedSceneVote struct {
	PublishedSceneID string
	CharacterID      string
	Vote             *bool         // nil = pending; *true / *false = cast
	VotedAt          *pgnanos.Time // first-cast timestamp
	LastChangedAt    *pgnanos.Time // updated on every cast
}
