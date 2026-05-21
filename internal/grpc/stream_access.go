// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// isSceneStream reports whether a stream is a scene IC or OOC subject
// in NATS dot-style: events.<gameID>.scene.<sceneID>.{ic,ooc}.
// Used by isPrivateStream, the I-17 gate, and scope_floor.
func isSceneStream(stream string) bool {
	parts := strings.Split(stream, ".")
	if len(parts) < 5 {
		return false
	}
	if parts[0] != "events" || parts[2] != "scene" {
		return false
	}
	facet := parts[len(parts)-1]
	return facet == "ic" || facet == "ooc"
}

// extractDotSceneID returns the scene ULID from a dot-style scene subject.
// Caller MUST check isSceneStream first; undefined behavior otherwise.
// Named extractDotSceneID to avoid collision with scope_floor.go's colon-style
// extractSceneID (which migrates in T13 / holomush-5rh.13.13).
func extractDotSceneID(stream string) (string, bool) {
	parts := strings.Split(stream, ".")
	if len(parts) < 5 || parts[0] != "events" || parts[2] != "scene" {
		return "", false
	}
	return parts[3], true
}

// isPrivateStream returns true if the stream requires membership to read.
// This is the gate for invariant I-17: private streams are readable only by
// members, with no policy override. Private stream types:
//   - character:<ulid>             — personal stream (only the owning character)
//   - events.<gid>.scene.<id>.ic  — scene IC stream (only scene members)
//   - events.<gid>.scene.<id>.ooc — scene OOC stream (only scene members)
//
// Phase 4: scene subjects use NATS dot-style per INV-P4-1 / ADR holomush-s9nu.
// character: prefix is unchanged (tracked separately by holomush-rops).
//
// Public streams (location:*, global, etc.) are gated by ABAC policy, not
// by this function.
func isPrivateStream(stream string) bool {
	return strings.HasPrefix(stream, "character:") || isSceneStream(stream)
}

// sessionHasMembership checks if the session has membership entitling it to
// read a private stream. This is Layer 1 of the two-layer authorization
// model (I-17): the ABAC engine is never consulted for private streams.
//
// Membership rules:
//   - character:<id>  → session's CharacterID must equal <id>
//   - scene:<id>:ic   → session must have a FocusMembership with that scene target
//   - scene:<id>:ooc  → same as ic (IC and OOC are scoped together)
//
// Returns false for malformed stream names, unknown stream types, or when
// info is nil (fail-closed).
func sessionHasMembership(info *session.Info, stream string) bool {
	if info == nil {
		return false
	}

	if strings.HasPrefix(stream, "character:") {
		if info.CharacterID == (ulid.ULID{}) {
			return false
		}
		charID := strings.TrimPrefix(stream, "character:")
		return info.CharacterID.String() == charID
	}

	if isSceneStream(stream) {
		fk, err := streamToFocusKey(stream)
		if err != nil {
			return false
		}
		for _, fm := range info.FocusMemberships {
			if fm.TargetID == (ulid.ULID{}) {
				continue
			}
			if fm.Kind == fk.Kind && fm.TargetID == fk.TargetID {
				return true
			}
		}
		return false
	}

	return false
}

// streamToFocusKey parses a dot-style scene subject into a FocusKey. Returns
// an error with INVALID_ARGUMENT code if the stream is not a scene stream,
// if the ULID is malformed, or if the stream format is incomplete.
//
// Expected format: "events.<gameID>.scene.<sceneULID>.{ic,ooc}".
func streamToFocusKey(stream string) (*session.FocusKey, error) {
	if !isSceneStream(stream) {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("stream", stream).
			Errorf("not a scene stream")
	}

	// "events.<gameID>.scene.<sceneULID>.<facet>" — sceneID is parts[3]
	sceneIDStr, ok := extractDotSceneID(stream)
	if !ok {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("stream", stream).
			Errorf("malformed scene stream: could not extract scene ID")
	}

	targetID, err := ulid.Parse(sceneIDStr)
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("stream", stream).
			Wrap(err)
	}

	return &session.FocusKey{
		Kind:     session.FocusKindScene,
		TargetID: targetID,
	}, nil
}
