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
//
// Enforces the exact 5-segment canonical shape: extra trailing segments,
// empty gameID, or empty sceneID are rejected so a malformed subject like
// events.X.scene.<sceneID>.extra.ic cannot inherit scene authorization.
func isSceneStream(stream string) bool {
	parts := strings.Split(stream, ".")
	if len(parts) != 5 {
		return false
	}
	if parts[0] != "events" || parts[1] == "" || parts[2] != "scene" || parts[3] == "" {
		return false
	}
	switch parts[4] {
	case "ic", "ooc":
		return true
	default:
		return false
	}
}

// extractSceneID returns the scene ULID from a dot-style scene subject
// (events.<gameID>.scene.<sceneID>.{ic,ooc}). Caller MUST check
// isSceneStream first; undefined behavior otherwise.
//
// Phase 4: this is the sole scene-ID extractor in the package. The
// colon-style extractor was removed when scope_floor.go's scene branch
// migrated to dot-style subjects (T13 / holomush-5rh.13.13).
func extractSceneID(stream string) (string, bool) {
	parts := strings.Split(stream, ".")
	if len(parts) != 5 || parts[0] != "events" || parts[1] == "" || parts[2] != "scene" || parts[3] == "" {
		return "", false
	}
	switch parts[4] {
	case "ic", "ooc":
		return parts[3], true
	default:
		return "", false
	}
}

// isCharacterStream reports whether a stream is a qualified personal character
// subject: events.<gameID>.character.<ULID> (exactly 4 segments). Dot-only per
// holomush-rops; the legacy character:<ulid> colon form is no longer accepted.
func isCharacterStream(stream string) bool {
	parts := strings.Split(stream, ".")
	return len(parts) == 4 && parts[0] == "events" && parts[1] != "" &&
		parts[2] == "character" && parts[3] != ""
}

// extractCharacterID returns the character ULID from a qualified character
// subject (events.<gameID>.character.<ULID>) and true, or "" and false when the
// stream is not a qualified character subject.
func extractCharacterID(stream string) (string, bool) {
	parts := strings.Split(stream, ".")
	if len(parts) == 4 && parts[0] == "events" && parts[1] != "" &&
		parts[2] == "character" && parts[3] != "" {
		return parts[3], true
	}
	return "", false
}

// isPrivateStream returns true if the stream requires membership to read.
// This is the gate for invariant I-17: private streams are readable only by
// members, with no policy override. Private stream types:
//   - events.<gid>.character.<id> — personal stream (only the owning character)
//   - events.<gid>.scene.<id>.ic  — scene IC stream (only scene members)
//   - events.<gid>.scene.<id>.ooc — scene OOC stream (only scene members)
//
// All private subjects use NATS dot-style: scene per INV-P4-1 / ADR holomush-s9nu,
// character per holomush-rops (the legacy character:<ulid> colon form is gone).
//
// Public streams (events.<gid>.location.<id>, global, etc.) are gated by ABAC
// policy, not by this function.
func isPrivateStream(stream string) bool {
	return isCharacterStream(stream) || isSceneStream(stream)
}

// sessionHasMembership checks if the session has membership entitling it to
// read a private stream. This is Layer 1 of the two-layer authorization
// model (I-17): the ABAC engine is never consulted for private streams.
//
// Membership rules:
//   - events.<gid>.character.<id>  → session's CharacterID must equal <id>
//   - events.<gid>.scene.<id>.ic   → session must have a FocusMembership with that scene target
//   - events.<gid>.scene.<id>.ooc  → same as ic (IC and OOC are scoped together)
//
// Returns false for malformed stream names, unknown stream types, or when
// info is nil (fail-closed).
func sessionHasMembership(info *session.Info, stream string) bool {
	if info == nil {
		return false
	}

	if charID, ok := extractCharacterID(stream); ok {
		if info.CharacterID == (ulid.ULID{}) {
			return false
		}
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
	sceneIDStr, ok := extractSceneID(stream)
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
