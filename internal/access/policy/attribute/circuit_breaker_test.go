// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_StartsInClosedState(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), nil)
	assert.Equal(t, CircuitStateClosed, cb.State())
}

func TestCircuitBreaker_OpensOnHighBudgetUtilization(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), nil)

	for range 10 {
		cb.RecordCall(100*time.Millisecond, 80*time.Millisecond)
	}

	assert.Equal(t, CircuitStateOpen, cb.State())
}

func TestCircuitBreaker_StaysClosedBelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), nil)

	for range 10 {
		cb.RecordCall(10*time.Millisecond, 80*time.Millisecond)
	}

	assert.Equal(t, CircuitStateClosed, cb.State())
}

func TestCircuitBreaker_StaysClosedBelowMinCalls(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), nil)

	for range 5 {
		cb.RecordCall(100*time.Millisecond, 80*time.Millisecond)
	}

	assert.Equal(t, CircuitStateClosed, cb.State())
}

func TestCircuitBreaker_AllowsProbeAfterOpenDuration(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.OpenDuration = 100 * time.Millisecond
	cb := NewCircuitBreaker("test", config, nil)

	for range 10 {
		cb.RecordCall(100*time.Millisecond, 80*time.Millisecond)
	}
	require.Equal(t, CircuitStateOpen, cb.State())

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, CircuitStateHalfOpen, cb.State())
}

func TestCircuitBreaker_ProbeSuccess_ClosesCircuit(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.OpenDuration = 100 * time.Millisecond
	cb := NewCircuitBreaker("test", config, nil)

	for range 10 {
		cb.RecordCall(100*time.Millisecond, 80*time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, CircuitStateHalfOpen, cb.State())

	cb.RecordProbeSuccess()
	assert.Equal(t, CircuitStateClosed, cb.State())
}

func TestCircuitBreaker_ProbeFailure_ReopensCircuit(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	config.OpenDuration = 100 * time.Millisecond
	cb := NewCircuitBreaker("test", config, nil)

	for range 10 {
		cb.RecordCall(100*time.Millisecond, 80*time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, CircuitStateHalfOpen, cb.State())

	cb.RecordProbeFailure()
	assert.Equal(t, CircuitStateOpen, cb.State())
}

func TestCircuitBreaker_ShouldSkip(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig(), nil)

	assert.False(t, cb.ShouldSkip())

	for range 10 {
		cb.RecordCall(100*time.Millisecond, 80*time.Millisecond)
	}

	assert.True(t, cb.ShouldSkip())
}
