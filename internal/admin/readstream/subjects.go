// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream

import (
	"strings"

	"github.com/holomush/holomush/internal/eventbus"
)

// subjectFor builds a single NATS subject for a context reference.
// Format: "events.<gameID>.<type>.<id1>[.<id2>...].>"
//
// MUST use dot-form subjects only. The legacy colon-translation shim
// (eventbus/subjectxlate) has been deleted — dot is the sole canonical form.
func subjectFor(c ContextRef, gameID string) eventbus.Subject {
	// Capacity: "events" + gameID + type + len(IDs) + ">" = 4 + len(IDs) tokens.
	parts := make([]string, 0, 3+len(c.IDs)+1)
	parts = append(parts, "events", gameID, c.Type)
	parts = append(parts, c.IDs...)
	parts = append(parts, ">")
	return eventbus.Subject(strings.Join(parts, "."))
}

// BuildSubjects maps a slice of ContextRef values to their NATS subjects.
//
// Empty (nil or zero-length) contexts → exactly one wildcard subject
// "events.<gameID>.>" which selects all events for the game.
//
// The output preserves the input order. MUST NOT mutate the input slice.
//
// Subject format: "events.<gameID>.<type>.<id1>[.<id2>...].>"
func BuildSubjects(contexts []ContextRef, gameID string) []eventbus.Subject {
	if len(contexts) == 0 {
		return []eventbus.Subject{eventbus.Subject("events." + gameID + ".>")}
	}
	out := make([]eventbus.Subject, len(contexts))
	for i, c := range contexts {
		out[i] = subjectFor(c, gameID)
	}
	return out
}
