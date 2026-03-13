// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestMockSessionResolver_Success(t *testing.T) {
	resolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, _ string) (string, error) {
			return "01ABC", nil
		},
	}

	characterID, err := resolver.ResolveSession(context.Background(), "web-123")
	require.NoError(t, err)
	assert.Equal(t, "01ABC", characterID)
}

func TestMockSessionResolver_SessionInvalid(t *testing.T) {
	resolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, _ string) (string, error) {
			return "", oops.Code("SESSION_INVALID").Errorf("session not found")
		},
	}

	characterID, err := resolver.ResolveSession(context.Background(), "invalid-999")
	require.Error(t, err)
	assert.Empty(t, characterID)

	errutil.AssertErrorCode(t, err, "SESSION_INVALID")
}

func TestMockSessionResolver_GenericError(t *testing.T) {
	resolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, _ string) (string, error) {
			return "", oops.Errorf("database connection failed")
		},
	}

	characterID, err := resolver.ResolveSession(context.Background(), "web-123")
	require.Error(t, err)
	assert.Empty(t, characterID)

	// Should not have SESSION_INVALID code
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.NotEqual(t, "SESSION_INVALID", oopsErr.Code())
}

func TestMockSessionResolver_NotConfigured(t *testing.T) {
	resolver := &mockSessionResolver{}

	characterID, err := resolver.ResolveSession(context.Background(), "web-123")
	require.Error(t, err)
	assert.Empty(t, characterID)
	assert.Contains(t, err.Error(), "mock not configured")
}

func TestMockSessionResolver_PassesThroughSessionID(t *testing.T) {
	var capturedSessionID string
	resolver := &mockSessionResolver{
		resolveFunc: func(_ context.Context, sessionID string) (string, error) {
			capturedSessionID = sessionID
			return "01ABC", nil
		},
	}

	_, err := resolver.ResolveSession(context.Background(), "web-999")
	require.NoError(t, err)
	assert.Equal(t, "web-999", capturedSessionID)
}

func TestMockSessionResolver_PassesThroughContext(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test"

	var capturedCtx context.Context
	resolver := &mockSessionResolver{
		resolveFunc: func(ctx context.Context, _ string) (string, error) {
			capturedCtx = ctx
			return "01ABC", nil
		},
	}

	ctx := context.WithValue(context.Background(), testKey, "test-value")
	_, err := resolver.ResolveSession(ctx, "web-123")
	require.NoError(t, err)

	assert.Equal(t, "test-value", capturedCtx.Value(testKey))
}
