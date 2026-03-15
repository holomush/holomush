// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
)

func TestPgListener_ImplementsInterface(_ *testing.T) {
	var _ policy.Listener = (*policy.PgListener)(nil)
}

func TestPgListener_CancelsOnContextDone(t *testing.T) {
	listener := policy.NewPgListener("postgres://invalid:5432/nonexistent")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch, err := listener.Listen(ctx)
	require.NoError(t, err)

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should close on context cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}
