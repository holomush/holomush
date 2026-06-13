// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz

import "context"

// DispatchContext carries the host-vouched ABAC subject and host-resolved
// acting-character attributes for one command/event delivery (INV-PLUGIN-51).
type DispatchContext struct {
	Subject    string            // host-vouched ABAC subject (access.CharacterSubject)
	Attributes map[string]string // host-resolved acting-character attributes (location, ...)
}

// AttributeResolver resolves host-vouched dispatch attributes for an ABAC
// subject (e.g. the acting character's "location") at delivery time. It is
// satisfied structurally by the production attribute resolver
// (access/policy/attribute.Resolver, via ResolveSubject) and by
// access/policy/attribute.CharacterProvider. The plugin hosts use it to
// populate DispatchContext.Attributes (holomush-eykuh.3); a nil resolver leaves
// Attributes nil, which is fail-closed at scope-enforcement time (the DSL
// evaluator treats missing attributes as false for every operator).
//
// pluginauthz MUST NOT import the attribute package — the dependency is one-way
// (the resolver satisfies this interface, not the other way around).
type AttributeResolver interface {
	ResolveSubject(ctx context.Context, subject string) (map[string]any, error)
}

type dispatchKey struct{}

// WithDispatch stamps the host-vouched dispatch context. Host-only; called from
// DeliverCommand/DeliverEvent before any plugin code runs (INV-PLUGIN-51).
func WithDispatch(ctx context.Context, dc DispatchContext) context.Context {
	return context.WithValue(ctx, dispatchKey{}, dc)
}

// DispatchForHost reads the dispatch context. Exported for the host-side
// readers in other packages (hostcap interceptor, command-registry servers,
// Lua hostfuncs). Plugins are not Go callers of this package and cannot set
// the key (only WithDispatch does), so exporting the reader is safe.
func DispatchForHost(ctx context.Context) (DispatchContext, bool) {
	dc, ok := ctx.Value(dispatchKey{}).(DispatchContext)
	return dc, ok
}
