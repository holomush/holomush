// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pruneDelete records one DeleteChannelLogOlderThan invocation.
type pruneDelete struct {
	subject string
	cutoff  time.Time
}

// fakePruneStore is a controllable channelPruneStore for prune sweep unit tests.
type fakePruneStore struct {
	infos     []channelPruneInfo
	listErr   error
	deleteErr error
	deletes   []pruneDelete
	counts    map[string]int64
}

func (f *fakePruneStore) ListChannelsForPrune(_ context.Context) ([]channelPruneInfo, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.infos, nil
}

func (f *fakePruneStore) DeleteChannelLogOlderThan(_ context.Context, subject string, cutoff time.Time) (int64, error) {
	f.deletes = append(f.deletes, pruneDelete{subject: subject, cutoff: cutoff})
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	return f.counts[subject], nil
}

func intPtr(n int) *int { return &n }

var fixedNow = time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

func newTestPruner(store channelPruneStore) *channelPruner {
	return &channelPruner{
		store:         store,
		gameID:        testGameID,
		defaultWindow: 720 * time.Hour,
		interval:      time.Hour,
		now:           func() time.Time { return fixedNow },
	}
}

func TestPruneSweepUsesDefaultWindowForNullRetentionNonAdmin(t *testing.T) {
	store := &fakePruneStore{infos: []channelPruneInfo{{ID: "ch1", Type: "public", RetentionDays: nil}}}
	p := newTestPruner(store)
	require.NoError(t, p.sweep(context.Background()))
	require.Len(t, store.deletes, 1)
	assert.Equal(t, dotStyleChannelSubject(testGameID, "ch1"), store.deletes[0].subject)
	assert.True(t, store.deletes[0].cutoff.Equal(fixedNow.Add(-720*time.Hour)),
		"cutoff must be now - default window")
}

func TestPruneSweepUsesPerChannelRetentionOverride(t *testing.T) {
	store := &fakePruneStore{infos: []channelPruneInfo{{ID: "ch2", Type: "private", RetentionDays: intPtr(7)}}}
	p := newTestPruner(store)
	require.NoError(t, p.sweep(context.Background()))
	require.Len(t, store.deletes, 1)
	assert.True(t, store.deletes[0].cutoff.Equal(fixedNow.Add(-7*24*time.Hour)),
		"cutoff must be now - retention_days")
}

func TestPruneSweepSkipsUnlimitedAdminChannel(t *testing.T) {
	store := &fakePruneStore{infos: []channelPruneInfo{{ID: "chAdmin", Type: "admin", RetentionDays: nil}}}
	p := newTestPruner(store)
	require.NoError(t, p.sweep(context.Background()))
	assert.Empty(t, store.deletes, "an admin channel with NULL retention is unlimited and MUST NOT be pruned")
}

func TestPruneSweepPrunesAdminChannelWithExplicitRetention(t *testing.T) {
	store := &fakePruneStore{infos: []channelPruneInfo{{ID: "chAdmin2", Type: "admin", RetentionDays: intPtr(30)}}}
	p := newTestPruner(store)
	require.NoError(t, p.sweep(context.Background()))
	require.Len(t, store.deletes, 1)
	assert.True(t, store.deletes[0].cutoff.Equal(fixedNow.Add(-30*24*time.Hour)),
		"an explicit retention on an admin channel is honored")
}

func TestPruneSweepEmptyBatchIsNoOp(t *testing.T) {
	store := &fakePruneStore{}
	p := newTestPruner(store)
	require.NoError(t, p.sweep(context.Background()))
	assert.Empty(t, store.deletes)
}

func TestPruneSweepContinuesPastPerChannelDeleteError(t *testing.T) {
	store := &fakePruneStore{
		infos:     []channelPruneInfo{{ID: "ch1", Type: "public"}, {ID: "ch2", Type: "public"}},
		deleteErr: errors.New("boom"),
	}
	p := newTestPruner(store)
	// A per-channel delete error is logged and the sweep continues over the batch.
	require.NoError(t, p.sweep(context.Background()))
	assert.Len(t, store.deletes, 2, "one failing delete must not abort the batch")
}

func TestPruneSweepReturnsErrorWhenListFails(t *testing.T) {
	store := &fakePruneStore{listErr: errors.New("db down")}
	p := newTestPruner(store)
	assert.Error(t, p.sweep(context.Background()))
}

func TestPrunerRunExitsOnContextCancel(t *testing.T) {
	store := &fakePruneStore{}
	p := newTestPruner(store)
	p.interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}
