// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

// CapabilityVocabulary is the controlled set of valid host-capability names a
// manifest may reference via `requires: [{capability: <name>}]` (spec §1). It
// holds the full taxonomy (spec §1); each token backs one
// `holomush.plugin.host.v1` service.
type CapabilityVocabulary struct {
	names map[string]struct{}
}

// NewCapabilityVocabulary returns an empty vocabulary.
func NewCapabilityVocabulary() *CapabilityVocabulary {
	return &CapabilityVocabulary{names: make(map[string]struct{})}
}

// Register adds a capability name to the vocabulary.
func (v *CapabilityVocabulary) Register(name string) { v.names[name] = struct{}{} }

// Has reports whether name is a registered capability.
func (v *CapabilityVocabulary) Has(name string) bool {
	_, ok := v.names[name]
	return ok
}

// CapabilityServiceNames is the single-source mapping from each host-capability
// token to the bare name of the holomush.plugin.host.v1 service that backs it
// (spec §1: each token maps to exactly one capability-scoped service). It is the
// canonical token↔service table — both the capability vocabulary
// (DefaultCapabilityVocabulary) and the Lua-binding generator
// (internal/plugin/luabridge/gen) read it, so the two cannot drift. Service
// names are bare (no "holomush.plugin.host.v1." prefix); the generator composes
// the fully-qualified name and the New<Svc>ServiceClient constructor from this.
var CapabilityServiceNames = map[string]string{
	"world.query":         "WorldQueryService",
	"world.mutation":      "WorldMutationService",
	"property":            "PropertyService",
	"session":             "SessionService",
	"session.admin":       "SessionAdminService",
	"focus":               "FocusService",
	"eval":                "EvalService",
	"emit":                "EmitService",
	"settings":            "SettingsService",
	"kv":                  "KVService",
	"stream.history":      "StreamHistoryService",
	"stream.subscription": "StreamSubscriptionService",
	"audit":               "AuditService",
	"command-registry":    "CommandRegistryService",
}

// DefaultCapabilityVocabulary returns the full host-capability taxonomy
// (sub-spec 2, spec §1). Each name maps to exactly one capability-scoped service
// in holomush.plugin.host.v1 (see CapabilityServiceNames). Ambient substrate
// (log, new_request_id, stdlib, config) is intentionally absent — it is not a
// capability (spec §4).
func DefaultCapabilityVocabulary() *CapabilityVocabulary {
	v := NewCapabilityVocabulary()
	for name := range CapabilityServiceNames {
		v.Register(name)
	}
	return v
}
