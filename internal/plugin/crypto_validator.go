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
	for i, e := range m.Crypto.Emits {
		if !validSensitivity(e.Sensitivity) {
			return oops.Code("PLUGIN_CRYPTO_INVALID_SENSITIVITY").
				With("plugin", m.Name).
				With("event_type", e.EventType).
				With("sensitivity", string(e.Sensitivity)).
				With("emits_index", i).
				Errorf("invalid sensitivity value (must be always|may|never)")
		}
		if e.EventType == "" {
			return oops.Code("PLUGIN_CRYPTO_EMPTY_EVENT_TYPE").
				With("plugin", m.Name).
				With("emits_index", i).
				Errorf("crypto.emits entry has empty event_type")
		}
		if seenEmit[e.EventType] {
			return oops.Code("PLUGIN_CRYPTO_DUPLICATE_EMIT").
				With("plugin", m.Name).
				With("event_type", e.EventType).
				Errorf("crypto.emits has duplicate event_type")
		}
		seenEmit[e.EventType] = true
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
			pluginName, eventType, ok := splitQualifiedRef(ref)
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
			_ = eventType // resolution against the referenced plugin's emits happens at loader-level (Task 4)
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
