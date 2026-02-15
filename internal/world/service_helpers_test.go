// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

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
}
