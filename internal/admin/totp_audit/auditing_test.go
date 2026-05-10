// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totpaudit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	totpaudit "github.com/holomush/holomush/internal/admin/totp_audit"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/totp"
)

// fakeTOTPService is a minimal totp.Service for decorator tests. Only
// the methods the test uses set non-zero values; pass-throughs return
// zero results.
type fakeTOTPService struct {
	verifyResult  totp.VerifyResult
	verifyErr     error
	consumeResult totp.ConsumeRecoveryResult
	consumeErr    error
	clearResult   totp.ClearResult
	clearErr      error
	recoverResult totp.RecoverAndClearResult
	recoverErr    error
	clearByCalled totp.ClearReason
	verifyCalls   int
	consumeCalls  int
	clearCalls    int
	recoverCalls  int
}

func (f *fakeTOTPService) PrepareBootstrap(_ context.Context, _ ulid.ULID) (totp.BootstrapPreparation, error) {
	return totp.BootstrapPreparation{}, nil
}

func (f *fakeTOTPService) CommitBootstrap(_ context.Context, _ totp.BootstrapPreparation) (totp.BootstrapResult, error) {
	return totp.BootstrapResult{}, nil
}

func (f *fakeTOTPService) BootstrapEnroll(_ context.Context, _ ulid.ULID) (totp.BootstrapResult, error) {
	return totp.BootstrapResult{}, nil
}

func (f *fakeTOTPService) PrepareEnroll(_ context.Context, _ ulid.ULID) (totp.EnrollPreparation, error) {
	return totp.EnrollPreparation{}, nil
}

func (f *fakeTOTPService) CommitEnroll(_ context.Context, _ totp.EnrollPreparation) (totp.EnrollResult, error) {
	return totp.EnrollResult{}, nil
}

func (f *fakeTOTPService) Enroll(_ context.Context, _ ulid.ULID) (totp.EnrollResult, error) {
	return totp.EnrollResult{}, nil
}

func (f *fakeTOTPService) IsEnrolled(_ context.Context, _ ulid.ULID) (bool, error) { return false, nil }

func (f *fakeTOTPService) Verify(_ context.Context, _ ulid.ULID, _ string) (totp.VerifyResult, error) {
	f.verifyCalls++
	return f.verifyResult, f.verifyErr
}

func (f *fakeTOTPService) ConsumeRecoveryCode(_ context.Context, _ ulid.ULID, _ string) (totp.ConsumeRecoveryResult, error) {
	f.consumeCalls++
	return f.consumeResult, f.consumeErr
}

func (f *fakeTOTPService) ClearTOTP(_ context.Context, _ ulid.ULID, by totp.ClearReason) (totp.ClearResult, error) {
	f.clearCalls++
	f.clearByCalled = by
	return f.clearResult, f.clearErr
}

func (f *fakeTOTPService) RecoverAndClear(_ context.Context, _ ulid.ULID, _ string) (totp.RecoverAndClearResult, error) {
	f.recoverCalls++
	return f.recoverResult, f.recoverErr
}

// fakePublisher captures Publish calls for assertion.
type fakePublisher struct {
	mu   sync.Mutex
	evts []eventbus.Event
	err  error // returned by Publish if non-nil
}

func (p *fakePublisher) Publish(_ context.Context, e eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.evts = append(p.evts, e)
	return p.err
}

func (p *fakePublisher) Events() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.evts))
	copy(out, p.evts)
	return out
}

// fakeClock returns a fixed time.
type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

func newAuditing(t *testing.T, ts totp.Service, pub eventbus.Publisher, logger *slog.Logger) *totpaudit.AuditingService {
	t.Helper()
	a, err := totpaudit.NewAuditingService(ts, pub, "testgame", fakeClock{t: time.Unix(1700000000, 0)}, logger)
	require.NoError(t, err)
	return a
}

func TestAuditingServiceVerifyEmitsLockedOnTransition(t *testing.T) {
	lockedUntil := time.Unix(1700000060, 0)
	ts := &fakeTOTPService{verifyResult: totp.VerifyResult{LockoutTransition: true, LockedUntil: &lockedUntil}}
	pub := &fakePublisher{}
	a := newAuditing(t, ts, pub, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	res, err := a.Verify(context.Background(), ulid.Make(), "123456")
	require.NoError(t, err)
	assert.True(t, res.LockoutTransition)
	require.Len(t, pub.Events(), 1)
	assert.Equal(t, eventbus.Type(totp.EventTypeLocked), pub.Events()[0].Type)
}

func TestAuditingServiceVerifyDoesNotEmitWithoutTransition(t *testing.T) {
	ts := &fakeTOTPService{verifyResult: totp.VerifyResult{LockoutTransition: false}}
	pub := &fakePublisher{}
	a := newAuditing(t, ts, pub, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	_, err := a.Verify(context.Background(), ulid.Make(), "123456")
	require.NoError(t, err)
	assert.Empty(t, pub.Events())
}

func TestAuditingServiceConsumeRecoveryCodeEmitsRecoveryConsumed(t *testing.T) {
	ts := &fakeTOTPService{consumeResult: totp.ConsumeRecoveryResult{RecoveryCodeID: ulid.Make(), AuditConsumedAt: time.Unix(1700000000, 0)}}
	pub := &fakePublisher{}
	a := newAuditing(t, ts, pub, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	_, err := a.ConsumeRecoveryCode(context.Background(), ulid.Make(), "code")
	require.NoError(t, err)
	require.Len(t, pub.Events(), 1)
	assert.Equal(t, eventbus.Type(totp.EventTypeRecoveryConsumed), pub.Events()[0].Type)
}

func TestAuditingServiceClearTOTPEmitsCleared(t *testing.T) {
	ts := &fakeTOTPService{clearResult: totp.ClearResult{AuditClearedAt: time.Unix(1700000000, 0)}}
	pub := &fakePublisher{}
	a := newAuditing(t, ts, pub, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	_, err := a.ClearTOTP(context.Background(), ulid.Make(), totp.ClearReason("admin_reset"))
	require.NoError(t, err)
	require.Len(t, pub.Events(), 1)
	assert.Equal(t, eventbus.Type(totp.EventTypeCleared), pub.Events()[0].Type)
	// Verify cleared_by surfaces through to the JSON payload.
	var got totp.ClearedPayload
	require.NoError(t, json.Unmarshal(pub.Events()[0].Payload, &got))
	assert.Equal(t, totp.ClearReason("admin_reset"), got.ClearedBy)
}

func TestAuditingServiceRecoverAndClearEmitsBoth(t *testing.T) {
	ts := &fakeTOTPService{recoverResult: totp.RecoverAndClearResult{
		RecoveryCodeID:  ulid.Make(),
		AuditConsumedAt: time.Unix(1700000000, 0),
		AuditClearedAt:  time.Unix(1700000001, 0),
	}}
	pub := &fakePublisher{}
	a := newAuditing(t, ts, pub, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	_, err := a.RecoverAndClear(context.Background(), ulid.Make(), "code")
	require.NoError(t, err)
	require.Len(t, pub.Events(), 2)
	assert.Equal(t, eventbus.Type(totp.EventTypeRecoveryConsumed), pub.Events()[0].Type, "first emit must be recovery_consumed")
	assert.Equal(t, eventbus.Type(totp.EventTypeCleared), pub.Events()[1].Type, "second emit must be cleared")
	// Verify the cleared event has cleared_by=recovery_code.
	var got totp.ClearedPayload
	require.NoError(t, json.Unmarshal(pub.Events()[1].Payload, &got))
	assert.Equal(t, totp.ClearReasonRecoveryCode, got.ClearedBy)
}

// TestAuditingServiceLogsAndContinuesOnPublishError — INV-D14: Publish
// failure logs at slog.Warn and does NOT cause the inner method's success
// to roll back (the inner Service has already committed PG state).
func TestAuditingServiceLogsAndContinuesOnPublishError(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	lockedUntil := time.Unix(1700000060, 0)
	ts := &fakeTOTPService{verifyResult: totp.VerifyResult{LockoutTransition: true, LockedUntil: &lockedUntil}}
	pub := &fakePublisher{err: errors.New("simulated publish failure")}
	a := newAuditing(t, ts, pub, logger)

	res, err := a.Verify(context.Background(), ulid.Make(), "123456")
	require.NoError(t, err, "inner success must propagate even when Publish fails")
	assert.True(t, res.LockoutTransition)
	logs := logBuf.String()
	require.Contains(t, logs, "Publish failed")
	require.Contains(t, logs, "INV-D14")
}

// TestAuditingServiceWrapsAllStateTransitionMethods asserts the decorator
// itself satisfies totp.Service (compile-time check) and that pass-through
// methods don't emit.
func TestAuditingServiceWrapsAllStateTransitionMethods(t *testing.T) {
	ts := &fakeTOTPService{}
	pub := &fakePublisher{}
	a := newAuditing(t, ts, pub, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	// Touch each pass-through method; verify no emits.
	_, _ = a.IsEnrolled(context.Background(), ulid.Make())
	_, _ = a.PrepareBootstrap(context.Background(), ulid.Make())
	_, _ = a.CommitBootstrap(context.Background(), totp.BootstrapPreparation{})
	_, _ = a.BootstrapEnroll(context.Background(), ulid.Make())
	_, _ = a.PrepareEnroll(context.Background(), ulid.Make())
	_, _ = a.CommitEnroll(context.Background(), totp.EnrollPreparation{})
	_, _ = a.Enroll(context.Background(), ulid.Make())

	assert.Empty(t, pub.Events(), "pass-through methods must not emit")
}

func TestNewAuditingServiceRejectsNilInner(t *testing.T) {
	pub := &fakePublisher{}
	_, err := totpaudit.NewAuditingService(nil, pub, "game", fakeClock{t: time.Now()}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inner totp.Service is required")
}

func TestNewAuditingServiceRejectsNilPublisher(t *testing.T) {
	ts := &fakeTOTPService{}
	_, err := totpaudit.NewAuditingService(ts, nil, "game", fakeClock{t: time.Now()}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eventbus.Publisher is required")
}

func TestNewAuditingServiceRejectsEmptyGameID(t *testing.T) {
	ts := &fakeTOTPService{}
	pub := &fakePublisher{}
	_, err := totpaudit.NewAuditingService(ts, pub, "", fakeClock{t: time.Now()}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gameID is required")
}

func TestNewAuditingServiceRejectsNilClock(t *testing.T) {
	ts := &fakeTOTPService{}
	pub := &fakePublisher{}
	_, err := totpaudit.NewAuditingService(ts, pub, "game", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "totp.Clock is required")
}

func TestAuditingServiceVerifyNilLockedUntilHandled(t *testing.T) {
	// LockoutTransition=true but LockedUntil=nil (edge case; inner shouldn't
	// produce this but decorator must not panic).
	ts := &fakeTOTPService{verifyResult: totp.VerifyResult{LockoutTransition: true, LockedUntil: nil}}
	pub := &fakePublisher{}
	a := newAuditing(t, ts, pub, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	_, err := a.Verify(context.Background(), ulid.Make(), "123456")
	require.NoError(t, err)
	// Emit still fires (locked_until is zero value in payload).
	require.Len(t, pub.Events(), 1)
	assert.Equal(t, eventbus.Type(totp.EventTypeLocked), pub.Events()[0].Type)
}
