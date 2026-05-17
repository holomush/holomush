// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package privacytest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/testsupport/privacytest"
)

// TestPrivacyHarnessSmoke exercises the ConnectGuest → SendCommand →
// DrainEvents → Logout path end-to-end to verify the harness wiring is
// correct. It does NOT test privacy invariants (those live in iwzt.9+).
//
// Acceptance criterion for holomush-iwzt.6:
// task test:int -- -run TestPrivacyHarnessSmoke ./internal/testsupport/privacytest/
func TestPrivacyHarnessSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ts := privacytest.Start(t)
	defer ts.Stop()

	sess := ts.ConnectGuest(ctx)
	require.NotEmpty(t, sess.SessionID)

	require.NoError(t, sess.SendCommand(ctx, "look"))

	// Smoke-test event delivery: wait briefly for ANY event; tolerate empty
	// (event flow exercised by Task 9+ integration tests).
	_ = sess.DrainEvents(ctx, 250*time.Millisecond)

	sess.Logout(ctx)
}
