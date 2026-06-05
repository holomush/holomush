// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/pkg/errutil"
)

// seedDEKRow inserts a fixture crypto_keys row for FK resolution.
func seedDEKRow(t *testing.T, pool *pgxpool.Pool, id int64, ctxType, ctxID string, version uint32) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO crypto_keys (id, context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants, created_at)
         VALUES ($1, $2, $3, $4, '\x00', 'test', 'test', '[]'::jsonb, $5)`,
		id, ctxType, ctxID, version, pgnanos.From(time.Now()))
	Expect(err).NotTo(HaveOccurred())
}

// mustOpenCheckpoint opens a checkpoint row and returns the RequestID.
// Convenience wrapper used by multiple tests.
func mustOpenCheckpoint(t *testing.T, repo *dek.CheckpointRepo, ctxID string, oldDEK int64) dek.RequestID {
	t.Helper()
	req, err := dek.NewCheckpointOpenRequest(
		"scene", ctxID,
		make([]byte, 32), make([]byte, 32),
		"01PLAYER", oldDEK,
		"",
	)
	Expect(err).NotTo(HaveOccurred())
	rid, err := repo.Open(context.Background(), req)
	Expect(err).NotTo(HaveOccurred())
	return rid
}

// Phase 5/6 DEK lifecycle — CheckpointRepo integration specs.
var _ = Describe("CheckpointRepo", func() {
	It("Open returns a RequestID", func() {
		pool := testIntegrationPool(suiteT)
		seedDEKRow(suiteT, pool, 100, "scene", "01ABC", 3)

		repo := dek.NewCheckpointRepo(pool)
		req, err := dek.NewCheckpointOpenRequest(
			"scene", "01ABC",
			make([]byte, 32), make([]byte, 32),
			"01PLAYER", 100,
			"",
		)
		Expect(err).NotTo(HaveOccurred())
		rid, err := repo.Open(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(rid).To(HaveLen(16), "ULID is 16 bytes")
	})

	It("Open concurrent same context is rejected", func() {
		pool := testIntegrationPool(suiteT)
		seedDEKRow(suiteT, pool, 100, "scene", "01ABC", 3)
		repo := dek.NewCheckpointRepo(pool)

		req1, err := dek.NewCheckpointOpenRequest(
			"scene", "01ABC",
			make([]byte, 32), make([]byte, 32),
			"01P1", 100,
			"",
		)
		Expect(err).NotTo(HaveOccurred())
		_, err = repo.Open(context.Background(), req1)
		Expect(err).NotTo(HaveOccurred())

		req2, err := dek.NewCheckpointOpenRequest(
			"scene", "01ABC",
			make([]byte, 32), make([]byte, 32),
			"01P2", 100,
			"",
		)
		Expect(err).NotTo(HaveOccurred())
		_, err = repo.Open(context.Background(), req2)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_REKEY_ALREADY_IN_PROGRESS")
	})

	It("UpdateStatus CAS rejects stale writer", func() {
		pool := testIntegrationPool(suiteT)
		seedDEKRow(suiteT, pool, 100, "scene", "01ABC", 3)
		repo := dek.NewCheckpointRepo(pool)
		rid := mustOpenCheckpoint(suiteT, repo, "01ABC", 100)

		// Row is at 'pending'. Try a transition from Phase2MintDEK → Phase3ReencryptCold
		// which the FSM allows, but the CAS predicate (status = 'phase2_mint_dek') fails
		// because the row is actually 'pending'. (INV-CRYPTO-88)
		err := repo.UpdateStatus(
			context.Background(), rid,
			dek.CheckpointStatusPhase2MintDEK,
			dek.CheckpointStatusPhase3ReencryptCold,
		)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_REKEY_STALE_TRANSITION")
	})

	It("Heartbeat updates timestamp", func() {
		pool := testIntegrationPool(suiteT)
		seedDEKRow(suiteT, pool, 100, "scene", "01ABC", 3)
		repo := dek.NewCheckpointRepo(pool)
		rid := mustOpenCheckpoint(suiteT, repo, "01ABC", 100)

		ckpt, err := repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		initial := ckpt.LastHeartbeatAt
		time.Sleep(10 * time.Millisecond)
		Expect(repo.Heartbeat(context.Background(), rid)).To(Succeed())
		ckpt2, err := repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt2.LastHeartbeatAt.After(initial)).To(BeTrue())
	})

	It("FindByContextAndArgs resumes existing checkpoint", func() {
		pool := testIntegrationPool(suiteT)
		seedDEKRow(suiteT, pool, 100, "scene", "01ABC", 3)
		repo := dek.NewCheckpointRepo(pool)

		opArgs := make([]byte, 32)
		opArgs[0] = 0xAB
		policy := make([]byte, 32)
		policy[0] = 0xCD

		req, err := dek.NewCheckpointOpenRequest(
			"scene", "01ABC",
			opArgs, policy,
			"01PLAYER", 100,
			"",
		)
		Expect(err).NotTo(HaveOccurred())
		rid, err := repo.Open(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		ckpt, found, err := repo.FindByContextAndArgs(context.Background(), "scene", "01ABC", opArgs)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(ckpt.RequestID).To(Equal(rid))

		// Different op_args_hash → not found.
		other := make([]byte, 32)
		other[0] = 0xEF
		_, found, err = repo.FindByContextAndArgs(context.Background(), "scene", "01ABC", other)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
	})

	It("ListExpired returns backdate-heartbeat rows", func() {
		pool := testIntegrationPool(suiteT)
		seedDEKRow(suiteT, pool, 100, "scene", "01ABC", 3)
		repo := dek.NewCheckpointRepo(pool)
		rid := mustOpenCheckpoint(suiteT, repo, "01ABC", 100)

		// Backdate heartbeat by 25h (BIGINT-ns: subtract 25*3600*10^9 = 90000*10^9).
		_, err := pool.Exec(context.Background(),
			`UPDATE crypto_rekey_checkpoints SET last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT - 90000::BIGINT * 1000000000 WHERE request_id = $1`,
			rid[:])
		Expect(err).NotTo(HaveOccurred())

		expired, err := repo.ListExpired(context.Background(), 24*time.Hour)
		Expect(err).NotTo(HaveOccurred())
		Expect(expired).To(HaveLen(1))
		Expect(expired[0].RequestID).To(Equal(rid))
	})

	It("MarkAborted persists abort reason", func() {
		pool := testIntegrationPool(suiteT)
		seedDEKRow(suiteT, pool, 100, "scene", "01ABC", 3)
		repo := dek.NewCheckpointRepo(pool)
		rid := mustOpenCheckpoint(suiteT, repo, "01ABC", 100)

		Expect(repo.MarkAborted(context.Background(), rid, "operator_abort")).To(Succeed())
		ckpt, err := repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusAborted))
		Expect(ckpt.AbortedAt).NotTo(BeNil())
		Expect(*ckpt.AbortedReason).To(Equal("operator_abort"))
	})
})
