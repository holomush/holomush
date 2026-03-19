// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import "github.com/oklog/ulid/v2"

// CharacterRef is a lightweight reference to a character, used by engine
// methods to avoid passing loose primitives (charID, charName, locationID).
// This type lives in core to avoid circular imports with the world package.
// world.Character can convert to CharacterRef at package boundaries.
type CharacterRef struct {
	ID         ulid.ULID
	Name       string
	LocationID ulid.ULID
}

// String returns the character's display name.
func (c CharacterRef) String() string {
	return c.Name
}
