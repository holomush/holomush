// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package approval_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/approval"
)

// insertPlayerForApproval inserts a minimal player row for use as a
// foreign-key anchor. The username uses the full ULID string to avoid
// the unique-constraint collision that occurs when concurrent tests
// generate ULIDs with the same leading 8 chars (same millisecond).
func insertPlayerForApproval(id string) {
	GinkgoHelper()
	ctx := context.Background()
	// Prefix + full ULID keeps username unique across concurrent tests.
	username := "apr-" + id
	_, err := testPool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'x')`,
		id, username)
	Expect(err).NotTo(HaveOccurred(), "insertPlayerForApproval")
	DeferCleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, id)
	})
}

// assertCode verifies err is an oops error with the expected code.
func assertCode(err error, want string) {
	GinkgoHelper()
	Expect(err).To(HaveOccurred())
	oopsErr, ok := oops.AsOops(err)
	Expect(ok).To(BeTrue(), "expected oops error, got %T", err)
	Expect(oopsErr.Code()).To(Equal(want))
}

var _ = Describe("Approval Repo", func() {
	Describe("Open and Get", func() {
		It("inserts a row via Open and retrieves it via Get", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			insertPlayerForApproval(primary)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := r.Get(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.PrimaryPlayerID).To(Equal(primary))
			Expect(got.OpKind).To(Equal("rekey"))
			Expect(got.CreatedAt.IsZero()).To(BeFalse())
			Expect(got.ApprovedAt).To(BeNil())
		})
	})

	Describe("Get filters expired rows (INV-CRYPTO-72)", func() {
		It("returns APPROVAL_NOT_FOUND for expired rows", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			insertPlayerForApproval(primary)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			// Force expiry server-side.
			tag, err := testPool.Exec(context.Background(),
				`UPDATE admin_approvals SET expires_at = (EXTRACT(EPOCH FROM now() - interval '1 minute') * 1e9)::BIGINT WHERE request_id = $1`,
				id[:])
			Expect(err).NotTo(HaveOccurred())
			Expect(tag.RowsAffected()).To(Equal(int64(1)), "expected exactly one approval row to be force-expired")

			_, err = r.Get(context.Background(), id)
			Expect(err).To(HaveOccurred())
			assertCode(err, "APPROVAL_NOT_FOUND")
		})
	})

	Describe("MarkApproved", func() {
		It("records the second-op approver on the happy path", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			secondOp := ulid.Make().String()
			insertPlayerForApproval(primary)
			insertPlayerForApproval(secondOp)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(r.MarkApproved(context.Background(), id, secondOp)).To(Succeed())

			got, err := r.Get(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ApprovedAt).NotTo(BeNil())
			Expect(got.ApprovedByPlayerID).To(Equal(secondOp))
		})

		It("rejects self-approval at the SQL layer (INV-CRYPTO-73)", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			insertPlayerForApproval(primary)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			err = r.MarkApproved(context.Background(), id, primary)
			Expect(err).To(HaveOccurred())
			assertCode(err, "DENY_DUAL_CONTROL_SELF")

			got, err := r.Get(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ApprovedAt).To(BeNil(), "row must remain pending after self-approval rejection")
		})

		It("rejects approval of an expired row (INV-CRYPTO-72)", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			secondOp := ulid.Make().String()
			insertPlayerForApproval(primary)
			insertPlayerForApproval(secondOp)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			tag, err := testPool.Exec(context.Background(),
				`UPDATE admin_approvals SET expires_at = (EXTRACT(EPOCH FROM now() - interval '1 minute') * 1e9)::BIGINT WHERE request_id = $1`,
				id[:])
			Expect(err).NotTo(HaveOccurred())
			Expect(tag.RowsAffected()).To(Equal(int64(1)), "expected exactly one approval row to be force-expired")

			err = r.MarkApproved(context.Background(), id, secondOp)
			Expect(err).To(HaveOccurred())
			assertCode(err, "DENY_APPROVAL_EXPIRED")
		})

		It("rejects a second MarkApproved on an already-approved row (INV-CRYPTO-74)", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			secondOp := ulid.Make().String()
			insertPlayerForApproval(primary)
			insertPlayerForApproval(secondOp)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(r.MarkApproved(context.Background(), id, secondOp)).To(Succeed())

			err = r.MarkApproved(context.Background(), id, secondOp)
			Expect(err).To(HaveOccurred())
			assertCode(err, "DENY_APPROVAL_ALREADY_APPROVED")
		})

		It("serializes concurrent calls: exactly one succeeds, rest get ALREADY_APPROVED", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			insertPlayerForApproval(primary)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			const N = 50
			secondOps := make([]string, N)
			for i := range secondOps {
				secondOps[i] = ulid.Make().String()
				insertPlayerForApproval(secondOps[i])
			}

			var success atomic.Int32
			var alreadyApproved atomic.Int32
			var wg sync.WaitGroup
			wg.Add(N)
			for i := 0; i < N; i++ {
				i := i
				go func() {
					defer wg.Done()
					err := r.MarkApproved(context.Background(), id, secondOps[i])
					if err == nil {
						success.Add(1)
						return
					}
					oopsErr, ok := oops.AsOops(err)
					if ok && oopsErr.Code() == "DENY_APPROVAL_ALREADY_APPROVED" {
						alreadyApproved.Add(1)
					}
				}()
			}
			wg.Wait()
			Expect(success.Load()).To(Equal(int32(1)), "exactly one MarkApproved should succeed")
			Expect(alreadyApproved.Load()).To(Equal(int32(N-1)), "all losers should see ALREADY_APPROVED")
		})
	})

	Describe("WaitForApproval", func() {
		It("returns the approved row once MarkApproved is called concurrently", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			secondOp := ulid.Make().String()
			insertPlayerForApproval(primary)
			insertPlayerForApproval(secondOp)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			deadline := time.Now().Add(10 * time.Second)

			// Approve in a goroutine after a short delay.
			go func() {
				time.Sleep(200 * time.Millisecond)
				_ = r.MarkApproved(context.Background(), id, secondOp)
			}()

			got, err := r.WaitForApproval(context.Background(), id, deadline)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ApprovedAt).NotTo(BeNil())
			Expect(got.ApprovedByPlayerID).To(Equal(secondOp))
		})

		It("returns APPROVAL_WAIT_DEADLINE when the deadline has already passed", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			insertPlayerForApproval(primary)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			// Deadline already in the past.
			deadline := time.Now().Add(-1 * time.Second)
			_, err = r.WaitForApproval(context.Background(), id, deadline)
			Expect(err).To(HaveOccurred())
			assertCode(err, "APPROVAL_WAIT_DEADLINE")
		})

		It("surfaces DENY_APPROVAL_EXPIRED immediately on force-expired row (CodeRabbit #4)", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			insertPlayerForApproval(primary)

			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: []byte("h"),
			})
			Expect(err).NotTo(HaveOccurred())

			// Force-expire the row so the next poll observes expires_at < now().
			tag, err := testPool.Exec(context.Background(),
				`UPDATE admin_approvals SET expires_at = (EXTRACT(EPOCH FROM now() - interval '1 minute') * 1e9)::BIGINT WHERE request_id = $1`,
				id[:])
			Expect(err).NotTo(HaveOccurred())
			Expect(tag.RowsAffected()).To(Equal(int64(1)), "expected exactly one approval row to be force-expired")

			// Use a generous deadline; the call MUST return well before then.
			deadline := time.Now().Add(30 * time.Second)
			start := time.Now()
			_, err = r.WaitForApproval(context.Background(), id, deadline)
			elapsed := time.Since(start)

			Expect(err).To(HaveOccurred())
			assertCode(err, "DENY_APPROVAL_EXPIRED")
			// Sanity: the call must short-circuit on expiry, not run to deadline.
			// Allow generous slack (1s) for goroutine scheduling and DB roundtrip.
			Expect(elapsed).To(BeNumerically("<", 1*time.Second),
				"WaitForApproval MUST return DENY_APPROVAL_EXPIRED quickly, not poll until deadline")
		})
	})

	Describe("GetByOpArgsHash (INV-CRYPTO-67)", func() {
		// openAndApprove is a spec-local helper that opens a fresh approval and
		// immediately marks it approved by secondOp, returning the resulting Approval.
		openAndApprove := func(r *approval.PostgresRepo, primary, secondOp, opKind string, opArgsHash []byte) approval.Approval {
			GinkgoHelper()
			id, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: opKind, OpArgsHash: opArgsHash,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(r.MarkApproved(context.Background(), id, secondOp)).To(Succeed())
			a, err := r.Get(context.Background(), id)
			Expect(err).NotTo(HaveOccurred())
			return a
		}

		It("returns an approved non-expired row not authored by excludePlayerID", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			secondOp := ulid.Make().String()
			other := ulid.Make().String()
			insertPlayerForApproval(primary)
			insertPlayerForApproval(secondOp)

			hash := []byte("hash-happy")
			openAndApprove(r, primary, secondOp, "rekey", hash)

			got, err := r.GetByOpArgsHash(context.Background(), "rekey", hash, other)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.PrimaryPlayerID).To(Equal(primary))
			Expect(got.ApprovedAt).NotTo(BeNil())
		})

		It("returns APPROVAL_NOT_FOUND for rows where expires_at <= now()", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			secondOp := ulid.Make().String()
			other := ulid.Make().String()
			insertPlayerForApproval(primary)
			insertPlayerForApproval(secondOp)

			hash := []byte("hash-expired")
			a := openAndApprove(r, primary, secondOp, "rekey", hash)

			// Force-expire the row.
			_, err := testPool.Exec(context.Background(),
				`UPDATE admin_approvals SET expires_at = (EXTRACT(EPOCH FROM now() - interval '1 minute') * 1e9)::BIGINT WHERE request_id = $1`,
				a.RequestID[:])
			Expect(err).NotTo(HaveOccurred())

			_, err = r.GetByOpArgsHash(context.Background(), "rekey", hash, other)
			Expect(err).To(HaveOccurred())
			assertCode(err, "APPROVAL_NOT_FOUND")
		})

		It("returns APPROVAL_NOT_FOUND when primary_player_id == excludePlayerID", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			secondOp := ulid.Make().String()
			insertPlayerForApproval(primary)
			insertPlayerForApproval(secondOp)

			hash := []byte("hash-self-author")
			openAndApprove(r, primary, secondOp, "rekey", hash)

			// Exclude the primary author — should be filtered.
			_, err := r.GetByOpArgsHash(context.Background(), "rekey", hash, primary)
			Expect(err).To(HaveOccurred())
			assertCode(err, "APPROVAL_NOT_FOUND")
		})

		It("returns APPROVAL_NOT_FOUND for rows where approved_at IS NULL", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary := ulid.Make().String()
			other := ulid.Make().String()
			insertPlayerForApproval(primary)

			hash := []byte("hash-unapproved")
			// Open without approving — approved_at stays NULL.
			_, err := r.Open(context.Background(), approval.OpenRequest{
				PrimaryPlayerID: primary, OpKind: "rekey", OpArgsHash: hash,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = r.GetByOpArgsHash(context.Background(), "rekey", hash, other)
			Expect(err).To(HaveOccurred())
			assertCode(err, "APPROVAL_NOT_FOUND")
		})

		It("returns the most-recently-approved row when two rows match", func() {
			r := approval.NewPostgresRepo(testPool, nil)
			primary1 := ulid.Make().String()
			primary2 := ulid.Make().String()
			secondOp1 := ulid.Make().String()
			secondOp2 := ulid.Make().String()
			other := ulid.Make().String()
			insertPlayerForApproval(primary1)
			insertPlayerForApproval(primary2)
			insertPlayerForApproval(secondOp1)
			insertPlayerForApproval(secondOp2)

			hash := []byte("hash-tiebreaker")

			// Insert first approval and capture its request_id.
			a1 := openAndApprove(r, primary1, secondOp1, "rekey", hash)

			// Insert second approval with a later approved_at.
			a2 := openAndApprove(r, primary2, secondOp2, "rekey", hash)

			// Force a1.approved_at to be clearly earlier so the ORDER BY is deterministic.
			_, err := testPool.Exec(context.Background(),
				`UPDATE admin_approvals SET approved_at = (EXTRACT(EPOCH FROM now() - interval '10 minutes') * 1e9)::BIGINT WHERE request_id = $1`,
				a1.RequestID[:])
			Expect(err).NotTo(HaveOccurred())

			got, err := r.GetByOpArgsHash(context.Background(), "rekey", hash, other)
			Expect(err).NotTo(HaveOccurred())
			// Should return the row approved most recently (a2).
			Expect(got.PrimaryPlayerID).To(Equal(a2.PrimaryPlayerID),
				"GetByOpArgsHash must return the most-recently-approved matching row")
		})
	})
})
