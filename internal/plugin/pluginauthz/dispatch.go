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
