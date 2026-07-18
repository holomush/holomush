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
	// Repo is a given value for callers that already hold the resolved
	// repository at construction (test literals). RepoProvider, resolved
	// first inside Start, is the production path — it wins when non-nil
	// (07-09 item 9). Post-plan no resolved Repo can exist at construction
	// time, since chain.NewPostgresRepo runs inside the memoized
	// wiring builder.
	Repo         Repo
	RepoProvider func() (Repo, error)

	// HandlersProvider resolves the set of chain Handler bundles to walk at
	// boot time, replacing the former concrete Handlers []Handler field
	// entirely (rev-5 settlement) — chainHandlers depends on
	// readStreamW.Handler -> rekeyW.Manager -> the pool, so it cannot be a
	// resolved slice at construction. Each Handler carries the Chain
	// metadata plus per-chain extraction and canonicalization callbacks.
	//
	// Per the R6 amendment (spec §3.6), Handler replaces the pre-amendment
	// behavior-carrying Chain struct: Chain is pure metadata; behavior lives
	// here in the Handler.
	HandlersProvider func() []Handler

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

// NewVerifierSubsystem constructs a VerifierSubsystem backed by cfg. It does
// not construct a Verifier — post-plan no resolved Repo can exist before
// StartAll, so construction happens at Start (07-09 item 9).
func NewVerifierSubsystem(cfg VerifierSubsystemConfig) *VerifierSubsystem {
	return &VerifierSubsystem{cfg: cfg}
}

// ID returns [lifecycle.SubsystemCryptoChainVerifier], reusing D's existing
// constant for the generalized audit-chain verifier subsystem.
func (s *VerifierSubsystem) ID() lifecycle.SubsystemID {
	return lifecycle.SubsystemCryptoChainVerifier
}

// DependsOn returns [Database, Auth, ABAC, EventBus] — Database because the
// verifier reads events_audit rows, and the other three because THE RULE
// (07-09 item 9) requires every wiring consumer to declare the
// wiring's full dependency set: this subsystem holds a RepoProvider backed
// by the memoized wiring builder, and whichever consumer resolves the
// provider first builds it. EventBus in particular is real, not a
// formality: the handler set is built from eventBusSub.Publisher() via
// readStreamW (core.go), so the bus must be up before the verifier can know
// which chains it is verifying. This forbids the reverse edge
// (EventBus -> CryptoChainVerifier) forever — see 07-10's MEDIUM-11.
func (s *VerifierSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemEventBus,
	}
}

// Start resolves RepoProvider FIRST (its error is the wiring-build error —
// StartAll aborts the boot), constructs the Verifier, THEN resolves
// HandlersProvider (safe error-free: the memoized build already succeeded),
// validates each registered Handler's Chain metadata, and walks every chain
// via [Verifier.VerifyAll]. Returns on the first failure.
//
// INV-CRYPTO-102: server boot MUST refuse with
// AUDIT_CHAIN_BROKEN (or a more specific AUDIT_CHAIN_* code) when any
// registered chain has a break.
func (s *VerifierSubsystem) Start(ctx context.Context) error {
	repo := s.cfg.Repo
	if s.cfg.RepoProvider != nil {
		resolved, err := s.cfg.RepoProvider()
		if err != nil {
			return err
		}
		repo = resolved
	}
	s.verifier = NewVerifier(repo)

	var handlers []Handler
	if s.cfg.HandlersProvider != nil {
		handlers = s.cfg.HandlersProvider()
	}

	for _, h := range handlers {
		if err := ValidateRegistration(h.Chain); err != nil {
			return oops.Code("AUDIT_CHAIN_INVALID_REGISTRATION").
				With("chain", h.Chain.SubjectPrefix).Wrap(err)
		}
	}
	for _, h := range handlers {
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
