// Copyright 2026 HoloMUSH Contributors

package command

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access/accesstest"
)

func TestMetricsRecorder_RecordsExecution(t *testing.T) {
	recorder := NewMetricsRecorder()
	recorder.SetCommandName("metrics_recorder_success")
	recorder.SetCommandSource("core")
	recorder.SetStatus(StatusSuccess)

	before := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_recorder_success", "source": "core", "status": StatusSuccess,
	}))

	recorder.Record()

	after := testutil.ToFloat64(CommandExecutions.With(prometheus.Labels{
		"command": "metrics_recorder_success", "source": "core", "status": StatusSuccess,
	}))

	assert.Equal(t, before+1, after)
}

func TestRateLimitMiddleware_Enforce(t *testing.T) {
	mockAccess := accesstest.NewMockAccessControl()

	ratelimiter := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 1,
		SustainedRate: 0.1,
	})
	defer ratelimiter.Close()

	middleware := NewRateLimitMiddleware(ratelimiter, mockAccess)

	charID := ulid.Make()
	sessionID := ulid.Make()
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		SessionID:   sessionID,
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})

	ctx := context.Background()
	span := trace.SpanFromContext(ctx)

	// First command allowed
	err := middleware.Enforce(ctx, exec, "ratelimit", span)
	require.NoError(t, err)

	// Second command limited
	err = middleware.Enforce(ctx, exec, "ratelimit", span)
	require.Error(t, err)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, CodeRateLimited, oopsErr.Code())
}

func TestRateLimitMiddleware_BypassCapability(t *testing.T) {
	mockAccess := accesstest.NewMockAccessControl()

	ratelimiter := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 1,
		SustainedRate: 0.1,
	})
	defer ratelimiter.Close()

	middleware := NewRateLimitMiddleware(ratelimiter, mockAccess)

	charID := ulid.Make()
	sessionID := ulid.Make()
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		SessionID:   sessionID,
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})

	mockAccess.Grant("char:"+charID.String(), "execute", CapabilityRateLimitBypass)

	ctx := context.Background()
	span := trace.SpanFromContext(ctx)

	for i := 0; i < 3; i++ {
		err := middleware.Enforce(ctx, exec, "ratelimit", span)
		require.NoError(t, err)
	}
}
