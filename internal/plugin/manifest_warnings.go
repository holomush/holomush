// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"fmt"

	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// parsedManifestPolicy pairs a manifest policy name with its parsed DSL AST.
type parsedManifestPolicy struct {
	name   string
	policy *dsl.Policy
}

// CheckManifestWarnings logs non-fatal warnings about potential policy coverage
// gaps in a plugin manifest. Called during loadPlugin after policy installation.
//
// The schemas parameter is retained for API compatibility with the caller in
// loadPlugin but is no longer consulted: policy/schema cross-validation has
// been promoted to a hard error path in ValidateManifestPolicySchemas. Only
// command coverage warnings (Warning 1, Warning 2) remain here as soft hints.
//
// Returns a slice of human-readable warning messages (never nil for easy length check).
func CheckManifestWarnings(manifest *Manifest, _ map[string]*types.NamespaceSchema) []string {
	var warnings []string

	// Parse all policies up front; skip unparseable ones silently — the
	// policy validator has already rejected hard errors before we get here.
	parsed := make([]parsedManifestPolicy, 0, len(manifest.Policies))
	for _, mp := range manifest.Policies {
		p, err := dsl.Parse(mp.DSL)
		if err != nil {
			continue
		}
		parsed = append(parsed, parsedManifestPolicy{name: mp.Name, policy: p})
	}

	// Warning 1: command with no execute policy covering its name.
	for i := range manifest.Commands {
		cmd := &manifest.Commands[i]
		if !hasExecutePolicyForCommand(parsed, cmd.Name) {
			warnings = append(warnings, fmt.Sprintf(
				"plugin %q: command %q has no policy that permits execute on resource is command with matching name",
				manifest.Name, cmd.Name,
			))
		}
	}

	// Warning 2: command with capabilities on a resource type but no permit
	// policy covering that (resource_type, action) pair for character principals.
	// Keying only by resource type masks missing per-action coverage; e.g. a
	// "write" capability is not covered by a "read" policy on the same type.
	for i := range manifest.Commands {
		cmd := &manifest.Commands[i]
		seen := make(map[string]bool)
		for _, cap := range cmd.Capabilities {
			rt := cap.Resource
			act := cap.Action
			if rt == "" || act == "" {
				continue
			}
			key := rt + "|" + act
			if seen[key] {
				continue
			}
			seen[key] = true
			if !hasCapabilityPolicy(parsed, rt, act) {
				warnings = append(warnings, fmt.Sprintf(
					"plugin %q: command %q declares capability %q on resource type %q but no character permit policy covers it",
					manifest.Name, cmd.Name, act, rt,
				))
			}
		}
	}

	return warnings
}

// hasExecutePolicyForCommand returns true when at least one parsed policy:
//   - has effect "permit"
//   - targets character principals (or wildcard principals)
//   - has action "execute" (or no action restriction, i.e. wildcard)
//   - has resource is "command"
//   - has a condition that matches the command name, OR has no name-restricting condition
//
// A `principal is plugin` execute policy does NOT cover character commands;
// the dispatcher checks character principals at runtime.
func hasExecutePolicyForCommand(policies []parsedManifestPolicy, cmdName string) bool {
	for _, pp := range policies {
		p := pp.policy
		if p.Effect != "permit" {
			continue
		}
		if p.Target == nil {
			continue
		}
		// Check principal type — only character or wildcard principals cover
		// character commands. Plugin/system principals don't.
		if !principalCoversCharacter(p.Target.Principal) {
			continue
		}
		// Check action covers "execute".
		if !actionCoversExecute(p.Target.Action) {
			continue
		}
		// Check resource is "command".
		if p.Target.Resource == nil || p.Target.Resource.Type != "command" {
			continue
		}
		// If there are no conditions, the policy is unconstrained — it covers
		// all commands. Note: len(nil) == 0, so this also handles the case
		// where ExtractCommandNames returns a non-nil empty slice.
		if p.Conditions == nil {
			return true
		}
		names := dsl.ExtractCommandNames(p.Conditions)
		if len(names) == 0 {
			// Conditions exist but don't restrict by command name — covers all.
			return true
		}
		for _, n := range names {
			if n == cmdName {
				return true
			}
		}
	}
	return false
}

// principalCoversCharacter returns true when the principal clause matches
// character principals: explicitly "character" or unconstrained (wildcard).
// Plugin and system principals are NOT covered.
func principalCoversCharacter(pc *dsl.PrincipalClause) bool {
	if pc == nil {
		return true // wildcard
	}
	return pc.Type == "" || pc.Type == "character"
}

// actionCoversExecute returns true when the action clause is a wildcard (no
// restriction) or explicitly includes "execute".
func actionCoversExecute(ac *dsl.ActionClause) bool {
	if ac == nil {
		return true
	}
	if len(ac.Actions) == 0 {
		return true // wildcard
	}
	for _, a := range ac.Actions {
		if a == "execute" {
			return true
		}
	}
	return false
}

// hasCapabilityPolicy returns true when at least one parsed policy permits
// the given (resource_type, action) for character principals. The policy must
// be a permit (not forbid), target character or wildcard principal, cover the
// action (or be action-wildcard), and target the given resource type.
func hasCapabilityPolicy(policies []parsedManifestPolicy, resourceType, action string) bool {
	for _, pp := range policies {
		p := pp.policy
		if p.Effect != "permit" {
			continue
		}
		if p.Target == nil || p.Target.Resource == nil {
			continue
		}
		if p.Target.Resource.Type != resourceType {
			continue
		}
		if !principalCoversCharacter(p.Target.Principal) {
			continue
		}
		if !actionCoversAction(p.Target.Action, action) {
			continue
		}
		return true
	}
	return false
}

// actionCoversAction returns true when the action clause is unrestricted
// (wildcard) or explicitly includes the requested action.
func actionCoversAction(ac *dsl.ActionClause, action string) bool {
	if ac == nil {
		return true
	}
	if len(ac.Actions) == 0 {
		return true // wildcard
	}
	for _, a := range ac.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// referencedResourceAttrs collects all attribute names (first path segment after
// "resource.<type>") referenced in the policy conditions. Specifically it looks
// for AttrRef nodes whose root is "resource" and whose path has at least two
// segments [type, attr, ...], and where the type matches the policy's declared
// resource type.
func referencedResourceAttrs(p *dsl.Policy) []string {
	if p.Conditions == nil || p.Target == nil || p.Target.Resource == nil {
		return nil
	}
	rt := p.Target.Resource.Type
	if rt == "" {
		return nil
	}
	var attrs []string
	collectFromBlock(p.Conditions, rt, &attrs)
	return attrs
}

func collectFromBlock(block *dsl.ConditionBlock, rt string, out *[]string) {
	if block == nil {
		return
	}
	for _, disj := range block.Disjunctions {
		for _, cond := range disj.Conditions {
			collectFromCond(cond, rt, out)
		}
	}
}

func collectFromCond(c *dsl.Condition, rt string, out *[]string) {
	if c == nil {
		return
	}
	if c.Negation != nil {
		collectFromCond(c.Negation, rt, out)
		return
	}
	if c.Parenthesized != nil {
		collectFromBlock(c.Parenthesized, rt, out)
		return
	}
	if c.IfThenElse != nil {
		collectFromCond(c.IfThenElse.If, rt, out)
		collectFromCond(c.IfThenElse.Then, rt, out)
		collectFromCond(c.IfThenElse.Else, rt, out)
		return
	}
	if c.Has != nil && c.Has.Root == "resource" && len(c.Has.Path) >= 2 && c.Has.Path[0] == rt {
		*out = append(*out, c.Has.Path[1])
		return
	}
	if c.Contains != nil && c.Contains.Root == "resource" && len(c.Contains.Path) >= 2 && c.Contains.Path[0] == rt {
		*out = append(*out, c.Contains.Path[1])
		return
	}
	if c.Like != nil {
		collectFromExpr(c.Like.Left, rt, out)
		return
	}
	if c.InList != nil {
		collectFromExpr(c.InList.Left, rt, out)
		return
	}
	if c.InExpr != nil {
		collectFromExpr(c.InExpr.Left, rt, out)
		collectFromExpr(c.InExpr.Right, rt, out)
		return
	}
	if c.Comparison != nil {
		collectFromExpr(c.Comparison.Left, rt, out)
		collectFromExpr(c.Comparison.Right, rt, out)
	}
}

func collectFromExpr(e *dsl.Expr, rt string, out *[]string) {
	if e == nil || e.AttrRef == nil {
		return
	}
	ref := e.AttrRef
	// We're looking for resource.<type>.<attr> paths.
	if ref.Root == "resource" && len(ref.Path) >= 2 && ref.Path[0] == rt {
		*out = append(*out, ref.Path[1])
	}
}
