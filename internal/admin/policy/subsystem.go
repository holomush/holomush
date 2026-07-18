// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
)

// CryptoPolicySubsystem is the lifecycle wrapper around EmitCurrentSnapshot.
// At Start it iterates configured policy_names and emits one snapshot per
// name. DependsOn AuditProjection (the audit projection must be running so
// the publisher chain commits to events_audit before subsequent subsystems
// observe). Fail-closed on Publish error per INV-CRYPTO-84.
type CryptoPolicySubsystem struct {
	cfg CryptoPolicySubsystemConfig
}

// CryptoPolicySubsystemConfig bundles the deps + policy_names list.
type CryptoPolicySubsystemConfig struct {
	// EmitDeps is a given value for callers that already hold the resolved
	// deps at construction (test literals). EmitDepsProvider, resolved once
	// at the top of Start, is the production path — it wins when non-nil
	// (07-09 item 9). The provider is backed by the memoized wiring
	// builder in cmd/holomush, which this subsystem never names directly.
	EmitDeps         EmitDeps
	EmitDepsProvider func() (EmitDeps, error)
	PolicyNames      []string // v1: ["dual_control_required"]
}

// NewCryptoPolicySubsystem constructs a new subsystem.
func NewCryptoPolicySubsystem(cfg CryptoPolicySubsystemConfig) *CryptoPolicySubsystem {
	return &CryptoPolicySubsystem{cfg: cfg}
}

// ID returns lifecycle.SubsystemCryptoPolicy.
func (s *CryptoPolicySubsystem) ID() lifecycle.SubsystemID {
	return lifecycle.SubsystemCryptoPolicy
}

// DependsOn returns AuditProjection plus THE RULE's wiring consumer
// superset {Database, Auth, ABAC, EventBus} (07-09 item 9) — this subsystem
// holds an EmitDepsProvider backed by the memoized wiring builder, and
// whichever consumer resolves the provider first builds it, so every
// consumer must declare the full dependency set.
func (s *CryptoPolicySubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemAuditProjection,
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemEventBus,
	}
}

// Start emits one snapshot for each configured policy_name. INV-CRYPTO-84:
// any Publish failure short-circuits the loop with a wrapped error,
// causing the lifecycle orchestrator to fail server start.
func (s *CryptoPolicySubsystem) Start(ctx context.Context) error {
	deps := s.cfg.EmitDeps
	if s.cfg.EmitDepsProvider != nil {
		resolved, err := s.cfg.EmitDepsProvider()
		if err != nil {
			return err
		}
		deps = resolved
	}
	for _, name := range s.cfg.PolicyNames {
		if err := EmitCurrentSnapshot(ctx, deps, name); err != nil {
			return oops.Code("CRYPTO_POLICY_EMIT_FAILED").
				With("policy_name", name).Wrap(err)
		}
	}
	return nil
}

// Stop is a no-op — the subsystem holds no resources.
func (s *CryptoPolicySubsystem) Stop(_ context.Context) error { return nil }

// Compile-time interface assertion.
var _ lifecycle.Subsystem = (*CryptoPolicySubsystem)(nil)
