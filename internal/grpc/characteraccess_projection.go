// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// projectPublic is the SOLE constructor of a PublicCharacter (01-SPEC §2.3). No
// handler may assemble a character-shaped message by struct literal, because the
// failure mode is not "a handler redacts wrongly" but "a handler that builds its
// own message never learns that redaction is a thing that happens" — which is
// exactly how a list surface drifts away from the detail surface it was supposed
// to match.
//
// ABSENCE, NEVER EMPTINESS (§8.9, §7.5). A value this viewer may not see and a
// value the character left blank MUST look identical on the wire, so an empty
// attribute is dropped from the map rather than emitted as a present-and-empty
// string; if they differed, the response shape itself would disclose which
// fields exist but are withheld. `name` and `description` are the two the
// character row always carries and they are set unconditionally: reachability is
// name's only gate (§8.8), and it has already been evaluated by the time this
// function runs.
//
// It emits no image in v0.13. `primary_image` stays nil and `gallery` stays
// empty because nothing in v0.13 mints a media identifier (§9.7); the message
// shape ships early so the schema need not change when upload arrives, not as a
// signal that upload is in scope.
func projectPublic(characterID string, desc world.CharacterDescription, profile map[string]string) *characteraccessv1.PublicCharacter {
	out := &characteraccessv1.PublicCharacter{
		Id:          characterID,
		Name:        desc.Name,
		Description: desc.Description,
	}

	for name, value := range profile {
		if value == "" {
			continue
		}
		if out.Profile == nil {
			out.Profile = make(map[string]string, len(profile))
		}
		out.Profile[name] = value
	}

	return out
}
