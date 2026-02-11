// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package store defines the PolicyStore interface and PostgreSQL implementation
// for persisting ABAC policies.
//
// StoredPolicy.Effect uses types.PolicyEffect (what a policy declares: "permit"/"forbid"),
// which is distinct from policy.Effect (what the engine decides at runtime: allow/deny/default_deny/system_bypass).
package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// StoredPolicy is the persisted form of an access policy.
// ID uses string (not ulid.ULID) because policy identifiers are not world entities
// and may originate from different ID schemes.
type StoredPolicy struct {
	ID          string
	Name        string
	Description string
	Effect      types.PolicyEffect // "permit" or "forbid" — what the policy declares
	Source      string             // "seed", "lock", "admin", "plugin"
	DSLText     string
	CompiledAST json.RawMessage // JSONB — pre-compiled by the caller
	Enabled     bool
	SeedVersion *int
	ChangeNote  string // populated on version upgrades; stored in access_policy_versions
	CreatedBy   string
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PolicyStore handles CRUD operations for access policies.
type PolicyStore interface {
	Create(ctx context.Context, p *StoredPolicy) error
	Get(ctx context.Context, name string) (*StoredPolicy, error)
	GetByID(ctx context.Context, id string) (*StoredPolicy, error)
	Update(ctx context.Context, p *StoredPolicy) error
	Delete(ctx context.Context, name string) error
	ListEnabled(ctx context.Context) ([]*StoredPolicy, error)
	List(ctx context.Context, opts ListOptions) ([]*StoredPolicy, error)
}

// ListOptions controls filtering for policy listing.
type ListOptions struct {
	Source  string              // filter by source ("seed", "lock", "admin", "plugin", or "" for all)
	Enabled *bool               // filter by enabled state (nil for all)
	Effect  *types.PolicyEffect // filter by effect ("permit", "forbid", or nil for all)
}

// ValidateSourceNaming enforces ADR 35: policies named "seed:*" MUST have source="seed",
// policies named "lock:*" MUST have source="lock", and vice versa.
func ValidateSourceNaming(name, source string) error {
	hasSeedPrefix := len(name) > 5 && name[:5] == "seed:"
	hasLockPrefix := len(name) > 5 && name[:5] == "lock:"

	if hasSeedPrefix && source != "seed" {
		return oops.Code("POLICY_SOURCE_MISMATCH").
			With("name", name).With("source", source).
			Errorf("policy named 'seed:*' must have source 'seed'")
	}
	if !hasSeedPrefix && source == "seed" {
		return oops.Code("POLICY_SOURCE_MISMATCH").
			With("name", name).With("source", source).
			Errorf("policy with source 'seed' must be named 'seed:*'")
	}
	if hasLockPrefix && source != "lock" {
		return oops.Code("POLICY_SOURCE_MISMATCH").
			With("name", name).With("source", source).
			Errorf("policy named 'lock:*' must have source 'lock'")
	}
	if !hasLockPrefix && source == "lock" {
		return oops.Code("POLICY_SOURCE_MISMATCH").
			With("name", name).With("source", source).
			Errorf("policy with source 'lock' must be named 'lock:*'")
	}

	return nil
}
