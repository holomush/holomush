// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/dsl"
)

// PolicyValidationContext carries the plugin's declared boundaries for policy
// validation. These values come from the plugin manifest.
type PolicyValidationContext struct {
	PluginName     string
	ResourceTypes  []string
	CommandNames   []string
	TrustEscalated bool
}

// ValidatePluginPolicy checks that a manifest policy respects the plugin trust
// boundary. It parses the DSL, inspects the principal/resource types, and
// enforces:
//   - principal "plugin" → always allowed
//   - principal "system" → always rejected
//   - principal "character" + resource in plugin's resource_types → allowed
//   - principal "character" + resource "command" → allowed only for plugin's own commands
//   - principal "character" + protected core type → rejected (unless trust-escalated)
//   - principal "character" + unrecognized type → rejected (unless trust-escalated)
//   - wildcard principal (no "is X") → rejected unless trust-escalated
func ValidatePluginPolicy(ctx PolicyValidationContext, mp ManifestPolicy) error {
	parsed, err := dsl.Parse(mp.DSL)
	if err != nil {
		return oops.In("policy").With("policy", mp.Name).Wrapf(err, "invalid DSL")
	}

	// Validate principal scope — ensure the policy doesn't reference other plugins.
	if ok, foreign := dsl.ValidatePrincipalScope(parsed, ctx.PluginName); !ok {
		return oops.In("policy").With("policy", mp.Name).With("foreign_name", foreign).
			Errorf("policy references foreign principal %q", foreign)
	}

	// Normalize the parsed target. The DSL allows policies like
	// `permit(principal, action in [...], resource);` where Principal/Resource
	// have empty type fields, and even malformed policies could leave clauses
	// nil. Reading .Type unconditionally would panic on nil.
	if parsed.Target == nil {
		return oops.In("policy").With("policy", mp.Name).With("plugin", ctx.PluginName).
			Errorf("policy is missing target clause")
	}
	var principalType, resourceType string
	if parsed.Target.Principal != nil {
		principalType = parsed.Target.Principal.Type
	}
	if parsed.Target.Resource != nil {
		resourceType = parsed.Target.Resource.Type
	}

	switch principalType {
	case "plugin":
		slog.Info("policy targets plugin principal — allowed",
			"plugin", ctx.PluginName, "policy", mp.Name)
		return nil

	case "system":
		return oops.In("policy").With("policy", mp.Name).With("plugin", ctx.PluginName).
			Errorf("plugins must not install policies for principal type \"system\"")

	case "character":
		return validateCharacterPolicy(ctx, mp.Name, parsed, resourceType)

	case "":
		// Unconstrained principal (`permit(principal, ...)`) — broader than
		// `principal is character`. Requires trust escalation; otherwise it
		// would defeat the trust gate by letting wildcard policies through.
		if !ctx.TrustEscalated {
			return oops.In("policy").With("policy", mp.Name).With("plugin", ctx.PluginName).
				Errorf("policy with unconstrained principal requires trust escalation")
		}
		return validateCharacterPolicy(ctx, mp.Name, parsed, resourceType)

	default:
		return oops.In("policy").With("policy", mp.Name).With("principal_type", principalType).
			Errorf("unrecognized principal type %q", principalType)
	}
}

// validateCharacterPolicy enforces trust boundary rules for policies that
// target character principals.
func validateCharacterPolicy(ctx PolicyValidationContext, policyName string, parsed *dsl.Policy, resourceType string) error {
	// Trust-escalated plugins bypass resource type restrictions.
	if ctx.TrustEscalated {
		slog.Warn("trust-escalated plugin installing character policy on potentially protected resource",
			"plugin", ctx.PluginName, "policy", policyName, "resource_type", resourceType)
		return nil
	}

	// "command" resource type requires additional checks on command names.
	if resourceType == "command" {
		return validateCommandPolicy(ctx, policyName, parsed)
	}

	// Resource type in plugin's declared resource_types → allowed.
	if resourceType != "" && isInSlice(resourceType, ctx.ResourceTypes) {
		return nil
	}

	// Protected core types → rejected.
	if ProtectedResourceTypes[resourceType] {
		return oops.In("policy").With("policy", policyName).With("plugin", ctx.PluginName).
			With("resource_type", resourceType).
			Errorf("resource type %q is protected — plugins cannot install character policies on core types", resourceType)
	}

	// Unrecognized type not in resource_types → rejected.
	if resourceType != "" {
		return oops.In("policy").With("policy", policyName).With("plugin", ctx.PluginName).
			With("resource_type", resourceType).
			Errorf("resource type %q is not in plugin's resource_types", resourceType)
	}

	// Wildcard resource (no type constraint) — reject without trust escalation.
	return oops.In("policy").With("policy", policyName).With("plugin", ctx.PluginName).
		Errorf("character policy with unconstrained resource requires trust escalation")
}

// validateCommandPolicy checks that a character+command policy only references
// the plugin's own command names.
func validateCommandPolicy(ctx PolicyValidationContext, policyName string, parsed *dsl.Policy) error {
	cmdNames := dsl.ExtractCommandNames(parsed.Conditions)

	// No command name conditions — the policy applies to all commands.
	// That's only allowed with trust escalation, which we've already checked.
	if len(cmdNames) == 0 {
		return oops.In("policy").With("policy", policyName).With("plugin", ctx.PluginName).
			Errorf("command policy without command name condition requires trust escalation")
	}

	for _, name := range cmdNames {
		if !isInSlice(name, ctx.CommandNames) {
			return oops.In("policy").With("policy", policyName).With("plugin", ctx.PluginName).
				With("command", name).
				Errorf("policy references foreign command %q — plugins can only target their own commands", name)
		}
	}

	return nil
}

func isInSlice(needle string, haystack []string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
