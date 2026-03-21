// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	cryptotls "crypto/tls"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notFoundErr wraps os.ErrNotExist to simulate file-not-found through oops wrapping.
func notFoundErr(msg string) error {
	return fmt.Errorf("%s: %w", msg, os.ErrNotExist)
}

func TestWaitForTLSCerts_ImmediateSuccess(t *testing.T) {
	deps := &GatewayDeps{}
	deps.GameIDExtractor = func(_ string) (string, error) {
		return "test-game", nil
	}
	deps.ClientTLSLoader = func(_, _, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}
	deps.ControlTLSLoader = func(_, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}

	result, err := waitForTLSCerts(context.Background(), deps, "/fake/certs", "gateway", time.Second)
	require.NoError(t, err)
	assert.Equal(t, "test-game", result.gameID)
	assert.NotNil(t, result.clientTLS)
	assert.NotNil(t, result.controlTLS)
}

func TestWaitForTLSCerts_PollsUntilAvailable(t *testing.T) {
	var calls atomic.Int32

	deps := &GatewayDeps{}
	deps.GameIDExtractor = func(_ string) (string, error) {
		n := calls.Add(1)
		if n <= 2 {
			return "", notFoundErr("root-ca.crt not found")
		}
		return "test-game", nil
	}
	deps.ClientTLSLoader = func(_, _, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}
	deps.ControlTLSLoader = func(_, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}

	start := time.Now()
	result, err := waitForTLSCerts(context.Background(), deps, "/fake/certs", "gateway", 5*time.Second)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "test-game", result.gameID)
	assert.GreaterOrEqual(t, calls.Load(), int32(3), "should have polled at least 3 times")
	assert.GreaterOrEqual(t, elapsed, 800*time.Millisecond, "should have waited for at least 2 poll intervals")
}

func TestWaitForTLSCerts_PermanentErrorFailsImmediately(t *testing.T) {
	permanentErr := fmt.Errorf("invalid certificate format")

	deps := &GatewayDeps{}
	deps.GameIDExtractor = func(_ string) (string, error) {
		return "", permanentErr
	}
	deps.ClientTLSLoader = func(_, _, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}
	deps.ControlTLSLoader = func(_, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}

	start := time.Now()
	_, err := waitForTLSCerts(context.Background(), deps, "/fake/certs", "gateway", 5*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid certificate format")
	assert.Less(t, elapsed, 500*time.Millisecond, "permanent errors should fail immediately without polling")
}

func TestWaitForTLSCerts_ClientTLSTransientRetries(t *testing.T) {
	var clientCalls atomic.Int32

	deps := &GatewayDeps{}
	deps.GameIDExtractor = func(_ string) (string, error) {
		return "test-game", nil
	}
	deps.ClientTLSLoader = func(_, _, _ string) (*cryptotls.Config, error) {
		n := clientCalls.Add(1)
		if n <= 2 {
			return nil, notFoundErr("gateway.crt not found")
		}
		return &cryptotls.Config{}, nil
	}
	deps.ControlTLSLoader = func(_, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}

	result, err := waitForTLSCerts(context.Background(), deps, "/fake/certs", "gateway", 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "test-game", result.gameID)
	assert.GreaterOrEqual(t, clientCalls.Load(), int32(3))
}

func TestWaitForTLSCerts_ControlTLSTransientRetries(t *testing.T) {
	var controlCalls atomic.Int32

	deps := &GatewayDeps{}
	deps.GameIDExtractor = func(_ string) (string, error) {
		return "test-game", nil
	}
	deps.ClientTLSLoader = func(_, _, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}
	deps.ControlTLSLoader = func(_, _ string) (*cryptotls.Config, error) {
		n := controlCalls.Add(1)
		if n <= 2 {
			return nil, notFoundErr("gateway.crt not found")
		}
		return &cryptotls.Config{}, nil
	}

	result, err := waitForTLSCerts(context.Background(), deps, "/fake/certs", "gateway", 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "test-game", result.gameID)
	assert.GreaterOrEqual(t, controlCalls.Load(), int32(3))
}

func TestWaitForTLSCerts_Timeout(t *testing.T) {
	deps := &GatewayDeps{}
	deps.GameIDExtractor = func(_ string) (string, error) {
		return "", notFoundErr("root-ca.crt not found")
	}
	deps.ClientTLSLoader = func(_, _, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}
	deps.ControlTLSLoader = func(_, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}

	start := time.Now()
	_, err := waitForTLSCerts(context.Background(), deps, "/fake/certs", "gateway", time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, elapsed >= 900*time.Millisecond, "should wait close to the timeout")
	assert.True(t, elapsed < 2*time.Second, "should not wait much longer than timeout")
}

func TestWaitForTLSCerts_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	deps := &GatewayDeps{}
	deps.GameIDExtractor = func(_ string) (string, error) {
		return "", notFoundErr("root-ca.crt not found")
	}
	deps.ClientTLSLoader = func(_, _, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}
	deps.ControlTLSLoader = func(_, _ string) (*cryptotls.Config, error) {
		return &cryptotls.Config{}, nil
	}

	// Cancel after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	_, err := waitForTLSCerts(ctx, deps, "/fake/certs", "gateway", 30*time.Second)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestIsTransientCertError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		transient bool
	}{
		{"nil error", nil, false},
		{"file not found", os.ErrNotExist, true},
		{"wrapped file not found", fmt.Errorf("reading cert: %w", os.ErrNotExist), true},
		{"deeply wrapped file not found", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", os.ErrNotExist)), true},
		{"permission denied", os.ErrPermission, false},
		{"generic error", fmt.Errorf("invalid certificate"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.transient, isTransientCertError(tt.err))
		})
	}
}
