// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

type fixedReporter struct {
	status lifecycle.HealthStatus
}

func (r *fixedReporter) HealthStatus() lifecycle.HealthStatus { return r.status }

func warmReporter() *fixedReporter {
	return &fixedReporter{status: lifecycle.HealthStatus{Tier: lifecycle.HealthWarm}}
}

func staleReporter() *fixedReporter {
	return &fixedReporter{status: lifecycle.HealthStatus{Tier: lifecycle.HealthStale}}
}

func TestReadinessRegistryEmptyIsReady(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	assert.True(t, reg.AllReady())
}

func TestReadinessRegistryAllWarmIsReady(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, warmReporter())
	reg.Register(lifecycle.SubsystemDatabase, warmReporter())
	assert.True(t, reg.AllReady())
}

func TestReadinessRegistryOneStaleNotReady(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, warmReporter())
	reg.Register(lifecycle.SubsystemDatabase, staleReporter())
	assert.False(t, reg.AllReady())
}

func TestReadinessRegistryDegradedIsReady(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, &fixedReporter{
		status: lifecycle.HealthStatus{Tier: lifecycle.HealthDegraded},
	})
	assert.True(t, reg.AllReady())
}

func TestReadinessRegistryStatusReturnsAll(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, warmReporter())
	reg.Register(lifecycle.SubsystemDatabase, staleReporter())

	status := reg.Status()
	require.Len(t, status, 2)
	assert.Equal(t, lifecycle.HealthWarm, status[lifecycle.SubsystemABAC].Tier)
	assert.Equal(t, lifecycle.HealthStale, status[lifecycle.SubsystemDatabase].Tier)
}

func TestReadinessRegistryWaitReadyImmediateSuccess(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, warmReporter())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := reg.WaitReady(ctx)
	require.NoError(t, err)
}

func TestReadinessRegistryWaitReadyTimeout(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemDatabase, staleReporter())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := reg.WaitReady(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
