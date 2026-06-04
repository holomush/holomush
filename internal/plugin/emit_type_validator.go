// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"sort"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// hostOwnedEmitTypes lists event-type strings that are host-owned (per
// pkg/plugin/event.go constants) and therefore filtered out of the
// registered set before INV-PLUGIN-32 set-equality comparison. Per INV-M2.
var hostOwnedEmitTypes = map[string]struct{}{
	string(pluginsdk.HostEventTypeSystem):          {},
	string(pluginsdk.HostEventTypeSessionEnded):    {},
	string(pluginsdk.HostEventTypeCommandResponse): {},
	string(pluginsdk.HostEventTypeCommandError):    {},
	string(pluginsdk.HostEventTypeArrive):          {},
	string(pluginsdk.HostEventTypeLeave):           {},
	string(pluginsdk.HostEventTypeMove):            {},
	string(pluginsdk.HostEventTypeLocationState):   {},
	string(pluginsdk.HostEventTypeExitUpdate):      {},
}

// EmitTypeMismatch describes the diff between a plugin's manifest-declared
// crypto.emits set and the SDK-registered emit-type set per INV-PLUGIN-32.
type EmitTypeMismatch struct {
	DeclaredButUnregistered []string
	RegisteredButUndeclared []string
}

// HasMismatch reports whether either direction of the equality check
// surfaced any extras.
func (m EmitTypeMismatch) HasMismatch() bool {
	return len(m.DeclaredButUnregistered) > 0 || len(m.RegisteredButUndeclared) > 0
}

// ValidateEmitTypeSetEquality compares the manifest-declared emit-type
// set against the SDK-registered set (with host-owned types filtered out
// per INV-M2). Per INV-PLUGIN-32, the two sets MUST be equal in both directions.
func ValidateEmitTypeSetEquality(declared, registered []string) EmitTypeMismatch {
	declSet := toEmitSet(declared)
	regSet := toEmitSet(filterHostOwned(registered))

	var mismatch EmitTypeMismatch
	for d := range declSet {
		if _, ok := regSet[d]; !ok {
			mismatch.DeclaredButUnregistered = append(mismatch.DeclaredButUnregistered, d)
		}
	}
	for r := range regSet {
		if _, ok := declSet[r]; !ok {
			mismatch.RegisteredButUndeclared = append(mismatch.RegisteredButUndeclared, r)
		}
	}
	sort.Strings(mismatch.DeclaredButUnregistered)
	sort.Strings(mismatch.RegisteredButUndeclared)
	return mismatch
}

// filterHostOwned removes host-owned event types from the registered
// set before INV-PLUGIN-32 comparison. Per INV-M2 — substrate filters; the
// SDK + hostfunc surface accepts any string (plugins MAY register host-
// owned types; the validator MUST NOT count them as plugin-owned).
func filterHostOwned(registered []string) []string {
	out := make([]string, 0, len(registered))
	for _, r := range registered {
		if _, host := hostOwnedEmitTypes[r]; !host {
			out = append(out, r)
		}
	}
	return out
}

func toEmitSet(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}
