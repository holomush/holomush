// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"log/slog"
	"regexp"
	"sort"

	"github.com/holomush/holomush/internal/access/policy"
)

// seedNamespaceRefPattern matches `(principal|resource).<namespace>.<attr>`
// references in seed policy DSL text. The first capture group is "principal"
// or "resource"; the second is the namespace name. Top-level refs like
// `principal.id` or `resource.id` (no trailing `.<attr>`) are intentionally
// NOT matched — they are injected directly by the resolver and need no
// provider (resolver.go:182-184, 197-199).
var seedNamespaceRefPattern = regexp.MustCompile(`\b(principal|resource)\.([a-z_]+)\.[a-z_]+`)

// validateSeedProviderCoverage walks the SEED CORPUS and identifies any
// `(principal|resource).<ns>.<attr>` references whose namespace is not in the
// set of registered providers. Returns the missing-namespace → affected-seed
// names map (sorted for stable output).
//
// Scope: this validator covers ONLY policies returned by
// `policy.SeedPolicies()` (the host-installed defaults). Plugin-installed
// policies are loaded later via the plugin manager's PolicyInstaller and
// are NOT scanned here — a plugin manifest that gates on `resource.kv.X`
// while no `kv` provider exists hits the SAME silent default-deny class
// this validator was written to surface, but it would not be caught.
// Plugin-policy validation is a separate concern; tracked as a follow-up.
//
// This catches the holomush-g776 / holomush-xxel bug class at construction
// time: when a seed gates on `resource.location.id` but no provider for the
// "location" namespace is registered, the attribute is never populated, and
// every check against that seed silently default-denies. The validator
// surfaces the gap loudly at startup instead of waiting for an e2e symptom.
//
// Returns an empty map when coverage is complete.
func validateSeedProviderCoverage(registered []string, seeds []policy.SeedPolicy) map[string][]string {
	registeredSet := make(map[string]struct{}, len(registered))
	for _, ns := range registered {
		registeredSet[ns] = struct{}{}
	}

	// namespace → set of seed names that reference it
	missing := make(map[string]map[string]struct{})
	for _, seed := range seeds {
		for _, match := range seedNamespaceRefPattern.FindAllStringSubmatch(seed.DSLText, -1) {
			// match[1] = "principal" | "resource"; match[2] = namespace name
			namespace := match[2]
			if _, ok := registeredSet[namespace]; ok {
				continue
			}
			if missing[namespace] == nil {
				missing[namespace] = make(map[string]struct{})
			}
			missing[namespace][seed.Name] = struct{}{}
		}
	}

	out := make(map[string][]string, len(missing))
	for ns, seedSet := range missing {
		names := make([]string, 0, len(seedSet))
		for n := range seedSet {
			names = append(names, n)
		}
		sort.Strings(names)
		out[ns] = names
	}
	return out
}

// warnOnMissingSeedCoverage logs one WARN per uncovered namespace, naming the
// seeds affected. Non-fatal by design — the original holomush-g776 bug shipped
// because the gap was silent at startup; this surfaces it loudly while keeping
// production resilient if a build accidentally omits a provider. A future
// hardening pass MAY upgrade to fail-closed once the seed corpus is stable and
// the test gap (privacytest harness bypasses real ABAC) is closed.
func warnOnMissingSeedCoverage(ctx context.Context, registered []string, seeds []policy.SeedPolicy) {
	missing := validateSeedProviderCoverage(registered, seeds)
	for _, namespace := range sortedKeys(missing) {
		slog.WarnContext(ctx,
			"ABAC setup: seed coverage gap — namespace referenced by seeds but no provider registered (all such seeds silently default-deny)",
			"namespace", namespace,
			"affected_seeds", missing[namespace],
			"reference", "holomush-xxel")
	}
}

// sortedKeys returns the keys of m sorted lexicographically. Used to make
// WARN output deterministic for log-analysis tooling.
func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
