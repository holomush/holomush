// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/pkg/errutil"
)

// seedDEKRow inserts a fixture crypto_keys row for FK resolution.
func seedDEKRow(t *testing.T, pool *pgxpool.Pool, id int64, ctxType, ctxID string, version uint32) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO crypto_keys (id, context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants, created_at)
         VALUES ($1, $2, $3, $4, '\x00', 'test', 'test', '[]'::jsonb, now())`,
		id, ctxType, ctxID, version)
	require.NoError(t, err)
}

// mustOpenCheckpoint opens a checkpoint row and returns the RequestID.
// Convenience wrapper used by multiple tests.
func mustOpenCheckpoint(t *testing.T, repo *dek.CheckpointRepo, ctxID string, oldDEK int64) dek.RequestID {
	t.Helper()
	req, err := dek.NewCheckpointOpenRequest(
		"scene", ctxID,
		make([]byte, 32), make([]byte, 32),
		"01PLAYER", oldDEK,
	)
	require.NoError(t, err)
	rid, err := repo.Open(context.Background(), req)
	require.NoError(t, err)
	return rid
}

func TestCheckpointRepo_Open_ReturnsRequestID(t *testing.T) {
	pool := testIntegrationPool(t)
	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)

	repo := dek.NewCheckpointRepo(pool)
	req, err := dek.NewCheckpointOpenRequest(
		"scene", "01ABC",
		make([]byte, 32), make([]byte, 32),
		"01PLAYER", 100,
	)
	require.NoError(t, err)
	rid, err := repo.Open(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, rid, 16, "ULID is 16 bytes")
}

func TestCheckpointRepo_Open_ConcurrentSameContext_Rejected(t *testing.T) {
	pool := testIntegrationPool(t)
	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)
	repo := dek.NewCheckpointRepo(pool)

	req1, err := dek.NewCheckpointOpenRequest(
		"scene", "01ABC",
		make([]byte, 32), make([]byte, 32),
		"01P1", 100,
	)
	require.NoError(t, err)
	_, err = repo.Open(context.Background(), req1)
	require.NoError(t, err)

	req2, err := dek.NewCheckpointOpenRequest(
		"scene", "01ABC",
		make([]byte, 32), make([]byte, 32),
		"01P2", 100,
	)
	require.NoError(t, err)
	_, err = repo.Open(context.Background(), req2)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_ALREADY_IN_PROGRESS")
}

func TestCheckpointRepo_UpdateStatus_CASRejectsStaleWriter(t *testing.T) {
	pool := testIntegrationPool(t)
	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)
	repo := dek.NewCheckpointRepo(pool)
	rid := mustOpenCheckpoint(t, repo, "01ABC", 100)

	// Row is at 'pending'. Try a transition from Phase2MintDEK → Phase3ReencryptCold
	// which the FSM allows, but the CAS predicate (status = 'phase2_mint_dek') fails
	// because the row is actually 'pending'. (INV-E1)
	err := repo.UpdateStatus(
		context.Background(), rid,
		dek.CheckpointStatusPhase2MintDEK,
		dek.CheckpointStatusPhase3ReencryptCold,
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_STALE_TRANSITION")
}

func TestCheckpointRepo_Heartbeat_UpdatesTimestamp(t *testing.T) {
	pool := testIntegrationPool(t)
	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)
	repo := dek.NewCheckpointRepo(pool)
	rid := mustOpenCheckpoint(t, repo, "01ABC", 100)

	ckpt, err := repo.Get(context.Background(), rid)
	require.NoError(t, err)
	initial := ckpt.LastHeartbeatAt
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, repo.Heartbeat(context.Background(), rid))
	ckpt2, err := repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.True(t, ckpt2.LastHeartbeatAt.After(initial))
}

func TestCheckpointRepo_FindByContextAndArgs_Resume(t *testing.T) {
	pool := testIntegrationPool(t)
	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)
	repo := dek.NewCheckpointRepo(pool)

	opArgs := make([]byte, 32)
	opArgs[0] = 0xAB
	policy := make([]byte, 32)
	policy[0] = 0xCD

	req, err := dek.NewCheckpointOpenRequest(
		"scene", "01ABC",
		opArgs, policy,
		"01PLAYER", 100,
	)
	require.NoError(t, err)
	rid, err := repo.Open(context.Background(), req)
	require.NoError(t, err)

	ckpt, found, err := repo.FindByContextAndArgs(context.Background(), "scene", "01ABC", opArgs)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, rid, ckpt.RequestID)

	// Different op_args_hash → not found.
	other := make([]byte, 32)
	other[0] = 0xEF
	_, found, err = repo.FindByContextAndArgs(context.Background(), "scene", "01ABC", other)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCheckpointRepo_ListExpired(t *testing.T) {
	pool := testIntegrationPool(t)
	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)
	repo := dek.NewCheckpointRepo(pool)
	rid := mustOpenCheckpoint(t, repo, "01ABC", 100)

	// Backdate heartbeat by 25h.
	_, err := pool.Exec(context.Background(),
		`UPDATE crypto_rekey_checkpoints SET last_heartbeat_at = now() - interval '25 hours' WHERE request_id = $1`,
		rid[:])
	require.NoError(t, err)

	expired, err := repo.ListExpired(context.Background(), 24*time.Hour)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, rid, expired[0].RequestID)
}

func TestCheckpointRepo_MarkAborted_AndPersistsReason(t *testing.T) {
	pool := testIntegrationPool(t)
	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)
	repo := dek.NewCheckpointRepo(pool)
	rid := mustOpenCheckpoint(t, repo, "01ABC", 100)

	require.NoError(t, repo.MarkAborted(context.Background(), rid, "operator_abort"))
	ckpt, err := repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusAborted, ckpt.Status)
	require.NotNil(t, ckpt.AbortedAt)
	require.Equal(t, "operator_abort", *ckpt.AbortedReason)
}
