// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package jobs holds the liveness registry for background jobs — the host
// subsystems (event-driven reactors, flushers) that act on the world without a
// human on the other end.
//
// The registry answers two questions and nothing else: is job <name> currently
// running, and which resource kinds did it declare it writes. Those two answers
// are what attribute.JobProvider stamps as principal.job.name and
// principal.job.writes, so a seed can gate on liveness and on the declared
// capability class (02.2-CONTEXT D-49, D-50, D-51).
//
// # Why this package has zero dependencies
//
// A Registry is a plain mutex-guarded value, constructed by whoever owns the
// job set and injected through configuration. It deliberately does NOT become a
// lifecycle.Subsystem: minting a lifecycle.SubsystemID is a multi-site cascade
// across the pinned topological start order
// (cmd/holomush/core_topo_order_test.go), and nothing here needs ordered
// start/stop — a registry with no running jobs answers "not running" for every
// name, which is the correct fail-closed answer.
//
// # The declaration narrows; the seed grants
//
// DeclaredWrites is SELF-ATTESTED: a job declares its own capability class in
// Go at registration. That is acceptable because the declaration can only
// NARROW authority. Nothing here authorizes anything — an ABAC seed must still
// grant the write, and both gates must pass (D-51).
package jobs

import (
	"slices"
	"sync"

	"github.com/samber/oops"
)

// Registry tracks which background jobs are currently running and what each of
// them declared it writes. The zero value is not usable; call NewRegistry.
//
// It is safe for concurrent use: registration happens on a job's own goroutine
// while the ABAC attribute resolver reads it on request-handling goroutines.
type Registry struct {
	mu sync.RWMutex
	// declaredWrites maps job name → the resource kinds that job declared it
	// writes ("character", "location", …). Presence of the key IS the liveness
	// signal; Unregister deletes it.
	declaredWrites map[string][]string
}

// NewRegistry returns an empty Registry. An empty registry reports every job as
// not running, so a provider built on it stamps no attributes and every
// attribute-conditioned permit fails to match — the correct fail-closed state
// for an entrypoint that runs no jobs.
func NewRegistry() *Registry {
	return &Registry{declaredWrites: make(map[string][]string)}
}

// Register marks a job as running and records its declared capability class.
// Registering an already-registered name replaces the previous declaration.
//
// Both arguments are required. An empty name would key a job no subject string
// can name, and an empty writes list would declare a job that may write nothing
// — either is a programming error at a call site, not a runtime condition, so
// both are rejected rather than silently accepted as a narrower grant.
//
// The writes slice is copied defensively: a caller mutating its own slice after
// registration MUST NOT be able to widen what the registry reports.
func (r *Registry) Register(name string, writes []string) error {
	if name == "" {
		return oops.Code("JOB_REGISTRATION_INVALID").
			Errorf("job name is required")
	}
	if len(writes) == 0 {
		return oops.Code("JOB_REGISTRATION_INVALID").
			With("job", name).
			Errorf("job %q must declare at least one write kind", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.declaredWrites[name] = slices.Clone(writes)
	return nil
}

// Unregister marks a job as no longer running. It is idempotent: unregistering
// an unknown name is a no-op, so a deferred Unregister on a failed start is
// safe.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.declaredWrites, name)
}

// IsJobRunning reports whether the named job is currently registered. This is
// the liveness gate D-49 ties authority to: a job that is not running resolves
// to no attributes at all.
func (r *Registry) IsJobRunning(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.declaredWrites[name]
	return ok
}

// DeclaredWrites returns a COPY of the named job's declared write kinds and
// whether the job is registered. The copy matters: the returned slice is
// stamped into an ABAC attribute bag, and a caller mutating it MUST NOT be able
// to change what a later evaluation sees.
func (r *Registry) DeclaredWrites(name string) ([]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	writes, ok := r.declaredWrites[name]
	if !ok {
		return nil, false
	}
	return slices.Clone(writes), true
}
