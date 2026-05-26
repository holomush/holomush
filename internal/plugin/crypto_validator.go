// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"strings"

	"github.com/samber/oops"
)

// ValidateCrypto enforces the manifest crypto.emits / crypto.consumes
// rules from spec §7.1. Caller MUST invoke after parsing the manifest
// and before adding it to the loader registry.
//
// Returns nil for manifests without a crypto section.
func ValidateCrypto(m *Manifest) error {
	if m.Crypto == nil {
		return nil
	}

	// Rule 1 & 2: every Sensitivity value is one of the closed enum;
	// event_type is non-empty and unique within this manifest.
	seenEmit := make(map[string]bool, len(m.Crypto.Emits))
	for i := range m.Crypto.Emits {
		e := &m.Crypto.Emits[i]
		// Normalize the stored value: trim whitespace so empty/duplicate
		// checks fire correctly AND so ResolveCryptoRefs (which compares
		// emits[i].EventType verbatim against the parsed ref) sees the
		// same canonical form. Without writing back, " whisper " would
		// pass validation but fail lookup.
		e.EventType = strings.TrimSpace(e.EventType)
		eventType := e.EventType
		if !validSensitivity(e.Sensitivity) {
			return oops.Code("PLUGIN_CRYPTO_INVALID_SENSITIVITY").
				With("plugin", m.Name).
				With("event_type", e.EventType).
				With("sensitivity", string(e.Sensitivity)).
				With("emits_index", i).
				Errorf("invalid sensitivity value (must be always|may|never)")
		}
		if eventType == "" {
			return oops.Code("PLUGIN_CRYPTO_EMPTY_EVENT_TYPE").
				With("plugin", m.Name).
				With("emits_index", i).
				Errorf("crypto.emits entry has empty event_type")
		}
		if seenEmit[eventType] {
			return oops.Code("PLUGIN_CRYPTO_DUPLICATE_EMIT").
				With("plugin", m.Name).
				With("event_type", eventType).
				Errorf("crypto.emits has duplicate event_type")
		}
		seenEmit[eventType] = true
		if e.Readback && e.Sensitivity == SensitivityNever {
			return oops.Code("PLUGIN_CRYPTO_READBACK_ON_NEVER").
				With("plugin", m.Name).
				With("event_type", e.EventType).
				With("emits_index", i).
				Errorf("readback:true is invalid on a sensitivity:never type")
		}
	}

	// Rule 3: requests_decryption is well-formed.
	for ci, c := range m.Crypto.Consumes {
		for ri, ref := range c.RequestsDecryption {
			// Rule 3a: no NATS wildcards anywhere in the ref. Bare "*", or
			// "*" / ">" in any position, are all rejected — qualified
			// <plugin>:<event_type> is a literal pair, not a subject pattern.
			if strings.ContainsRune(ref, '*') || strings.ContainsRune(ref, '>') {
				return oops.Code("PLUGIN_CRYPTO_WILDCARD_DECRYPT").
					With("plugin", m.Name).
					With("consumes_index", ci).
					With("decryption_index", ri).
					With("ref", ref).
					Errorf("requests_decryption MUST NOT contain wildcards; enumerate event types")
			}
			// Rule 3b: must be qualified <plugin>:<event_type>.
			pluginName, _, ok := splitQualifiedRef(ref)
			if !ok {
				return oops.Code("PLUGIN_CRYPTO_UNQUALIFIED_REF").
					With("plugin", m.Name).
					With("ref", ref).
					Errorf("requests_decryption ref MUST be <plugin>:<event_type>")
			}
			// Rule 3c: pluginName must be either this plugin (self) or in dependencies.
			if pluginName != m.Name {
				if _, dep := m.Dependencies[pluginName]; !dep {
					return oops.Code("PLUGIN_CRYPTO_REF_NOT_REQUIRED").
						With("plugin", m.Name).
						With("ref_plugin", pluginName).
						With("ref", ref).
						Errorf("requests_decryption references plugin %q not listed in dependencies", pluginName)
				}
			}
		}
	}

	return nil
}

func validSensitivity(s Sensitivity) bool {
	switch s {
	case SensitivityAlways, SensitivityMay, SensitivityNever:
		return true
	}
	return false
}

// ResolveCryptoRefs verifies every requests_decryption reference points
// at a plugin in the registry whose manifest declares the named event
// type with sensitivity in {always, may}. Caller is the loader, after
// all manifests in this load batch have been parsed.
//
// Caller MUST have validated m with ValidateCrypto first; ResolveCryptoRefs
// assumes every requests_decryption ref is well-formed (qualified
// <plugin>:<event_type>). With an unvalidated manifest, malformed refs
// will surface as PLUGIN_CRYPTO_REF_PLUGIN_NOT_LOADED rather than a
// dedicated error code.
//
// registry maps plugin name → that plugin's emit declarations. The
// loader populates this from the manifests it has already accepted.
// Self-references (m.Name → its own emits) are resolved against m
// directly, so a plugin's manifest can request decryption for its
// own emitted event types without listing itself in the registry.
func ResolveCryptoRefs(m *Manifest, registry map[string][]CryptoEmit) error {
	if m.Crypto == nil {
		return nil
	}
	for ci, c := range m.Crypto.Consumes {
		for ri, ref := range c.RequestsDecryption {
			pluginName, eventType, _ := splitQualifiedRef(ref)
			emits := m.Crypto.Emits
			if pluginName != m.Name {
				e, ok := registry[pluginName]
				if !ok {
					return oops.Code("PLUGIN_CRYPTO_REF_PLUGIN_NOT_LOADED").
						With("plugin", m.Name).
						With("ref_plugin", pluginName).
						With("ref", ref).
						With("consumes_index", ci).
						With("decryption_index", ri).
						Errorf("requests_decryption references plugin not yet loaded")
				}
				emits = e
			}
			var found *CryptoEmit
			for i := range emits {
				if emits[i].EventType == eventType {
					found = &emits[i]
					break
				}
			}
			if found == nil {
				return oops.Code("PLUGIN_CRYPTO_UNKNOWN_EVENT_REF").
					With("plugin", m.Name).
					With("ref", ref).
					Errorf("requests_decryption references event type not declared by referenced plugin")
			}
			if found.Sensitivity == SensitivityNever {
				return oops.Code("PLUGIN_CRYPTO_REF_NEVER_SENSITIVE").
					With("plugin", m.Name).
					With("ref", ref).
					Errorf("requests_decryption MUST NOT reference SensitivityNever event types")
			}
		}
	}
	return nil
}

// splitQualifiedRef parses "<plugin>:<event_type>" into its components.
// Returns ok=false if the form does not match.
func splitQualifiedRef(ref string) (pluginName, eventType string, ok bool) {
	colon := strings.IndexByte(ref, ':')
	if colon <= 0 || colon == len(ref)-1 {
		return "", "", false
	}
	pluginName = ref[:colon]
	eventType = ref[colon+1:]
	if pluginName == "" || eventType == "" {
		return "", "", false
	}
	return pluginName, eventType, true
}
