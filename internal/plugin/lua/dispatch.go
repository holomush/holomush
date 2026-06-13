// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/pkg/errutil"
)

// stampDispatch stamps the host-vouched pluginauthz.DispatchContext onto ctx
// before the Lua state's context is set, so in-VM hostfuncs inherit the
// host-vouched ABAC subject and attributes (INV-PLUGIN-51).
//
// Only character actors are vouched: when the ctx carries an actor with
// Kind == core.ActorCharacter and a non-empty ID, the subject is
// access.CharacterSubject(actor.ID). For any other actor kind (plugin, system,
// none) the ctx is returned unchanged — absence is fail-closed at
// scope-enforcement time.
//
// When a dispatch attribute resolver is wired (WithDispatchAttributeResolver),
// the acting character's attributes (notably "location", backing
// scope:own-location) are resolved and projected onto the dispatch context. A
// resolver error or a nil resolver leaves Attributes nil, which is fail-closed
// at scope-enforcement time: the DSL evaluator treats missing attributes as
// false for every operator.
func (h *Host) stampDispatch(ctx context.Context) context.Context {
	actor, ok := core.ActorFromContext(ctx)
	if !ok || actor.Kind != core.ActorCharacter || actor.ID == "" {
		return ctx
	}
	subject := access.CharacterSubject(actor.ID)

	var attrs map[string]string
	if h.dispatchAttrResolver != nil {
		raw, err := h.dispatchAttrResolver.ResolveSubject(ctx, subject)
		if err != nil {
			// Fail-closed: leave attrs nil so scope-enforcement denies rather
			// than evaluating against a partial bag.
			errutil.LogErrorContext(ctx, "resolve dispatch attributes failed", err, "subject", subject)
		} else {
			attrs = stringAttrs(raw)
		}
	}

	return pluginauthz.WithDispatch(ctx, pluginauthz.DispatchContext{
		Subject:    subject,
		Attributes: attrs,
	})
}

// stringAttrs projects the string-valued keys of a resolved attribute bag into
// a string map suitable for pluginauthz.DispatchContext.Attributes, dropping
// non-string values (bools, slices, ...). It returns nil — never an empty
// map — when the input has no string values, so "no attributes" stays == nil
// and matches the default (no-resolver) behavior.
func stringAttrs(raw map[string]any) map[string]string {
	var out map[string]string
	for k, v := range raw {
		if s, ok := v.(string); ok {
			if out == nil {
				out = make(map[string]string, len(raw))
			}
			out[k] = s
		}
	}
	return out
}
