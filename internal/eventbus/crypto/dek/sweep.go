// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dek — RekeyCheckpointSweepSubsystem.
//
// CheckpointSweepSubsystem runs a boot-time sweep followed by a periodic
// background scan that aborts non-terminal checkpoints whose last_heartbeat_at
// is older than TTL (default 24h). Each abort emits a chained rekey audit
// event satisfying INV-CRYPTO-105.
//
// The subsystem depends on SubsystemCryptoChainVerifier (chain integrity
// confirmed before any new emission), SubsystemEventBus, and
// SubsystemAuditProjection per spec §6.2 lifecycle ordering.
package dek

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
)

// CheckpointSweepConfig holds the constructor arguments for CheckpointSweepSubsystem.
type CheckpointSweepConfig struct {
	// Repo/AuditEmitter are given values for callers that already hold the
	// resolved storage at construction (integration-test literals).
	// DepsProvider, resolved once at the top of Start, is the production
	// path — it wins when non-nil (07-09 item 9). Backed by the memoized
	// wiring builder in cmd/holomush, which this package never names
	// directly.
	Repo         *CheckpointRepo
	AuditEmitter AuditEmitter
	DepsProvider func() (*CheckpointRepo, AuditEmitter, error)
	Logger       *slog.Logger
	// TTL is the maximum allowed age of last_heartbeat_at for a non-terminal
	// checkpoint before it is auto-aborted. Defaults to 24h when ≤ 0.
	TTL time.Duration
	// Interval between background scans. Defaults to 1h when ≤ 0.
	Interval time.Duration
}

// CheckpointSweepSubsystem is a lifecycle.Subsystem that auto-aborts
// non-terminal rekey checkpoints whose heartbeat has exceeded the TTL.
// It runs a synchronous sweep at Start and then ticks every Interval.
type CheckpointSweepSubsystem struct {
	cfg    CheckpointSweepConfig
	cancel context.CancelFunc
	done   chan struct{}
}

// NewCheckpointSweepSubsystem constructs a CheckpointSweepSubsystem.
// Defaults are applied to TTL (24h) and Interval (1h) if unset.
func NewCheckpointSweepSubsystem(cfg CheckpointSweepConfig) *CheckpointSweepSubsystem {
	if cfg.TTL <= 0 {
		cfg.TTL = 24 * time.Hour
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Hour
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &CheckpointSweepSubsystem{cfg: cfg}
}

// ID satisfies lifecycle.Subsystem.
func (s *CheckpointSweepSubsystem) ID() lifecycle.SubsystemID {
	return lifecycle.SubsystemRekeyCheckpointSweep
}

// DependsOn satisfies lifecycle.Subsystem.
//
// The sweep emits chained audit events on TTL abort, requiring:
//   - SubsystemCryptoChainVerifier: chain integrity confirmed at boot
//     before any new emission (spec §6.2).
//   - SubsystemEventBus: audit events route to JetStream.
//   - SubsystemAuditProjection: emitted events land in events_audit.
//
// Database, Auth, and ABAC are THE RULE's wiring consumer superset
// (07-09 item 9) — this subsystem holds a DepsProvider backed by the
// memoized wiring builder, and whichever consumer resolves the
// provider first builds it, so every consumer must declare the full set.
func (s *CheckpointSweepSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemCryptoChainVerifier,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemAuditProjection,
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemABAC,
	}
}

// Start resolves the checkpoint repo + audit emitter (DepsProvider wins over
// the given Repo/AuditEmitter fields when non-nil), runs an immediate sweep,
// then launches the background tick loop. A failure in the boot-time sweep
// is fatal (returns non-nil error).
func (s *CheckpointSweepSubsystem) Start(ctx context.Context) error {
	if s.cfg.DepsProvider != nil {
		repo, emitter, err := s.cfg.DepsProvider()
		if err != nil {
			return err
		}
		s.cfg.Repo = repo
		s.cfg.AuditEmitter = emitter
	}
	if err := s.sweepOnce(ctx); err != nil {
		return oops.Code("DEK_REKEY_SWEEP_BOOT_FAILED").Wrap(err)
	}
	sctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go s.loop(sctx)
	return nil
}

// Stop cancels the background loop and waits for it to drain.
func (s *CheckpointSweepSubsystem) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.done == nil {
		return nil
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return oops.Code("DEK_REKEY_SWEEP_STOP_TIMEOUT").Wrap(ctx.Err())
	}
}

// loop is the background goroutine.
func (s *CheckpointSweepSubsystem) loop(ctx context.Context) {
	defer close(s.done)
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.sweepOnce(ctx); err != nil {
				s.cfg.Logger.ErrorContext(ctx, "rekey checkpoint sweep iteration failed", "err", err)
			}
		}
	}
}

// sweepOnce lists all expired (non-terminal, heartbeat > TTL) checkpoints
// and aborts each with a chained audit emit. Per-row errors are logged
// but do not halt the scan; sweepOnce returns nil unless ListExpired fails.
func (s *CheckpointSweepSubsystem) sweepOnce(ctx context.Context) error {
	expired, err := s.cfg.Repo.ListExpired(ctx, s.cfg.TTL)
	if err != nil {
		return err
	}
	for i := range expired {
		if aerr := s.abortAndAudit(ctx, expired[i], "ttl_expired"); aerr != nil {
			s.cfg.Logger.ErrorContext(ctx, "rekey checkpoint sweep abort failed",
				"request_id", expired[i].RequestID.String(), "err", aerr)
		}
	}
	return nil
}

// SweepOnceForTest exposes sweepOnce for white-box integration tests
// that drive the sweep directly without the tick loop. Tests that use
// this bypass the background goroutine entirely.
func (s *CheckpointSweepSubsystem) SweepOnceForTest(ctx context.Context) error {
	return s.sweepOnce(ctx)
}

// abortAndAudit marks the checkpoint aborted via a CAS UPDATE then
// emits a chained audit event. INV-CRYPTO-105.
func (s *CheckpointSweepSubsystem) abortAndAudit(ctx context.Context, ckpt Checkpoint, reason string) error {
	if err := s.cfg.Repo.MarkAborted(ctx, ckpt.RequestID, reason); err != nil {
		return oops.Code("DEK_REKEY_SWEEP_ABORT_FAILED").Wrap(err)
	}

	policyHashArr := ckpt.PolicyHash()

	payload := RekeyAuditPayload{
		RequestID:     ckpt.RequestID.String(),
		Context:       RekeyAuditContext{Type: ckpt.ContextType, ID: ckpt.ContextID},
		OldDEK:        RekeyAuditDEK{ID: ckpt.OldDEKID},
		Justification: "aborted by sweep: " + reason,
		PolicyHash:    fmt.Sprintf("sha256:%s", hex.EncodeToString(policyHashArr[:])),
		ForceDestroy:  false,
		StartedAt:     ckpt.StartedAt,
		CompletedAt:   time.Now(),
		SpecVersion:   "2026-04-25-event-payload-crypto-design.md @ §6.3",
	}
	if ckpt.NewDEKID != nil {
		payload.NewDEK = RekeyAuditDEK{ID: *ckpt.NewDEKID}
	}

	if _, _, err := s.cfg.AuditEmitter.Emit(ctx, payload); err != nil {
		return oops.Code("DEK_REKEY_SWEEP_AUDIT_FAILED").Wrap(err)
	}
	return nil
}

// Compile-time assertion that CheckpointSweepSubsystem satisfies lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*CheckpointSweepSubsystem)(nil)
