// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
)

// CryptoChainVerifierSubsystem runs policy.VerifyChain at startup BEFORE
// EventBus / AuditProjection start, per design spec §6 ordering.
// Fails-closed (returns error from Start) on any chain integrity
// violation — the lifecycle orchestrator refuses to start the server.
type CryptoChainVerifierSubsystem struct {
	cfg CryptoChainVerifierConfig
}

// CryptoChainVerifierConfig bundles the deps + policy_names list.
type CryptoChainVerifierConfig struct {
	Pool        *pgxpool.Pool
	GameID      string
	PolicyNames []string // v1: ["dual_control_required"]
}

// NewCryptoChainVerifierSubsystem constructs a new subsystem.
func NewCryptoChainVerifierSubsystem(cfg CryptoChainVerifierConfig) *CryptoChainVerifierSubsystem {
	return &CryptoChainVerifierSubsystem{cfg: cfg}
}

// ID returns lifecycle.SubsystemCryptoChainVerifier.
func (s *CryptoChainVerifierSubsystem) ID() lifecycle.SubsystemID {
	return lifecycle.SubsystemCryptoChainVerifier
}

// DependsOn returns just SubsystemDatabase — the verifier reads
// events_audit directly with the connection pool.
func (s *CryptoChainVerifierSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}
}

// Start verifies the chain for each configured policy_name.
// INV-D11: any integrity violation produces a wrapped error and the
// orchestrator fails server start.
func (s *CryptoChainVerifierSubsystem) Start(ctx context.Context) error {
	for _, name := range s.cfg.PolicyNames {
		subject := "events." + s.cfg.GameID + ".system.crypto_policy." + name
		if err := VerifyChain(ctx, s.cfg.Pool, subject, name); err != nil {
			return oops.Code("CRYPTO_CHAIN_VERIFY_FAILED").
				With("policy_name", name).Wrap(err)
		}
	}
	return nil
}

// Stop is a no-op — the subsystem holds no resources.
func (s *CryptoChainVerifierSubsystem) Stop(_ context.Context) error { return nil }

// Compile-time interface assertion.
var _ lifecycle.Subsystem = (*CryptoChainVerifierSubsystem)(nil)
