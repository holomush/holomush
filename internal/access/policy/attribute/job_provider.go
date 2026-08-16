// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"strings"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// JobRegistry is the narrow view of internal/jobs.Registry this provider needs:
// is the job running, and what did it declare it writes. Declared narrowly here
// (mirroring PluginRegistry) so package attribute does not depend on the job
// subsystem.
type JobRegistry interface {
	// IsJobRunning reports whether the named job is currently running.
	IsJobRunning(name string) bool
	// DeclaredWrites returns the job's declared capability class and whether
	// the job is registered.
	DeclaredWrites(name string) ([]string, bool)
}

// JobProvider resolves attributes for background-job subjects ("job:<name>").
//
// It is PluginProvider's shape with a job registry substituted, and the
// substitution is the point: authority is tied to LIVENESS (02.2-CONTEXT
// D-49). A ref naming a job that is not running resolves to (nil, nil) — no
// attributes are stamped at all — so every attribute-conditioned permit fails
// to match and the request default-denies. That works BECAUSE of the ABAC
// fail-safe semantics, not around them: a missing attribute is false for every
// operator (ADR holomush-iv43).
//
// It is therefore load-bearing that the unresolved case resolves to NOTHING
// rather than to a placeholder. An empty-string (or empty-list) sentinel is
// fail-OPEN in this DSL — it is a RESOLVED value that matches any other
// unresolved peer (.claude/rules/abac-providers.md).
type JobProvider struct {
	registry JobRegistry
}

// NewJobProvider creates a provider that resolves job subject attributes.
//
// A nil registry is tolerated and fails closed: every job resolves to
// (nil, nil), which is the correct answer for an entrypoint that runs no jobs.
func NewJobProvider(registry JobRegistry) *JobProvider {
	return &JobProvider{registry: registry}
}

// Namespace returns the attribute namespace for job subjects.
func (p *JobProvider) Namespace() string { return "job" }

// ResolveSubject returns job attributes for a "job:<name>" subject ref.
//
// FOREIGN REFS. Resolver.resolveEntity hands the FULL entity ref to EVERY
// registered provider, so this method is called with "character:01ABC",
// "plugin:builder-bot" and every other subject in the system. The explicit
// prefix guard below is what stops this provider from stamping job.* keys onto
// another principal's bag — without it, a registry that happened to answer true
// for the full foreign ref would forge a job identity. The guard also short-
// circuits BEFORE the registry lookup, so a foreign ref never reaches it.
func (p *JobProvider) ResolveSubject(_ context.Context, subjectID string) (map[string]any, error) {
	if subjectID == "" {
		return nil, nil
	}
	if !strings.HasPrefix(subjectID, access.SubjectJob) {
		return nil, nil
	}
	name := strings.TrimPrefix(subjectID, access.SubjectJob)
	if name == "" {
		return nil, nil
	}
	if p.registry == nil || !p.registry.IsJobRunning(name) {
		return nil, nil
	}
	// The BARE name, not the prefixed ref: it equals bags.Subject["id"] (which
	// the resolver stamps provider-independently as the substring after the
	// first ':'), so `principal.job.name == "fixture"` reads naturally.
	attrs := map[string]any{"name": name}

	// The declared capability class (D-50). It is SELF-ATTESTED and that is
	// fine, because it can only NARROW: a seed must still grant the write, and
	// both gates must pass (D-51).
	//
	// The witness is emitted on EVERY path; only the VALUE is omitted. An empty
	// or nil writes VALUE would be a RESOLVED value that containsAll evaluates
	// against — the list-flavored empty-string sentinel
	// .claude/rules/abac-providers.md forbids — so an undeclared class drops
	// the key entirely and every capability-class condition then evaluates
	// false.
	if writes, ok := p.registry.DeclaredWrites(name); ok && len(writes) > 0 {
		attrs["writes"] = writes
		attrs["has_writes"] = true
	} else {
		attrs["has_writes"] = false
	}

	return attrs, nil
}

// ResolveResource returns nil — jobs are a principal, never a resource.
func (p *JobProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// Schema returns the attribute schema for job subjects.
func (p *JobProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"name": types.AttrTypeString,
			// AttrTypeStringList, not AttrTypeString. Declaring it as a scalar
			// still registers successfully and then misbehaves silently under
			// containsAll, so registration succeeding proves nothing about the
			// type — TestJobProviderSchemaDeclaresWritesAsAStringList asserts it
			// directly.
			"writes":     types.AttrTypeStringList,
			"has_writes": types.AttrTypeBool,
		},
	}
}
