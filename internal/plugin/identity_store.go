// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"sync"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/store"
)

// IdentityStore owns the plugin identity registry: the name-to-ULID maps
// populated at bootstrap from the plugins table and mutated on load/unload.
//
// nameByID resolves THREE populations:
//  1. Currently-active plugins (rows with gc_at IS NULL).
//  2. Historically-registered plugins (rows with gc_at IS NOT NULL) —
//     preserved for the registry's lifetime per INV-PLUGIN-17, so events
//     emitted before a plugin was unloaded stay attributable.
//  3. Compile-time system actor sentinels registered by Bootstrap
//     (SystemActorULID -> "system", WorldServiceActorULID -> "world-service").
//     Sentinels live in nameByID ONLY and are never swept.
//
// activeByName resolves only currently-loaded plugins.
//
// The store carries its own sync.RWMutex. It was extracted from Manager,
// where both maps were guarded by the Manager's general-purpose mutex; that
// shared lock coupled the load-time and runtime halves of the Manager and is
// what this type exists to break.
//
// LOCK DISCIPLINE: every method takes and releases the store's lock in one
// short critical section and calls nothing that acquires Manager.mu. Callers
// MUST NOT invoke any of these methods while holding Manager.mu — no code
// path may hold both locks at once, so no lock ordering exists to violate.
type IdentityStore struct {
	mu           sync.RWMutex
	nameByID     map[ulid.ULID]string
	activeByName map[string]ulid.ULID

	// repo persists plugin identity. Nil is a supported configuration (the
	// WithPluginRepo test seam): the store then runs in-memory only, and
	// Bootstrap, Sweep, and Upsert degrade to no-ops rather than failing.
	repo store.PluginRepo

	// retentionDays is the plugin row TTL in days; 0 disables the sweep.
	retentionDays int
}

// Compile-time proof that the extracted store still satisfies the contract
// internal/grpc and the plugin emit stamp sites consume.
var _ IdentityRegistry = (*IdentityStore)(nil)

// NewIdentityStore returns an empty identity store. The maps are allocated
// here rather than in Bootstrap so a caller may Register before (or without)
// bootstrapping — which is what makes the store constructible and testable
// on its own, with no Manager and no harness.
func NewIdentityStore(repo store.PluginRepo, retentionDays int) *IdentityStore {
	return &IdentityStore{
		nameByID:      make(map[ulid.ULID]string),
		activeByName:  make(map[string]ulid.ULID),
		repo:          repo,
		retentionDays: retentionDays,
	}
}

// HasRepo reports whether a persistence layer is wired. loadPlugin uses this
// to choose between minting a ULID through Upsert and generating one
// in-memory.
func (s *IdentityStore) HasRepo() bool {
	return s.repo != nil
}

// Bootstrap registers the system sentinels and, when a repo is wired,
// hydrates the maps from the plugins table.
//
// Step order matters: sentinels are registered first so a plugin row that
// collides with a reserved sentinel ULID is rejected rather than silently
// shadowing it.
func (s *IdentityStore) Bootstrap(ctx context.Context) error {
	s.mu.Lock()
	// Step 1: system sentinels. Not in activeByName and not in the plugins
	// table — a different identity domain.
	s.nameByID[core.SystemActorULID] = "system"
	s.nameByID[core.WorldServiceActorULID] = "world-service"
	s.mu.Unlock()

	if s.repo == nil {
		return nil
	}

	// Step 2: load existing plugin rows from persistence. Reject sentinel
	// collisions defensively. The repo call happens outside the lock; the
	// store is not yet published to concurrent readers at bootstrap time.
	rows, err := s.repo.ListAll(ctx)
	if err != nil {
		return oops.Code("PLUGIN_MANAGER_BOOTSTRAP").Wrap(err)
	}

	for i := range rows {
		row := &rows[i]
		if core.IsSentinelULID(row.ID) {
			return oops.Code("PLUGIN_ROW_USES_SENTINEL_ID").
				With("name", row.Name).
				With("id", row.ID.String()).
				Errorf("plugin row uses a reserved sentinel ULID")
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range rows {
		row := &rows[i]
		s.nameByID[row.ID] = row.Name
		if row.GcAt == nil {
			s.activeByName[row.Name] = row.ID
		}
	}
	return nil
}

// NameByID implements IdentityRegistry.
func (s *IdentityStore) NameByID(id ulid.ULID) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	name, ok := s.nameByID[id]
	return name, ok
}

// IDByName implements IdentityRegistry.
func (s *IdentityStore) IDByName(name string) (ulid.ULID, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.activeByName[name]
	return id, ok
}

// Register binds a plugin's ULID to its name in both directions.
//
// loadPlugin calls this BEFORE host.Load — downstream code may emit during
// Load and needs to resolve the plugin name via IDByName. Unregister is its
// paired rollback; the two MUST move together.
func (s *IdentityStore) Register(id ulid.ULID, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nameByID[id] = name
	s.activeByName[name] = id
}

// Unregister undoes a Register. This is the load-failure rollback path, and
// it is the ONLY caller permitted to remove a nameByID entry: the identity
// was never successfully published, so retaining it would resolve a ULID
// that no emitted event can carry.
func (s *IdentityStore) Unregister(id ulid.ULID, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nameByID, id)
	delete(s.activeByName, name)
}

// Deactivate removes a plugin's active name binding on unload.
//
// The nameByID entry is intentionally retained for historical resolution
// (INV-PLUGIN-17) so events emitted before the unload remain attributable.
func (s *IdentityStore) Deactivate(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeByName, name)
	// nameByID intentionally retained for historical resolution.
}

// Sweep deactivates plugin rows that have aged past the retention window and
// returns the swept rows so the caller can log them.
//
// A no-op when no repo is wired or retention is disabled (0). Swept rows lose
// their active binding only; their nameByID entries are retained for
// historical resolution (INV-PLUGIN-17), which is also why the system
// sentinels — nameByID-only by construction — are structurally out of reach
// of this sweep.
func (s *IdentityStore) Sweep(ctx context.Context) ([]store.PluginRow, error) {
	if s.repo == nil || s.retentionDays <= 0 {
		return nil, nil
	}

	swept, err := s.repo.SweepInactive(ctx, s.retentionDays)
	if err != nil {
		return nil, oops.Code("PLUGIN_MANAGER_SWEEP").Wrap(err)
	}

	s.mu.Lock()
	for i := range swept {
		delete(s.activeByName, swept[i].Name)
		// nameByID intentionally retained for historical resolution.
	}
	s.mu.Unlock()

	return swept, nil
}

// Upsert passes a plugin row through to the persistence layer, minting or
// refreshing its stable ULID. Callers MUST check HasRepo first; calling this
// with no repo wired is a programming error.
func (s *IdentityStore) Upsert(
	ctx context.Context,
	in store.PluginUpsertInput,
) (ulid.ULID, *store.DriftReport, error) {
	return s.repo.Upsert(ctx, in) //nolint:wrapcheck // caller adds plugin context
}
