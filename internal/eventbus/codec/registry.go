// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec

import (
	"fmt"
	"sync"
)

// registry holds all host-known codecs. Closed enumeration — plugins
// cannot register codecs. The variable is package-private; access via
// Resolve / All / RegisterForTest.
var (
	regMu    sync.RWMutex
	registry = map[Name]Codec{
		NameIdentity: IdentityCodec{},
	}
)

// Resolve returns the codec for the given name, or error if unknown.
// Hard-fails on unknown names — callers MUST NOT silently fall back to
// identity.
func Resolve(name Name) (Codec, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	c, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("codec: unknown name %q", name)
	}
	return c, nil
}

// All returns a copy of all registered codec names. Used by the meta-test
// to assert const ↔ registry sync.
func All() []Name {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Name, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	return out
}

// RegisterForTest installs a codec at runtime. Production code MUST NOT
// call this — it is intended for tests that exercise a custom codec
// (e.g., a stub encrypt/decrypt for property tests). Returns a cleanup
// func that restores the prior state.
func RegisterForTest(c Codec) func() {
	regMu.Lock()
	prev, hadPrev := registry[c.Name()]
	registry[c.Name()] = c
	regMu.Unlock()
	return func() {
		regMu.Lock()
		defer regMu.Unlock()
		if hadPrev {
			registry[c.Name()] = prev
		} else {
			delete(registry, c.Name())
		}
	}
}
