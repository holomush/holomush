// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/internal/totp/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

// Test fixtures —— a stub credentialsValidator and a writer that fails
// after a configurable number of bytes (for printEnrollment error paths).

type stubValidator struct {
	player *auth.Player
	err    error
}

func (s *stubValidator) ValidateCredentials(_ context.Context, _, _ string) (*auth.Player, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.player, nil
}

// failAfterWriter returns io.ErrShortWrite after `failAt` bytes have
// been written. Used to exercise printEnrollment's write-error paths.
type failAfterWriter struct {
	written int
	failAt  int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.written >= w.failAt {
		return 0, io.ErrShortWrite
	}
	remain := w.failAt - w.written
	if len(p) <= remain {
		w.written += len(p)
		return len(p), nil
	}
	w.written += remain
	return remain, io.ErrShortWrite
}

func sampleEnrollment() totp.Enrollment {
	return totp.Enrollment{
		Secret:          "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
		ProvisioningURI: "otpauth://totp/holomush-default:alice?secret=...",
		RecoveryCodes: []string{
			"aaaa-bbbb-cccc-dddd",
			"eeee-ffff-1111-2222",
			"3333-4444-5555-6666",
		},
	}
}

// runBootstrapEnroll happy + error paths.

func TestRunBootstrapEnrollHappyPath(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	// totp.Service isn't in mockery's set; stub directly for clearer
	// per-method overrides than .On(...) chains would give.
	stubSvc := &stubTOTPService{
		bootstrapResult: totp.BootstrapResult{
			Enrollment:      sampleEnrollment(),
			AuditConsumedAt: time.Now().UTC(),
			AuditPlayerID:   pid,
			BootstrapKey:    "totp_v1",
		},
	}

	out := &bytes.Buffer{}
	require.NoError(t, runBootstrapEnroll(ctx, repo, stubSvc, "alice", out))
	assert.Contains(t, out.String(), "TOTP enrolled for alice")
	assert.Contains(t, out.String(), pid.String())
	assert.Contains(t, out.String(), "aaaa-bbbb-cccc-dddd")
}

func TestRunBootstrapEnrollPropagatesPlayerLookupError(t *testing.T) {
	ctx := context.Background()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "ghost").
		Return("", errors.New("player not found"))

	out := &bytes.Buffer{}
	err := runBootstrapEnroll(ctx, repo, &stubTOTPService{}, "ghost", out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "player not found")
}

func TestRunBootstrapEnrollRejectsMalformedPlayerID(t *testing.T) {
	ctx := context.Background()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").
		Return("not-a-ulid", nil)

	out := &bytes.Buffer{}
	err := runBootstrapEnroll(ctx, repo, &stubTOTPService{}, "alice", out)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_TOTP_PLAYER_ULID_PARSE")
}

func TestRunBootstrapEnrollPropagatesServiceError(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	svc := &stubTOTPService{bootstrapErr: totp.ErrBootstrapAlreadyConsumed}

	out := &bytes.Buffer{}
	err := runBootstrapEnroll(ctx, repo, svc, "alice", out)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_BOOTSTRAP_CONSUMED")
}

// runEnroll happy + error paths.

func TestRunEnrollHappyPath(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	validator := &stubValidator{player: &auth.Player{ID: pid, Username: "alice"}}
	svc := &stubTOTPService{
		enrollResult: totp.EnrollResult{
			Enrollment:      sampleEnrollment(),
			AuditEnrolledAt: time.Now().UTC(),
			AuditPlayerID:   pid,
		},
	}

	out := &bytes.Buffer{}
	require.NoError(t, runEnroll(ctx, validator, svc, "alice", "hunter2", out))
	assert.Contains(t, out.String(), "TOTP enrolled for alice")
}

func TestRunEnrollPropagatesValidatorError(t *testing.T) {
	ctx := context.Background()
	validator := &stubValidator{err: errors.New("bad password")}

	out := &bytes.Buffer{}
	err := runEnroll(ctx, validator, &stubTOTPService{}, "alice", "wrong", out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad password")
}

func TestRunEnrollPropagatesServiceError(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	validator := &stubValidator{player: &auth.Player{ID: pid, Username: "alice"}}
	svc := &stubTOTPService{enrollErr: totp.ErrAlreadyEnrolled}

	out := &bytes.Buffer{}
	err := runEnroll(ctx, validator, svc, "alice", "hunter2", out)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_ALREADY_ENROLLED")
}

// runRecover happy + error paths.

func TestRunRecoverHappyPath(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	svc := &stubTOTPService{
		consumeResult: totp.ConsumeRecoveryResult{
			RecoveryCodeID:  ulid.Make(),
			AuditConsumedAt: time.Now().UTC(),
			AuditPlayerID:   pid,
		},
		clearResult: totp.ClearResult{
			ClearedBy:      totp.ClearReasonRecoveryCode,
			AuditClearedAt: time.Now().UTC(),
			AuditPlayerID:  pid,
			WasEnrolled:    true,
		},
	}

	out := &bytes.Buffer{}
	require.NoError(t, runRecover(ctx, repo, svc, "alice", "aaaa-bbbb-cccc-dddd", out))
	assert.Contains(t, out.String(), "TOTP cleared for alice")
	assert.Contains(t, out.String(), "holomush admin totp enroll --username alice")
}

func TestRunRecoverHidesPlayerLookupFailure(t *testing.T) {
	ctx := context.Background()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "ghost").
		Return("", errors.New("player not found"))

	out := &bytes.Buffer{}
	err := runRecover(ctx, repo, &stubTOTPService{}, "ghost", "aaaa-bbbb-cccc-dddd", out)
	// Timing-safe surface: identical error to wrong-recovery-code case.
	require.ErrorIs(t, err, totp.ErrInvalidRecoveryCode)
}

func TestRunRecoverHidesMalformedPlayerID(t *testing.T) {
	ctx := context.Background()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return("not-a-ulid", nil)

	out := &bytes.Buffer{}
	err := runRecover(ctx, repo, &stubTOTPService{}, "alice", "aaaa-bbbb-cccc-dddd", out)
	require.ErrorIs(t, err, totp.ErrInvalidRecoveryCode)
}

func TestRunRecoverPropagatesConsumeError(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	svc := &stubTOTPService{consumeErr: totp.ErrInvalidRecoveryCode}

	out := &bytes.Buffer{}
	err := runRecover(ctx, repo, svc, "alice", "aaaa", out)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_INVALID_RECOVERY_CODE")
}

func TestRunRecoverPropagatesClearError(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	svc := &stubTOTPService{
		consumeResult: totp.ConsumeRecoveryResult{
			RecoveryCodeID: ulid.Make(),
			AuditPlayerID:  pid,
		},
		clearErr: errors.New("pg delete failed"),
	}

	out := &bytes.Buffer{}
	err := runRecover(ctx, repo, svc, "alice", "aaaa-bbbb-cccc-dddd", out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pg delete failed")
}

func TestRunRecoverWriteFailureSurfaces(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	svc := &stubTOTPService{
		consumeResult: totp.ConsumeRecoveryResult{RecoveryCodeID: ulid.Make()},
		clearResult:   totp.ClearResult{WasEnrolled: true},
	}

	err := runRecover(ctx, repo, svc, "alice", "aaaa", &failAfterWriter{failAt: 0})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_TOTP_PRINT_FAILED")
}

// printEnrollment write-error paths.

func TestPrintEnrollmentSurfaceHeaderWriteError(t *testing.T) {
	err := printEnrollment(&failAfterWriter{failAt: 0}, "alice", "01HZ", sampleEnrollment())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_TOTP_PRINT_FAILED")
}

func TestPrintEnrollmentSurfaceRecoveryCodeWriteError(t *testing.T) {
	// Big enough to write the header, small enough to fail mid-recovery-codes.
	// Header is roughly 240 bytes; cap a bit above.
	err := printEnrollment(&failAfterWriter{failAt: 260}, "alice", "01HZ", sampleEnrollment())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_TOTP_PRINT_FAILED")
}

func TestPrintEnrollmentSurfaceFooterWriteError(t *testing.T) {
	// Big enough to write header + 3 recovery codes, fail on footer.
	err := printEnrollment(&failAfterWriter{failAt: 400}, "alice", "01HZ", sampleEnrollment())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_TOTP_PRINT_FAILED")
}

// stubTOTPService is a minimal totp.Service for testing the run functions
// without needing a generated mock. Only methods exercised by the run
// functions are implemented; others panic if called.
type stubTOTPService struct {
	bootstrapResult totp.BootstrapResult
	bootstrapErr    error
	enrollResult    totp.EnrollResult
	enrollErr       error
	consumeResult   totp.ConsumeRecoveryResult
	consumeErr      error
	clearResult     totp.ClearResult
	clearErr        error
}

func (s *stubTOTPService) BootstrapEnroll(_ context.Context, _ ulid.ULID) (totp.BootstrapResult, error) {
	return s.bootstrapResult, s.bootstrapErr
}

func (s *stubTOTPService) Enroll(_ context.Context, _ ulid.ULID) (totp.EnrollResult, error) {
	return s.enrollResult, s.enrollErr
}

func (s *stubTOTPService) Verify(_ context.Context, _ ulid.ULID, _ string) (totp.VerifyResult, error) {
	panic("stubTOTPService.Verify not implemented")
}

func (s *stubTOTPService) IsEnrolled(_ context.Context, _ ulid.ULID) (bool, error) {
	panic("stubTOTPService.IsEnrolled not implemented")
}

func (s *stubTOTPService) ConsumeRecoveryCode(_ context.Context, _ ulid.ULID, _ string) (totp.ConsumeRecoveryResult, error) {
	return s.consumeResult, s.consumeErr
}

func (s *stubTOTPService) ClearTOTP(_ context.Context, _ ulid.ULID, _ totp.ClearReason) (totp.ClearResult, error) {
	return s.clearResult, s.clearErr
}

