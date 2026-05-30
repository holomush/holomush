// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"strings"

	"github.com/samber/oops"
)

// Qualify turns a domain-relative stream reference (e.g. "location.01ABC",
// "character.01XYZ", "global") into a fully-qualified JetStream subject by
// prepending "events.<gameID>.". References already starting with "events."
// are returned unchanged (idempotent), so gameID is not consulted on that path;
// gameID MUST be non-empty for any relative reference that still needs it.
//
// This is the single host-side gameID-injection point introduced by
// holomush-rops; it replaces subjectxlate.Legacy. Producers and clients, which
// lack the gameID, emit relative references; Qualify is applied at the emit and
// read-entry boundaries (spec §1 "where gameID enters").
func Qualify(gameID, relativeRef string) (Subject, error) {
	if relativeRef == "" {
		return "", oops.Errorf("empty stream reference")
	}
	if strings.HasPrefix(relativeRef, "events.") {
		return NewSubject(relativeRef)
	}
	if gameID == "" {
		return "", oops.With("ref", relativeRef).Errorf("game id required to qualify stream reference")
	}
	return NewSubject("events." + gameID + "." + relativeRef)
}
