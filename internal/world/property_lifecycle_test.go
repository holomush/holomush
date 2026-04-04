// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/holomush/holomush/pkg/errutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockOrphanFinder struct {
	countResult     int
	countErr        error
	deleteResult    int
	deleteErr       error
	deleteCalled    bool
	deleteOlderThan time.Duration
}

func (m *mockOrphanFinder) CountOrphans(_ context.Context) (int, error) {
	return m.countResult, m.countErr
}

func (m *mockOrphanFinder) DeleteOrphans(_ context.Context, olderThan time.Duration) (int, error) {
	m.deleteCalled = true
	m.deleteOlderThan = olderThan
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

func TestOrphanDetectorStartupCheckSucceedsAboveThreshold(t *testing.T) {
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(&mockOrphanFinder{countResult: 150})
	err := detector.StartupCheck(context.Background())
	require.NoError(t, err)
}

func TestOrphanDetectorStartupCheckNoErrorBelowThreshold(t *testing.T) {
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(&mockOrphanFinder{countResult: 5})
	err := detector.StartupCheck(context.Background())
	require.NoError(t, err)
}

func TestOrphanDetectorCleanupWarnsFirstThenDeletes(t *testing.T) {
	finder := &mockOrphanFinder{countResult: 5, deleteResult: 3}
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(finder)

	detector.RunCleanup(context.Background())
	assert.False(t, finder.deleteCalled)

	detector.RunCleanup(context.Background())
	assert.True(t, finder.deleteCalled)
	assert.Equal(t, 24*time.Hour, finder.deleteOlderThan)
}

func TestOrphanDetectorCleanupNoOrphansNoDeletion(t *testing.T) {
	finder := &mockOrphanFinder{countResult: 0}
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(finder)

	detector.RunCleanup(context.Background())
	assert.False(t, finder.deleteCalled)
}

func TestOrphanDetectorStartupCheckNilFinderReturnsNil(t *testing.T) {
	detector := NewOrphanDetector(DefaultOrphanConfig())
	// No finder set — should return nil without error
	err := detector.StartupCheck(context.Background())
	require.NoError(t, err)
}

func TestOrphanDetectorStartupCheckCountError(t *testing.T) {
	countErr := errors.New("database unavailable")
	finder := &mockOrphanFinder{countErr: countErr}
	detector := NewOrphanDetector(DefaultOrphanConfig())
	detector.SetFinder(finder)

	err := detector.StartupCheck(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, countErr)
	errutil.AssertErrorCode(t, err, "ORPHAN_STARTUP_CHECK_FAILED")
}

func TestOrphanDetectorRunCleanupNilFinderDoesNothing(t *testing.T) {
	detector := NewOrphanDetector(DefaultOrphanConfig())
	// No finder set — RunCleanup should be a no-op
	assert.NotPanics(t, func() {
		detector.RunCleanup(context.Background())
	})
}

func TestOrphanDetectorRunCleanupCountError(t *testing.T) {
	countErr := errors.New("scan failed")
	finder := &mockOrphanFinder{countErr: countErr}
	detector := NewOrphanDetector(DefaultOrphanConfig())
	detector.SetFinder(finder)

	// Should not panic; error is logged, not returned
	assert.NotPanics(t, func() {
		detector.RunCleanup(context.Background())
	})
	assert.False(t, finder.deleteCalled, "delete must not be called when count fails")
}

func TestOrphanDetectorRunCleanupDeleteError(t *testing.T) {
	deleteErr := errors.New("delete failed")
	finder := &mockOrphanFinder{countResult: 5, deleteErr: deleteErr}
	detector := NewOrphanDetector(OrphanConfig{Threshold: 100, GracePeriod: 24 * time.Hour, Interval: time.Hour})
	detector.SetFinder(finder)

	// First cycle: warns, no delete
	detector.RunCleanup(context.Background())
	assert.False(t, finder.deleteCalled)

	// Second cycle: attempts delete, which fails — should not panic
	assert.NotPanics(t, func() {
		detector.RunCleanup(context.Background())
	})
	assert.True(t, finder.deleteCalled)
}

func TestOrphanDetectorZeroIntervalDefaultsTo24h(t *testing.T) {
	detector := NewOrphanDetector(OrphanConfig{Interval: 0, GracePeriod: time.Hour, Threshold: 10})
	require.NotNil(t, detector)
	assert.Equal(t, 24*time.Hour, detector.config.Interval)
}
