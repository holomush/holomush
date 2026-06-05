// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package totpaudit provides the AuditingService decorator that wraps
// totp.Service to emit crypto.totp_* lifecycle events on observed state
// transitions. Per design spec §7 and INV-CRYPTO-81.
package totpaudit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/totp"
)

// AuditingService wraps totp.Service to emit lifecycle audit events on
// observed state transitions. Sub-epic A's host-shell CLIs continue to
// use the raw totp.Service (no eventbus access — R5 Option Y). All
// server-side callers SHOULD wire through this decorator so emissions
// happen automatically.
type AuditingService struct {
	inner  totp.Service
	pub    eventbus.Publisher
	gameID string
	clock  totp.Clock
	logger *slog.Logger
}

// NewAuditingService constructs an AuditingService. inner / pub / clock
// must be non-nil and gameID must be non-empty. logger defaults to
// slog.Default() if nil.
func NewAuditingService(
	inner totp.Service,
	pub eventbus.Publisher,
	gameID string,
	clock totp.Clock,
	logger *slog.Logger,
) (*AuditingService, error) {
	if inner == nil {
		return nil, oops.Code("TOTP_AUDIT_NIL_INNER").Errorf("inner totp.Service is required")
	}
	if pub == nil {
		return nil, oops.Code("TOTP_AUDIT_NIL_PUB").Errorf("eventbus.Publisher is required")
	}
	if gameID == "" {
		return nil, oops.Code("TOTP_AUDIT_EMPTY_GAMEID").Errorf("gameID is required")
	}
	if clock == nil {
		return nil, oops.Code("TOTP_AUDIT_NIL_CLOCK").Errorf("totp.Clock is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &AuditingService{inner: inner, pub: pub, gameID: gameID, clock: clock, logger: logger}, nil
}

// emit publishes one audit event. Per INV-CRYPTO-81, Publish failure is
// logged via slog.Warn and does NOT roll back the inner Service's PG
// state.
func (a *AuditingService) emit(ctx context.Context, subjectStr, eventTypeStr string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		a.logger.WarnContext(ctx, "totp_audit: payload marshal failed; emit skipped",
			"event_type", eventTypeStr, "subject", subjectStr, "error", err)
		return
	}
	subj, err := eventbus.NewSubject(subjectStr)
	if err != nil {
		a.logger.WarnContext(ctx, "totp_audit: invalid subject; emit skipped",
			"subject", subjectStr, "error", err)
		return
	}
	evtType, err := eventbus.NewType(eventTypeStr)
	if err != nil {
		a.logger.WarnContext(ctx, "totp_audit: invalid event type; emit skipped",
			"event_type", eventTypeStr, "error", err)
		return
	}
	ev := eventbus.NewEvent(subj, evtType, eventbus.Actor{Kind: eventbus.ActorKindSystem}, body)
	ev.Timestamp = a.clock.Now() // honour the injected clock rather than time.Now() inside NewEvent
	if err := a.pub.Publish(ctx, ev); err != nil {
		a.logger.WarnContext(ctx, "totp_audit: Publish failed; audit event lost (informational, INV-CRYPTO-81)",
			"event_type", eventTypeStr, "subject", subjectStr, "publish_error", err)
	}
}

// Verify wraps inner.Verify and emits crypto.totp_locked iff the result
// shows a NULL->non-NULL locked_until transition.
func (a *AuditingService) Verify(ctx context.Context, pid ulid.ULID, code string) (totp.VerifyResult, error) {
	res, err := a.inner.Verify(ctx, pid, code)
	if err != nil {
		return res, oops.Wrap(err)
	}
	if res.LockoutTransition {
		var lockedUntil time.Time
		if res.LockedUntil != nil {
			lockedUntil = *res.LockedUntil
		}
		a.emit(
			ctx,
			totp.SubjectLocked(a.gameID, pid.String()),
			totp.EventTypeLocked,
			totp.LockedPayload{
				PlayerID:    pid.String(),
				LockedAt:    a.clock.Now(),
				LockedUntil: lockedUntil,
				Reason:      "brute_force_protection",
			},
		)
	}
	return res, nil
}

// ConsumeRecoveryCode wraps inner and emits crypto.totp_recovery_code_consumed.
func (a *AuditingService) ConsumeRecoveryCode(ctx context.Context, pid ulid.ULID, code string) (totp.ConsumeRecoveryResult, error) {
	res, err := a.inner.ConsumeRecoveryCode(ctx, pid, code)
	if err != nil {
		return res, oops.Wrap(err)
	}
	a.emit(
		ctx,
		totp.SubjectRecoveryConsumed(a.gameID, pid.String()),
		totp.EventTypeRecoveryConsumed,
		totp.RecoveryConsumedPayload{
			PlayerID:       pid.String(),
			ConsumedAt:     res.AuditConsumedAt,
			RecoveryCodeID: res.RecoveryCodeID.String(),
		},
	)
	return res, nil
}

// ClearTOTP wraps inner and emits crypto.totp_cleared.
func (a *AuditingService) ClearTOTP(ctx context.Context, pid ulid.ULID, by totp.ClearReason) (totp.ClearResult, error) {
	res, err := a.inner.ClearTOTP(ctx, pid, by)
	if err != nil {
		return res, oops.Wrap(err)
	}
	a.emit(
		ctx,
		totp.SubjectCleared(a.gameID, pid.String()),
		totp.EventTypeCleared,
		totp.ClearedPayload{
			PlayerID:  pid.String(),
			ClearedAt: res.AuditClearedAt,
			ClearedBy: by,
		},
	)
	return res, nil
}

// RecoverAndClear wraps inner and emits BOTH events on success: in order,
// recovery_consumed then cleared (with cleared_by=recovery_code).
// §7 partial-emit-failure window is documented in the spec.
func (a *AuditingService) RecoverAndClear(ctx context.Context, pid ulid.ULID, code string) (totp.RecoverAndClearResult, error) {
	res, err := a.inner.RecoverAndClear(ctx, pid, code)
	if err != nil {
		return res, oops.Wrap(err)
	}
	a.emit(
		ctx,
		totp.SubjectRecoveryConsumed(a.gameID, pid.String()),
		totp.EventTypeRecoveryConsumed,
		totp.RecoveryConsumedPayload{
			PlayerID:       pid.String(),
			ConsumedAt:     res.AuditConsumedAt,
			RecoveryCodeID: res.RecoveryCodeID.String(),
		},
	)
	a.emit(
		ctx,
		totp.SubjectCleared(a.gameID, pid.String()),
		totp.EventTypeCleared,
		totp.ClearedPayload{
			PlayerID:  pid.String(),
			ClearedAt: res.AuditClearedAt,
			ClearedBy: totp.ClearReasonRecoveryCode,
		},
	)
	return res, nil
}

// Pass-throughs for methods that don't currently emit. Future server-
// side enroll / bootstrap callers will gain emit shims here.

// IsEnrolled delegates to the inner Service. No audit event is emitted.
func (a *AuditingService) IsEnrolled(ctx context.Context, pid ulid.ULID) (bool, error) {
	ok, err := a.inner.IsEnrolled(ctx, pid)
	if err != nil {
		return false, oops.Wrap(err)
	}
	return ok, nil
}

// PrepareBootstrap delegates to the inner Service. No audit event is emitted.
func (a *AuditingService) PrepareBootstrap(ctx context.Context, pid ulid.ULID) (totp.BootstrapPreparation, error) {
	res, err := a.inner.PrepareBootstrap(ctx, pid)
	if err != nil {
		return res, oops.Wrap(err)
	}
	return res, nil
}

// CommitBootstrap delegates to the inner Service. No audit event is emitted.
func (a *AuditingService) CommitBootstrap(ctx context.Context, prep totp.BootstrapPreparation) (totp.BootstrapResult, error) {
	res, err := a.inner.CommitBootstrap(ctx, prep)
	if err != nil {
		return res, oops.Wrap(err)
	}
	return res, nil
}

// BootstrapEnroll delegates to the inner Service. No audit event is emitted.
func (a *AuditingService) BootstrapEnroll(ctx context.Context, pid ulid.ULID) (totp.BootstrapResult, error) {
	res, err := a.inner.BootstrapEnroll(ctx, pid)
	if err != nil {
		return res, oops.Wrap(err)
	}
	return res, nil
}

// PrepareEnroll delegates to the inner Service. No audit event is emitted.
func (a *AuditingService) PrepareEnroll(ctx context.Context, pid ulid.ULID) (totp.EnrollPreparation, error) {
	res, err := a.inner.PrepareEnroll(ctx, pid)
	if err != nil {
		return res, oops.Wrap(err)
	}
	return res, nil
}

// CommitEnroll delegates to the inner Service. No audit event is emitted.
func (a *AuditingService) CommitEnroll(ctx context.Context, prep totp.EnrollPreparation) (totp.EnrollResult, error) {
	res, err := a.inner.CommitEnroll(ctx, prep)
	if err != nil {
		return res, oops.Wrap(err)
	}
	return res, nil
}

// Enroll delegates to the inner Service. No audit event is emitted.
func (a *AuditingService) Enroll(ctx context.Context, pid ulid.ULID) (totp.EnrollResult, error) {
	res, err := a.inner.Enroll(ctx, pid)
	if err != nil {
		return res, oops.Wrap(err)
	}
	return res, nil
}

// Compile-time interface assertion: AuditingService is itself a totp.Service.
var _ totp.Service = (*AuditingService)(nil)
