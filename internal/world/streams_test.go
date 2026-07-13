// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/world"
)

func TestLocationStreamUsesDotRelativeForm(t *testing.T) {
	id := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if got := world.LocationStream(id); got != "location."+id.String() {
		t.Fatalf("got %q, want dot-relative location.<id>", got)
	}
}

func TestCharacterStreamUsesDotRelativeForm(t *testing.T) {
	id := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if got := world.CharacterStream(id); got != "character."+id.String() {
		t.Fatalf("got %q, want dot-relative character.<id>", got)
	}
}

func TestBroadcastLocationStreamUsesDotWildcard(t *testing.T) {
	if got := world.BroadcastLocationStream(); got != "location.*" {
		t.Fatalf("got %q, want location.*", got)
	}
}
