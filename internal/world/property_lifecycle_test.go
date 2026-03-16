// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockOrphanFinder struct {
	countResult  int
	countErr     error
	deleteResult int
	deleteErr    error
	deleteCalled bool
}

func (m *mockOrphanFinder) CountOrphans(_ context.Context) (int, error) {
	return m.countResult, m.countErr
}

func (m *mockOrphanFinder) DeleteOrphans(_ context.Context, _ time.Duration) (int, error) {
	m.deleteCalled = true
	return m.deleteResult, m.deleteErr
}

func TestOrphanConfig_Defaults(t *testing.T) {
	config := DefaultOrphanConfig()
	assert.Equal(t, 24*time.Hour, config.GracePeriod)
	assert.Equal(t, 100, config.Threshold)
	assert.Equal(t, 24*time.Hour, config.Interval)
}

func TestOrphanDetector_Construction(t *testing.T) {
	detector := NewOrphanDetector(DefaultOrphanConfig())
	require.NotNil(t, detector)
}

func TestOrphanDetector_StartupCheck_LogsErrorAboveThreshold(t *testing.T) {
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(&mockOrphanFinder{countResult: 150})
	err := detector.StartupCheck(context.Background())
	require.NoError(t, err)
}

func TestOrphanDetector_StartupCheck_NoErrorBelowThreshold(t *testing.T) {
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(&mockOrphanFinder{countResult: 5})
	err := detector.StartupCheck(context.Background())
	require.NoError(t, err)
}

func TestOrphanDetector_Cleanup_WarnsFirstThenDeletes(t *testing.T) {
	finder := &mockOrphanFinder{countResult: 5, deleteResult: 3}
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(finder)

	detector.RunCleanup(context.Background())
	assert.False(t, finder.deleteCalled)

	detector.RunCleanup(context.Background())
	assert.True(t, finder.deleteCalled)
}

func TestOrphanDetector_Cleanup_NoOrphans_NoDeletion(t *testing.T) {
	finder := &mockOrphanFinder{countResult: 0}
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(finder)

	detector.RunCleanup(context.Background())
	assert.False(t, finder.deleteCalled)
}
