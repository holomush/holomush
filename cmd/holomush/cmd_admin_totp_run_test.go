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
	"github.com/stretchr/testify/mock"
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

// runBootstrapEnroll cases (table-driven per repo style guideline).

func TestRunBootstrapEnroll(t *testing.T) {
	pid := ulid.Make()
	cases := []struct {
		name       string
		setup      func(repo *mocks.MockRepository, svc *stubTOTPService) // configures mocks for the case
		username   string
		assertErr  func(t *testing.T, err error)
		assertOut  func(t *testing.T, out string)
	}{
		{
			name: "happy path",
			setup: func(repo *mocks.MockRepository, svc *stubTOTPService) {
				repo.EXPECT().PlayerIDFromUsername(mock.Anything, "alice").Return(pid.String(), nil)
				svc.bootstrapResult = totp.BootstrapResult{
					Enrollment:      sampleEnrollment(),
					AuditConsumedAt: time.Now().UTC(),
					AuditPlayerID:   pid,
					BootstrapKey:    "totp_v1",
				}
			},
			username:  "alice",
			assertErr: func(t *testing.T, err error) { require.NoError(t, err) },
			assertOut: func(t *testing.T, out string) {
				assert.Contains(t, out, "TOTP enrolled for alice")
				assert.Contains(t, out, pid.String())
				assert.Contains(t, out, "aaaa-bbbb-cccc-dddd")
			},
		},
		{
			name: "player lookup error propagates",
			setup: func(repo *mocks.MockRepository, _ *stubTOTPService) {
				repo.EXPECT().PlayerIDFromUsername(mock.Anything, "ghost").
					Return("", errors.New("player not found"))
			},
			username: "ghost",
			assertErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "player not found")
			},
		},
		{
			name: "malformed player_id ULID rejected",
			setup: func(repo *mocks.MockRepository, _ *stubTOTPService) {
				repo.EXPECT().PlayerIDFromUsername(mock.Anything, "alice").Return("not-a-ulid", nil)
			},
			username: "alice",
			assertErr: func(t *testing.T, err error) {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, "ADMIN_TOTP_PLAYER_ULID_PARSE")
			},
		},
		{
			name: "service error propagates",
			setup: func(repo *mocks.MockRepository, svc *stubTOTPService) {
				repo.EXPECT().PlayerIDFromUsername(mock.Anything, "alice").Return(pid.String(), nil)
				svc.bootstrapErr = totp.ErrBootstrapAlreadyConsumed
			},
			username: "alice",
			assertErr: func(t *testing.T, err error) {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, "TOTP_BOOTSTRAP_CONSUMED")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := mocks.NewMockRepository(t)
			svc := &stubTOTPService{}
			tc.setup(repo, svc)

			out := &bytes.Buffer{}
			err := runBootstrapEnroll(context.Background(), repo, svc, tc.username, out)
			tc.assertErr(t, err)
			if tc.assertOut != nil {
				tc.assertOut(t, out.String())
			}
		})
	}
}

// runEnroll cases (table-driven).

func TestRunEnroll(t *testing.T) {
	pid := ulid.Make()
	cases := []struct {
		name      string
		validator *stubValidator
		setupSvc  func(*stubTOTPService)
		assertErr func(t *testing.T, err error)
		assertOut func(t *testing.T, out string)
	}{
		{
			name:      "happy path",
			validator: &stubValidator{player: &auth.Player{ID: pid, Username: "alice"}},
			setupSvc: func(svc *stubTOTPService) {
				svc.enrollResult = totp.EnrollResult{
					Enrollment:      sampleEnrollment(),
					AuditEnrolledAt: time.Now().UTC(),
					AuditPlayerID:   pid,
				}
			},
			assertErr: func(t *testing.T, err error) { require.NoError(t, err) },
			assertOut: func(t *testing.T, out string) { assert.Contains(t, out, "TOTP enrolled for alice") },
		},
		{
			name:      "validator rejects credentials",
			validator: &stubValidator{err: errors.New("bad password")},
			setupSvc:  func(_ *stubTOTPService) {},
			assertErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "bad password")
			},
		},
		{
			name:      "service error propagates",
			validator: &stubValidator{player: &auth.Player{ID: pid, Username: "alice"}},
			setupSvc:  func(svc *stubTOTPService) { svc.enrollErr = totp.ErrAlreadyEnrolled },
			assertErr: func(t *testing.T, err error) {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, "TOTP_ALREADY_ENROLLED")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubTOTPService{}
			tc.setupSvc(svc)

			out := &bytes.Buffer{}
			err := runEnroll(context.Background(), tc.validator, svc, "alice", "hunter2", out)
			tc.assertErr(t, err)
			if tc.assertOut != nil {
				tc.assertOut(t, out.String())
			}
		})
	}
}

// runRecover cases. The atomic-error case mirrors the runBootstrapEnroll
// shape; the timing-safe cases (lookup hide / bad-ulid hide) and the
// write-error case are kept separate where the assertion shape differs.

func TestRunRecoverHappyPath(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	svc := &stubTOTPService{
		recoverAndClearResult: totp.RecoverAndClearResult{
			RecoveryCodeID:  ulid.Make(),
			WasEnrolled:     true,
			AuditConsumedAt: time.Now().UTC(),
			AuditClearedAt:  time.Now().UTC(),
			AuditPlayerID:   pid,
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
	errutil.AssertErrorCode(t, err, "TOTP_INVALID_RECOVERY_CODE")
}

func TestRunRecoverHidesMalformedPlayerID(t *testing.T) {
	ctx := context.Background()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return("not-a-ulid", nil)

	out := &bytes.Buffer{}
	err := runRecover(ctx, repo, &stubTOTPService{}, "alice", "aaaa-bbbb-cccc-dddd", out)
	errutil.AssertErrorCode(t, err, "TOTP_INVALID_RECOVERY_CODE")
}

func TestRunRecoverPropagatesAtomicError(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	// RecoverAndClear surfaces a wrapped TOTP_INVALID_RECOVERY_CODE
	// when the recovery code doesn't match (Service wraps the repo's
	// ErrInvalidRecoveryCode with oops.With("player_id", ...)).
	svc := &stubTOTPService{recoverAndClearErr: totp.ErrInvalidRecoveryCode}

	out := &bytes.Buffer{}
	err := runRecover(ctx, repo, svc, "alice", "aaaa", out)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_INVALID_RECOVERY_CODE")
}

func TestRunRecoverWriteFailureSurfaces(t *testing.T) {
	ctx := context.Background()
	pid := ulid.Make()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().PlayerIDFromUsername(ctx, "alice").Return(pid.String(), nil)

	svc := &stubTOTPService{
		recoverAndClearResult: totp.RecoverAndClearResult{
			RecoveryCodeID: ulid.Make(),
			WasEnrolled:    true,
		},
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
	bootstrapResult         totp.BootstrapResult
	bootstrapErr            error
	enrollResult            totp.EnrollResult
	enrollErr               error
	consumeResult           totp.ConsumeRecoveryResult
	consumeErr              error
	clearResult             totp.ClearResult
	clearErr                error
	recoverAndClearResult   totp.RecoverAndClearResult
	recoverAndClearErr      error
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

func (s *stubTOTPService) RecoverAndClear(_ context.Context, _ ulid.ULID, _ string) (totp.RecoverAndClearResult, error) {
	return s.recoverAndClearResult, s.recoverAndClearErr
}

