// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package subjectxlate bridges legacy colon-delimited stream names
// (e.g. "character:01ABC", "location:01XYZ") to JetStream-native
// dot-delimited subjects (e.g. "events.main.character.01ABC").
//
// F5 will migrate plugin/host code to JetStream-native subjects,
// at which point the translation becomes a no-op. Until then, both
// the publisher (F1) and subscriber (F3) need the same mapping.
package subjectxlate

import (
	"strings"

	"github.com/samber/oops"
)

// Legacy prepends events.<gameID>. and replaces ':' with '.'.
// Subjects already prefixed with "events." are returned unchanged.
//
// Mapping:
//
//	"<ns>[:<a>[:<b>[...]]]" → "events.<gameID>.<ns>[.<a>[.<b>[...]]]"
//
// Tokens must satisfy eventbus.NewSubject rules (letters, digits, `_`, `-`).
func Legacy(subject, gameID string) (string, error) {
	if strings.HasPrefix(subject, "events.") {
		return subject, nil
	}
	if gameID == "" {
		return "", oops.Errorf("game id required to translate legacy subject")
	}
	parts := strings.Split(subject, ":")
	for i, p := range parts {
		if p == "" {
			return "", oops.With("subject", subject).
				Errorf("legacy subject has empty token at position %d", i)
		}
	}
	out := make([]string, 0, len(parts)+2)
	out = append(out, "events", gameID)
	out = append(out, parts...)
	return strings.Join(out, "."), nil
}

// ToLegacy reverses Legacy for subjects prefixed with events.<gameID>.
// Subjects without the prefix are returned unchanged (already legacy or
// ambient). The inverse is lossy for subjects carrying wildcards; we
// only exercise it against concrete delivered-event subjects.
func ToLegacy(subject, gameID string) string {
	prefix := "events." + gameID + "."
	if !strings.HasPrefix(subject, prefix) {
		// Some subjects don't carry the game id (legacy or non-prefixed
		// test fixtures); fall back to stripping just "events." and
		// returning the remainder joined by ':'.
		if strings.HasPrefix(subject, "events.") {
			rest := strings.TrimPrefix(subject, "events.")
			// Drop the first token (the game id) if one is present.
			if i := strings.IndexByte(rest, '.'); i >= 0 {
				rest = rest[i+1:]
			}
			return strings.ReplaceAll(rest, ".", ":")
		}
		return subject
	}
	rest := strings.TrimPrefix(subject, prefix)
	return strings.ReplaceAll(rest, ".", ":")
}
