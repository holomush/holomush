// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/outbox"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakeGenesisStore is a test double for the consumer-owned outbox.GenesisStore
// interface, letting the orchestration be tested without a database. It proves the
// GenesisService reaches storage ONLY through the interface (round-4 A3).
type fakeGenesisStore struct {
	snapshot    wmodel.GenesisSnapshotResult
	snapshotErr error
	reset       wmodel.EpochResetResult
	resetErr    error
	epoch       int64

	snapshotGame string
	resetGame    string
	snapshotRuns int
	resetRuns    int
}

func (f *fakeGenesisStore) EmitGenesisSnapshot(_ context.Context, gameID string) (wmodel.GenesisSnapshotResult, error) {
	f.snapshotRuns++
	f.snapshotGame = gameID
	return f.snapshot, f.snapshotErr
}

func (f *fakeGenesisStore) CurrentEpoch(_ context.Context, _ string) (int64, error) {
	return f.epoch, nil
}

func (f *fakeGenesisStore) AdvanceEpoch(_ context.Context, gameID string) (wmodel.EpochResetResult, error) {
	f.resetRuns++
	f.resetGame = gameID
	return f.reset, f.resetErr
}

// compile-time proof the fake satisfies the consumer-owned interface.
var _ outbox.GenesisStore = (*fakeGenesisStore)(nil)

func TestGenesisServiceEmitSnapshotDrivesTheStore(t *testing.T) {
	store := &fakeGenesisStore{snapshot: wmodel.GenesisSnapshotResult{Epoch: 3, Emitted: 4, Skipped: 1}}
	svc := outbox.NewGenesisService(store, "main", nil)

	res, err := svc.EmitSnapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), res.Epoch)
	assert.Equal(t, 4, res.Emitted)
	assert.Equal(t, 1, res.Skipped)
	assert.Equal(t, 1, store.snapshotRuns, "EmitSnapshot drives the store exactly once")
	assert.Equal(t, "main", store.snapshotGame, "the service's game id is threaded to the store")
}

func TestGenesisServiceEmitSnapshotWrapsStoreError(t *testing.T) {
	store := &fakeGenesisStore{snapshotErr: errors.New("boom")}
	svc := outbox.NewGenesisService(store, "main", nil)

	_, err := svc.EmitSnapshot(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "WORLD_GENESIS_SNAPSHOT_FAILED")
}

func TestGenesisServiceResetEpochDrivesTheStore(t *testing.T) {
	store := &fakeGenesisStore{reset: wmodel.EpochResetResult{PreviousEpoch: 1, NewEpoch: 2, Quarantined: 3, OriginPosition: 1}}
	svc := outbox.NewGenesisService(store, "arena", nil)

	res, err := svc.ResetEpoch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.NewEpoch)
	assert.Equal(t, int64(3), res.Quarantined)
	assert.Equal(t, int64(1), res.OriginPosition)
	assert.Equal(t, 1, store.resetRuns)
	assert.Equal(t, "arena", store.resetGame)
}

func TestGenesisServiceResetEpochWrapsStoreError(t *testing.T) {
	store := &fakeGenesisStore{resetErr: errors.New("boom")}
	svc := outbox.NewGenesisService(store, "main", nil)

	_, err := svc.ResetEpoch(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "WORLD_EPOCH_RESET_FAILED")
}
