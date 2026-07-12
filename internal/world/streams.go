// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import "github.com/oklog/ulid/v2"

// Domain-relative stream-name builders. The host qualifier (eventbus.Qualify)
// prepends "events.<gameID>." to produce the fully-qualified subject; these
// builders return the relative reference only. They are consumed by the gRPC
// subscribe/focus layer (internal/grpc) to name the streams a session follows —
// they are NOT an emit path. The post-commit emit path (events.go) was deleted in
// 05-06 (D-03); these builders were relocated here so they survive that deletion.
const (
	streamDomainLocation  = "location."
	streamDomainCharacter = "character."
)

// LocationStream returns the domain-relative stream reference for a location.
func LocationStream(id ulid.ULID) string {
	return streamDomainLocation + id.String()
}

// CharacterStream returns the domain-relative stream reference for a character.
func CharacterStream(id ulid.ULID) string {
	return streamDomainCharacter + id.String()
}

// BroadcastLocationStream returns the relative reference matching all locations.
func BroadcastLocationStream() string {
	return streamDomainLocation + "*"
}
