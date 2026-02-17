// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

// MockAccessPolicyEngine is a mock for testing checkAccess.
type MockAccessPolicyEngine struct {
	mock.Mock
}

func (m *MockAccessPolicyEngine) Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(types.Decision), args.Error(1)
}

// logCapture is a test slog handler that captures log records.
type logCapture struct {
	records []slog.Record
	mu      sync.Mutex
}

func (lc *logCapture) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (lc *logCapture) Handle(_ context.Context, r slog.Record) error {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.records = append(lc.records, r)
	return nil
}
func (lc *logCapture) WithAttrs(_ []slog.Attr) slog.Handler { return lc }
func (lc *logCapture) WithGroup(_ string) slog.Handler      { return lc }

func (lc *logCapture) findRecord(msg string) *slog.Record {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for i := range lc.records {
		if lc.records[i].Message == msg {
			return &lc.records[i]
		}
	}
	return nil
}

func (lc *logCapture) getAttr(r *slog.Record, key string) (slog.Value, bool) {
	var val slog.Value
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			val = a.Value
			found = true
			return false
		}
		return true
	})
	return val, found
}

func TestCheckAccess(t *testing.T) {
	ctx := context.Background()
	subject := "user:123"
	action := "read"
	resource := "location:456"

	t.Run("returns nil when access is allowed", func(t *testing.T) {
		engine := new(MockAccessPolicyEngine)
		engine.On("Evaluate", ctx, types.AccessRequest{
			Subject: subject, Action: action, Resource: resource,
		}).Return(types.NewDecision(types.EffectAllow, "policy matched", "policy-1"), nil)

		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, action, resource, "LOCATION")

		assert.NoError(t, err)
		engine.AssertExpectations(t)
	})

	t.Run("returns LOCATION_ACCESS_DENIED when permission denied", func(t *testing.T) {
		engine := new(MockAccessPolicyEngine)
		engine.On("Evaluate", ctx, types.AccessRequest{
			Subject: subject, Action: action, Resource: resource,
		}).Return(types.NewDecision(types.EffectDefaultDeny, "no policy match", ""), nil)

		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, action, resource, "LOCATION")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_DENIED")
		assert.ErrorIs(t, err, ErrPermissionDenied)
		engine.AssertExpectations(t)
	})

	t.Run("returns LOCATION_ACCESS_EVALUATION_FAILED on engine error", func(t *testing.T) {
		engineErr := errors.New("policy engine down")
		engine := new(MockAccessPolicyEngine)
		engine.On("Evaluate", ctx, types.AccessRequest{
			Subject: subject, Action: action, Resource: resource,
		}).Return(types.Decision{}, engineErr)

		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, action, resource, "LOCATION")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, ErrAccessEvaluationFailed)
		engine.AssertExpectations(t)
	})

	t.Run("uses entity prefix to generate error codes", func(t *testing.T) {
		engine := new(MockAccessPolicyEngine)
		engine.On("Evaluate", ctx, types.AccessRequest{
			Subject: subject, Action: action, Resource: resource,
		}).Return(types.NewDecision(types.EffectDeny, "denied", "policy-2"), nil)

		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, action, resource, "EXIT")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_DENIED")
		assert.ErrorIs(t, err, ErrPermissionDenied)
		engine.AssertExpectations(t)
	})

	t.Run("wraps context.Canceled as evaluation failure", func(t *testing.T) {
		engine := new(MockAccessPolicyEngine)
		engine.On("Evaluate", ctx, types.AccessRequest{
			Subject: subject, Action: action, Resource: resource,
		}).Return(types.Decision{}, context.Canceled)

		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, action, resource, "LOCATION")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, context.Canceled)
		engine.AssertExpectations(t)
	})

	t.Run("wraps context.DeadlineExceeded as evaluation failure", func(t *testing.T) {
		engine := new(MockAccessPolicyEngine)
		engine.On("Evaluate", ctx, types.AccessRequest{
			Subject: subject, Action: action, Resource: resource,
		}).Return(types.Decision{}, context.DeadlineExceeded)

		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, action, resource, "LOCATION")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		engine.AssertExpectations(t)
	})

	t.Run("preserves oops error chain on engine error", func(t *testing.T) {
		inner := oops.Errorf("inner problem")
		engine := new(MockAccessPolicyEngine)
		engine.On("Evaluate", ctx, types.AccessRequest{
			Subject: subject, Action: action, Resource: resource,
		}).Return(types.Decision{}, inner)

		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, action, resource, "EXIT")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "EXIT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, ErrAccessEvaluationFailed)
		engine.AssertExpectations(t)
	})

	t.Run("returns evaluation failure for empty subject", func(t *testing.T) {
		engine := new(MockAccessPolicyEngine)
		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, "", action, resource, "LOCATION")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, ErrAccessEvaluationFailed)
		engine.AssertNotCalled(t, "Evaluate") // engine should never be called
	})

	t.Run("returns evaluation failure for empty action", func(t *testing.T) {
		engine := new(MockAccessPolicyEngine)
		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, "", resource, "LOCATION")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, ErrAccessEvaluationFailed)
		engine.AssertNotCalled(t, "Evaluate") // engine should never be called
	})

	t.Run("returns evaluation failure for empty resource", func(t *testing.T) {
		engine := new(MockAccessPolicyEngine)
		svc := &Service{engine: engine}
		err := svc.checkAccess(ctx, subject, action, "", "LOCATION")

		assert.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, err, ErrAccessEvaluationFailed)
		engine.AssertNotCalled(t, "Evaluate") // engine should never be called
	})

	t.Run("logs structured fields on engine error", func(t *testing.T) {
		// Set up log capture
		capture := &logCapture{}
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(capture))
		defer slog.SetDefault(oldLogger)

		engineErr := errors.New("policy engine timeout")
		engine := new(MockAccessPolicyEngine)

		// Create the expected request using the constructor
		expectedReq, _ := types.NewAccessRequest(subject, action, resource)
		engine.On("Evaluate", ctx, expectedReq).Return(types.Decision{}, engineErr)

		svc := &Service{engine: engine}
		_ = svc.checkAccess(ctx, subject, action, resource, "LOCATION")

		// Verify log record exists with correct message
		record := capture.findRecord("access evaluation failed")
		require.NotNil(t, record, "expected log record 'access evaluation failed'")

		// Verify structured fields
		subjectVal, ok := capture.getAttr(record, "subject")
		assert.True(t, ok, "missing 'subject' field")
		assert.Equal(t, subject, subjectVal.String())

		actionVal, ok := capture.getAttr(record, "action")
		assert.True(t, ok, "missing 'action' field")
		assert.Equal(t, action, actionVal.String())

		resourceVal, ok := capture.getAttr(record, "resource")
		assert.True(t, ok, "missing 'resource' field")
		assert.Equal(t, resource, resourceVal.String())

		errorVal, ok := capture.getAttr(record, "error")
		assert.True(t, ok, "missing 'error' field")
		assert.Contains(t, errorVal.String(), "policy engine timeout")

		engine.AssertExpectations(t)
	})
}
