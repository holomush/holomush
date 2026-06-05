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
	EmitDeps    EmitDeps
	PolicyNames []string // v1: ["dual_control_required"]
}

// NewCryptoPolicySubsystem constructs a new subsystem.
func NewCryptoPolicySubsystem(cfg CryptoPolicySubsystemConfig) *CryptoPolicySubsystem {
	return &CryptoPolicySubsystem{cfg: cfg}
}

// ID returns lifecycle.SubsystemCryptoPolicy.
func (s *CryptoPolicySubsystem) ID() lifecycle.SubsystemID {
	return lifecycle.SubsystemCryptoPolicy
}

// DependsOn returns the subsystem dependency list — only AuditProjection.
func (s *CryptoPolicySubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemAuditProjection}
}

// Start emits one snapshot for each configured policy_name. INV-CRYPTO-84:
// any Publish failure short-circuits the loop with a wrapped error,
// causing the lifecycle orchestrator to fail server start.
func (s *CryptoPolicySubsystem) Start(ctx context.Context) error {
	for _, name := range s.cfg.PolicyNames {
		if err := EmitCurrentSnapshot(ctx, s.cfg.EmitDeps, name); err != nil {
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
