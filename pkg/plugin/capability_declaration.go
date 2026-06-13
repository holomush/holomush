// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import "github.com/samber/oops"

// capabilityRequirement pairs a predicate reporting whether a provider opts into
// a host capability (by implementing its *Aware interface) with the non-exempt
// capability tokens that opt-in requires the manifest to declare. Exempt
// capabilities (emit, command-registry) carry an empty tokens slice: opting in
// is allowed with no declaration because they are self-gated (emit fence /
// host-vouched dispatch subject), matching hostcap.declarationExemptCapabilities.
type capabilityRequirement struct {
	awareName  string         // *Aware interface name, for error messages
	implements func(any) bool // does the provider implement the interface?
	tokens     []string       // non-exempt capability tokens it grants
}

// hostCapabilityRequirements is the single source of truth mapping each
// host-capability *Aware interface to the capability tokens a provider
// implementing it MUST declare in its manifest requires: (INV-PLUGIN-54).
// FocusClientAware grants BOTH focus and stream.history (one interface backed by
// FocusServiceClient + StreamHistoryServiceClient; pkg/plugin/focus_client.go).
var hostCapabilityRequirements = []capabilityRequirement{
	{"EventSinkAware", func(p any) bool { _, ok := p.(EventSinkAware); return ok }, nil}, // emit: exempt
	{"FocusClientAware", func(p any) bool { _, ok := p.(FocusClientAware); return ok }, []string{"focus", "stream.history"}},
	{"HostEvaluatorAware", func(p any) bool { _, ok := p.(HostEvaluatorAware); return ok }, []string{"eval"}},
	{"SettingsClientAware", func(p any) bool { _, ok := p.(SettingsClientAware); return ok }, []string{"settings"}},
	{"SnapshotDecryptorAware", func(p any) bool { _, ok := p.(SnapshotDecryptorAware); return ok }, []string{"audit"}},
	{"CommandListerAware", func(p any) bool { _, ok := p.(CommandListerAware); return ok }, nil}, // command-registry: exempt
}

// validateDeclaredCapabilities returns a CAPABILITY_NOT_DECLARED error when the
// provider implements a host-capability *Aware interface for a non-exempt
// capability token absent from declared. Fail-closed: any undeclared token fails
// plugin Init (and thus load), the host-side load-time half of INV-PLUGIN-54.
func validateDeclaredCapabilities(provider any, declared []string) error {
	declaredSet := make(map[string]bool, len(declared))
	for _, c := range declared {
		declaredSet[c] = true
	}
	for _, req := range hostCapabilityRequirements {
		if !req.implements(provider) {
			continue
		}
		for _, tok := range req.tokens {
			if !declaredSet[tok] {
				return oops.Code("CAPABILITY_NOT_DECLARED").
					With("capability", tok).
					With("aware_interface", req.awareName).
					Errorf("plugin implements %s but did not declare capability %q in its manifest requires:", req.awareName, tok)
			}
		}
	}
	return nil
}
