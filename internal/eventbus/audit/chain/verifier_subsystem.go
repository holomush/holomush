// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
)

// VerifierSubsystemConfig carries the dependencies for [VerifierSubsystem].
type VerifierSubsystemConfig struct {
	// Repo is the audit-chain repository used by the verifier to discover
	// scopes and load entries.
	Repo Repo

	// Handlers is the set of chain Handler bundles to walk at boot time.
	// Each Handler carries the Chain metadata plus per-chain extraction and
	// canonicalization callbacks. Registered by owning packages at wiring time
	// (e.g. dek.RegisterRekey(vs)).
	//
	// Per the R6 amendment (spec §3.6), Handler replaces the pre-amendment
	// behavior-carrying Chain struct: Chain is pure metadata; behavior lives
	// here in the Handler.
	Handlers []Handler

	// Logger is used for progress/diagnostic messages during chain walks.
	Logger *slog.Logger
}

// VerifierSubsystem is a [lifecycle.Subsystem] that verifies every registered
// audit chain at server boot. Start validates the registration shape of each
// Handler's Chain metadata (INV-CRYPTO-113, INV-CRYPTO-114) and then calls
// [Verifier.VerifyAll] for each registered chain. Any integrity failure is
// fatal — the server refuses to start.
//
// The subsystem reuses the existing [lifecycle.SubsystemCryptoChainVerifier]
// constant (declared by sub-epic D). The broader generalized implementation
// (walking policy_set + rekey chains) replaces D's policy-specific subsystem
// behind the same ID.
type VerifierSubsystem struct {
	cfg      VerifierSubsystemConfig
	verifier Verifier
}

// NewVerifierSubsystem constructs a VerifierSubsystem backed by cfg.Repo.
// The Verifier is created internally via [NewVerifier].
func NewVerifierSubsystem(cfg VerifierSubsystemConfig) *VerifierSubsystem {
	return &VerifierSubsystem{
		cfg:      cfg,
		verifier: NewVerifier(cfg.Repo),
	}
}

// ID returns [lifecycle.SubsystemCryptoChainVerifier], reusing D's existing
// constant for the generalized audit-chain verifier subsystem.
func (s *VerifierSubsystem) ID() lifecycle.SubsystemID {
	return lifecycle.SubsystemCryptoChainVerifier
}

// DependsOn returns [lifecycle.SubsystemDatabase]: the verifier reads
// events_audit rows, so the DB must be up before the chain walk runs.
func (s *VerifierSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}
}

// Start validates each registered Handler's Chain metadata and then walks
// every chain via [Verifier.VerifyAll]. Returns on the first failure.
//
// INV-CRYPTO-102: server boot MUST refuse with
// AUDIT_CHAIN_BROKEN (or a more specific AUDIT_CHAIN_* code) when any
// registered chain has a break.
func (s *VerifierSubsystem) Start(ctx context.Context) error {
	for _, h := range s.cfg.Handlers {
		if err := ValidateRegistration(h.Chain); err != nil {
			return oops.Code("AUDIT_CHAIN_INVALID_REGISTRATION").
				With("chain", h.Chain.SubjectPrefix).Wrap(err)
		}
	}
	for _, h := range s.cfg.Handlers {
		s.cfg.Logger.InfoContext(ctx, "verifying audit chain", "chain", h.Chain.SubjectPrefix)
		if err := s.verifier.VerifyAll(ctx, h); err != nil {
			return err //nolint:wrapcheck // AUDIT_CHAIN_* oops codes pass through from Verifier as-is
		}
	}
	return nil
}

// Stop is a no-op: the verifier subsystem holds no background goroutines.
func (s *VerifierSubsystem) Stop(_ context.Context) error { return nil }

// Compile-time assertion: VerifierSubsystem satisfies lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*VerifierSubsystem)(nil)
