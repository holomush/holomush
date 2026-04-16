// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// isPrivateStream returns true if the stream requires membership to read.
// This is the gate for invariant I-17: private streams are readable only by
// members, with no policy override. Private stream types:
//   - character:<ulid>  — personal stream (only the owning character)
//   - scene:<ulid>:ic   — scene IC stream (only scene members)
//   - scene:<ulid>:ooc  — scene OOC stream (only scene members)
//
// Public streams (location:*, global, etc.) are gated by ABAC policy, not
// by this function.
func isPrivateStream(stream string) bool {
	return strings.HasPrefix(stream, "character:") || strings.HasPrefix(stream, "scene:")
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

	if strings.HasPrefix(stream, "scene:") {
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

// streamToFocusKey parses a scene stream name into a FocusKey. Returns
// an error with INVALID_ARGUMENT code if the stream is not a scene stream,
// if the ULID is malformed, or if the stream format is incomplete.
//
// Expected format: "scene:<ulid>:ic" or "scene:<ulid>:ooc".
func streamToFocusKey(stream string) (*session.FocusKey, error) {
	if !strings.HasPrefix(stream, "scene:") {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("stream", stream).
			Errorf("not a scene stream")
	}

	// "scene:<ulid>:<suffix>" → parts = ["scene", "<ulid>", "<suffix>"]
	parts := strings.SplitN(stream, ":", 3)
	if len(parts) < 3 {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("stream", stream).
			Errorf("malformed scene stream: expected scene:<ulid>:<suffix>")
	}

	if parts[2] != "ic" && parts[2] != "ooc" {
		return nil, oops.Code("INVALID_ARGUMENT").
			With("stream", stream).
			With("suffix", parts[2]).
			Errorf("unknown scene stream suffix: expected ic or ooc")
	}

	targetID, err := ulid.Parse(parts[1])
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
