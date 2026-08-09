// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"github.com/holomush/holomush/internal/access/policy/types"
)

// ActionNamespaceSchema returns the schema for the `action` attribute
// namespace: the caller-supplied per-request bag.
//
// THIS FUNCTION IS THE SINGLE SOURCE OF TRUTH FOR THE ACTION KEY SET.
// Its provenance is the audit at
// .planning/phases/02.2-background-job-authorization-model/02.2-ACTION-AUDIT.md,
// which enumerates every action.* reference reachable from the tree with a
// declare / do-not-declare verdict per key. Adding a key here without adding it
// to that audit — or the reverse — is a drift bug, and
// TestActionNamespaceSchemaDeclaresExactlyTheAuditedFiveKeys exists to catch it.
//
// Why the set matters: validateAttributes (compiler.go:149-171) HARD-ERRORS for
// the `action` namespace specifically — an undeclared key referenced by any
// compiled policy fails cache.Reload, which fails BuildABACStack, which fails
// boot. That branch is currently unreachable in production (see
// [Register]'s call site in internal/access/setup/setup.go); phase plan 02.2-04
// wires the compiler to the populated registry and makes it live.
//
// Why every key is a LITERAL dotted string: IsRegistered(namespace, key) is an
// exact map lookup, and the compiler composes its lookup key as
// strings.Join(ref.Path, ".") (compiler.go:157,161). So the DSL reference
// `action.job.trigger_subject` looks up the namespace `action` paired with the
// whole dotted remainder as ONE key. Nesting a `job` sub-namespace here would
// miss that lookup and hard-error at boot.
//
// (Deliberately phrased without quoting the dotted literal: the acceptance gate
// for this file counts quoted declarations, and prose spelling the same token
// would inflate the count and silently defeat it.)
//
// The `action` namespace is registered by a direct [Register] call rather than
// by a provider (D-60): it is a caller-supplied bag, not an entity that gets
// resolved, so a provider whose ResolveSubject/ResolveResource returned
// (nil, nil) purely to carry Schema() would misrepresent what it is.
func ActionNamespaceSchema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			// Resolver-owned. attribute.Resolver stamps bags.Action["name"] =
			// req.Action on EVERY request (resolver.go:185, and :273 on the
			// subject-only path), and it is reserved against caller supply
			// (types.go:108-110). Registration is also a written spec MUST:
			// docs/specs/abac/01-core-types.md:430-435.
			"name": types.AttrTypeString,

			// Host-vouched, and SHIPPED. Referenced by
			// seed:plugin-world-mutation-own-location (seed.go:332), which is
			// the plugin scope fence behind INV-PLUGIN-50/52. Produced by
			// internal/plugin/hostcap/interceptor.go:294 and carried into the
			// request at internal/plugin/pluginauthz/capability.go:50.
			//
			// OMITTING THIS KEY BREAKS BOOT once 02.2-04 lands, because that
			// shipped seed references it. Note also that this key must NOT be
			// added to types.go's reservedActionKeys: the host's own scope
			// fence supplies it through NewAccessRequest, so denylisting it
			// would make every scope-eligible plugin capability call fail with
			// ACCESS_REQUEST_RESERVED_ATTRIBUTE. Declaring in the schema and
			// reserving on the supply side are opposite operations.
			"dispatch_location": types.AttrTypeString,

			// The D-54 event-provenance triple, produced by world.JobCaller
			// (internal/world/caller.go:171,174,177) and reaching the engine
			// through world.Service.checkAccess (service.go:251). Namespaced
			// under `job.` per D-57 so a job can never collide with a
			// resolver-owned action key.
			//
			// trigger_event_type and trigger_subject are already SHIPPED —
			// seed:job-fixture-instance-scoped (seed.go:517) binds both.
			// trigger_event_id is produced but not yet referenced by any seed;
			// it is declared for completeness of the triple, which costs
			// nothing and pre-authorizes the Phase 3 seeds that will bind it.
			"job.trigger_event_id":   types.AttrTypeString,
			"job.trigger_event_type": types.AttrTypeString,
			"job.trigger_subject":    types.AttrTypeString,
		},
	}
}

// NewActionOnlySchemaRegistry returns a fresh [SchemaRegistry] carrying the
// `action` namespace and nothing else.
//
// It exists for policy-compilation sites that have no provider set to draw on —
// the bootstrap seed installer (internal/bootstrap/setup/subsystem.go) and the
// WithRealABAC test harness (internal/testsupport/integrationtest/real_abac.go).
// Handing those sites this registry makes the `action` hard-error branch behave
// IDENTICALLY at every compilation site, which is what lets a typo'd action.*
// reference fail uniformly instead of depending on which code path compiled the
// policy.
//
// The asymmetry is deliberate: provider-owned namespaces are absent, so
// resource.<ns>.<key> references at those sites produce no validation warnings.
// That is correct — those sites genuinely have no providers, and warning about
// namespaces nobody registered would be noise, not signal.
//
// It panics on a registration error because the only way [Register] can fail
// here is a programming error in this file (empty namespace, nil schema, no
// attributes, or an invalid AttrType) — all of which are caught by
// TestActionNamespaceSchemaDeclaresExactlyTheAuditedFiveKeys and its siblings
// before any binary ships.
func NewActionOnlySchemaRegistry() *SchemaRegistry {
	reg := NewSchemaRegistry()
	if err := Register(reg, "action", ActionNamespaceSchema()); err != nil {
		panic("attribute: registering the action namespace on a fresh registry must not fail: " + err.Error())
	}
	return reg
}
