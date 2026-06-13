// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// stampDispatch stamps the host-vouched pluginauthz.DispatchContext onto ctx
// before plugin code runs, so in-process plugin invocations inherit the
// host-vouched ABAC subject (INV-PLUGIN-51).
//
// Only character actors are vouched: when actor.Kind == core.ActorCharacter
// and actor.ID is non-empty, the subject is access.CharacterSubject(actor.ID).
// For any other actor kind (plugin, system, unset) the ctx is returned
// unchanged — absence is fail-closed at scope-enforcement time.
//
// Attributes (the dispatch location backing scope:own-location) are resolved by
// a follow-up wiring task (holomush-eykuh.3); nil is fail-closed at
// scope-enforcement time, since the DSL evaluator treats missing attributes as
// false for every operator.
func stampDispatch(ctx context.Context, actor core.Actor) context.Context {
	if actor.Kind != core.ActorCharacter || actor.ID == "" {
		return ctx
	}
	return pluginauthz.WithDispatch(ctx, pluginauthz.DispatchContext{
		Subject:    access.CharacterSubject(actor.ID),
		Attributes: nil,
	})
}
