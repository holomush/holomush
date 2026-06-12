// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package luabridge

import (
	lua "github.com/yuin/gopher-lua"
	"google.golang.org/grpc"
)

// RegisterHostCaps injects the generated host-capability Lua tables for the
// capability tokens that the named plugin declared in its manifest. It is the
// declaration gate: only tokens listed in declaredCaps receive a global — a
// token the plugin did not declare is never injected (INV-PLUGIN-44/45).
//
// Token lookup is against registeredHostCapBindings (bindings_gen.go). A token
// with no entry (no host-cap binding for it — e.g. it is a non-host capability
// token) is silently skipped; the manifest validator already ensured it is
// valid.
//
// Coexistence (spec §5): if a global named after the token is already set on L
// (e.g. the legacy hostfunc cap_*.go path already injected it), the bridge
// skips it without overwriting. This prevents double-injection when the legacy
// shim and the bridge would both set the same global name (e.g. "session" is set
// by both cap_session.go and registerSessionService). The first writer wins;
// the bridge is a no-op on pre-populated globals.
//
// pluginName is passed through to the registrar for potential per-plugin
// scoping (reserved for future use; current generated registrars ignore it).
func RegisterHostCaps(L *lua.LState, conn grpc.ClientConnInterface, pluginName string, declaredCaps []string) { //nolint:gocritic // L is the idiomatic gopher-lua name
	for _, token := range declaredCaps {
		registrar, ok := registeredHostCapBindings[token]
		if !ok {
			// No binding for this token — it may be a non-host capability
			// (e.g. a service dependency) or the vocabulary has not yet grown
			// to cover it. Silently skip.
			continue
		}
		// Defensive no-clobber: if the global is already set by the legacy
		// hostfunc shim (e.g. cap_session.go sets "session"), do not overwrite
		// it. The legacy surface wins for production plugins on the old path;
		// test-fixture plugins that opt into the bridge should not also be
		// wired through the legacy cap_*.go for the same token.
		if L.GetGlobal(token) != lua.LNil {
			continue
		}
		registrar(L, conn, pluginName)
	}
}
