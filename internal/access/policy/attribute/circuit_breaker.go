// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

// Circuit breaker states.
const (
	CircuitStateClosed   CircuitState = iota // normal operation
	CircuitStateOpen                         // skip provider
	CircuitStateHalfOpen                     // single probe allowed
)

// CircuitBreakerConfig holds circuit breaker parameters.
type CircuitBreakerConfig struct {
	WindowDuration     time.Duration
	BudgetThreshold    float64
	CallRatioThreshold float64
	MinCalls           int
	OpenDuration       time.Duration
}

// DefaultCircuitBreakerConfig returns the MUST-level spec parameters.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		WindowDuration:     60 * time.Second,
		BudgetThreshold:    0.8,
		CallRatioThreshold: 0.5,
		MinCalls:           10,
		OpenDuration:       60 * time.Second,
	}
}

type callRecord struct {
	timestamp   time.Time
	utilization float64
}

// CircuitBreaker tracks provider health and short-circuits when degraded.
// CircuitBreaker tracks provider health and short-circuits when degraded.
type CircuitBreaker struct {
	mu            sync.Mutex
	provider      string
	config        CircuitBreakerConfig
	state         CircuitState
	calls         []callRecord
	openedAt      time.Time
	tripsCounter  prometheus.Counter
	probeInFlight bool
}

// NewCircuitBreaker creates a circuit breaker for the named provider.
func NewCircuitBreaker(provider string, config CircuitBreakerConfig, tripsCounter prometheus.Counter) *CircuitBreaker {
	return &CircuitBreaker{
		provider:     provider,
		config:       config,
		state:        CircuitStateClosed,
		calls:        make([]callRecord, 0, config.MinCalls*2),
		tripsCounter: tripsCounter,
	}
}

// maybeTransitionToHalfOpen transitions from open to half-open if the open
// duration has elapsed. Must be called with cb.mu held.
func (cb *CircuitBreaker) maybeTransitionToHalfOpen() {
	if cb.state == CircuitStateOpen && time.Since(cb.openedAt) >= cb.config.OpenDuration {
		cb.state = CircuitStateHalfOpen
		cb.probeInFlight = false
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.maybeTransitionToHalfOpen()
	return cb.state
}

// ShouldSkip returns true if the provider should be skipped (circuit open and no probe available).
func (cb *CircuitBreaker) ShouldSkip() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.maybeTransitionToHalfOpen()
	return cb.state == CircuitStateOpen || (cb.state == CircuitStateHalfOpen && cb.probeInFlight)
}

// TryAcquireProbe atomically checks if a probe is available and acquires it.
// Returns true only for the first caller when the circuit is half-open and no
// probe is in flight. Subsequent callers get false until the probe completes.
func (cb *CircuitBreaker) TryAcquireProbe() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.maybeTransitionToHalfOpen()
	if cb.state == CircuitStateHalfOpen && !cb.probeInFlight {
		cb.probeInFlight = true
		return true
	}
	return false
}

// IsProbe returns true if the circuit is half-open and should allow a single probe.
//
// Deprecated: Use TryAcquireProbe for safe one-shot probe admission.
func (cb *CircuitBreaker) IsProbe() bool {
	return cb.TryAcquireProbe()
}

// RecordCall records a provider call with its duration and budget.
func (cb *CircuitBreaker) RecordCall(actualDuration, budget time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	utilization := float64(actualDuration) / float64(budget)

	cb.calls = append(cb.calls, callRecord{
		timestamp:   time.Now(),
		utilization: utilization,
	})

	cb.pruneOldCalls()
	cb.checkTrip()
}

// RecordProbeSuccess closes the circuit after a successful probe.
func (cb *CircuitBreaker) RecordProbeSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitStateClosed
	cb.calls = cb.calls[:0]
	cb.probeInFlight = false
	slog.Info("provider circuit breaker closed after successful probe",
		"provider", cb.provider,
	)
}

// RecordProbeFailure re-opens the circuit after a failed probe.
func (cb *CircuitBreaker) RecordProbeFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitStateOpen
	cb.openedAt = time.Now()
	cb.probeInFlight = false
	slog.Warn("provider circuit breaker re-opened after failed probe",
		"provider", cb.provider,
	)
}

func (cb *CircuitBreaker) pruneOldCalls() {
	cutoff := time.Now().Add(-cb.config.WindowDuration)
	i := 0
	for i < len(cb.calls) && cb.calls[i].timestamp.Before(cutoff) {
		i++
	}
	if i > 0 {
		cb.calls = cb.calls[i:]
	}
}

func (cb *CircuitBreaker) checkTrip() {
	if cb.state != CircuitStateClosed {
		return
	}

	if len(cb.calls) < cb.config.MinCalls {
		return
	}

	exceeding := 0
	for _, call := range cb.calls {
		if call.utilization > cb.config.BudgetThreshold {
			exceeding++
		}
	}

	ratio := float64(exceeding) / float64(len(cb.calls))
	if ratio > cb.config.CallRatioThreshold {
		cb.state = CircuitStateOpen
		cb.openedAt = time.Now()
		slog.Warn("provider circuit breaker opened",
			"provider", cb.provider,
			"exceeding_calls", exceeding,
			"total_calls", len(cb.calls),
			"ratio", ratio,
		)
		if cb.tripsCounter != nil {
			cb.tripsCounter.Inc()
		}
	}
}
