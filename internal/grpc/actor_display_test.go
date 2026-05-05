// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
)

type stubIDReg struct{ namesByID map[ulid.ULID]string }

func (s *stubIDReg) NameByID(id ulid.ULID) (string, bool) {
	name, ok := s.namesByID[id]
	return name, ok
}
func (s *stubIDReg) IDByName(string) (ulid.ULID, bool) { return ulid.ULID{}, false }

func TestActorIDStringResolvesPluginNameViaRegistry(t *testing.T) {
	pluginID := core.NewULID() // fresh ULID; stub maps it to a plugin name
	reg := &stubIDReg{namesByID: map[ulid.ULID]string{pluginID: "core-scenes"}}

	got := actorIDString(eventbus.Actor{Kind: eventbus.ActorKindPlugin, ID: pluginID}, reg)
	assert.Equal(t, "core-scenes", got)
}

func TestActorIDStringResolvesSystemSentinelViaRegistry(t *testing.T) {
	reg := &stubIDReg{namesByID: map[ulid.ULID]string{
		core.SystemActorULID: "system",
	}}

	got := actorIDString(eventbus.Actor{Kind: eventbus.ActorKindSystem, ID: core.SystemActorULID}, reg)
	assert.Equal(t, "system", got)
}

func TestActorIDStringFallsBackToULIDStringForCharacter(t *testing.T) {
	charID := core.NewULID()
	reg := &stubIDReg{namesByID: nil} // character ULIDs not in registry

	got := actorIDString(eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: charID}, reg)
	assert.Equal(t, charID.String(), got)
}

func TestActorIDStringPreservesZeroULIDGuard(t *testing.T) {
	reg := &stubIDReg{}
	got := actorIDString(eventbus.Actor{Kind: eventbus.ActorKindUnknown}, reg)
	assert.Equal(t, "", got, "zero ULID MUST stringify to empty per existing wire contract")
}
