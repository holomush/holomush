// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
)

// CryptoPolicySubsystem is the lifecycle wrapper around EmitCurrentSnapshot.
// At Activate it iterates configured policy_names and emits one snapshot per
// name. DependsOn AuditProjection (the audit projection must be running so
// the publisher chain commits to events_audit before subsequent subsystems
// observe). Fail-closed on Publish error per INV-CRYPTO-84.
type CryptoPolicySubsystem struct {
	cfg  CryptoPolicySubsystemConfig
	deps EmitDeps
	// emitted tracks, per policy name, whether its snapshot has been
	// successfully published (D-13.2 row 15, round 6 WARNING: a single
	// bool is unsound for multi-policy configs — set after the loop, a
	// retry would re-emit the successful prefix; set before the loop, it
	// would suppress policies that never emitted). Marked on each name's
	// OWN successful emit, so a retry after a mid-loop failure resumes at
	// exactly the not-yet-emitted suffix.
	emitted map[string]bool
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

// Prepare resolves the emit deps (EmitDepsProvider wins over the given
// EmitDeps field when non-nil) — wiring only, no emit (D-13.3 row 15). The
// snapshot emit is domain traffic and belongs in Activate.
func (s *CryptoPolicySubsystem) Prepare(_ context.Context) error {
	deps := s.cfg.EmitDeps
	if s.cfg.EmitDepsProvider != nil {
		resolved, err := s.cfg.EmitDepsProvider()
		if err != nil {
			return err
		}
		deps = resolved
	}
	s.deps = deps
	return nil
}

// Activate emits one snapshot for each configured policy_name that has not
// yet successfully emitted. INV-CRYPTO-84: any Publish failure
// short-circuits the loop with a wrapped error, causing the lifecycle
// orchestrator to fail server start. Idempotent per policy name (D-13.2 row
// 15): a repeated Activate — including a retry after a mid-loop failure —
// re-emits only the names not yet marked in s.emitted, so it neither
// duplicates a successful emit nor silently skips one that never ran.
func (s *CryptoPolicySubsystem) Activate(ctx context.Context) error {
	if s.emitted == nil {
		s.emitted = make(map[string]bool, len(s.cfg.PolicyNames))
	}
	for _, name := range s.cfg.PolicyNames {
		if s.emitted[name] {
			continue
		}
		if err := EmitCurrentSnapshot(ctx, s.deps, name); err != nil {
			return oops.Code("CRYPTO_POLICY_EMIT_FAILED").
				With("policy_name", name).Wrap(err)
		}
		s.emitted[name] = true
	}
	return nil
}

// Stop is a no-op — the subsystem holds no resources.
func (s *CryptoPolicySubsystem) Stop(_ context.Context) error { return nil }

// Compile-time interface assertion.
var _ lifecycle.Subsystem = (*CryptoPolicySubsystem)(nil)
