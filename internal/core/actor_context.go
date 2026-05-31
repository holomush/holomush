// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import "context"

type actorContextKey struct{}

type owningPlayerContextKey struct{}

// WithActor returns a child context carrying the host-stamped event actor.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// ActorFromContext returns the host-stamped event actor carried on ctx.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorContextKey{}).(Actor)
	return actor, ok
}

// WithOwningPlayer returns a child context carrying the host-vouched owning
// player ULID of the acting character. It is stamped at command dispatch
// (internal/command/dispatcher.go) from the authenticated executor's player ID
// and is consumed by PLAYER-scope settings ownership so a plugin can only
// read/write the settings of the player that owns the acting character.
//
// The value MUST originate from the host-stamped dispatch context, NEVER from
// plugin- or Lua-supplied arguments — it is the trust anchor the PLAYER-scope
// ownership gate compares the request's principal_id against.
func WithOwningPlayer(ctx context.Context, playerID string) context.Context {
	return context.WithValue(ctx, owningPlayerContextKey{}, playerID)
}

// OwningPlayerFromContext returns the host-vouched owning player ULID carried on
// ctx by WithOwningPlayer. The boolean is false when no owning player was
// stamped (e.g. a pure event-handler dispatch with no player context), in which
// case PLAYER-scope ownership fails closed.
func OwningPlayerFromContext(ctx context.Context) (string, bool) {
	playerID, ok := ctx.Value(owningPlayerContextKey{}).(string)
	return playerID, ok
}
